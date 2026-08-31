// Package controlplane implements the spine: the ControlPlaneService gRPC API backed by a durable
// store, a secrets store, and a model runner. Channels feed it through the event log; the dashboard
// and CLI drive it through the API.
//
// The server holds no domain state of its own. Workspaces and sessions live in the store, so a restart
// resumes conversations instead of orphaning them. The one thing it still keeps in the process is
// the map of live sandboxes, which is a handle to a running container rather than a fact worth
// keeping; reattaching those after a restart is its own job.
package controlplane

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/auth"
	"github.com/atlantic-blue/krewe/internal/capacity"
	"github.com/atlantic-blue/krewe/internal/deploy"
	"github.com/atlantic-blue/krewe/internal/display"
	"github.com/atlantic-blue/krewe/internal/flow"
	"github.com/atlantic-blue/krewe/internal/headroom"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/manual"
	"github.com/atlantic-blue/krewe/internal/messaging"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/name"
	"github.com/atlantic-blue/krewe/internal/repository"
	"github.com/atlantic-blue/krewe/internal/role"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/secrets"
	"github.com/atlantic-blue/krewe/internal/skill"
	"github.com/atlantic-blue/krewe/internal/store"
	"github.com/atlantic-blue/krewe/internal/telemetry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Where a session is. These five are the whole vocabulary of Session.status, written down here because
// the console colours by them and a sixth invented at a call site would come out uncoloured.
const (
	// StatusIdle is a session waiting for you: no task is running and the last one landed.
	StatusIdle = "idle"
	// StatusRunning is a task under way. A detached dispatch sets it before it answers, so the
	// listing drawn straight afterwards says the session is busy rather than ready.
	StatusRunning = "running"
	// StatusFailed is a session whose last task did not land. The task record carries why.
	StatusFailed = "failed"
	// StatusStopped is a session that was put down. Its sandbox is gone and its history is not.
	StatusStopped = "stopped"
	// StatusReclaimed is a session the system took the container back from. Its sandbox is gone and
	// everything else it has is not, so the next task builds a fresh one over the same conversation
	// and the same files.
	//
	// It is deliberately not stopped. A stop is an operator's decision and somebody reading it goes
	// looking for who made it; a reclaim is the system saving memory on a session nobody is using, and
	// somebody reading it looks for nothing, because the next dispatch fixes it.
	StatusReclaimed = store.StatusReclaimed
)

// Info is what this control plane is running, reported over the API so an operator can see which
// system they are about to act on. It is configuration: never a secret, and never a health verdict.
type Info struct {
	// Model is the backend a task runs against, for example "claude-code".
	Model string
	// Sandbox is what a session is isolated in, for example "docker".
	Sandbox string
	// Store is where workspaces and sessions are kept, for example "postgres".
	Store string
	// State is where a conversation and a project's files are kept, for example "host directory".
	// Empty means they live in the container and are destroyed with it.
	State string
	// Events is the event log a task is recorded on. Empty means nothing is connected to it.
	Events string
	// Secrets is where a workspace's credentials are kept, for example "postgres, sealed".
	Secrets string
	// SandboxBuild is the build of the system the sandbox image was made from. Empty means the image
	// does not say, and nothing is then claimed about it.
	SandboxBuild string
	// Version is the build this control plane binary was made from, stamped in at build time. Empty
	// is a binary nobody stamped, which the tool reports as unknown rather than as a difference.
	Version string
}

// Config is everything the control plane is built from. It is a struct rather than a parameter list
// because the list had already reached four and a caller could silently swap two of them.
type Config struct {
	Store    store.Store
	Runner   model.Runner
	Provider sandbox.Provider
	Secrets  secrets.Store
	// Storage is where a workspace's conversation store lives on the host. The control plane reads it
	// to tell a session whose conversation is still there from one whose handle outlived it.
	Storage sandbox.Storage
	// Events is the log every task is written to. Nil means nowhere, which is a stack with no broker
	// configured rather than an error: tasks run, and nothing records that they did.
	Events messaging.EventLog
	// Info describes the three above in words, for the console's status block.
	Info Info
	// Skills are the capabilities the system has been given, read from files, and every session gets them.
	// A skill imported into the store and attached to a workspace reaches that workspace's sessions as
	// well, which is the other half of the same idea. See docs/SKILLS.md.
	Skills []skill.Skill
	// SkillsHost is the skills directory as the host daemon sees it, which is what a bind mount needs.
	// Empty means skills are not mounted, the same way an unset data directory means state is not kept.
	SkillsHost string
	// SandboxImage is named in the refusal when a skill needs a binary that is not in it.
	SandboxImage string
	// GitAuthor is who a commit made inside a sandbox is by: both a name and an address, or neither.
	// Without it git refuses to commit rather than guessing.
	GitAuthor Identity
	// Reachable is the address a session should dial to reach this control plane, put into every
	// sandbox as QC_GRPC_ADDR so `krewe` inside one drives the system without being told where it is.
	//
	// Empty means a session cannot reach the system at all, which is the default: a session that can
	// drive the system can also stop other sessions, so it is turned on rather than assumed.
	Reachable string
	// DriverToken is the driver's own token, handed to it beside the system's address, because an
	// address without a token is a door that will not open. It is the driver's rather than the
	// operator's so the system can tell the two apart and refuse the driver the calls that grant
	// capability (see DeniedToDriver).
	DriverToken string
	// BirthPermissionMode is what a session's tasks may do when nothing else says. It used to be a
	// constant here, so every session that did not come through the console's wizard arrived in
	// acceptEdits and the only way to change that was to edit the binary. Empty keeps acceptEdits,
	// because an upgrade that quietly widens what a session may do is the worst way to learn this
	// setting exists.
	BirthPermissionMode string
	// DescribeEvery is how many tasks past its description a conversation goes before the system writes
	// it again. Zero is off.
	DescribeEvery int
	// Headroom reads the machine the system runs on. Nil means the system cannot read it, and every
	// figure then says unknown, which is what a system with no daemon to ask should say.
	Headroom headroom.Source
	// SystemReserve is what the system holds back for its own containers before it admits any sandbox.
	// The zero value takes the measured floor. What binds is the larger of this and what the system's
	// own containers are actually holding, read on every sample.
	SystemReserve capacity.Request
	// HeadroomEvery is how often the machine is read. Zero takes headroom.Every.
	HeadroomEvery time.Duration
	// StartWait is how long a dispatch is given to get from a session row to a sandbox ready for its
	// first task. Zero takes startWait, which is measured from what a healthy start costs.
	StartWait time.Duration
	// ExportWait is how long one record's export to the event log is given before it is dropped.
	// Zero takes exportWait.
	ExportWait time.Duration
	// FlowPollEvery is how often the flow poller looks for runs to carry on: a wait that came due, a
	// schedule that fired, a step whose job ended. Zero takes flow.DefaultPollEvery.
	FlowPollEvery time.Duration
	// JobTickEvery is how often the job controller looks at the job the system holds. Zero takes
	// job.DefaultTickEvery.
	JobTickEvery time.Duration
	// JobLease is how long a controller holds a job before another may take it. Zero takes
	// job.DefaultLease, which is derived from what a tick costs rather than chosen.
	JobLease time.Duration
	// ControllerName is what this system writes on the leases it takes, on a job and on a
	// trigger alike. Empty mints one, which is right for a test and wrong for a system an investigator
	// has to read a record from.
	ControllerName string
}

// Identity is who a commit is by.
type Identity struct {
	Name  string
	Email string
}

// Complete says whether this is enough to commit with. Half of one is worse than none: git refuses
// either way, and a half identity looks configured.
func (i Identity) Complete() bool {
	return strings.TrimSpace(i.Name) != "" && strings.TrimSpace(i.Email) != ""
}

// Server implements quaycrewv1.ControlPlaneServiceServer.
type Server struct {
	quaycrewv1.UnimplementedControlPlaneServiceServer
	store store.Store
	// secrets reads a workspace's own over the system's, so every path that asks a workspace what it
	// holds gets the system's too and none of them has to remember to ask twice. The unmerged levels
	// are still reachable through it, for the one caller that wants them apart: an operator's
	// listing, which says where each secret came from.
	secrets  secrets.Levels
	runner   model.Runner
	provider sandbox.Provider
	storage  sandbox.Storage
	// flows begins and drives automation runs. It dispatches through this same server, so a flow
	// can do exactly what the operator who started it could do and nothing more.
	flows flowRunner
	// flowPoller resumes waiting runs whose time has come. Started by whoever owns the process,
	// because a goroutine hidden inside a constructor is a lifetime nobody can see.
	flowPoller *flow.Poller
	// jobController makes reality match the job the system holds. Started the same way and for the
	// same reason.
	jobController *job.Controller
	// grants are the credentials this system has minted for jobs, so a call carrying one is
	// recognised as that job rather than as the operator.
	grants *grants
	// reachable is where a session dials to reach this control plane. Empty means it cannot.
	reachable string
	// driverToken is the driver's own token, handed only to the driver so its calls are recognised
	// as the driver's.
	driverToken string
	// gitAuthor is who a commit made inside a sandbox is by.
	gitAuthor Identity
	// birthMode is what a session's tasks may do when nothing else says. Empty means acceptEdits.
	birthMode string
	// describeEvery is how many tasks past its description a conversation goes before the system writes
	// it again. Zero is off, which is what a system paying for automation runs wants.
	describeEvery int
	// describing counts the descriptions still being written, so a test can wait for them rather than
	// sleeping and a shutdown can tell whether any are in flight.
	describing sync.WaitGroup
	// tasking counts the detached tasks still running, for the same reasons: a task nobody is waiting
	// on is still a task, and a process that cannot count them cannot tell whether it is idle.
	tasking sync.WaitGroup
	// skills are the capabilities a session is given, and where they are on the host.
	skills       []skill.Skill
	skillsHost   string
	sandboxImage string
	events       messaging.EventLog
	info         Info
	// taskMetrics publishes what each task spent. Nil records nothing, which is what a system with no
	// telemetry provider installed does.
	taskMetrics *telemetry.TaskMetrics
	// placed is what the system has put on this machine and what it has promised to put there. It is
	// what admission adds up, and it is a ledger rather than a reading because a container appears
	// seconds after the job that asked for it was admitted.
	placed *capacity.Ledger
	// reserve is the floor under what the system keeps for its own containers.
	reserve capacity.Request
	// headroom keeps the last reading of the machine. Everything that reports it reads the sampler
	// and never the daemon, so a slow daemon slows the sampler and never a command.
	headroom *headroom.Sampler

	// lastHealth is what the last probe of the parts a dispatch has to write to found. It is kept
	// rather than taken on demand for the reason the headroom sample is: the part that is down is the
	// part that takes longest to say so, and a view must not wait on it.
	healthMu   sync.RWMutex
	lastHealth HealthReading

	// startWait is the budget from a session row to a sandbox ready for its first task, and
	// exportWait what one export to the event log is given. Both are fields rather than constants so
	// a test can drive a system whose waits run out while somebody is watching.
	startWait  time.Duration
	exportWait time.Duration

	// starts is the system's one sandbox start at a time, held as a channel rather than a mutex
	// because a mutex cannot be given up: a start that never ended held every later dispatch behind
	// it with no way out. A slot taken from here is abandoned when the budget runs out.
	starts chan struct{}

	sandboxesMu sync.Mutex
	sandboxes   map[string]sandbox.Sandbox // one per session, created lazily, closed on stop

	// running is the task each session has in flight, and how to stop it. A task runs the model
	// through a context, so holding the cancel is what makes stopping one possible at all; without
	// it the only way to end a task was to kill the client, which does not reliably end anything.
	runningMu sync.Mutex
	running   map[string]*running

	// opened is the conversations this process has watched a model runtime open. It is what a system
	// that keeps no state on the host has in place of a transcript to look at.
	opened opened
}

