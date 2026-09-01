// Command controlplane is the spine service: it serves the ControlPlaneService gRPC API. The model
// backend and the sandbox provider are chosen by configuration, so the control plane depends only on
// the interfaces, not on any concrete implementation. The default model backend is the Claude Code
// adapter (your subscription, no API cost); each session runs in its own sandbox (Docker by default).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/auth"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/forge"
	"github.com/atlantic-blue/quay-krewe/internal/headroom"
	"github.com/atlantic-blue/quay-krewe/internal/logging"
	"github.com/atlantic-blue/quay-krewe/internal/messaging"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/skill"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"github.com/atlantic-blue/quay-krewe/internal/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// version is the build this control plane is, stamped in at build time by the image build. A system
// that cannot say which build it is leaves an operator diagnosing a defect that is already fixed.
var version = "dev"

func main() {
	// Asked whether the system beside it is serving, this binary asks and exits. It is the container
	// health check, and it goes before anything else here because it starts nothing.
	if len(os.Args) > 1 && os.Args[1] == healthArg {
		os.Exit(probeHealth())
	}

	serviceName := envOr("QC_SERVICE_NAME", "controlplane")
	logger := logging.Init(serviceName, os.Stdout)

	otelEndpoint := envOr("QC_OTEL_ENDPOINT", "localhost:4317")
	// Loopback unless the operator says otherwise: the port is the whole system, so it is not
	// published to the network by default. The compose stack overrides this, because in a container
	// loopback is the container, and binds the host side to loopback instead.
	grpcAddr := envOr("QC_GRPC_ADDR", "127.0.0.1:50051")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdown, err := telemetry.Init(ctx, serviceName, otelEndpoint)
	if err != nil {
		logger.Error("telemetry init failed", "error", err)
		os.Exit(1)
	}
	// Every line goes to the collector as well as to stdout, now that there is a pipeline to take
	// it. Stdout keeps carrying all of it: it is the signal that still works when the collector does
	// not.
	logger = logging.AlsoExport(serviceName, os.Stdout)

	// QC_MODEL is the backend; QC_CLAUDE_MODEL is which model that backend runs against. Two keys
	// because they are two decisions: a system can run the echo backend, which has no model at all.
	runner, err := model.NewRunner(os.Getenv("QC_MODEL"), os.Getenv("QC_WORKDIR"), os.Getenv("QC_CLAUDE_MODEL"))
	if err != nil {
		logger.Error("model runner config failed", "error", err)
		os.Exit(1)
	}
	// The kinds are resolved through the same functions the constructors use, so what the control
	// plane says it is running cannot drift from what it built.
	modelKind, _ := model.ResolveKind(os.Getenv("QC_MODEL"))

	bornIn, err := birthPermissionMode(os.Getenv("QC_PERMISSION_MODE"))
	if err != nil {
		logger.Error("configuration", "error", err)
		os.Exit(1)
	}

	// QC_DATA_DIR is where this process writes a workspace's conversation store and a project's
	// files; QC_DATA_HOST is the same directory as the host daemon sees it, which is what a sandbox
	// actually mounts. In a container the two differ, so both are needed; run this on the host and
	// they are the same path. Neither set means state stays in the container and dies with it.
	storage := sandbox.Storage{Dir: os.Getenv("QC_DATA_DIR"), Host: os.Getenv("QC_DATA_HOST")}
	if storage.Dir == "" {
		logger.Warn("no QC_DATA_DIR set: a session's conversation lives inside its container and is destroyed with it")
	}

	provider, err := sandbox.NewProvider(os.Getenv("QC_SANDBOX"), sandbox.Options{
		Image:   os.Getenv("QC_SANDBOX_IMAGE"),
		Mounts:  splitAndTrim(os.Getenv("QC_SANDBOX_MOUNTS")),
		Storage: storage,
		// The system's own network, which only the driver joins, and the host paths only it gets.
		// Unset leaves the driver on the session network below, which reaches the system and nothing
		// else, and leaves it seeing nothing of the machine.
		Network: os.Getenv("QC_SANDBOX_NETWORK"),
		// The network every sandbox joins so a session can reach this control plane. The compose
		// file creates it and puts this service on it, and the store, the broker and the dashboards
		// are not on it. Unset leaves a session unable to reach the system, so its credential buys it
		// nothing.
		SessionNetwork: os.Getenv("QC_SESSION_NETWORK"),
		DriverMounts:   splitAndTrim(os.Getenv("QC_DRIVER_MOUNTS")),
		// How much memory one session may take. Unset gives it no limit, so it advertises the whole
		// machine to node, Go, jest and webpack, and the kernel kills it against what is actually
		// free. A session reads whichever of the two it got with krewe room.
		Memory: strings.TrimSpace(os.Getenv("QC_SANDBOX_MEMORY")),
	})
	if err != nil {
		logger.Error("sandbox provider config failed", "error", err)
		os.Exit(1)
	}
	sandboxKind, _ := sandbox.ResolveKind(os.Getenv("QC_SANDBOX"))
	if sandboxKind == sandbox.KindDocker && strings.TrimSpace(os.Getenv("QC_SANDBOX_MEMORY")) == "" {
		logger.Warn("no QC_SANDBOX_MEMORY set: a session sizes node, Go, jest and webpack against the " +
			"whole machine, and the kernel kills them against what the rest of it has left")
	}
	// The skills the operator has written, read from files. A skill that does not make sense stops
	// the system starting rather than going quietly missing later, because a capability that is absent
	// without a reason is one the session improvises around.
	skills, err := skill.Load(os.Getenv("QC_SKILLS_DIR"))
	if err != nil {
		logger.Error("skills", "error", err)
		os.Exit(1)
	}
	if len(skills) > 0 && os.Getenv("QC_SKILLS_HOST") == "" {
		logger.Warn("skills are not mounted: set QC_SKILLS_HOST to the skills directory as the host sees it",
			"skills", len(skills))
	}
	if notice, retired := sandboxSecretsRetired(os.Getenv("QC_SANDBOX_SECRETS")); retired {
		logger.Warn(notice)
	}
	if notice, mismatched := unreachableSystem(sandboxKind,
		os.Getenv("QC_SANDBOX_CONTROL_PLANE"), os.Getenv("QC_SESSION_NETWORK")); mismatched {
		logger.Warn(notice)
	}
	// Which build the sandbox image was made from, read once at startup: it is configuration, and an
	// image is not rebuilt under a running control plane without restarting this stack anyway.
	sandboxBuild := sandbox.ImageBuild(ctx, os.Getenv("QC_SANDBOX_IMAGE"))

	durable, storeKind, err := openStore(ctx, os.Getenv("QC_DATABASE_URL"), logger)
	if err != nil {
		logger.Error("store open failed", "error", err)
		os.Exit(1)
	}
	defer durable.Close()

	credentials, secretsKind := openSecrets(durable, storage, logger)

	token := systemToken(storage, logger, auth.TokenFile)
	driverToken := systemToken(storage, logger, auth.DriverTokenFile)

	events, eventsKind := openEventLog(os.Getenv("QC_KAFKA_SEEDS"), logger)
	defer events.Close()

	leaseSetting, leaseNotice := renamedSetting("QC_JOB_LEASE", os.Getenv)
	if leaseNotice != "" {
		logger.Warn(leaseNotice)
	}

	server := controlplane.NewServer(controlplane.Config{
		// How often a session describes itself, from the system's configuration.
		DescribeEvery: controlplane.DescribeEvery(os.Getenv("QC_DESCRIBE_EVERY")),
		// How long the controller holds a job before another may take it, and the name it
		// writes on the hold so an investigator knows which machine stopped.
		JobLease:       controlplane.JobLease(leaseSetting, logger),
		ControllerName: controlplane.ControllerName(os.Hostname),
		// What reads the machine. Only where a daemon is what makes the sandboxes: a system running
		// sessions on the host has no daemon to ask, and it reports unknown rather than shelling out
		// to a command that is not there.
		Headroom: headroomSource(sandboxKind, logger),
		// What reads back the pull requests the crew opened. The credential is the system's own secret,
		// read at the moment it is needed: an operator sets one while the system runs, and a reader that
		// read it once at startup would report unknown until somebody restarted the stack.
		Forge: &forge.GitHub{Token: func(ctx context.Context) (string, error) {
			return credentials.GetSystem(ctx, forge.TokenName)
		}},
		// What the system holds back for its own containers before it admits any sandbox. The control
		// plane, the database and the event log are containers inside the same runtime the work
		// fills, so a system that reserves nothing goes down with its own workload.
		SystemReserve: controlplane.EnvReserve(logger),
		Store:         durable,
		Runner:        runner,
		Provider:      provider,
		Secrets:       credentials,
		Storage:       storage,
		Events:        events,
		// What a session's tasks may do when it is born, from the system's configuration.
		BirthPermissionMode: bornIn,
		// Where a session dials to reach this control plane. Unset means it cannot.
		Reachable: os.Getenv("QC_SANDBOX_CONTROL_PLANE"),
		// The driver's own token, handed to it beside the address above: recognised, and refused
		// the calls that grant capability.
		DriverToken: driverToken,
		// The capabilities a session is given, and where they are on the host so they can be mounted.
		Skills:       skills,
		SkillsHost:   os.Getenv("QC_SKILLS_HOST"),
		SandboxImage: os.Getenv("QC_SANDBOX_IMAGE"),
		// Who a commit made inside a sandbox is by. Both or neither: git refuses on either missing.
		GitAuthor: controlplane.Identity{
			Name:  os.Getenv("QC_GIT_AUTHOR_NAME"),
			Email: os.Getenv("QC_GIT_AUTHOR_EMAIL"),
		},
		Info: controlplane.Info{
			Model:   modelKind,
			Sandbox: sandboxKind,
			Store:   storeKind,
			State:   stateKind(storage),
			Events:  eventsKind,
			Secrets: secretsKind,
			// What the sandboxes are running, so the tool can say when the system has moved on and
			// they have not.
			SandboxBuild: sandboxBuild,
			// Which build this control plane is, so the tool can say when it and the system are
			// different builds. Stamped in at build time, the way the tool is.
			Version: version,
		},
	})
	// Waits that came due while the system was down are resumed on the way up, and every one after
	// that on a tick: a wait is a row, so a restart loses none of them.
	go server.RunFlowPoller(ctx)

	// And the jobs the system holds are made to happen the same way: a controller reads the rows, sends
	// a task for what has not started, and writes what came back. Declared intent is a row, so a job
	// declared while the system was down starts on the way up.
	go server.RunJobController(ctx)

	// And the machine itself, on its own timer. The header reads the last sample rather than the
	// daemon, because reading the daemon takes as long as the daemon takes and the header redraws
	// every second.
	go server.RunHeadroom(ctx)

	// And the pull requests those jobs opened, on a timer of their own. The crew used to keep the
	// address and never look at it again, so a change that merged and a change whose checks went red
	// an hour later read the same. Every page reads the row this writes and never the forge.
	go server.RunPullRequests(ctx)

	// And the parts the system has to write to before a dispatch starts, so a view of them reads a probe
	// rather than a guess. The health check keeps this fresh wherever one runs; the timer is for a system
	// nobody checks, which had no reading at all.
	go server.RunHealth(ctx)

	// What strayed while the system was down is reaped on the way up: a container whose session was
	// stopped, archived or deleted after this process last saw it is running for nobody.
	server.ReapStrays(ctx)

	// And what survived is counted, before a single job is admitted. Containers outlive the process
	// that made them, so a system that started counting from zero would admit a whole machine's worth
	// of work onto a machine that is already full.
	server.SeedCapacity(ctx)

	// And what was mid task when the system went down is settled the same way: a task runs in this
	// process, so a session the store still calls running is one whose task died with the last one.
	server.SettleTasks(ctx)

	// A system with no skills at all is a fresh one, and it starts with the ones this build ships
	// with rather than making every operator import them by hand. Only ever on an empty catalogue,
	// so it is a starting point and not a policy that undoes a decision.
	server.Seed(ctx, envOr("QC_SEED_SKILLS_DIR", controlplane.SeedDir), logger)

	// And the hooks, on the same terms: only into a system that holds none, so taking one off and
	// restarting does not put it back.
	server.SeedHooks(ctx, envOr("QC_SEED_HOOKS_DIR", controlplane.SeedHooksDir), logger)

	grpcServer := grpc.NewServer(append(
		telemetry.ServerOptions(),
		auth.ServerOptions(auth.Policy{
			Token: token, DriverToken: driverToken, Denied: controlplane.DeniedToDriver,
			// A session running a job presents a credential of its own, minted for that
			// job and holding only the verbs its role declared.
			Grants: server.Grants(), DeniedToJob: controlplane.DeniedToJob,
		})...,
	)...)
	quaycrewv1.RegisterControlPlaneServiceServer(grpcServer, server)
	// What the container health check asks. It writes rather than reads, because this system answered
	// every read for an hour while it started no work at all.
	grpc_health_v1.RegisterHealthServer(grpcServer, controlplane.NewHealth(server))

	listener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		logger.Error("listen failed", "addr", grpcAddr, "error", err)
		os.Exit(1)
	}

	go func() {
		logger.Info("control plane serving", "grpc", grpcAddr, "otel_endpoint", otelEndpoint)
		if err := grpcServer.Serve(listener); err != nil {
			logger.Error("grpc serve stopped", "error", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	grpcServer.GracefulStop()

	// Draining requests is not draining tasks: a detached task is a goroutine nobody is calling, so
	// without this the process exits mid task and the session comes back up settled as failed. Given a
	// minute, which is a task's grace and not its length.
	tasksCtx, doneWaiting := context.WithTimeout(context.Background(), time.Minute)
	server.WaitForTasks(tasksCtx)
	doneWaiting()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
}

// headroomSource is what reads the machine the system runs on, and nil where there is nothing to read
// it with. Nil is a system that reports unknown headroom, which is the honest answer for a system whose
// sessions do not run on a daemon at all.
func headroomSource(sandboxKind string, logger *slog.Logger) headroom.Source {
	if sandboxKind != sandbox.KindDocker {
		logger.Info("sessions do not run on a docker daemon, so the system reports no headroom",
			"sandbox", sandboxKind)
		return nil
	}
	return headroom.Daemon{}
}

// openEventLog returns the log tasks are exported to. With QC_KAFKA_SEEDS set it is Kafka, spoken
// to Redpanda locally. Without it, nothing is exported and nothing is lost: history is written to
// the store in the same breath as the task, and the log only ever carried a copy for a second
// consumer. The status block says the export is off rather than leaving an empty column to read as
// fine.
//
// The producer connects lazily, so a broker that is not up yet does not stop the control plane from
// serving. A publish that cannot reach it is dropped rather than failing the task.
func openEventLog(seeds string, logger *slog.Logger) (messaging.EventLog, string) {
	brokers := splitAndTrim(seeds)
	if len(brokers) == 0 {
		logger.Info("no QC_KAFKA_SEEDS set: history is kept in the store, and there is no audit export")
		return messaging.Discard{}, ""
	}
	client, err := messaging.NewClient(brokers...)
	if err != nil {
		logger.Warn("event log unavailable, so tasks are not exported; history in the store is unaffected", "error", err)
		return messaging.Discard{}, ""
	}
	logger.Info("event log ready", "backend", "kafka", "seeds", brokers)
	return client, "kafka"
}

// systemToken is a token a caller has to present, minted the first time and kept beside the key that
// seals secrets. With nowhere to keep one the system refuses every caller rather than serving them
// all: the guard failing open is the one thing it must never do. A token file that exists but
// cannot be read is a misconfiguration worth stopping for, not working around.
func systemToken(storage sandbox.Storage, logger *slog.Logger, file string) string {
	if storage.Dir == "" {
		logger.Warn("the system has nowhere to keep a token and will refuse every caller: set QC_DATA_DIR")
		return ""
	}
	token, err := auth.TokenAt(filepath.Join(storage.Dir, file))
	if err != nil {
		logger.Error("system token", "error", err)
		os.Exit(1)
	}
	return token
}

// openSecrets returns where a workspace's credentials are kept.
//
// Beside the rest of the durable state when there is any, so the subscription token stops being lost
// on every restart, which is the thing that has made this stack unusable between sessions. Sealed with
// a key on the host, so holding the database is not enough to read one.
//
// The key is made rather than asked for: a step the operator has to perform before anything works is
// a step that gets skipped. Anything that goes wrong here falls back to memory with the reason said
// out loud, because a system that will not start is worse than one that forgets a token.
func openSecrets(durable store.Store, storage sandbox.Storage, logger *slog.Logger) (secrets.Store, string) {
	postgres, durableStore := durable.(*store.Postgres)
	if !durableStore {
		logger.Warn("secrets are kept in memory: set QC_DATABASE_URL and they will survive a restart")
		return secrets.NewMemory(), "memory, lost on restart"
	}
	if storage.Dir == "" {
		logger.Warn("secrets are kept in memory: there is nowhere on the host to keep the key that seals them")
		return secrets.NewMemory(), "memory, lost on restart"
	}

	key, err := secrets.KeyAt(filepath.Join(storage.Dir, "secrets.key"))
	if err != nil {
		logger.Warn("secrets are kept in memory", "error", err)
		return secrets.NewMemory(), "memory, lost on restart"
	}
	kept, err := secrets.NewPostgres(postgres.Pool(), key)
	if err != nil {
		logger.Warn("secrets are kept in memory", "error", err)
		return secrets.NewMemory(), "memory, lost on restart"
	}
	return kept, "postgres, sealed"
}

// openStore returns the durable store. With QC_DATABASE_URL set it is Postgres, and the migrations
// are applied on the way up. Without it the store is in memory, which loses every workspace and
// session on restart and is only appropriate for a throwaway stack.
func openStore(ctx context.Context, databaseURL string, logger *slog.Logger) (store.Store, string, error) {
	if databaseURL == "" {
		logger.Warn("no QC_DATABASE_URL set, using the in memory store: workspaces and sessions will not survive a restart")
		return store.NewMemory(), "memory", nil
	}
	durable, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		return nil, "", err
	}
	logger.Info("store ready", "backend", "postgres")
	return durable, "postgres", nil
}

// stateKind names where a session's conversation and files are kept, for the status block. It is
// deliberately not a promise that any particular session's sandbox has the mounts: a sandbox created
// before they were configured does not, which is why an upgrade clears the old ones.
func stateKind(storage sandbox.Storage) string {
	if storage.Dir == "" {
		return ""
	}
	return "host directory " + storage.Host
}

// sandboxSecretsRetired says what to tell an operator whose configuration still names the allowlist
// that used to decide which secrets reached a sandbox, and whether there is anything to say.
//
// A setting that quietly stops being read is worse than one that never existed: the operator chose
// which secrets could travel, the system now hands over all of a workspace's own, and nothing on the
// screen would say the two disagree.
func sandboxSecretsRetired(value string) (string, bool) {
	if strings.TrimSpace(value) == "" {
		return "", false
	}
	return "QC_SANDBOX_SECRETS is set and is no longer read: a workspace's secrets reach that " +
		"workspace's sandboxes, so the list can be removed from the system's configuration", true
}

// renamedSettings are the system's settings that changed their name, against what they are called now.
//
// A table rather than a case each, so the next rename is covered the moment its entry is added. The
// value is still read from the old name where the new one says nothing, because a system that silently
// went back to the default lease after an upgrade is the exact failure a rename must not cause.
var renamedSettings = map[string]string{
	"QC_WORK_LEASE": "QC_JOB_LEASE",
}

// renamedSetting reads one setting through its old name as well as its new one, and says what to
// tell an operator whose file still carries the old spelling.
//
// Silence would be worse than the warning. An operator who tuned the lease and then upgraded would
// keep a file that looks configured while the system ran the measured default, and nothing on the
// screen would say the two disagree.
func renamedSetting(now string, read func(string) string) (value, notice string) {
	value = strings.TrimSpace(read(now))
	for was, becomes := range renamedSettings {
		if becomes != now {
			continue
		}
		old := strings.TrimSpace(read(was))
		if old == "" {
			continue
		}
		if value == "" {
			return old, was + " is set and is called " + now + " now: it is still being read, and " +
				"renaming it in the system's configuration is what makes that stop being luck"
		}
		return value, was + " is set and is called " + now + " now: " + now +
			" is set too and wins, so the old line can be removed from the system's configuration"
	}
	return value, ""
}

// unreachableSystem says what to tell an operator whose system hands out an address no session can
// resolve, and whether there is anything to say.
//
// This is the fault the check exists for, and it is silent from both ends. The system tells a session
// running a job where it is and mints it a credential; the sandbox joins no network that reaches
// that address; and the session reports "produced zero addresses", which reads as the system being
// down rather than as configuration. Only this process can see both halves.
//
// A system that tells a session nothing is not warned about. That system hands out no credential either,
// so the two halves agree.
func unreachableSystem(kind, reachable, sessionNetwork string) (string, bool) {
	if kind != sandbox.KindDocker || reachable == "" || sessionNetwork != "" {
		return "", false
	}
	return "QC_SANDBOX_CONTROL_PLANE is set and QC_SESSION_NETWORK is not: a session running a job " +
		"is told where the system is, and its sandbox joins no network that reaches that address, so " +
		"every call it makes fails to resolve the name", true
}

func splitAndTrim(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// birthPermissionMode reads what a session's tasks may do when it is born.
//
// It refuses a value that is not a mode rather than falling back, because falling back is silent: a
// system configured for "planning" would run every task in acceptEdits and look exactly like a system
// configured for acceptEdits. Startup is where the operator is standing and can fix it.
//
// Empty is not a refusal. It is what every system's configuration says until somebody sets this, and it
// keeps the mode every session has had since the control plane was written.
func birthPermissionMode(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	mode, known := model.PermissionModeNamed(value)
	if !known {
		return "", fmt.Errorf("QC_PERMISSION_MODE is %q, which is not a mode: the modes are %s",
			value, strings.Join(model.PermissionModesOffered(), ", "))
	}
	return mode, nil
}
