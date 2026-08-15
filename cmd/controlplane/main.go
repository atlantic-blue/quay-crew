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

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/auth"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/skill"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/atlantic-blue/quay-crew/internal/telemetry"
	"google.golang.org/grpc"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	serviceName := envOr("QC_SERVICE_NAME", "controlplane")
	otelEndpoint := envOr("QC_OTEL_ENDPOINT", "localhost:4317")
	// Loopback unless the operator says otherwise: the port is the whole crew, so it is not
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

	runner, err := model.NewRunner(os.Getenv("QC_MODEL"), os.Getenv("QC_WORKDIR"))
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
		// The network the driver joins, and the host paths only it gets. Unset leaves the driver
		// where it can reach nothing and see nothing of the machine.
		Network:      os.Getenv("QC_SANDBOX_NETWORK"),
		DriverMounts: splitAndTrim(os.Getenv("QC_DRIVER_MOUNTS")),
	})
	if err != nil {
		logger.Error("sandbox provider config failed", "error", err)
		os.Exit(1)
	}
	sandboxKind, _ := sandbox.ResolveKind(os.Getenv("QC_SANDBOX"))
	// The skills the operator has written, read from files. A skill that does not make sense stops
	// the crew starting rather than going quietly missing later, because a capability that is absent
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

	token := crewToken(storage, logger, auth.TokenFile)
	driverToken := crewToken(storage, logger, auth.DriverTokenFile)

	events, eventsKind := openEventLog(os.Getenv("QC_KAFKA_SEEDS"), logger)
	defer events.Close()

	server := controlplane.NewServer(controlplane.Config{
		// How often a thread describes itself, from the crew's configuration.
		DescribeEvery: controlplane.DescribeEvery(os.Getenv("QC_DESCRIBE_EVERY")),
		Store:         durable,
		Runner:        runner,
		Provider:      provider,
		Secrets:       credentials,
		Storage:       storage,
		Events:        events,
		// What a thread's turns may do when it is born, from the crew's configuration.
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
			// What the sandboxes are running, so the tool can say when the crew has moved on and
			// they have not.
			SandboxBuild: sandboxBuild,
		},
	})
	// Waits that came due while the crew was down are resumed on the way up, and every one after
	// that on a tick: a wait is a row, so a restart loses none of them.
	go server.RunFlowPoller(ctx)

	// What strayed while the crew was down is reaped on the way up: a container whose thread was
	// stopped, archived or deleted after this process last saw it is running for nobody.
	server.ReapStrays(ctx)

	// And what was mid turn when the crew went down is settled the same way: a turn runs in this
	// process, so a thread the store still calls running is one whose turn died with the last one.
	server.SettleTurns(ctx)

	// A crew with no skills at all is a fresh one, and it starts with the ones this build ships
	// with rather than making every operator import them by hand. Only ever on an empty catalogue,
	// so it is a starting point and not a policy that undoes a decision.
	server.Seed(ctx, envOr("QC_SEED_SKILLS_DIR", controlplane.SeedDir), logger)

	grpcServer := grpc.NewServer(auth.ServerOptions(token, driverToken, controlplane.DeniedToDriver)...)
	quaycrewv1.RegisterControlPlaneServiceServer(grpcServer, server)

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

	// Draining requests is not draining turns: a detached turn is a goroutine nobody is calling, so
	// without this the process exits mid turn and the thread comes back up settled as failed. Given a
	// minute, which is a turn's grace and not its length.
	turnsCtx, doneWaiting := context.WithTimeout(context.Background(), time.Minute)
	server.WaitForTurns(turnsCtx)
	doneWaiting()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
}

// openEventLog returns the log turns are exported to. With QC_KAFKA_SEEDS set it is Kafka, spoken
// to Redpanda locally. Without it, nothing is exported and nothing is lost: history is written to
// the store in the same breath as the turn, and the log only ever carried a copy for a second
// consumer. The status block says the export is off rather than leaving an empty column to read as
// fine.
//
// The producer connects lazily, so a broker that is not up yet does not stop the control plane from
// serving. A publish that cannot reach it is dropped rather than failing the turn.
func openEventLog(seeds string, logger *slog.Logger) (messaging.EventLog, string) {
	brokers := splitAndTrim(seeds)
	if len(brokers) == 0 {
		logger.Info("no QC_KAFKA_SEEDS set: history is kept in the store, and there is no audit export")
		return messaging.Discard{}, ""
	}
	client, err := messaging.NewClient(brokers...)
	if err != nil {
		logger.Warn("event log unavailable, so turns are not exported; history in the store is unaffected", "error", err)
		return messaging.Discard{}, ""
	}
	logger.Info("event log ready", "backend", "kafka", "seeds", brokers)
	return client, "kafka"
}

// crewToken is a token a caller has to present, minted the first time and kept beside the key that
// seals secrets. With nowhere to keep one the crew refuses every caller rather than serving them
// all: the guard failing open is the one thing it must never do. A token file that exists but
// cannot be read is a misconfiguration worth stopping for, not working around.
func crewToken(storage sandbox.Storage, logger *slog.Logger, file string) string {
	if storage.Dir == "" {
		logger.Warn("the crew has nowhere to keep a token and will refuse every caller: set QC_DATA_DIR")
		return ""
	}
	token, err := auth.TokenAt(filepath.Join(storage.Dir, file))
	if err != nil {
		logger.Error("crew token", "error", err)
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
// out loud, because a crew that will not start is worse than one that forgets a token.
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
// deliberately not a promise that any particular thread's sandbox has the mounts: a sandbox created
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
// which secrets could travel, the crew now hands over all of a workspace's own, and nothing on the
// screen would say the two disagree.
func sandboxSecretsRetired(value string) (string, bool) {
	if strings.TrimSpace(value) == "" {
		return "", false
	}
	return "QC_SANDBOX_SECRETS is set and is no longer read: a workspace's secrets reach that " +
		"workspace's sandboxes, so the list can be removed from the crew's configuration", true
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

// birthPermissionMode reads what a thread's turns may do when it is born.
//
// It refuses a value that is not a mode rather than falling back, because falling back is silent: a
// crew configured for "planning" would run every turn in acceptEdits and look exactly like a crew
// configured for acceptEdits. Startup is where the operator is standing and can fix it.
//
// Empty is not a refusal. It is what every crew's configuration says until somebody sets this, and it
// keeps the mode every thread has had since the control plane was written.
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