// NewServer builds a control plane over a durable store, a model runner (the Claude Code adapter by
// default), a sandbox provider (one sandbox per session) and a secrets store.
func NewServer(cfg Config) *Server {
	server := &Server{
		store:         cfg.Store,
		secrets:       secrets.Levels{Store: cfg.Secrets},
		runner:        cfg.Runner,
		provider:      cfg.Provider,
		storage:       cfg.Storage,
		events:        eventsOr(cfg.Events),
		info:          cfg.Info,
		reachable:     cfg.Reachable,
		driverToken:   cfg.DriverToken,
		gitAuthor:     cfg.GitAuthor,
		birthMode:     cfg.BirthPermissionMode,
		describeEvery: cfg.DescribeEvery,
		skills:        cfg.Skills,
		skillsHost:    cfg.SkillsHost,
		sandboxImage:  cfg.SandboxImage,
		sandboxes:     make(map[string]sandbox.Sandbox),
		headroom:      headroom.NewSampler(cfg.Headroom, cfg.HeadroomEvery),
		placed:        capacity.NewLedger(),
		reserve:       reserveOr(cfg.SystemReserve),
		startWait:     orWait(cfg.StartWait, startWait),
		exportWait:    orWait(cfg.ExportWait, exportWait),
		starts:        make(chan struct{}, 1),
		grants:        newGrants(),
	}
	// Creating an instrument fails only on a name this package chose, so a failure here is a defect
	// in this file rather than an operator's problem. Say it and carry on unmeasured: a system that
	// will not start because a counter would not be made is worse than a system with no counter.
	metrics, err := telemetry.NewTaskMetrics()
	if err != nil {
		slog.Warn("tasks are not being measured", "error", err)
	}
	server.taskMetrics = metrics
	// The engine dispatches through this same server rather than dialing it: it is already inside
	// the process, and a run is started by a caller the interceptor has already authenticated. It
	// reaches nothing the caller could not, because these are the same two methods.
	engine := flow.NewEngine(cfg.Store, server, server, server)
	server.flows = engine
	// The same name the job controller writes on its leases, because both claims are this system's and
	// an investigator reading either wants to know which system took it.
	server.flowPoller = flow.NewPoller(engine, cfg.FlowPollEvery, nil).Owned(cfg.ControllerName)
	// The controller reads and writes rows and dispatches through this same server, which is the
	// property that lets it move out of this process later without changing a line of its logic.
	server.jobController = job.NewController(cfg.Store, server, server, server, nil).
		Every(cfg.JobTickEvery).Leasing(cfg.JobLease).Owned(cfg.ControllerName).
		Redacting(server).Exporting(server).Reading(server).
		// The machine, asked before a job is started rather than after it has been killed.
		Placing(server).
		// The credentials a job's sessions hold, taken back the moment the job ends. It is what ends
		// a credential in a working system; its expiry is the backstop behind it.
		Revoking(server).
		// The signal that stops a reclaim closing a container an operator is typing into. Without it
		// the controller reclaims nothing, whatever the workspace's times say.
		Watching(server)
	return server
}

// reserveOr is what the system keeps back for itself, and the measured floor where it was given
// nothing on that axis.
func reserveOr(configured capacity.Request) capacity.Request {
	return configured.Or(capacity.DefaultReserve())
}

// orWait is the budget the system was configured with, and the measured default when it was given none.
func orWait(configured, standard time.Duration) time.Duration {
	if configured > 0 {
		return configured
	}
	return standard
}

// eventsOr is the log to publish on, and Discard when there is none, so nothing downstream has to
// ask whether there is a broker before writing a record.
func eventsOr(log messaging.EventLog) messaging.EventLog {
	if log == nil {
		return messaging.Discard{}
	}
	return log
}

// ListTasks returns a session's history, oldest first, from the tasks the dispatch path writes in
// the same breath as each task.
//
// It reads the store rather than the log: the log is the write side and replaying it on every
// request would make a listing cost more the longer a system has been running.
func (s *Server) ListTasks(ctx context.Context, req *quaycrewv1.ListTasksRequest) (*quaycrewv1.ListTasksResponse, error) {
	if req.GetSession() == "" {
		return nil, status.Error(codes.InvalidArgument, "session is required")
	}
	if _, err := s.store.GetSession(ctx, req.GetSession()); err != nil {
		return nil, storeError(err, "session")
	}
	tasks, err := s.store.ListTasks(ctx, req.GetSession(), int(req.GetLimit()))
	if err != nil {
		return nil, storeError(err, "list tasks")
	}
	return &quaycrewv1.ListTasksResponse{Tasks: tasks}, nil
}

// GetInfo reports what this control plane is running.
func (s *Server) GetInfo(_ context.Context, _ *quaycrewv1.GetInfoRequest) (*quaycrewv1.GetInfoResponse, error) {
	return &quaycrewv1.GetInfoResponse{
		Model:        s.info.Model,
		Sandbox:      s.info.Sandbox,
		Store:        s.info.Store,
		State:        s.info.State,
		Events:       s.info.Events,
		Secrets:      s.info.Secrets,
		SandboxBuild: s.info.SandboxBuild,
		Version:      s.info.Version,
	}, nil
}

// GetUsage adds up what every conversation in the system has cost. Its own call rather than part of
// GetInfo, which answers configuration and is fetched once. Archived sessions are counted: a total
// that shrinks when somebody tidies up is worse than no total.
func (s *Server) GetUsage(ctx context.Context, _ *quaycrewv1.GetUsageRequest) (*quaycrewv1.GetUsageResponse, error) {
	var total sandbox.Usage
	var counted int64
	for _, archived := range []bool{false, true} {
		sessions, err := s.store.ListSessions(ctx, store.SessionFilter{Archived: archived})
		if err != nil {
			return nil, storeError(err, "list sessions")
		}
		for _, session := range sessions {
			spent := s.storage.ConversationUsage(boxOf(session), session.GetModelSessionId())
			if spent.Empty() {
				continue
			}
			total = total.Add(spent)
			counted++
		}
	}
	return &quaycrewv1.GetUsageResponse{
		Total: &quaycrewv1.Usage{
			Input:        total.Input,
			Output:       total.Output,
			CacheRead:    total.CacheRead,
			CacheWritten: total.CacheWritten,
		},
		Sessions: counted,
	}, nil
}

// sandboxError maps a sandbox failure onto the status the caller should see.
//
// A refusal that already says what it is keeps saying it. Wrapping everything as Internal turned "the
// github skill needs gh and the image does not have it", which the operator can act on, into a server
// fault, which reads as the system being broken.
func sandboxError(err error, what string) error {
	if _, said := status.FromError(err); said && status.Code(err) != codes.Unknown {
		return err
	}
	return status.Errorf(codes.Internal, "%s: %v", what, err)
}

// storeError maps a store failure onto the status the caller should see.
func storeError(err error, what string) error {
	if errors.Is(err, store.ErrNotFound) {
		return status.Errorf(codes.NotFound, "%s not found", what)
	}
	return status.Errorf(codes.Internal, "%s: %v", what, err)
}

// sandboxFor returns the session's sandbox, creating it on first use.
//
// The environment is set on the sandbox itself rather than on each task, so attaching to the
// conversation is authenticated too. A token set after the first task does not reach the existing
// sandbox: stop the session to get a fresh one.
func (s *Server) sandboxFor(ctx context.Context, session *quaycrewv1.Session) (sandbox.Sandbox, error) {
	// One start at a time, as it has always been, and now abandonable: the system took this as a mutex
	// held across the whole start, so a start that never ended held every later dispatch, and every
	// stop, behind it.
	if err := waited(ctx, session.GetId(), waitStartAhead, s.takeStart); err != nil {
		return nil, err
	}
	defer func() { <-s.starts }()
	// Always ask the provider, never a remembered handle. What this process believes about containers
	// and what the daemon actually has drift constantly: an upgrade reaps them, a prune removes them,
	// a machine restart is fine but anything that removes one behind the control plane's back leaves
	// a handle here pointing at nothing, and a name is handed to the operator for a container that is
	// not there. Creating is idempotent, so the daemon is the source of truth and this map is only
	// what to close later.
	s.syncContext(ctx, session)
	// What the session holds, which is not everything it was given: a skill whose secret the
	// workspace has not set is left out here rather than improvised around in the sandbox, and the
	// listing carries the reason. See withoutUnusable.
	caps := s.capabilityOf(ctx, session)
	// The hooks the session runs under, written out and mounted beside its skills. A hook reaches a
	// container when the container is built and never after, which is why attaching one says so.
	mounts := caps.mounts
	if mount, under := s.renderHooks(ctx, session); under {
		mounts = append(mounts, mount)
	}
	// The machine, one last time, before anything is created. The controller has already decided
	// this fits and is holding the room under the same key; this is the check that stands between a
	// dispatch nobody scheduled and a runtime that stops answering.
	if err := s.placeSandbox(ctx, session); err != nil {
		return nil, err
	}
	cfg := boxOf(session)
	// What it asked for travels with it, so the container carries the request the system reserved.
	cfg.Request = s.requestFor(ctx, session.GetWorkspace())
	// No credential at sandbox birth. A sandbox keeps the configuration it was made with, so a
	// credential written here would label every later task with the first task's grant, and one
	// minted after birth would never reach the container at all. It travels on the task instead.
	cfg.Env = environ(s.taskEnv(ctx, session, ""))
	cfg.Mounts = mounts
	cfg.Driver = session.GetDriver()
	var box sandbox.Sandbox
	if err := waited(ctx, session.GetId(), waitContainer, func(ctx context.Context) error {
		made, err := s.provider.Create(ctx, cfg)
		box = made
		return err
	}); err != nil {
		return nil, err
	}
	// A sandbox that cannot be provisioned is closed rather than left running and untracked, so the
	// next attempt starts clean instead of adopting a half made one, and a failing clone or setup
	// does not strand a container per attempt. Only a sandbox this process was not already
	// accountable for: closing an adopted one that this process holds would take a live conversation
	// down over a transient failure.
	if err := waited(ctx, session.GetId(), waitSetup, func(ctx context.Context) error {
		return s.provision(ctx, session, caps.held, box)
	}); err != nil {
		if !s.holding(session.GetId()) {
			// Closed with a context of its own: the one that came in here is the one that just ran
			// out, and tearing a half made container down needs a working context to do it with.
			closing, done := context.WithTimeout(context.WithoutCancel(ctx), tidyWait)
			_ = box.Close(closing)
			done()
			// And its room goes back with it, or a machine loses a sandbox's worth of capacity to
			// every setup that failed.
			s.unplaceSandbox(session.GetId())
		}
		return nil, err
	}
	s.keepSandbox(session.GetId(), box)
	// What this sandbox was born holding, recorded so a listing can say when the workspace's
	// skills have moved on underneath it. Only when the row holds nothing: a non empty answer
	// means the container already existed and was adopted, and its birth set is the one already
	// written, not whatever is current now.
	if born, err := s.store.SessionSkills(ctx, session.GetId()); err == nil && born == "" {
		_ = s.store.SetSessionSkills(ctx, session.GetId(), skill.FingerprintHeld(caps.held))
	}
	return box, nil
}

// provision is everything a fresh sandbox needs before its first task.
//
// A session's working directory starts empty on purpose: a repository is cloned in conversation,
// following the git skill, so the only provisioning a sandbox needs is its skills.
func (s *Server) provision(ctx context.Context, session *quaycrewv1.Session, held []skill.Held, box sandbox.Sandbox) error {
	// Before the skills, because a skill's setup can need a credential that is mounted rather than
	// exported, and a setup that runs before the file is there fails for a reason nothing explains.
	if err := s.readySecretFiles(ctx, session, box); err != nil {
		return err
	}
	if err := s.readySkills(ctx, session, held, box); err != nil {
		return err
	}
	// After the skills, because a skill can refuse the task and signing never does: a sandbox that
	// cannot be set up to sign is one that asks the operator to commit, which the git skill covers.
	return s.readySigning(ctx, session, box)
}

// readySkills checks the sandbox has what its skills need, and runs each skill's setup once.
//
// Inside the container because that is the only place that knows what the image carries. Once,
// because a sandbox is adopted across tasks and a setup script run on every task is a script whose
// author has to think about being run a thousand times. The marker lives in the container, so a
// replaced container runs setup again, which is right: it is the container that was set up.
func (s *Server) readySkills(ctx context.Context, session *quaycrewv1.Session, held []skill.Held, box sandbox.Sandbox) error {
	for _, given := range held {
		for _, binary := range given.Binaries {
			if s.has(ctx, box, binary) {
				continue
			}
			return status.Errorf(codes.FailedPrecondition,
				"the %s skill needs %s and the sandbox image does not have it. Add it to %s",
				given.Name, binary, s.imageName())
		}
		if !given.HasSetup {
			continue
		}
		at := path.Join(sandbox.SkillsPath, given.Name)
		marker := path.Join("/tmp", ".quay-setup-"+given.Name)
		proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c",
			"[ -f " + marker + " ] || { " + path.Join(at, skill.SetupFile) + " && touch " + marker + "; }"}})
		if err != nil {
			return status.Errorf(codes.Internal, "set up the %s skill: %v", given.Name, err)
		}
		_, _ = io.Copy(io.Discard, proc.Stdout())
		if err := proc.Wait(); err != nil {
			return status.Errorf(codes.FailedPrecondition,
				"the %s skill could not set itself up in the sandbox: %v: %s",
				given.Name, err, proc.Stderr())
		}
	}
	return nil
}

// has says whether a command is in the sandbox.
func (s *Server) has(ctx context.Context, box sandbox.Sandbox, binary string) bool {
	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c", "command -v " + binary}})
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, proc.Stdout())
	return proc.Wait() == nil
}

// imageName is the image to go and fix, and something readable when the system was not told which it is.
func (s *Server) imageName() string {
	if s.sandboxImage == "" {
		return "the sandbox image"
	}
	return s.sandboxImage
}

// syncContext makes the files the model reads agree with the store, in both directions.
//
// The store holds context because a pod has no host directory to mount. The file is a rendering of
// it, and it is read back first: an agent that wrote into its own CLAUDE.md has learned something,
// and overwriting that would make the system's memory worse than a text file.
//
// A failure here never fails a task.
func (s *Server) syncContext(ctx context.Context, session *quaycrewv1.Session) {
	s.syncContextExcept(ctx, session, contextLevel{})
}

// syncContextExcept is syncContext with one level exempt from the read back, because somebody has
// just set it.
//
// `krewe context set` is the operator saying what a level is now. Reading the file back for that level
// first would hand them back the very body they were replacing, so at that one level the store wins
// and everything else is still read back and kept.
func (s *Server) syncContextExcept(ctx context.Context, session *quaycrewv1.Session, settled contextLevel) {
	dirs := s.storage.MyDirs(boxOf(session))
	if len(dirs) != 2 {
		return
	}
	// A session running as a role is never read back into the system's memory.
	//
	// It was given a brief rather than the system's context, so what sits in its file is not an edit of
	// what the store holds: whoever wrote it had never seen the body it would be replacing. Reading it
	// back is also the hole in the boundary, and the one that matters most. A role that receives no
	// context still writes a note at the end of its file, unmarked text belongs to the last scope,
	// and the last scope of the outer file is the workspace. One session that was given nothing would
	// then be writing what every session in the workspace is told.
	if session.GetRole() != "" {
		s.renderContext(ctx, session)
		return
	}
	for at, levels := range contextFiles(session) {
		// Read back first. Something inside the sandbox writing into its own memory has learned
		// something, and overwriting that on the next task would make the system's memory strictly
		// worse than a text file.
		// The skills index is a section in the same file and is not a level: it is rendered from what the
		// session holds rather than written by anybody. It is named in every file so the read back
		// recognises its mark and drops what sits under it, because text under a mark this build does not
		// know is swept into the innermost level, which stores the index as though the operator had typed
		// it and renders it again underneath itself on the next task. The index belongs only to the outer
		// file, but a build that wrote it into the session's own file has been and gone, so the inner
		// file's read back has to know the mark too. It goes first, never last: the last scope is where
		// unmarked text belongs, and a note an agent appends is a note, not an index.
		scopes := make([]string, 0, len(levels)+1)
		scopes = append(scopes, sandbox.SkillsScope, sandbox.RoleScope)
		for _, level := range levels {
			scopes = append(scopes, string(level.scope))
		}
		if onDisk, found := sandbox.ReadMemory(dirs[at]); found {
			written := sandbox.Decompose(onDisk, scopes)
			for _, level := range levels {
				if level == settled {
					continue
				}
				body, said := written[string(level.scope)]
				if !said {
					continue
				}
				kept, err := s.store.GetContext(ctx, level.scope, level.owner)
				if err != nil || kept == body {
					continue
				}
				// An unmarked file was never rendered by the system, so what is in it cannot be an
				// edit of what the store holds: whoever wrote it had never seen the store's body.
				// Treating it as one costs the store's body entirely, which is how a driver taught
				// the manual lost it again on the first sync after being told. Both are kept.
				if !sandbox.Marked(onDisk) {
					body = join(kept, body)
					if body == kept {
						continue
					}
				}
				_ = s.store.SetContext(ctx, level.scope, level.owner, body)
			}
		}
	}
	s.renderContext(ctx, session)
}

// join puts two bodies together, keeping both, and keeps the first as it is when it already says the
// second.
func join(kept, added string) string {
	switch {
	case strings.TrimSpace(kept) == "":
		return added
	case strings.TrimSpace(added) == "", strings.Contains(kept, strings.TrimSpace(added)):
		return kept
	default:
		return strings.TrimRight(kept, "\n") + "\n\n" + added
	}
}

// renderContext writes what the store holds into the files the model reads, and reads nothing back:
// `krewe context set` is the operator saying what the context is now, so the store wins.
//
// A conversation already running does not see it. The tool reads its memory at the start, so a change
// lands on the next task or the next open.
func (s *Server) renderContext(ctx context.Context, session *quaycrewv1.Session) {
	dirs := s.storage.MyDirs(boxOf(session))
	if len(dirs) != 2 {
		return
	}
	// What the session may be told. A role receives the system's context only where it says so, and the
	// brief of the role it runs as always: the brief is not context, it is the whole instruction of
	// this session.
	receivesContext := s.receives(ctx, session, role.MaterialContext)
	brief := s.roleBrief(ctx, session)
	for at, levels := range contextFiles(session) {
		sections := make([]sandbox.Section, 0, len(levels)+2)
		// Who the session is, first, because everything under it is read in that light. It goes in
		// the outer file beside the workspace's context, which is the file every session reads
		// first, and it is rendered every task and never read back.
		if at == outerFile && brief != "" {
			sections = append(sections, sandbox.Section{Scope: sandbox.RoleScope, Body: brief})
		}
		// A role that does not receive context is given none of it: the levels are not rendered, so
		// the file holds the brief and whatever the session itself wrote, and nothing else.
		told := levels
		if !receivesContext {
			told = nil
		}
		for _, level := range told {
			body, err := s.store.GetContext(ctx, level.scope, level.owner)
			if err != nil {
				continue
			}
			// A level can carry a swept skills index, from a build whose read back did not know the mark
			// in the session's own file. An index is rendered state, never context, so the store is put
			// right here, once, rather than rendering the stale index on every task from now on.
			if cleaned, swept := sandbox.WithoutSection(body, sandbox.SkillsScope); swept {
				body = cleaned
				_ = s.store.SetContext(ctx, level.scope, level.owner, body)
			}
			sections = append(sections, sandbox.Section{Scope: string(level.scope), Body: body})
		}
		// The index goes in the outer file, beside the workspace's own context, because every session in
		// a workspace holds the same skills: the system's, and the ones attached to that workspace. It is
		// rendered from what the session holds every time and never read back, so a skill edited in its
		// own directory reaches the next sandbox and an edit from inside one does not survive.
		if at == outerFile {
			if index := s.renderSkills(ctx, session, dirs[at]); index != "" {
				sections = append(sections, sandbox.Section{Scope: sandbox.SkillsScope, Body: index})
			}
		}
		_ = sandbox.WriteMemory(dirs[at], sandbox.Compose(sections))
	}
}

// renderSkills writes the workspace's own skills out of the store onto the host, and returns the index
// naming everything the session holds.
//
// The files and the index are written together on purpose. An index naming a brief that is not there
// sends the model to open a file that does not exist, and files with no index are a capability nothing
// ever mentions.
//
// The system's skills are not written: they are already a directory the operator keeps, and they are
// mounted from it. Only the ones that live in the store have to be put somewhere before they can be.
//
// A failure here does not fail a task, for the same reason a context failure does not. The model is told
// what it holds rather than made to depend on being told.
func (s *Server) renderSkills(ctx context.Context, session *quaycrewv1.Session, _ string) string {
	caps := s.capabilityOf(ctx, session)
	if caps.attachedKnown {
		if dir, ok := s.storage.WorkspaceSkillsDir(session.GetWorkspace()); ok {
			skills := make([]skill.Skill, 0, len(caps.attached))
			for _, one := range caps.attached {
				skills = append(skills, one.Skill)
			}
			if err := sandbox.WriteSkills(dir, skills); err != nil {
				return ""
			}
		}
	}
	// The paths in the index are the sandbox's, because the model is the reader and its view of these
	// directories is the mount rather than the host path this process just wrote to.
	return skill.Index(caps.held)
}

// renderTo writes a changed context out to every live session that reads it, so a change reaches the
// sandboxes that are already running rather than waiting for each of them to be replaced.
//
// Archived sessions are left alone. They are put away, their sandboxes are gone, and the render would
// only create directories for conversations nobody is having.
func (s *Server) renderTo(ctx context.Context, scope store.ContextScope, owner string) {
	sessions, err := s.store.ListSessions(ctx, store.SessionFilter{})
	if err != nil {
		return
	}
	for _, session := range sessions {
		if reads(session, scope, owner) {
			s.syncContextExcept(ctx, session, contextLevel{scope, owner})
		}
	}
}

// reads says whether a session's memory files carry this level of context.
func reads(session *quaycrewv1.Session, scope store.ContextScope, owner string) bool {
	switch scope {
	case store.ContextSystem:
		return true
	case store.ContextWorkspace:
		return session.GetWorkspace() == owner
	case store.ContextProject:
		return session.GetProject() == owner
	case store.ContextSession:
		return session.GetId() == owner
	default:
		return false
	}
}

// The two memory files a session reads: the outer one in the workspace's conversation store, carrying
// the system's context, the workspace's, and the index of the skills the session holds; the inner one in
// the session's own working directory, carrying the project's context and the session's own.
const (
	outerFile = 0
	innerFile = 1
)

// contextLevel is one level of context and whose it is.
type contextLevel struct {
	scope store.ContextScope
	owner string
}

// contextFiles is what goes in each of a session's two memory files: the outer two levels in the
// conversation store's directory, which every session in the workspace reads, and the inner two in
// this session's own working directory, which only it reads.
func contextFiles(session *quaycrewv1.Session) [][]contextLevel {
	return [][]contextLevel{
		{{store.ContextSystem, ""}, {store.ContextWorkspace, session.GetWorkspace()}},
		{{store.ContextProject, session.GetProject()}, {store.ContextSession, session.GetId()}},
	}
}

// closeSandbox tears down and forgets a session's sandbox.
//
// The handle is closed when this process holds one, and the provider removes by name either way: the
// map is a process map, so after a restart it is empty while every container runs on, which is how
// stopping a session used to mark the row and leave the container.
func (s *Server) closeSandbox(ctx context.Context, sessionID string) {
	// The room goes back first. Everything below can fail, and a machine that keeps counting a
	// container it has removed admits less work every time one is stopped.
	s.unplaceSandbox(sessionID)
	s.sandboxesMu.Lock()
	box, ok := s.sandboxes[sessionID]
	delete(s.sandboxes, sessionID)
	s.sandboxesMu.Unlock()
	if ok {
		_ = box.Close(ctx)
	}
	_ = s.provider.Remove(ctx, sessionID)
	// With the sandbox goes what it was born holding: the next one is born with the current set,
	// so a session with no sandbox is never stale.
	_ = s.store.SetSessionSkills(ctx, sessionID, "")
}

// stopSessions stops every live session the filter matches and closes its sandbox. It is what
// deleting has to do to the things it hides: sessions keep their history, and a container running
// for a session nobody can see is the leak archiving already closes.
func (s *Server) stopSessions(ctx context.Context, filter store.SessionFilter) []*quaycrewv1.Session {
	sessions, err := s.store.ListSessions(ctx, filter)
	if err != nil {
		return nil
	}
	for _, session := range sessions {
		if session.GetStatus() != StatusStopped {
			_ = s.store.StopSession(ctx, session.GetId())
			s.emit(ctx, session, KindSessionStopped, "")
		}
		s.closeSandbox(ctx, session.GetId())
	}
	return sessions
}

// ReapStrays removes the sandboxes of sessions that no longer want one: the row is gone, archived, or
// says stopped. Anything stopped or deleted while the system was down was marked in the store and left
// running on the daemon, so this runs once at startup.
func (s *Server) ReapStrays(ctx context.Context) {
	ids, err := s.provider.Stranded(ctx)
	if err != nil {
		return
	}
	for _, id := range ids {
		session, err := s.store.GetSession(ctx, id)
		switch {
		case errors.Is(err, store.ErrNotFound):
			_ = s.provider.Remove(ctx, id)
			s.unplaceSandbox(id)
		case err != nil:
		case session.GetArchivedAt() != nil, session.GetStatus() == StatusStopped:
			_ = s.provider.Remove(ctx, id)
			s.unplaceSandbox(id)
		}
	}
}

// WaitForTasks blocks until every detached task has landed, or until the caller gives up.
//
// A detached task is a goroutine and not a call, so draining requests does not drain it: a graceful
// stop that skips this exits mid task, and the session comes back up settled as failed for no better
// reason than that nobody waited. Bounded by the caller, because a task takes as long as the job
// takes and a shutdown cannot.
func (s *Server) WaitForTasks(ctx context.Context) {
	landed := make(chan struct{})
	go func() {
		defer close(landed)
		s.tasking.Wait()
	}()
	select {
	case <-landed:
	case <-ctx.Done():
	}
}

// SettleTasks marks every session the store still calls running as failed, and runs once at startup.
//
// A task runs in this process. Nothing survives the process going down, so a row saying running on
// the way up is a task that died with the last one, and leaving it says a session is busy for as long
// as the system lives. That reads as a hung conversation and there is nothing to wait for.
func (s *Server) SettleTasks(ctx context.Context) {
	sessions, err := s.store.ListSessions(ctx, store.SessionFilter{})
	if err != nil {
		return
	}
	for _, session := range sessions {
		if session.GetStatus() != StatusRunning {
			continue
		}
		s.recordTask(ctx, session.GetId(), session.GetModelSessionId(), StatusFailed)
		s.settleHistory(ctx, session, "the system restarted while this task was running, so it did not finish")
	}
}

// settleHistory marks whatever the session left open as failed. A task is written when it starts, so
// the row is already there, carrying what the operator asked: closing it says which task died rather
// than that some task did. A session with nothing open still gets a record, because a session the
// store calls running was working on something and a history that says nothing is worse than a
// history that says this much.
func (s *Server) settleHistory(ctx context.Context, session *quaycrewv1.Session, why string) {
	open, err := s.store.ListTasks(ctx, session.GetId(), 0)
	if err == nil {
		settled := false
		for _, task := range open {
			if task.GetStatus() != StatusRunning {
				continue
			}
			s.landTask(ctx, session, &quaycrewv1.TaskEvent{
				Id: task.GetId(), Session: session.GetId(), Workspace: session.GetWorkspace(),
				Project: session.GetProject(), Handle: session.GetHandle(),
				Prompt: task.GetPrompt(), OccurredAt: task.GetOccurredAt(),
			}, StatusFailed, "", why)
			settled = true
		}
		if settled {
			return
		}
	}
	s.recordHistory(ctx, session, &quaycrewv1.TaskEvent{Status: StatusFailed, Failure: why})
}

// CreateWorkspace creates a workspace at runtime.
func (s *Server) CreateWorkspace(ctx context.Context, req *quaycrewv1.CreateWorkspaceRequest) (*quaycrewv1.CreateWorkspaceResponse, error) {
	if err := name.ValidateWorkspace(req.GetName()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	workspace, err := s.store.CreateWorkspace(ctx, req.GetName())
	if err != nil {
		return nil, storeError(err, "create workspace")
	}
	return &quaycrewv1.CreateWorkspaceResponse{Workspace: workspace}, nil
}

// GetWorkspace returns a workspace by id.
func (s *Server) GetWorkspace(ctx context.Context, req *quaycrewv1.GetWorkspaceRequest) (*quaycrewv1.GetWorkspaceResponse, error) {
	workspace, err := s.store.GetWorkspace(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "workspace")
	}
	return &quaycrewv1.GetWorkspaceResponse{Workspace: workspace}, nil
}

// ListWorkspaces lists all workspaces.
func (s *Server) ListWorkspaces(ctx context.Context, _ *quaycrewv1.ListWorkspacesRequest) (*quaycrewv1.ListWorkspacesResponse, error) {
	workspaces, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return nil, storeError(err, "list workspaces")
	}
	return &quaycrewv1.ListWorkspacesResponse{Workspaces: workspaces}, nil
}

// DeleteWorkspace removes a workspace, stopping what it hides first.
func (s *Server) DeleteWorkspace(ctx context.Context, req *quaycrewv1.DeleteWorkspaceRequest) (*quaycrewv1.DeleteWorkspaceResponse, error) {
	gone := s.stopSessions(ctx, store.SessionFilter{Workspace: req.GetId()})
	if err := s.store.DeleteWorkspace(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "workspace")
	}
	// After the delete, so nothing is announced that did not happen. The workspace is gone by now, so
	// these reach the store and not the log: the export names its topic after a workspace there is no
	// longer a row for.
	for _, session := range gone {
		s.emit(ctx, session, KindSessionDeleted, "")
	}
	return &quaycrewv1.DeleteWorkspaceResponse{}, nil
}

// AttachChannel attaches a channel to a workspace.
func (s *Server) AttachChannel(ctx context.Context, req *quaycrewv1.AttachChannelRequest) (*quaycrewv1.AttachChannelResponse, error) {
	if req.GetWorkspace() == "" || req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace and id are required")
	}
	channel, err := s.store.AttachChannel(ctx, req.GetWorkspace(), req.GetId(), req.GetKind())
	if err != nil {
		return nil, storeError(err, "workspace")
	}
	return &quaycrewv1.AttachChannelResponse{Channel: channel}, nil
}

// SetSecret stores a secret in the secrets backend, on one workspace or on the system. The value is
// never returned.
func (s *Server) SetSecret(ctx context.Context, req *quaycrewv1.SetSecretRequest) (*quaycrewv1.SetSecretResponse, error) {
	if err := refusedScope(req.GetScope()); err != nil {
		return nil, err
	}
	system := req.GetScope() == systemScope
	if req.GetKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}
	if !system {
		if req.GetWorkspace() == "" {
			return nil, status.Error(codes.InvalidArgument, "workspace and key are required")
		}
		if _, err := s.store.GetWorkspace(ctx, req.GetWorkspace()); err != nil {
			return nil, storeError(err, "workspace")
		}
	}
	secret := secrets.Secret{
		Name:       req.GetKey(),
		Value:      req.GetValue(),
		Projection: projectionOf(req.GetProjection()),
	}
	// Refused here rather than only in the backend, so a name that cannot be a file name comes back
	// as a bad request instead of as an internal failure the caller cannot act on.
	if err := secret.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// The signing key used to be set, and setting it now would put the private key in every
	// container's environment, where docker inspect reads it for the life of the container. Refused
	// rather than quietly accepted and ignored: a key that looks stored and never signs anything is
	// worse than one that was never accepted. A key's passphrase is worth what the key is worth, so
	// it is held to the same rule.
	if mountedOnly[secret.Name] && secret.Projection.Or() != secrets.File {
		where := req.GetWorkspace()
		if system {
			where = systemScope
		}
		return nil, status.Errorf(codes.InvalidArgument,
			"a signing key is mounted, not set: krewe secret mount %s %s <path to the file holding it>",
			where, secret.Name)
	}
	if system {
		if err := s.secrets.SetSystem(ctx, secret); err != nil {
			return nil, status.Errorf(codes.Internal, "set secret: %v", err)
		}
		return &quaycrewv1.SetSecretResponse{}, nil
	}
	if err := s.secrets.Set(ctx, req.GetWorkspace(), secret); err != nil {
		return nil, status.Errorf(codes.Internal, "set secret: %v", err)
	}
	return &quaycrewv1.SetSecretResponse{}, nil
}

// projectionOf reads the wire's answer for how a secret reaches a sandbox. Unspecified is the
// environment, so a client written before projections existed sets the secrets it always set.
func projectionOf(projection quaycrewv1.SecretProjection) secrets.Projection {
	if projection == quaycrewv1.SecretProjection_SECRET_PROJECTION_FILE {
		return secrets.File
	}
	return secrets.Env
}

// wireProjection is projectionOf the other way round, for a listing.
func wireProjection(projection secrets.Projection) quaycrewv1.SecretProjection {
	if projection.Or() == secrets.File {
		return quaycrewv1.SecretProjection_SECRET_PROJECTION_FILE
	}
	return quaycrewv1.SecretProjection_SECRET_PROJECTION_ENV
}

// ListSecrets says what each workspace has set, and never what any of it says.
//
// The response has no field for a value, so this cannot leak one by mistake rather than by policy.
// What an operator needs from a list of secrets is whether the thing is there, and everything else
// about it is the system's business.
func (s *Server) ListSecrets(ctx context.Context, req *quaycrewv1.ListSecretsRequest) (*quaycrewv1.ListSecretsResponse, error) {
	workspaces, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return nil, storeError(err, "list workspaces")
	}

	// The system's own first and once, rather than repeated under every workspace that reads them. They
	// are in the answer even when one workspace was asked for, because they reach that workspace too
	// and a listing that hid them would say a token is missing that is already there.
	system, err := s.secrets.ListSystem(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list secrets: %v", err)
	}
	out := make([]*quaycrewv1.SecretRef, 0, len(system)+len(workspaces))
	for _, ref := range system {
		out = append(out, &quaycrewv1.SecretRef{
			Name:       ref.Name,
			Projection: wireProjection(ref.Projection),
			System:     true,
		})
	}
	for _, workspace := range workspaces {
		if req.GetWorkspace() != "" && workspace.GetId() != req.GetWorkspace() {
			continue
		}
		// What this workspace set itself, unmerged: a workspace that overrode a system secret shows as
		// having its own, and the system's row above says the system holds one under that name too.
		refs, err := s.secrets.Store.List(ctx, workspace.GetId())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "list secrets: %v", err)
		}
		for _, ref := range refs {
			out = append(out, &quaycrewv1.SecretRef{
				Workspace:     workspace.GetId(),
				WorkspaceName: workspace.GetName(),
				Name:          ref.Name,
				Projection:    wireProjection(ref.Projection),
			})
		}
	}
	return &quaycrewv1.ListSecretsResponse{Secrets: out}, nil
}

// CreateProject adds a body of work to a workspace.
func (s *Server) CreateProject(ctx context.Context, req *quaycrewv1.CreateProjectRequest) (*quaycrewv1.CreateProjectResponse, error) {
	if req.GetWorkspace() == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace is required")
	}
	if err := name.Validate("project", req.GetName()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	project, err := s.store.CreateProject(ctx, req.GetWorkspace(), req.GetName())
	if err != nil {
		return nil, storeError(err, "workspace")
	}
	return &quaycrewv1.CreateProjectResponse{Project: project}, nil
}

// GetProject returns a project by id.
func (s *Server) GetProject(ctx context.Context, req *quaycrewv1.GetProjectRequest) (*quaycrewv1.GetProjectResponse, error) {
	project, err := s.store.GetProject(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "project")
	}
	return &quaycrewv1.GetProjectResponse{Project: project}, nil
}

// ListProjects lists projects, optionally within one workspace.
func (s *Server) ListProjects(ctx context.Context, req *quaycrewv1.ListProjectsRequest) (*quaycrewv1.ListProjectsResponse, error) {
	projects, err := s.store.ListProjects(ctx, req.GetWorkspace())
	if err != nil {
		return nil, storeError(err, "list projects")
	}
	return &quaycrewv1.ListProjectsResponse{Projects: projects}, nil
}

// SetProjectRepository records where a project's work lands, and what kind of repository it is.
//
// The address is held to its shape here, while the person who typed it is looking, for the same
// reason a job's is: an address nothing can be pushed to is worth finding out about now rather than
// an hour into the work. The kind is held to two words. Saying nothing keeps the kind the project
// holds, and means public on a project nobody has told, because free pipeline minutes are the cheaper
// of the two and a project that cannot be public is the one that has to say so.
func (s *Server) SetProjectRepository(ctx context.Context, req *quaycrewv1.SetProjectRepositoryRequest) (*quaycrewv1.SetProjectRepositoryResponse, error) {
	if req.GetProject() == "" {
		return nil, status.Error(codes.InvalidArgument, "which project: say where with an address")
	}
	address := repository.Tidy(req.GetRepository())
	if err := repository.Usable(address); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "this project works in %s", err.Error())
	}
	kind, err := repository.Kind(req.GetVisibility())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// An omitted kind keeps the kind the project holds. Saying nothing means public on a project
	// nobody has told, and it used to mean public on every write, so an operator correcting an
	// address dropped that project from private to public in the same command and was told in the
	// same breath that its pipeline minutes were free.
	if strings.TrimSpace(req.GetVisibility()) == "" {
		held, err := s.store.GetProject(ctx, req.GetProject())
		if err != nil {
			return nil, storeError(err, "project")
		}
		if held.GetVisibility() != "" {
			kind = held.GetVisibility()
		}
	}
	recorded, err := s.store.SetProjectRepository(ctx, req.GetProject(), address, kind)
	if err != nil {
		return nil, storeError(err, "project")
	}
	return &quaycrewv1.SetProjectRepositoryResponse{Project: recorded}, nil
}

// DeleteProject removes a project, stopping what it hides first.
func (s *Server) DeleteProject(ctx context.Context, req *quaycrewv1.DeleteProjectRequest) (*quaycrewv1.DeleteProjectResponse, error) {
	gone := s.stopSessions(ctx, store.SessionFilter{Project: req.GetId()})
	if err := s.store.DeleteProject(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "project")
	}
	for _, session := range gone {
		s.emit(ctx, session, KindSessionDeleted, "")
	}
	return &quaycrewv1.DeleteProjectResponse{}, nil
}

// SetDeployTarget records where a project ships: the account, the region inside it, and the identity
// a pipeline assumes to get there.
//
// The target is read whole before anything is written, so a refused one leaves the project saying
// what it said before. A project answering "where does this go" with something nobody agreed to is
// worse than one answering nothing.
//
// It is not on the driver's deny list. The record grants no reach: the credentials that deploy come
// from secrets, which a driver may not touch, and a session that could set this could already delete
// the project whole.
func (s *Server) SetDeployTarget(ctx context.Context, req *quaycrewv1.SetDeployTargetRequest) (*quaycrewv1.SetDeployTargetResponse, error) {
	if _, err := s.store.GetProject(ctx, req.GetProject()); err != nil {
		return nil, storeError(err, "project")
	}
	target, err := deploy.ParseTarget(
		req.GetTarget().GetAccount(), req.GetTarget().GetRegion(), req.GetTarget().GetIdentity())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.store.SetDeployTarget(ctx, req.GetProject(), target); err != nil {
		return nil, storeError(err, "project")
	}
	written, err := s.store.GetProject(ctx, req.GetProject())
	if err != nil {
		return nil, storeError(err, "project")
	}
	return &quaycrewv1.SetDeployTargetResponse{Project: written}, nil
}

// Dispatch starts or continues a session, running one task through the model runner.
func (s *Server) Dispatch(ctx context.Context, req *quaycrewv1.DispatchRequest) (*quaycrewv1.DispatchResponse, error) {
	// Said before anything is done, because a dispatch that stopped inside this call wrote no line at
	// all and the system read as healthy while it answered none of them.
	slog.InfoContext(ctx, "a dispatch arrived",
		"project", req.GetProject(), "handle", req.GetHandle(), "detached", req.GetDetach())
	if req.GetProject() == "" {
		return nil, status.Error(codes.InvalidArgument, "project is required")
	}
	if req.GetText() == "" {
		return nil, status.Error(codes.InvalidArgument, "text is required")
	}

	// The role is resolved before the session exists, so a step naming a role the workspace does not
	// hold leaves no session and no container behind: it is refused, by name, having done nothing.
	if named := req.GetRole(); named != "" {
		owner, err := s.store.GetProject(ctx, req.GetProject())
		if err != nil {
			return nil, storeError(err, "project")
		}
		if _, err := s.roleFor(ctx, owner.GetWorkspace(), named); err != nil {
			return nil, err
		}
	}

	handle := req.GetHandle()
	if handle == "" {
		handle = store.NewID()
	}
	// The title is tidied the way a label is, and for the same reasons: it goes into a listing row, so
	// a newline in it draws a row two rows tall and a long one pushes the columns that say what the
	// session is doing off the screen.
	session, created, err := s.store.FindOrCreateSession(ctx, req.GetProject(), handle,
		store.Birth{Mode: s.birthMode, Role: req.GetRole(), Title: tidyLabel(req.GetTitle())})
	if err != nil {
		return nil, storeError(err, "project")
	}
	if created {
		s.emit(ctx, session, KindSessionCreated, "")
	}
	// A handle is matched whether the session is put away or not, so a dispatch to one the operator
	// archived used to start a container for a session nobody can see. Archiving stops the session, and
	// stopped has to mean nothing runs here again until somebody brings it back.
	if session.GetArchivedAt() != nil {
		return nil, status.Errorf(codes.FailedPrecondition,
			"session %s is archived: restore it first", display.ShortID(session.GetHandle()))
	}

	// The conversation is named before any task runs in it, so the system can say which conversation a
	// session is working in while it is working rather than once it has finished. A session opened
	// meanwhile lands in the conversation doing the work.
	s.nameConversation(ctx, session)

	// Anything that fails from here on says so on the session's row. The row exists by now, and a row
	// left idle with no task and no container reads as a session waiting for work rather than one
	// that never started.
	//
	// A mode given here applies before the sandbox is built, because a sandbox is born with its
	// capabilities and never drifts: setting it afterwards costs a restart, and a session born unable
	// to read its own skills is the failure this prevents.
	if mode := req.GetPermissionMode(); mode != "" {
		if !model.KnownPermissionMode(mode) {
			return nil, s.startFailed(ctx, session, req.GetText(), status.Errorf(codes.InvalidArgument,
				"%q is not a permission mode: use %s, %s or %s",
				mode, model.PermissionPlan, model.PermissionAcceptEdits, model.PermissionBypass))
		}
		if err := s.store.SetPermissionMode(ctx, session.GetId(), mode); err != nil {
			return nil, s.startFailed(ctx, session, req.GetText(), storeError(err, "session"))
		}
		session.PermissionMode = mode
	}

	if req.GetDetach() {
		// Marked running before the goroutine starts, not inside it: the caller is about to be told the
		// session exists, and a session that reads idle in the listing it draws next is a session the
		// operator will type into while its first task is still running. The task itself marks it
		// running too, which covers the waited path, and doing it twice costs a row update.
		s.recordTask(ctx, session.GetId(), session.GetModelSessionId(), StatusRunning)
		// The task is written down before the answer, for the same reason and one more. A caller told
		// the dispatch happened reads the history next, and a history that still ends on the task
		// before this one reads as though this one was never asked for. The one more is that a
		// controller sending a job's task again reads that history to decide what came of it: written
		// inside the goroutine, the record of the attempt before is still the last one there for as
		// long as the goroutine takes to start, and the controller would answer for this attempt with
		// the last attempt's failure.
		opened := s.beginTask(ctx, session, req.GetText())
		// Detached from the caller's context as well as from its patience. The caller is a console that
		// answers a keystroke and moves on, so its context is cancelled the moment it does, and a task
		// carrying that context would be killed by the very thing that started it.
		s.tasking.Add(1)
		go func(session *quaycrewv1.Session, text, credential string, opened *quaycrewv1.TaskEvent) {
			defer s.tasking.Done()
			_, _ = s.task(context.WithoutCancel(ctx), session, text, credential, opened)
		}(session, req.GetText(), s.credentialFor(ctx, req.GetJob()), opened)
		return &quaycrewv1.DispatchResponse{Id: session.GetId(), Handle: handle}, nil
	}

	reply, err := s.task(ctx, session, req.GetText(), s.credentialFor(ctx, req.GetJob()), nil)
	if err != nil {
		return nil, err
	}
	return &quaycrewv1.DispatchResponse{Id: session.GetId(), Handle: handle, Reply: reply}, nil
}

// task runs one task of a session and records what came of it, whichever way it was dispatched. Both
// roads meet here so a detached task and a waited one cannot come to mean different things: the same
// sandbox, the same recording, the same description behind it.
//
// opened is the task record a caller has already written, which a detached dispatch does before it
// answers. Nil opens one here, which is what the waited road does.
func (s *Server) task(ctx context.Context, session *quaycrewv1.Session, text, credential string,
	opened *quaycrewv1.TaskEvent) (string, error) {
	// Registered before anything runs, so a stop that arrives a moment after the dispatch has
	// something to cancel. The task runs under this context from here down, which is what makes
	// `krewe stop` end the model rather than only mark a row.
	ctx, held := s.beginRunning(ctx, session.GetId())
	defer s.endRunning(session.GetId(), held)
	// Both of these happen before any job does, and that is the point of them. An operator has to be
	// able to see what a session was asked to do while it is doing it, whether or not the caller
	// waited: a task typed at a terminal used to record nothing at all until it landed, so a session
	// working for half an hour read idle, with the task burning its tokens invisible.
	s.recordTask(ctx, session.GetId(), session.GetModelSessionId(), StatusRunning)
	task := opened
	if task == nil {
		task = s.beginTask(ctx, session, text)
	}
	// Emitted here rather than where the task was asked for, so both roads say the same thing: a
	// detached task and a waited one are one path from this line down.
	s.emit(ctx, session, KindSessionStarted, text)

	box, err := s.startSandbox(ctx, session)
	if err != nil {
		if asked, reason := held.stopped(); asked {
			return "", s.landStopped(ctx, session, task, model.Response{}, reason)
		}
		s.recordTask(ctx, session.GetId(), "", StatusFailed)
		// The system's own words for a task it never started, from the one place they are written down.
		// The controller reads this to tell a job it could not start from a job that was wrong, and
		// puts the first back to pending rather than failing it.
		failure := job.NoSandbox + ": " + err.Error()
		s.landTask(ctx, session, task, StatusFailed, "", failure)
		s.emit(ctx, session, KindSessionErrored, failure)
		return "", sandboxError(err, "create sandbox")
	}

	// Named here as well as at dispatch, so a task always carries a conversation whatever road it
	// arrived by, and started decides which of the two ways it is named on the command line.
	conversation := s.nameConversation(ctx, session)
	resp, err := s.runner.Run(ctx, box, model.Request{
		Text:                text,
		ModelSessionID:      conversation,
		ConversationStarted: s.conversationStarted(session, conversation),
		PermissionMode:      permissionModeOf(session, s.birthMode),
		Env:                 withTraceparent(ctx, s.taskEnv(ctx, session, credential)),
		Settings:            s.settingsFor(ctx, session),
	})
	if err != nil {
		if asked, reason := held.stopped(); asked {
			return "", s.landStopped(ctx, session, task, resp, reason)
		}
		s.recordTask(ctx, session.GetId(), "", StatusFailed)
		// A task that failed still spent what it spent, and a bill that counts only the tasks that
		// worked understates itself in exactly the situation somebody is investigating.
		s.measureTask(ctx, session, resp, StatusFailed)
		// The error itself, not a sentence about tasks. Every failure used to read "the model did not
		// complete the task", so a deadline, a crash and a refusal were one indistinguishable line and
		// the operator had nothing to act on.
		s.landTask(ctx, session, task, StatusFailed, "", taskFailure(err))
		s.emit(ctx, session, KindSessionErrored, taskFailure(err))
		return "", status.Errorf(codes.Internal, "run task: %v", err)
	}
	s.recordTask(ctx, session.GetId(), s.confirmConversation(ctx, session, resp.ModelSessionID), StatusIdle)
	s.measureTask(ctx, session, resp, StatusIdle)
	s.landTask(ctx, session, task, StatusIdle, resp.Reply, "")
	s.emit(ctx, session, KindSessionCompleted, resp.Reply)

	// Behind the answer, so the operator waits for their task rather than for the system to think of a
	// name for it. Only the identifier crosses into it: everything else is read again in there, so
	// nothing this call is still holding can be written underneath it.
	s.describing.Add(1)
	go func(sessionID string) {
		defer s.describing.Done()
		s.describeSession(context.WithoutCancel(ctx), sessionID)
	}(session.GetId())

	return resp.Reply, nil
}

// withTraceparent adds this task's trace context to the environment the command runs with.
//
// On the task and never on the sandbox, and that is the whole point of it being here rather than in
// taskEnv. A sandbox is born with its environment and is then reused across every later task, so a
// trace context written at birth labels the tenth task with the first task's span for as long as the
// container lives. The same trap the system's refusal message names about a capability granted after
// birth: the container already running never sees the new value, and the value it does hold is the
// one from a moment that has passed.
//
// The honest limit, stated because a reader will look for it: nothing inside the container adopts
// this yet. The model's own tool does not read it, so what the system gets is one span per attempt
// written by the system, around a job whose inside is opaque.
func withTraceparent(ctx context.Context, env map[string]string) map[string]string {
	parent := telemetry.Traceparent(ctx)
	if parent == "" {
		return env
	}
	if env == nil {
		env = map[string]string{}
	}
	env[telemetry.TraceparentEnv] = parent
	return env
}

// measureTask publishes what a task spent and where it was spent.
//
// The workspace and the project are on it because the useful question is never "what did the system
// cost" but "what did this job cost". The model is on it because a system that moved from
// one model to another wants to see the step.
func (s *Server) measureTask(ctx context.Context, session *quaycrewv1.Session, resp model.Response, status string) {
	s.taskMetrics.Record(ctx, telemetry.TaskMeasurement{
		// Names rather than identifiers, the way the audit export already publishes them: nobody
		// groups a cost dashboard by a uuid. A lookup that fails falls back to the identifier, so a
		// measurement is never lost to a naming problem.
		Workspace: s.workspaceName(ctx, session.GetWorkspace()),
		Project:   s.projectName(ctx, session.GetProject()),
		Model:     s.info.Model,
		Status:    status,
		Usage:     resp.Usage,
		CostUSD:   resp.CostUSD,
		Reported:  resp.UsageReported,
	})
}

func (s *Server) workspaceName(ctx context.Context, id string) string {
	workspace, err := s.store.GetWorkspace(ctx, id)
	if err != nil || workspace.GetName() == "" {
		return id
	}
	return workspace.GetName()
}

func (s *Server) projectName(ctx context.Context, id string) string {
	project, err := s.store.GetProject(ctx, id)
	if err != nil || project.GetName() == "" {
		return id
	}
	return project.GetName()
}

// taskFailure is what the operator is told a failed task failed of.
//
// A cancelled task is named for what actually happened to it, because "context canceled" describes
// the plumbing rather than the event, and the two causes need telling apart: a deadline is a caller
// that would not wait, and a cancellation is a caller that went away.
func taskFailure(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "the task ran past the time the caller allowed it"
	case errors.Is(err, context.Canceled):
		return "the task was cancelled before it finished"
	default:
		return err.Error()
	}
}

// permissionModeOf is the mode a session's tasks run in. A session from before the mode was written
// down has none, and every one of those has been running acceptEdits, so that is what it keeps.
func permissionModeOf(session *quaycrewv1.Session, born string) string {
	if mode := session.GetPermissionMode(); model.KnownPermissionMode(mode) {
		return mode
	}
	if model.KnownPermissionMode(born) {
		return born
	}
	return model.PermissionAcceptEdits
}

// ListContexts says where the files the model reads live: for one project, or for the whole system.
//
// The tool runs on the operator's machine and knows nothing of where this process keeps data, so the
// paths come from here. Both the console and the command line are clients of this one call rather
// than each working the layout out for itself, which is the only way the two cannot drift.
func (s *Server) ListContexts(ctx context.Context, req *quaycrewv1.ListContextsRequest) (*quaycrewv1.ListContextsResponse, error) {
	projects, err := s.contextProjects(ctx, req.GetProject())
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	workspaces, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return nil, storeError(err, "list workspaces")
	}
	for _, workspace := range workspaces {
		names[workspace.GetId()] = workspace.GetName()
	}

	dirs := make([]*quaycrewv1.ContextDir, 0, len(projects)*2+1)
	// The system's own context, first, because it is the level everything else sits inside, and however
	// narrow the question it is part of the answer: every session in the system reads it. It belongs to
	// no directory, being rendered into every workspace's file, so there is no one file to name.
	dirs = append(dirs, s.contextDir(ctx, store.ContextSystem, "", "system", sandbox.Context{}))
	seenWorkspace := map[string]bool{}
	for _, project := range projects {
		// The directories, where the system can describe them. A system that was told no data directory
		// can describe none, and what a level says is held in the store rather than on a disk, so the
		// row still carries the body and simply names no directory. Dropping the row instead took
		// every project's context out of the one call that reads a level back.
		workspaceDir, projectDir := sandbox.Context{}, sandbox.Context{}
		if found := s.storage.Contexts(sandbox.Config{
			ID: "listing", Workspace: project.GetWorkspace(), Project: project.GetId(),
		}); len(found) == 2 {
			workspaceDir, projectDir = found[0], found[1]
		}
		// One row per workspace however many projects it holds: the workspace's context is one thing,
		// and listing it twice would read as two.
		if !seenWorkspace[project.GetWorkspace()] {
			seenWorkspace[project.GetWorkspace()] = true
			dirs = append(dirs, s.contextDir(ctx, store.ContextWorkspace,
				project.GetWorkspace(), names[project.GetWorkspace()], workspaceDir))
		}
		dirs = append(dirs, s.contextDir(ctx, store.ContextProject,
			project.GetId(), project.GetName(), projectDir))
	}
	// A workspace with no projects contributed no row at all, because the rows were built by walking
	// projects. Its context is stored and rendered either way, so writing an org's context into a
	// fresh workspace and then being told the system held nothing was the listing's fault, not the
	// write's. There is no directory to name yet: a context directory belongs to a project.
	if req.GetProject() == "" {
		for _, workspace := range workspaces {
			if seenWorkspace[workspace.GetId()] {
				continue
			}
			dirs = append(dirs, s.contextDir(ctx, store.ContextWorkspace,
				workspace.GetId(), workspace.GetName(), sandbox.Context{}))
		}
	}
	return &quaycrewv1.ListContextsResponse{Dirs: dirs}, nil
}

// SetContext records what the model should be told at a scope, and renders it into every directory
// that already exists for it, so a sandbox already running picks it up on its next task.
func (s *Server) SetContext(ctx context.Context, req *quaycrewv1.SetContextRequest) (*quaycrewv1.SetContextResponse, error) {
	if err := refusedScope(req.GetScope()); err != nil {
		return nil, err
	}
	scope := store.ContextScope(req.GetScope())
	if !store.KnownContextScope(scope) {
		return nil, status.Errorf(codes.InvalidArgument,
			"%q is not a scope: use %s, %s or %s",
			req.GetScope(), store.ContextSystem, store.ContextWorkspace, store.ContextProject)
	}
	if scope != store.ContextSystem && req.GetOwner() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "a %s context needs to say which one", scope)
	}
	if err := s.store.SetContext(ctx, scope, req.GetOwner(), req.GetBody()); err != nil {
		return nil, storeError(err, "context")
	}
	// Out to the sessions that read it. Without this a context only reached a sandbox when that
	// sandbox was created, so telling a running session something did nothing you could see until it
	// was replaced, which is not a thing an operator would think to do.
	s.renderTo(ctx, scope, req.GetOwner())
	return &quaycrewv1.SetContextResponse{
		Dir: &quaycrewv1.ContextDir{
			Scope: req.GetScope(), Owner: req.GetOwner(),
			Body: req.GetBody(), Written: req.GetBody() != "",
		},
	}, nil
}

// contextProjects is the projects a listing covers: one when asked for, else every one the system has.
func (s *Server) contextProjects(ctx context.Context, project string) ([]*quaycrewv1.Project, error) {
	if project == "" {
		projects, err := s.store.ListProjects(ctx, "")
		if err != nil {
			return nil, storeError(err, "list projects")
		}
		return projects, nil
	}
	found, err := s.store.GetProject(ctx, project)
	if err != nil {
		return nil, storeError(err, "project")
	}
	return []*quaycrewv1.Project{found}, nil
}

// contextDir describes one scope's context: where its rendering sits, and what the store holds, which
// is the answer to "what is the model actually told here".
func (s *Server) contextDir(ctx context.Context, scope store.ContextScope, owner, name string, found sandbox.Context) *quaycrewv1.ContextDir {
	body, err := s.store.GetContext(ctx, scope, owner)
	if err != nil {
		body = ""
	}
	return &quaycrewv1.ContextDir{
		Scope:   string(scope),
		Name:    name,
		Owner:   owner,
		Host:    found.Host,
		Sandbox: found.Sandbox,
		Memory:  found.Memory,
		Body:    body,
		Written: body != "",
	}
}

// SetSessionPermissionMode changes what a session's tasks may do without asking.
//
// The mode belongs to the session rather than to a task, so a session started to plan something keeps
// planning instead of being re armed on every dispatch. An unknown mode is refused here rather than
// handed to the model, which would take it as far as its own argument parser and no further.
// ImportSkill takes a skill into the system from the files a client read out of its directory.
//
// The files travel and this side validates, because the control plane runs in a container where a path
// on the operator's machine means nothing, and because one validator is one answer: a client that
// checked for itself would be a second, quietly different, set of rules.
func (s *Server) ImportSkill(ctx context.Context, req *quaycrewv1.ImportSkillRequest) (*quaycrewv1.ImportSkillResponse, error) {
	files := make([]skill.File, 0, len(req.GetFiles()))
	for _, file := range req.GetFiles() {
		files = append(files, skill.File{
			Path:       file.GetPath(),
			Body:       file.GetBody(),
			Executable: file.GetExecutable(),
		})
	}
	loaded, err := skill.FromFiles(files)
	if err != nil {
		// The refusal is the skill package's own sentence, which names what is wrong and what to do
		// about it. Wrapping it in something vaguer would lose the only useful part.
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	imported := store.Imported{Skill: loaded}
	if err := s.store.ImportSkill(ctx, imported); err != nil {
		if errors.Is(err, store.ErrSkillChanged) {
			return nil, status.Errorf(codes.FailedPrecondition,
				"%s version %d is already imported and is a different skill. Raise the version in %s: a workspace pins the version it holds, so changing one underneath it would change what a running session can do.",
				loaded.Name, loaded.Version, skill.ManifestFile)
		}
		return nil, storeError(err, "import skill")
	}
	stored, err := s.store.GetSkill(ctx, loaded.Name, loaded.Version)
	if err != nil {
		return nil, storeError(err, "read the imported skill")
	}
	return &quaycrewv1.ImportSkillResponse{Skill: asSkill(stored)}, nil
}

// ListSkills says what the system can do, or what one workspace holds.
func (s *Server) ListSkills(ctx context.Context, req *quaycrewv1.ListSkillsRequest) (*quaycrewv1.ListSkillsResponse, error) {
	// A session's listing is the same answer its sandbox is built from, so the listing cannot say
	// one thing while the sandbox does another.
	if req.GetSession() != "" {
		session, err := s.store.GetSession(ctx, req.GetSession())
		if err != nil {
			return nil, storeError(err, "session")
		}
		caps := s.capabilityOf(ctx, session)
		out := make([]*quaycrewv1.Skill, 0, len(caps.held)+len(caps.leftOut))
		for _, one := range caps.held {
			out = append(out, skillAsProto(one.Skill))
		}
		// The ones the workspace holds and the session was not given, so the listing answers why a
		// skill the operator attached is nowhere in the conversation.
		for _, one := range caps.leftOut {
			carried := skillAsProto(one.Skill)
			carried.LeftOut = one.Why
			out = append(out, carried)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].GetName() < out[j].GetName() })
		return &quaycrewv1.ListSkillsResponse{Skills: out}, nil
	}
	var held []store.Imported
	var err error
	if req.GetWorkspace() != "" {
		held, err = s.store.WorkspaceSkills(ctx, req.GetWorkspace())
		if err == nil {
			held = s.withSystemSkills(ctx, held)
		}
	} else {
		held, err = s.store.ListSkills(ctx)
	}
	if err != nil {
		return nil, storeError(err, "list skills")
	}
	// Which of them the system holds, so a listing says where a skill came from rather than leaving the
	// operator to guess why a workspace they attached nothing to has four.
	system := map[string]bool{}
	if held, err := s.store.SystemSkills(ctx); err == nil {
		for _, one := range held {
			system[one.Name] = true
		}
	}
	out := make([]*quaycrewv1.Skill, 0, len(held))
	for _, one := range held {
		carried := asSkill(one)
		carried.System = system[one.Name]
		// A workspace's listing answers the same question its sessions do, so a skill its secrets
		// leave out says so here rather than only once a session exists. The system's own listing has
		// no workspace to answer for, so it says nothing.
		if req.GetWorkspace() != "" {
			carried.LeftOut = s.secretMissing(ctx, req.GetWorkspace(), skill.Held{Skill: one.Skill})
		}
		out = append(out, carried)
	}
	return &quaycrewv1.ListSkillsResponse{Skills: out}, nil
}

// AttachSkill gives a workspace a skill, for every sandbox born from now on. A session already
// running keeps what its sandbox was born with: the mount, the secrets and the setup only happen
// at container creation, so the honest thing is to mark it stale in the listing rather than
// rewrite an index it cannot follow.
func (s *Server) AttachSkill(ctx context.Context, req *quaycrewv1.AttachSkillRequest) (*quaycrewv1.AttachSkillResponse, error) {
	if err := refusedScope(req.GetScope()); err != nil {
		return nil, err
	}
	// The system holding a skill is one statement about every workspace, so it renders to every live
	// session rather than to one workspace's.
	if req.GetScope() == systemScope {
		attached, err := s.store.AttachSystemSkill(ctx, req.GetName())
		if err != nil {
			return nil, storeError(err, "attach skill")
		}
		s.renderTo(ctx, store.ContextSystem, "")
		carried := asSkill(attached)
		carried.System = true
		return &quaycrewv1.AttachSkillResponse{Skill: carried}, nil
	}
	attached, err := s.store.AttachSkill(ctx, req.GetWorkspace(), req.GetName())
	if err != nil {
		return nil, storeError(err, "attach skill")
	}
	s.renderTo(ctx, store.ContextWorkspace, req.GetWorkspace())
	return &quaycrewv1.AttachSkillResponse{Skill: asSkill(attached)}, nil
}

// DetachSkill takes a skill away from a workspace, and takes its files off the sessions that held it.
func (s *Server) DetachSkill(ctx context.Context, req *quaycrewv1.DetachSkillRequest) (*quaycrewv1.DetachSkillResponse, error) {
	if err := refusedScope(req.GetScope()); err != nil {
		return nil, err
	}
	if req.GetScope() == systemScope {
		if err := s.store.DetachSystemSkill(ctx, req.GetName()); err != nil {
			return nil, storeError(err, "detach skill")
		}
		s.renderTo(ctx, store.ContextSystem, "")
		return &quaycrewv1.DetachSkillResponse{}, nil
	}
	if err := s.store.DetachSkill(ctx, req.GetWorkspace(), req.GetName()); err != nil {
		return nil, storeError(err, "detach skill")
	}
	s.renderTo(ctx, store.ContextWorkspace, req.GetWorkspace())
	return &quaycrewv1.DetachSkillResponse{}, nil
}

// asSkill renders a skill for a client. The files never travel back: a client asked what the system can
// do, not for a copy of every script.
func asSkill(one store.Imported) *quaycrewv1.Skill {
	out := skillAsProto(one.Skill)
	if !one.ImportedAt.IsZero() {
		out.ImportedAt = timestamppb.New(one.ImportedAt)
	}
	return out
}

// skillAsProto is a skill on the wire, however the system came to hold it. A skill from the system's
// own directory has no imported moment, which is why the timestamp is asSkill's business.
func skillAsProto(one skill.Skill) *quaycrewv1.Skill {
	out := &quaycrewv1.Skill{
		Name:     one.Name,
		Version:  int32(one.Version),
		Summary:  one.Summary,
		Binaries: one.Binaries,
	}
	// Sorted, because a map has no order and a listing that shuffles between reads is a listing nobody
	// can diff.
	for _, name := range one.SecretNames() {
		out.Secrets = append(out.Secrets, &quaycrewv1.SkillSecret{
			Name:    name,
			Purpose: one.Secrets[name],
		})
	}
	return out
}

func (s *Server) SetSessionPermissionMode(ctx context.Context, req *quaycrewv1.SetSessionPermissionModeRequest) (*quaycrewv1.SetSessionPermissionModeResponse, error) {
	if !model.KnownPermissionMode(req.GetMode()) {
		return nil, status.Errorf(codes.InvalidArgument,
			"%q is not a permission mode: use %s, %s or %s",
			req.GetMode(), model.PermissionPlan, model.PermissionAcceptEdits, model.PermissionBypass)
	}
	// The one place where skipping every permission means the host rather than a container. The local
	// backend is a stopgap for running without Docker, and arming a task there gives the model the
	// machine the operator is sitting at.
	if req.GetMode() == model.PermissionBypass && s.info.Sandbox == "local" {
		return nil, status.Errorf(codes.FailedPrecondition,
			"this system runs tasks on the host, not in a container, so %s would give the model your machine",
			model.PermissionBypass)
	}
	if _, err := s.store.GetSession(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "session")
	}
	if err := s.store.SetPermissionMode(ctx, req.GetId(), req.GetMode()); err != nil {
		return nil, storeError(err, "session")
	}
	return &quaycrewv1.SetSessionPermissionModeResponse{Session: s.reread(ctx, req.GetId())}, nil
}

// SetSessionLabel records what the operator calls a conversation. Empty clears it.
//
// A label is trimmed and capped rather than refused. It is a name somebody typed, so the only ways it
// can be wrong are leading space, which is invisible, and being long enough to push every other
// column off the screen. Neither is worth a refusal the operator has to read.
func (s *Server) SetSessionLabel(ctx context.Context, req *quaycrewv1.SetSessionLabelRequest) (*quaycrewv1.SetSessionLabelResponse, error) {
	if _, err := s.store.GetSession(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "session")
	}
	if err := s.store.SetLabel(ctx, req.GetId(), tidyLabel(req.GetLabel())); err != nil {
		return nil, storeError(err, "session")
	}
	return &quaycrewv1.SetSessionLabelResponse{Session: s.reread(ctx, req.GetId())}, nil
}

// labelLimit is how much of a label is kept. A listing gives the name one column among ten, so a
// label longer than this is one nobody can read anyway, and keeping it whole would only push the
// columns that say what the session is doing off the screen.
const labelLimit = 60

// tidyLabel is a label as it is stored: trimmed, one line, and capped.
//
// A newline matters more than it looks. A label goes into a listing row, and a stored newline draws a
// row that is two rows tall, which breaks the cursor and every count of what is on screen.
func tidyLabel(label string) string {
	tidy := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(label, "\r", " "), "\n", " "))
	if runes := []rune(tidy); len(runes) > labelLimit {
		return strings.TrimSpace(string(runes[:labelLimit]))
	}
	return tidy
}

// recordTask stores the outcome of a task. A store failure here must not replace the task's own
// result, which the operator already has, so it is not returned; a later read shows a stale status
// rather than the task appearing to have failed when it did not.
//
// An archived session keeps its conversation handle and its status. Archiving stops a session and takes
// its container away, and the task that was running in it lands afterwards: recording what that task
// came to put the session back to idle, or marked it failed, so a session the operator had just put
// away read as one still working. The handle is still written, because restoring the session has to
// come back to the conversation it was in.
func (s *Server) recordTask(ctx context.Context, sessionID, modelSessionID, sessionStatus string) {
	if session, err := s.store.GetSession(ctx, sessionID); err == nil && session.GetArchivedAt() != nil {
		sessionStatus = session.GetStatus()
	}
	_ = s.store.RecordTask(ctx, sessionID, modelSessionID, sessionStatus)
}

// environ renders an environment map as the "KEY=value" entries a sandbox expects, sorted so the
// result is stable.
func environ(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]string, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, key+"="+values[key])
	}
	return entries
}

// taskEnv gathers the environment a task runs with from the workspace's secrets. A workspace that has
// set none, or a model backend that needs none, simply runs with no extra env: nothing here fails a
// task, because a secret that cannot be read is a worse reason to refuse job than to attempt it.
func (s *Server) taskEnv(ctx context.Context, session *quaycrewv1.Session, credential string) map[string]string {
	env := map[string]string{}
	// Who this session is. The volume is shared by every session in the workspace, so anything a
	// session puts there needs a name of its own to avoid the session beside it: the git brief names
	// a working tree and a branch after this. It is an identifier the system already shows and never a
	// credential, so every session is told it rather than the driver alone.
	if id := session.GetId(); id != "" {
		env[sandbox.SessionIDEnv] = id
	}
	// Where to reach the system, so `krewe` run inside the driver works with nothing to configure. The
	// driver holds the system's own interface, which is why it is told here and why it is told the
	// driver's token: an ordinary session has no business driving the system. A session running a job
	// is told the same address below, with a credential that buys it four verbs rather than
	// the system.
	//
	// Every sandbox is on a network that reaches the control plane, so being told is what decides
	// this and not what the container can address. The network carries no permission: a session
	// presenting no credential is refused every call.
	if session.GetDriver() && s.reachable != "" {
		env[grpcAddrEnv] = s.reachable
		// The token travels with the address and only with it: an ordinary session is told
		// neither, and an address the system refuses to answer would read as the system being down.
		if s.driverToken != "" {
			env[auth.TokenEnv] = s.driverToken
		}
	}
	// The credential this one task runs under, where the task runs a job. It is minted for
	// that job, carries the verbs its role declared, and expires with it, so a value read out of the
	// container grants what that job could do and only until it ends.
	//
	// Never written at sandbox birth, and that is the constraint the obvious design fails on: a
	// sandbox keeps what it was created with, so a credential put there would label every later task
	// with the first task's grant, and one minted afterwards would never reach the container.
	if credential != "" && s.reachable != "" {
		env[grpcAddrEnv] = s.reachable
		env[auth.TokenEnv] = credential
	}
	// Who a commit is by. All four, because git wants an author and a committer and refuses on either
	// missing: setting the author alone produces "Committer identity unknown" and a wall of advice,
	// halfway through whatever the session was doing. The committer is the author, because a session
	// commits as the operator rather than on behalf of somebody else.
	if s.gitAuthor.Complete() {
		env["GIT_AUTHOR_NAME"] = s.gitAuthor.Name
		env["GIT_AUTHOR_EMAIL"] = s.gitAuthor.Email
		env["GIT_COMMITTER_NAME"] = s.gitAuthor.Name
		env["GIT_COMMITTER_EMAIL"] = s.gitAuthor.Email
	}
	// Everything the workspace holds. Setting a secret on a workspace is the operator saying its
	// sessions may use it, and a skill is attached to a workspace by the same person, so a further
	// list of which names are allowed out was a third answer to a question already answered twice.
	//
	// The model's own token is asked for by name as well, so a store that cannot enumerate still
	// runs a task rather than failing one for a reason the operator cannot see.
	//
	// A mounted secret is left out. It reaches the sandbox as a file, and putting it here as well
	// would hand back the exposure the file exists to avoid: a container's environment is readable
	// through docker inspect for the life of that container.
	named := []string{model.ClaudeCodeOAuthTokenEnv}
	stored, err := s.secrets.List(ctx, session.GetWorkspace())
	if err == nil {
		for _, ref := range stored {
			if ref.Projection.Or() == secrets.File {
				continue
			}
			named = append(named, ref.Name)
		}
	}
	for _, name := range named {
		if _, already := env[name]; already {
			continue
		}
		// QC_ is the system's own configuration: the address a session dials and the token it dials
		// with are put here by the system, so a workspace secret answering to one of those would be
		// posing as the system rather than being handed out by it. CLAUDE_ names travel, since the
		// model's token is one and is set exactly this way.
		if strings.HasPrefix(name, systemOwnPrefix) {
			continue
		}
		value, err := s.secrets.Get(ctx, session.GetWorkspace(), name)
		if err != nil || value == "" {
			continue
		}
		env[name] = value
	}
	// The model's token a second time, under a name Claude Code leaves alone. The CLI strips
	// CLAUDE_CODE_OAUTH_TOKEN from every process a task starts, so a hook fired on a message holds no
	// credential and cannot ask a model anything. The value is the same value; only the name is new.
	//
	// The system owns this name, so it is written last and a workspace secret answering to it does not
	// stand in for the token a task actually runs under.
	if token := env[model.ClaudeCodeOAuthTokenEnv]; token != "" {
		env[model.ModelTokenEnv] = token
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

// grpcAddrEnv is what the krewe command line reads to find a control plane. A session gets it so the
// tool inside a sandbox needs no arguments.
const grpcAddrEnv = "QC_GRPC_ADDR"

// systemOwnPrefix marks the names the system puts into a sandbox itself, so a workspace secret cannot
// take one of them and be read as configuration the system wrote.
const systemOwnPrefix = "QC_"

// ListSessions lists sessions, optionally filtered by workspace.
//
// A request that asks for presence gets each idle session's sandbox read as well, which is what
// lets a listing tell a conversation nobody is watching from an empty container. It is asked for
// rather than always done because the answer costs a question to the sandbox per row, and the
// callers that resolve an address or find a session by name have no use for it.
func (s *Server) ListSessions(ctx context.Context, req *quaycrewv1.ListSessionsRequest) (*quaycrewv1.ListSessionsResponse, error) {
	sessions, err := s.store.ListSessions(ctx, store.SessionFilter{
		Workspace: req.GetWorkspace(),
		Project:   req.GetProject(),
		Archived:  req.GetArchived(),
	})
	if err != nil {
		return nil, storeError(err, "list sessions")
	}
	for _, session := range sessions {
		s.withUsage(session)
		s.withContextWindow(session)
	}
	s.withStaleness(ctx, sessions)
	if req.GetPresence() {
		s.withPresence(ctx, sessions)
	}
	return &quaycrewv1.ListSessionsResponse{Sessions: sessions}, nil
}

// GetSession returns a session by id.
func (s *Server) GetSession(ctx context.Context, req *quaycrewv1.GetSessionRequest) (*quaycrewv1.GetSessionResponse, error) {
	session, err := s.store.GetSession(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	s.withUsage(session)
	s.withContextWindow(session)
	s.withStaleness(ctx, []*quaycrewv1.Session{session})
	return &quaycrewv1.GetSessionResponse{Session: session}, nil
}

// withStaleness marks the sessions whose live sandbox was born before the workspace's current skill
// set. A session with no recorded birth set has no live sandbox and is never stale. The current set
// is computed once per workspace, not once per session, because a listing is most of what the
// console asks for.
func (s *Server) withStaleness(ctx context.Context, sessions []*quaycrewv1.Session) {
	current := map[string]string{}
	for _, session := range sessions {
		born, err := s.store.SessionSkills(ctx, session.GetId())
		if err != nil || born == "" {
			continue
		}
		workspace := session.GetWorkspace()
		if _, known := current[workspace]; !known {
			current[workspace] = skill.FingerprintHeld(s.capabilityOf(ctx, session).held)
		}
		session.Stale = born != current[workspace]
	}
}

// withUsage puts what a session's conversation has cost onto it, read from the transcript the model
// keeps rather than from anything the system recorded.
//
// It has to come from there: the conversations worth counting are the ones held in the panel, and
// those never pass through the control plane at all. A session nobody has spoken in is left without a
// figure rather than reported as costing nothing, because those are different things.
func (s *Server) withUsage(session *quaycrewv1.Session) {
	spent := s.storage.ConversationUsage(boxOf(session), session.GetModelSessionId())
	if spent.Empty() {
		return
	}
	session.Usage = &quaycrewv1.Usage{
		Input:        spent.Input,
		Output:       spent.Output,
		CacheRead:    spent.CacheRead,
		CacheWritten: spent.CacheWritten,
	}
}

// withContextWindow puts how full the model's context window is onto a session.
//
// A different question from what the conversation cost, and the one that decides whether it is still
// worth continuing: cost only grows, while the window empties again when the model compacts. The used
// figure comes from the transcript, and the size of the window from whatever the model runtime last
// told a session in this workspace, because nothing else in the system knows it.
//
// A conversation nobody has spoken in is left without a figure. A window nobody has been told the
// size of is reported as a count with no size, which a listing shows as tokens rather than as a share
// it made up.
func (s *Server) withContextWindow(session *quaycrewv1.Session) {
	carried := s.storage.ConversationContext(boxOf(session), session.GetModelSessionId())
	if carried.Empty() {
		return
	}
	size, _ := s.storage.ContextWindowSize(session.GetWorkspace())
	session.ContextWindow = &quaycrewv1.ContextWindow{Used: carried.Carried(), Size: size}
}

// AttachSession describes how to open a session's conversation.
//
// It returns the container and the command, and no credential. The conversation handle is a pointer
// into the model's own store, not a secret, so the operator's environment supplies the subscription
// token when they run it. Keeping the token out of this response is deliberate: a value the secrets
// backend holds should not become readable through the API just because a client asks nicely.
func (s *Server) AttachSession(ctx context.Context, req *quaycrewv1.AttachSessionRequest) (*quaycrewv1.AttachSessionResponse, error) {
	session, err := s.store.GetSession(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	// The row never decides whether a conversation opens. It is the system's own bookkeeping, and it
	// used to be the gate: a drain puts every live session down, `make upgrade` drains, so after an
	// upgrade the whole system said stopped and every session refused to open. An archived session was
	// worse. Archiving sets the row to stopped as well, so the refusal read "is stopped: restart it
	// first", and restarting an archived session is itself refused. Attach named the one action that
	// could not be taken.
	//
	// So the row is brought up to date rather than obeyed. A session somebody is talking in is
	// neither put away nor put down, and a row that said otherwise would have the next startup reap
	// the container out from under the conversation.
	if session.GetArchivedAt() != nil || session.GetStatus() == StatusStopped {
		if session.GetArchivedAt() != nil {
			if err := s.store.RestoreSession(ctx, session.GetId()); err != nil {
				return nil, storeError(err, "restore session")
			}
		}
		if session.GetStatus() == StatusStopped {
			if err := s.store.RestartSession(ctx, session.GetId()); err != nil {
				return nil, storeError(err, "restart session")
			}
		}
		// Read back rather than patched in place, so the conversation below is recorded against the
		// state the store now holds rather than the one this call arrived with.
		if session, err = s.store.GetSession(ctx, req.GetId()); err != nil {
			return nil, storeError(err, "session")
		}
	}
	// Opening a session opens the conversation the session already holds. The system names one before
	// any task runs in it, so by the time anybody can open a session there is a name to open, and
	// naming one here would be naming a second conversation beside the first.
	//
	// A session with no name at all is one nothing has ever been dispatched to, or one carried over
	// from a system that named conversations after the task rather than before it. Naming it here is
	// what makes it openable: a first task that failed used to leave a session in the listing that
	// nobody could open at all.
	if session.GetModelSessionId() == "" {
		if session.GetStatus() == StatusRunning {
			// The one case where naming a conversation here is the defect rather than the fix. The task
			// is working in a conversation this system never named and cannot know the name of until the
			// task lands, and a name minted now would open an empty conversation beside the job.
			return nil, status.Errorf(codes.FailedPrecondition,
				"session %s is running a task that named its own conversation: it lands on the session "+
					"when the task finishes, and opening the session now would start a second conversation "+
					"beside the one doing the work", display.ShortID(session.GetHandle()))
		}
		s.nameConversation(ctx, session)
	}
	// A handle can outlive what it points at, and a conversation the system has just named has no
	// transcript either, so the two cannot be told apart here. The sandbox decides: it resumes a
	// transcript that exists and starts one under the same name when it does not.
	// The live sandboxes are a map in this process, so a restart empties it while the row still says
	// idle. State is on the host, so a fresh container over the same mounts is the same conversation.
	if _, err := s.sandboxFor(ctx, session); err != nil {
		return nil, sandboxError(err, "start sandbox")
	}
	// Inside tmux, so detaching leaves the model running. -A attaches to the session already there
	// rather than starting a second beside it, and the permission mode is the session's own, or a
	// session armed to skip permissions asks anyway the moment it is opened.
	return &quaycrewv1.AttachSessionResponse{
		Sandbox: sandbox.ContainerName(session.GetId()),
		Argv: []string{"tmux", "new-session", "-A", "-s", sandbox.AttachedSessionName,
			sandbox.OpenConversation, session.GetModelSessionId(), permissionModeOf(session, s.birthMode)},
	}, nil
}

// RestartSession gives a session a fresh sandbox and leaves it idle, whatever state it was in.
//
// The sandbox is started here rather than on the next task, so the operator can attach into the
// conversation straight away instead of having to dispatch a task to make the container exist. That
// is only safe because a session's state lives on the host now: the sandbox this creates is a new
// container over the same conversation store and the same project files.
//
// A session that is already live is stopped first rather than refused. Restarting is what the operator
// reaches for when the container is wrong: a wedged task, a shell that will not answer, a credential
// the sandbox was born without. Refusing until it was stopped made that two keys, and the second was
// the one that did the job, so the first key read as broken.
//
// An archived session is refused. It is put away, and bringing one back is what restore is for.
func (s *Server) RestartSession(ctx context.Context, req *quaycrewv1.RestartSessionRequest) (*quaycrewv1.RestartSessionResponse, error) {
	session, err := s.store.GetSession(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	if session.GetArchivedAt() != nil {
		return nil, status.Errorf(codes.FailedPrecondition,
			"session %s is archived: restore it first", display.ShortID(session.GetHandle()))
	}
	// The old container goes before the new one is asked for, so the session never holds two, and a
	// task running in the old one loses the room it was working in, which is the point of the key.
	if session.GetStatus() != StatusStopped {
		if err := s.store.StopSession(ctx, req.GetId()); err != nil {
			return nil, storeError(err, "session")
		}
		s.closeSandbox(ctx, req.GetId())
	}
	if _, err := s.sandboxFor(ctx, session); err != nil {
		return nil, sandboxError(err, "create sandbox")
	}
	if err := s.store.RestartSession(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "session")
	}
	restarted, err := s.store.GetSession(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	return &quaycrewv1.RestartSessionResponse{Session: restarted}, nil
}

// ArchiveSession puts a session away, stopping it first if it is running.
//
// Nothing is deleted, by anyone, here: the row, the conversation handle, the conversation store on
// the host and the project's files are all untouched. Archiving is a stamp, so restoring is clearing
// it. Deleting a conversation is a separate decision and not something to slip in behind a key.
func (s *Server) ArchiveSession(ctx context.Context, req *quaycrewv1.ArchiveSessionRequest) (*quaycrewv1.ArchiveSessionResponse, error) {
	session, err := s.store.GetSession(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	if session.GetArchivedAt() != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "session %s is already archived", req.GetId())
	}
	// A container left running for a session nobody can see is exactly the leak this project keeps
	// finding, so put the sandbox away with the session.
	if session.GetStatus() != StatusStopped {
		if err := s.store.StopSession(ctx, req.GetId()); err != nil {
			return nil, storeError(err, "session")
		}
		s.closeSandbox(ctx, req.GetId())
	}
	if err := s.store.ArchiveSession(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "session")
	}
	archived := s.reread(ctx, req.GetId())
	s.emit(ctx, archived, KindSessionArchived, "")
	return &quaycrewv1.ArchiveSessionResponse{Session: archived}, nil
}

// RestoreSession brings an archived session back into the default listing. It comes back stopped,
// which is what it is: archiving stopped it, and starting a container again is what restart is for.
func (s *Server) RestoreSession(ctx context.Context, req *quaycrewv1.RestoreSessionRequest) (*quaycrewv1.RestoreSessionResponse, error) {
	session, err := s.store.GetSession(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	if session.GetArchivedAt() == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "session %s is not archived", req.GetId())
	}
	if err := s.store.RestoreSession(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "session")
	}
	restored := s.reread(ctx, req.GetId())
	s.emit(ctx, restored, KindSessionRestored, "")
	return &quaycrewv1.RestoreSessionResponse{Session: restored}, nil
}

// reread returns the session as it now is, so a caller does not have to ask again. A read that fails
// here is not worth failing the write that already succeeded, so it yields nothing rather than an
// error the caller would misread as the action not having happened.
func (s *Server) reread(ctx context.Context, id string) *quaycrewv1.Session {
	session, err := s.store.GetSession(ctx, id)
	if err != nil {
		return nil
	}
	return session
}

// StopSession marks a session stopped and tears down its sandbox.
func (s *Server) StopSession(ctx context.Context, req *quaycrewv1.StopSessionRequest) (*quaycrewv1.StopSessionResponse, error) {
	if err := s.store.StopSession(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "session")
	}
	s.closeSandbox(ctx, req.GetId())
	s.emit(ctx, s.reread(ctx, req.GetId()), KindSessionStopped, "")
	return &quaycrewv1.StopSessionResponse{}, nil
}

// DrainSessions puts every live session down, so whatever is about to take the containers away finds
// nothing running.
//
// `make upgrade` removes sandboxes by name from the daemon. A container removed that way takes a task
// in flight with it, which lands as "model: run exited: exit status 137, and it said nothing about
// why", and it leaves the row still holding the skills its container was born with while the
// container is gone, so the listing measures staleness against a birth set nothing has any more.
// Going through here instead stops each session first: the row says stopped, which is true, and the
// birth set is cleared with the sandbox.
//
// A task under way refuses the drain rather than being interrupted, and the answer names what is
// working so the operator can wait for it. Force is the operator saying they know, and then the drain
// says what it interrupted rather than doing it quietly.
func (s *Server) DrainSessions(ctx context.Context, req *quaycrewv1.DrainSessionsRequest) (*quaycrewv1.DrainSessionsResponse, error) {
	sessions, err := s.store.ListSessions(ctx, store.SessionFilter{})
	if err != nil {
		return nil, storeError(err, "list sessions")
	}
	var live, working []*quaycrewv1.Session
	for _, session := range sessions {
		if session.GetStatus() == StatusStopped {
			continue
		}
		live = append(live, session)
		if session.GetStatus() == StatusRunning {
			working = append(working, session)
		}
	}
	if len(working) > 0 && !req.GetForce() {
		return nil, status.Errorf(codes.FailedPrecondition,
			"%s still working: %s. Wait for it, or drain anyway and lose the task",
			taskWord(len(working)), namesOf(working))
	}
	for _, session := range live {
		if err := s.store.StopSession(ctx, session.GetId()); err != nil {
			return nil, storeError(err, "session")
		}
		s.closeSandbox(ctx, session.GetId())
	}
	return &quaycrewv1.DrainSessionsResponse{Stopped: live, Working: working}, nil
}

// taskWord counts the tasks in flight in words, because "1 tasks still working" is the sentence that
// makes an operator doubt the rest of the message.
func taskWord(count int) string {
	if count == 1 {
		return "1 task is"
	}
	return fmt.Sprintf("%d tasks are", count)
}

// namesOf lists sessions the way the operator sees them: the handle they would type, and the label
// they read, for as many as fit before the sentence stops being a sentence.
func namesOf(sessions []*quaycrewv1.Session) string {
	const most = 5
	names := make([]string, 0, len(sessions))
	for _, session := range sessions {
		name := display.ShortID(session.GetHandle())
		if label := session.GetLabel(); label != "" {
			name += " (" + label + ")"
		}
		names = append(names, name)
	}
	if len(names) > most {
		return strings.Join(names[:most], ", ") + fmt.Sprintf(" and %d more", len(names)-most)
	}
	return strings.Join(names, ", ")
}

// OpenDriver returns the project's driver, the session that drives the system, creating it the first
// time somebody opens it. It is what `krewe` opens beside the console.
func (s *Server) OpenDriver(ctx context.Context, req *quaycrewv1.OpenDriverRequest) (*quaycrewv1.OpenDriverResponse, error) {
	if req.GetProject() == "" {
		return nil, status.Error(codes.InvalidArgument, "a driver belongs to a project, so name one")
	}
	session, err := s.store.FindOrCreateDriver(ctx, req.GetProject())
	if err != nil {
		return nil, storeError(err, "project")
	}
	s.teachDriver(ctx, session)
	return &quaycrewv1.OpenDriverResponse{Session: session}, nil
}

// teachDriver writes what krewe is into the driver's own context, so it opens knowing what it is for
// rather than having to be told every time. It is the system describing itself: the command list the
// tool prints, and the behaviour specification the binary carries.
//
// The driver's own level, not the project's, and written once: overwriting on every open would make
// it the one context nobody can change.
func (s *Server) teachDriver(ctx context.Context, session *quaycrewv1.Session) {
	if existing, err := s.store.GetContext(ctx, store.ContextSession, session.GetId()); err == nil && existing != "" {
		return
	}
	if err := s.store.SetContext(ctx, store.ContextSession, session.GetId(), manual.Text()); err != nil {
		// A driver that has not been told what krewe is still opens, and can be told by hand. Refusing
		// to open the system over it would be worse than opening one that has to ask.
		return
	}
	// Out to the file the driver reads, rather than waiting for its sandbox to be made. Notes it
	// already had are kept: this is the system adding what it is, not replacing what was there.
	s.syncContext(ctx, session)
}
