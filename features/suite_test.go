// Package features holds the executable specification of Quay Crew.
//
// The feature files next to this one state what the product does, in language a reader who is not
// holding the code can follow. The steps below drive the control plane over its real gRPC interface,
// the same one the command line tool, the channels and the dashboard use, so a scenario survives the
// implementation changing underneath it.
//
// Scope this to the control plane contract. A behaviour that is better said as a Go table test
// belongs in the package it tests, not here.
//
// The model runner and the sandbox provider are doubles, so these scenarios are fast and need no
// Docker daemon and no subscription. They therefore prove routing, session identity, sandbox
// lifecycle and error handling, and they deliberately do not prove that a real turn executes. That
// is the job of the dispatch smoke in continuous integration, which boots the composed stack and
// runs a turn for real, and of the gated test in internal/model that needs a live subscription.
package features_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/auth"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/flow"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/skill"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/cucumber/godog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"time"
)

func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"."},
			TestingT: t,
			// Strict fails the run on an undefined or pending step, so a scenario can never
			// silently pass by not being implemented.
			Strict: true,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("behaviour specifications failed")
	}
	// A run that finds no feature files reports success, which would make this suite green while
	// proving nothing. Fail loudly instead.
	if scenariosRun.Load() == 0 {
		t.Fatal("no scenarios ran: no feature files were found, so this suite proves nothing")
	}
}

// scenariosRun counts the scenarios executed, so an empty run cannot pass as a green one.
var scenariosRun atomic.Int64

// recordingRunner is a model runner double that records every turn it was asked to run and hands
// back a distinct conversation id each time, so a scenario can assert which conversation the next
// turn resumed.
type recordingRunner struct {
	mu       sync.Mutex
	requests []model.Request
	// failNext makes the next turn fail, which is how a scenario gets a session that exists but has
	// no conversation behind it.
	failNext bool
	// takes is how long a turn pretends to take. Zero is instant, which is right for almost every
	// scenario and wrong for any scenario about something happening while a turn is under way:
	// with an instant model a whole automation finishes before the next step runs, and a scenario
	// about stopping one would be racing rather than specifying.
	takes time.Duration
	// gate holds every turn open until it is closed, which is takes without the guesswork: a scenario
	// about what is true *while* a turn runs cannot be written against a clock, because the clock is
	// a different length on every machine. Nil runs straight through.
	gate chan struct{}
	// started is closed when the first turn begins, so a scenario can know a turn is genuinely under
	// way rather than infer it from how long a step took.
	started chan struct{}
	once    sync.Once
}

// hold makes every turn wait, and returns the func that lets them go.
func (r *recordingRunner) hold() func() {
	r.mu.Lock()
	r.gate, r.started = make(chan struct{}), make(chan struct{})
	gate := r.gate
	r.mu.Unlock()
	return func() { close(gate) }
}

// waitForTurn blocks until a turn has reached the runner, so a scenario never asserts on a thread
// whose turn has not started yet.
func (r *recordingRunner) waitForTurn() error {
	r.mu.Lock()
	started := r.started
	r.mu.Unlock()
	if started == nil {
		return fmt.Errorf("no turn was held, so there is nothing to wait for")
	}
	select {
	case <-started:
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("no turn reached the model runner")
	}
}

var _ model.Runner = (*recordingRunner)(nil)

func (r *recordingRunner) Run(_ context.Context, _ sandbox.Sandbox, req model.Request) (model.Response, error) {
	r.mu.Lock()
	takes, gate, started := r.takes, r.gate, r.started
	r.mu.Unlock()
	if started != nil {
		r.once.Do(func() { close(started) })
	}
	if gate != nil {
		<-gate
	}
	if takes > 0 {
		time.Sleep(takes)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	if r.failNext {
		r.failNext = false
		return model.Response{}, fmt.Errorf("the model refused this turn")
	}
	return model.Response{
		Reply:          "you said: " + req.Text,
		ModelSessionID: fmt.Sprintf("conversation-%d", len(r.requests)),
	}, nil
}

func (r *recordingRunner) turn(i int) (model.Request, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i < 0 || i >= len(r.requests) {
		return model.Request{}, false
	}
	return r.requests[i], true
}

// lastRequest is the turn the model was asked to run most recently, which is what a scenario about
// what a turn ran as has to look at.
func (r *recordingRunner) lastRequest() model.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) == 0 {
		return model.Request{}
	}
	return r.requests[len(r.requests)-1]
}

func (r *recordingRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

// turn is what the operator saw come back from one dispatch.
type turn struct {
	sessionID string
	threadID  string
	reply     string
}

// world is one scenario's state. A fresh one is built before each scenario, so scenarios never leak
// into each other.
type world struct {
	grpcServer *grpc.Server
	// listener is kept so a scenario can dial the same crew as a different caller, presenting
	// something other than what the world's own client presents.
	listener *bufconn.Listener
	// token is the crew's token, which every caller has to present to be served.
	token string
	// driverToken is the driver's own token: recognised, and refused the calls that grant capability.
	driverToken string
	conn        *grpc.ClientConn
	client      quaycrewv1.ControlPlaneServiceClient
	provider    *sandbox.FakeProvider
	runner      *recordingRunner
	// realRunner replaces the recording double when a scenario is about what the real one does with
	// what came out of the sandbox. A double that hands back a canned error cannot say anything about
	// an explanation built from a stream.
	realRunner model.Runner
	// reachable is the address a session is told to dial for the crew, empty when it cannot reach it.
	reachable string
	// gitAuthor is who a commit made inside a sandbox is by.
	gitAuthor controlplane.Identity
	// flowRun is the run the last flow step started.
	flowRun flow.Run
	// flowRunID is the run the operator surface steps started, and driverErr what the driver was
	// told when it tried something.
	flowRunID string
	driverErr error
	// server is the control plane itself, kept so a scenario can drive what main does at startup
	// rather than only what a client can call.
	server *controlplane.Server
	// release lets go of a turn a scenario is holding open, and is nil when none is held.
	release func()
	// otherWorkspaceID is a second workspace, for the scenarios about what one workspace's
	// attachment does and does not reach.
	otherWorkspaceID string
	// seedHooks says this scenario is a crew that seeds the shipped hooks, so a restart seeds again
	// the way the real main does.
	seedHooks bool
	// skillsDir is where the scenario's skills are written, and skills is what was read from it.
	skillsDir string
	skills    []skill.Skill
	// drivers are the sessions returned by opening the crew, so a scenario can say it was the same one.
	drivers []*quaycrewv1.Thread
	secrets secrets.Store
	store   store.Store
	// events is the log the control plane publishes turns to. A scenario asserts on what landed on
	// it. Setting it to nil is how a scenario says the stack has no broker configured.
	events *messaging.Memory
	// storage is a real conversation store on disk, so a scenario can say what the model kept and
	// what it did not. The scenarios that do not care about it seed every conversation they start.
	storage sandbox.Storage
	// info is what the control plane reports about itself, describing the doubles the scenarios
	// actually run against rather than a stack nobody here has.
	info controlplane.Info

	workspaceID        string
	workspaceName      string
	projectID          string
	projectName        string
	secondWorkspaceID  string
	turns              []turn
	lastErr            error
	lastSecretResponse *quaycrewv1.SetSecretResponse
	lastSecrets        *quaycrewv1.ListSecretsResponse
	lastSkills         *quaycrewv1.ListSkillsResponse
}

type worldKey struct{}

func worldFrom(ctx context.Context) *world {
	w, _ := ctx.Value(worldKey{}).(*world)
	return w
}

// start builds a control plane with doubles behind it and serves it over an in memory listener, so
// the scenarios exercise the real gRPC path without a port.
func (w *world) start() error {
	dir, err := os.MkdirTemp("", "quaycrew-features-")
	if err != nil {
		return fmt.Errorf("conversation store for the scenario: %w", err)
	}
	w.storage = sandbox.Storage{Dir: dir, Host: dir}
	w.skillsDir = filepath.Join(dir, "skills")
	w.provider = &sandbox.FakeProvider{}
	w.runner = &recordingRunner{}
	w.secrets = secrets.NewMemory()
	w.store = store.NewMemory()
	w.events = messaging.NewMemory()
	w.info = controlplane.Info{Model: "fake", Sandbox: "fake", Store: "memory", Events: "memory"}
	// Every scenario runs against a crew that guards itself, the way a real one does, so the whole
	// suite proves the authenticated path and not a special unguarded one.
	w.token = "the-token-this-scenario-was-minted"
	w.driverToken = "the-driver-token-this-scenario-was-minted"
	return w.serve()
}

// eventLog is the log the control plane is built with. A scenario that unhooks the broker sets
// w.events to nil, and a typed nil pointer handed to an interface is not nil, so it is spelled out
// here rather than left to the caller to get right.
func (w *world) eventLog() messaging.EventLog {
	if w.events == nil {
		return nil
	}
	return w.events
}

// settled waits for every detached turn to land, so a scenario asserting on what a turn left behind
// is never asserting on a turn still running. The same wait the real shutdown does.
func (w *world) settled(ctx context.Context) error {
	waiting, giveUp := context.WithTimeout(ctx, 10*time.Second)
	defer giveUp()
	w.server.WaitForTurns(waiting)
	if waiting.Err() != nil {
		return fmt.Errorf("a detached turn never landed")
	}
	return nil
}

// restart tears the control plane down and stands a new one up over the same store, model and
// sandbox provider, which is what a process restart looks like from the outside. Anything the new
// instance can still see was in the store rather than in the old process.
func (w *world) restart() error {
	w.stop()
	return w.serve()
}

// serve stands up a control plane over the world's existing dependencies.
func (w *world) serve() error {
	listener := bufconn.Listen(1024 * 1024)
	w.listener = listener
	w.grpcServer = grpc.NewServer(auth.ServerOptions(w.token, w.driverToken, controlplane.DeniedToDriver)...)
	w.server = controlplane.NewServer(controlplane.Config{
		Store: w.store, Runner: w.turnRunner(), Provider: w.provider, Secrets: w.secrets,
		Storage: w.storage, Info: w.info, Events: w.eventLog(), Reachable: w.reachable,
		GitAuthor: w.gitAuthor, DriverToken: w.driverToken,
		Skills: w.skills, SkillsHost: w.skillsDir, SandboxImage: "quaycrew-sandbox:test",
	})
	// The way the real main starts: what strayed while the crew is down is reaped on the way up, and
	// a thread the store still calls running is settled, because its turn died with the last process.
	w.server.ReapStrays(context.Background())
	w.server.SettleTurns(context.Background())
	// The way the real main starts, for a scenario about what survives a restart: the shipped hooks
	// are offered again, and a crew that already holds some is left exactly as it is.
	if w.seedHooks {
		w.server.SeedHooks(context.Background(), "../hooks",
			slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	quaycrewv1.RegisterControlPlaneServiceServer(w.grpcServer, w.server)
	go func() { _ = w.grpcServer.Serve(listener) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(auth.Credentials(w.token)),
	)
	if err != nil {
		return fmt.Errorf("dial the control plane: %w", err)
	}
	w.conn = conn
	w.client = quaycrewv1.NewControlPlaneServiceClient(conn)
	return nil
}

// turnRunner is what runs a turn: the recording double, unless a scenario asked for the real one.
func (w *world) turnRunner() model.Runner {
	if w.realRunner != nil {
		return w.realRunner
	}
	return w.runner
}

func (w *world) stop() {
	if w.conn != nil {
		_ = w.conn.Close()
	}
	if w.grpcServer != nil {
		w.grpcServer.Stop()
	}
}

func (w *world) lastTurn() (turn, error) {
	if len(w.turns) == 0 {
		return turn{}, fmt.Errorf("no turn has been dispatched yet")
	}
	return w.turns[len(w.turns)-1], nil
}

// dispatch runs one turn and records either the result or the error, so a Then step can assert on
// whichever the scenario is about.
func (w *world) dispatch(ctx context.Context, project, thread, text string) error {
	resp, err := w.client.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project, Handle: thread, Text: text})
	w.lastErr = err
	if err != nil {
		return nil
	}
	w.turns = append(w.turns, turn{sessionID: resp.GetId(), threadID: resp.GetHandle(), reply: resp.GetReply()})
	return w.keepConversation(ctx, resp.GetId())
}

// keepConversation writes what the real model writes. The recording runner hands back a conversation
// id with nothing behind it, and the control plane now looks in the store before it offers to resume
// one, so the double has to keep what the thing it stands in for keeps.
func (w *world) keepConversation(ctx context.Context, sessionID string) error {
	resp, err := w.client.GetThread(ctx, &quaycrewv1.GetThreadRequest{Id: sessionID})
	if err != nil {
		return err
	}
	session := resp.GetThread()
	if session.GetModelSessionId() == "" {
		return nil
	}
	dir := w.conversationDir(session.GetWorkspace())
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return fmt.Errorf("seeding the conversation store: %w", err)
	}
	path := filepath.Join(dir, session.GetModelSessionId()+sandbox.ConversationFile)
	if err := os.WriteFile(path, []byte("{}\n"), 0o666); err != nil {
		return fmt.Errorf("writing the conversation: %w", err)
	}
	return nil
}

// conversationDir is where the model keeps a workspace's conversations, one directory per working
// directory, which is the same path in every sandbox.
// sandboxesMadeFor counts the sandboxes genuinely made for the current session, which is not the same
// as the times the provider was asked: adopting one that already exists makes nothing.
func (w *world) sandboxesMadeFor(want int) error {
	current, err := w.lastTurn()
	if err != nil {
		return err
	}
	var made int
	for _, created := range w.provider.Created {
		if created.ID == current.sessionID {
			made++
		}
	}
	if made != want {
		return fmt.Errorf("%d sandboxes were made for the session, want %d", made, want)
	}
	return nil
}

func (w *world) conversationDir(workspace string) string {
	return filepath.Join(w.storage.Dir, "workspaces", workspace, "claude", "projects", "-home-agent-workspace")
}

func (w *world) createWorkspace(ctx context.Context, name string) error {
	resp, err := w.client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: name})
	w.lastErr = err
	if err != nil {
		return nil
	}
	w.workspaceID = resp.GetWorkspace().GetId()
	w.workspaceName = resp.GetWorkspace().GetName()
	return nil
}

// createProject adds a body of work to the world's current workspace.
func (w *world) createProject(ctx context.Context, name string) error {
	resp, err := w.client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Workspace: w.workspaceID, Name: name})
	w.lastErr = err
	if err != nil {
		return nil
	}
	w.projectID = resp.GetProject().GetId()
	w.projectName = resp.GetProject().GetName()
	return nil
}

func initializeScenario(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		w := &world{}
		if err := w.start(); err != nil {
			return ctx, err
		}
		return context.WithValue(ctx, worldKey{}, w), nil
	})
	// The console keeps its steps in console_steps_test.go, next to its own feature file.
	initializeConsoleSteps(sc)
	initializeFlowSteps(sc)
	initializeFlowSurfaceSteps(sc)
	initializeFirstRunSteps(sc)
	initializeProjectSteps(sc)
	initializeAddressSteps(sc)
	initializeInfoSteps(sc)
	initializeEventsSteps(sc)
	initializeTurnsSteps(sc)
	initializeTurnsViewSteps(sc)
	initializeAttachSteps(sc)
	initializeContextSteps(sc)
	initializeSandboxEnvSteps(sc)
	initializeAuthSteps(sc)
	initializeWorkspaceSteps(sc)
	initializeWizardSteps(sc)
	initializeReachableSteps(sc)
	initializeDriverSteps(sc)
	initializeDriverContextSteps(sc)
	initializeUsageSteps(sc)
	initializeSkillSteps(sc)
	initializeSigningSteps(sc)
	initializeSecretFileSteps(sc)
	initializeGitConfigSteps(sc)
	initializeWizardModeSteps(sc)
	initializeDetachSteps(sc)
	initializeHookSteps(sc)
	initializeHookSandboxSteps(sc)
	initializeSeededHookSteps(sc)
	initializeHookVersionSteps(sc)
	initializeImportedSkillSteps(sc)
	initializeFailureSteps(sc)
	initializePanelSteps(sc)
	// Tear the control plane down. The scenario's own failure is already recorded, so this returns
	// nil rather than the incoming error, which would be reported a second time as a hook failure.
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		scenariosRun.Add(1)
		if w := worldFrom(ctx); w != nil {
			w.stop()
			// The conversation store outlives a restart, which is the point of it, so it is cleaned
			// up here rather than in stop.
			if w.storage.Dir != "" {
				_ = os.RemoveAll(w.storage.Dir)
			}
		}
		return ctx, nil
	})

	// Given.
	sc.Step(`^a running control plane$`, func(ctx context.Context) error {
		if worldFrom(ctx) == nil {
			return fmt.Errorf("the control plane was not started")
		}
		return nil
	})
	sc.Step(`^a second workspace named "([^"]*)"$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		resp, err := w.client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: name})
		if err != nil {
			return err
		}
		// createWorkspace would move the world's current workspace, and the background's workspace is the
		// one the other steps mean, so record this one separately.
		w.secondWorkspaceID = resp.GetWorkspace().GetId()
		return nil
	})
	sc.Step(`^a workspace named "([^"]*)"$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		if err := w.createWorkspace(ctx, name); err != nil {
			return err
		}
		if w.lastErr != nil {
			return fmt.Errorf("create the workspace: %w", w.lastErr)
		}
		return nil
	})
	sc.Step(`^a project named "([^"]*)"$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		if err := w.createProject(ctx, name); err != nil {
			return err
		}
		if w.lastErr != nil {
			return fmt.Errorf("create the project: %w", w.lastErr)
		}
		return nil
	})
	sc.Step(`^the workspace has the subscription token "([^"]*)"$`, func(ctx context.Context, token string) error {
		w := worldFrom(ctx)
		_, err := w.client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
			Workspace: w.workspaceID, Key: model.ClaudeCodeOAuthTokenEnv, Value: token,
		})
		return err
	})
	sc.Step(`^a session started by dispatching "([^"]*)"$`, func(ctx context.Context, text string) error {
		w := worldFrom(ctx)
		if err := w.dispatch(ctx, w.projectID, "", text); err != nil {
			return err
		}
		return w.lastErr
	})

	// When: sessions.
	sc.Step(`^the operator dispatches "([^"]*)" to the project$`, func(ctx context.Context, text string) error {
		w := worldFrom(ctx)
		return w.dispatch(ctx, w.projectID, "", text)
	})
	sc.Step(`^the operator dispatches "([^"]*)" to the same thread$`, func(ctx context.Context, text string) error {
		w := worldFrom(ctx)
		previous, err := w.lastTurn()
		if err != nil {
			return err
		}
		return w.dispatch(ctx, w.projectID, previous.threadID, text)
	})
	sc.Step(`^the operator dispatches "([^"]*)" to a new thread$`, func(ctx context.Context, text string) error {
		w := worldFrom(ctx)
		return w.dispatch(ctx, w.projectID, "", text)
	})
	sc.Step(`^the operator dispatches "([^"]*)" to project "([^"]*)"$`, func(ctx context.Context, text, workspace string) error {
		return worldFrom(ctx).dispatch(ctx, workspace, "", text)
	})
	sc.Step(`^the control plane restarts$`, func(ctx context.Context) error {
		return worldFrom(ctx).restart()
	})
	sc.Step(`^the operator stops the session$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTurn()
		if err != nil {
			return err
		}
		_, w.lastErr = w.client.StopThread(ctx, &quaycrewv1.StopThreadRequest{Id: current.sessionID})
		return w.lastErr
	})
	// A refusal is the point of two of these scenarios, so the error is recorded rather than returned.
	sc.Step(`^the operator restarts the session$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTurn()
		if err != nil {
			return err
		}
		_, w.lastErr = w.client.RestartThread(ctx, &quaycrewv1.RestartThreadRequest{Id: current.sessionID})
		return nil
	})
	sc.Step(`^the thread is set to permission mode "([^"]*)"$`, func(ctx context.Context, mode string) error {
		w := worldFrom(ctx)
		current, err := w.lastTurn()
		if err != nil {
			return err
		}
		_, w.lastErr = w.client.SetThreadPermissionMode(ctx,
			&quaycrewv1.SetThreadPermissionModeRequest{Id: current.sessionID, Mode: mode})
		return nil
	})
	sc.Step(`^the turn ran in permission mode "([^"]*)"$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		got := w.runner.lastRequest().PermissionMode
		if got != want {
			return fmt.Errorf("the turn ran as %q, want %q", got, want)
		}
		return nil
	})
	sc.Step(`^the operator archives the session$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTurn()
		if err != nil {
			return err
		}
		_, w.lastErr = w.client.ArchiveThread(ctx, &quaycrewv1.ArchiveThreadRequest{Id: current.sessionID})
		return nil
	})
	sc.Step(`^the operator restores the session$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTurn()
		if err != nil {
			return err
		}
		_, w.lastErr = w.client.RestoreThread(ctx, &quaycrewv1.RestoreThreadRequest{Id: current.sessionID})
		return nil
	})
	sc.Step(`^the operator restarts a session that does not exist$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.RestartThread(ctx, &quaycrewv1.RestartThreadRequest{Id: "ghost"})
		return nil
	})
	// Restarting starts the container straight away, which is the whole difference between it and
	// simply marking a row idle: a second sandbox for the same session is the evidence.
	sc.Step(`^a second sandbox has been created for that session$`, func(ctx context.Context) error {
		return worldFrom(ctx).sandboxesMadeFor(2)
	})

	// The control plane must ask the provider rather than answer from the database row. Whether that
	// starts a container or adopts one already carrying the name is the provider's business; what
	// matters here is that nobody hands out a container name without asking whether it is there.
	sc.Step(`^the control plane asked for that session's sandbox$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTurn()
		if err != nil {
			return err
		}
		for _, call := range w.provider.Calls {
			if call.ID == current.sessionID {
				return nil
			}
		}
		return fmt.Errorf("the sandbox provider was never asked about session %s", current.sessionID)
	})

	// When: workspaces.
	sc.Step(`^the operator creates a workspace named "([^"]*)"$`, func(ctx context.Context, name string) error {
		return worldFrom(ctx).createWorkspace(ctx, name)
	})
	sc.Step(`^the operator fetches the workspace "([^"]*)"$`, func(ctx context.Context, id string) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.GetWorkspace(ctx, &quaycrewv1.GetWorkspaceRequest{Id: id})
		return nil
	})
	sc.Step(`^the operator deletes the workspace$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.DeleteWorkspace(ctx, &quaycrewv1.DeleteWorkspaceRequest{Id: w.workspaceID})
		return w.lastErr
	})
	sc.Step(`^the operator deletes the project$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.DeleteProject(ctx, &quaycrewv1.DeleteProjectRequest{Id: w.projectID})
		return w.lastErr
	})
	sc.Step(`^every sandbox the crew made is closed$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.provider.Boxes) == 0 {
			return fmt.Errorf("no sandbox was ever created, so this scenario is not testing the close")
		}
		for i, box := range w.provider.Boxes {
			if !box.Closed {
				return fmt.Errorf("sandbox %d is still open", i)
			}
		}
		return nil
	})
	sc.Step(`^the session's row says stopped while its container still runs$`,
		func(ctx context.Context) error {
			w := worldFrom(ctx)
			current, err := w.lastTurn()
			if err != nil {
				return err
			}
			// Straight into the store, past the control plane, which is what the historical leak
			// looks like: the row and the daemon disagreeing.
			return w.store.StopSession(ctx, current.sessionID)
		})
	sc.Step(`^every command run in a sandbox fails$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.provider.ExitErr = fmt.Errorf("exit status 1")
		return nil
	})
	sc.Step(`^the operator attaches a "([^"]*)" channel called "([^"]*)" to workspace "([^"]*)"$`,
		func(ctx context.Context, kind, id, workspace string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.AttachChannel(ctx, &quaycrewv1.AttachChannelRequest{Workspace: workspace, Id: id, Kind: kind})
			return nil
		})
	sc.Step(`^the operator sets the secret "([^"]*)" to "([^"]*)"$`, func(ctx context.Context, key, value string) error {
		w := worldFrom(ctx)
		w.lastSecretResponse, w.lastErr = w.client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
			Workspace: w.workspaceID, Key: key, Value: value,
		})
		return w.lastErr
	})
	sc.Step(`^the operator sets the secret "([^"]*)" to "([^"]*)" on workspace "([^"]*)"$`,
		func(ctx context.Context, key, value, workspace string) error {
			w := worldFrom(ctx)
			w.lastSecretResponse, w.lastErr = w.client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
				Workspace: workspace, Key: key, Value: value,
			})
			return nil
		})

	// Then: turns and replies.
	sc.Step(`^the reply is "([^"]*)"$`, func(ctx context.Context, want string) error {
		current, err := worldFrom(ctx).lastTurn()
		if err != nil {
			return err
		}
		if current.reply != want {
			return fmt.Errorf("reply is %q, want %q", current.reply, want)
		}
		return nil
	})
	sc.Step(`^both turns ran in the same session$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.turns) != 2 {
			return fmt.Errorf("expected 2 turns, got %d", len(w.turns))
		}
		if w.turns[0].sessionID != w.turns[1].sessionID {
			return fmt.Errorf("turns ran in different sessions: %q and %q", w.turns[0].sessionID, w.turns[1].sessionID)
		}
		return nil
	})
	sc.Step(`^the turns ran in different sessions$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.turns) != 2 {
			return fmt.Errorf("expected 2 turns, got %d", len(w.turns))
		}
		if w.turns[0].sessionID == w.turns[1].sessionID {
			return fmt.Errorf("both turns ran in session %q, expected different sessions", w.turns[0].sessionID)
		}
		return nil
	})
	sc.Step(`^the second turn resumed the conversation the first turn started$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.runner.count() != 2 {
			return fmt.Errorf("expected the runner to have run 2 turns, got %d", w.runner.count())
		}
		first, _ := w.runner.turn(0)
		second, _ := w.runner.turn(1)
		if first.ModelSessionID != "" {
			return fmt.Errorf("the first turn asked to resume %q, expected it to start a new conversation", first.ModelSessionID)
		}
		if second.ModelSessionID != "conversation-1" {
			return fmt.Errorf("the second turn resumed %q, want the first turn's conversation-1", second.ModelSessionID)
		}
		return nil
	})

	// Then: sandboxes and sessions.
	sc.Step(`^(\d+) sandboxe?s? (?:has|have) been created$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		if got := len(w.provider.Created); got != want {
			return fmt.Errorf("%d sandboxes were created (%v), want %d", got, w.provider.Created, want)
		}
		return nil
	})
	sc.Step(`^the sandbox belongs to the session$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTurn()
		if err != nil {
			return err
		}
		if len(w.provider.Created) == 0 {
			return fmt.Errorf("no sandbox was created")
		}
		if w.provider.Created[0].ID != current.sessionID {
			return fmt.Errorf("sandbox was created for %q, want the session %q", w.provider.Created[0].ID, current.sessionID)
		}
		return nil
	})
	sc.Step(`^the sandbox was created for the session's project and workspace$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.provider.Created) == 0 {
			return fmt.Errorf("no sandbox was created")
		}
		created := w.provider.Created[0]
		if created.Workspace != w.workspaceID {
			return fmt.Errorf("the sandbox was created for workspace %q, want %q", created.Workspace, w.workspaceID)
		}
		if created.Project != w.projectID {
			return fmt.Errorf("the sandbox was created for project %q, want %q", created.Project, w.projectID)
		}
		return nil
	})
	sc.Step(`^the session still holds the conversation the first turn started$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTurn()
		if err != nil {
			return err
		}
		resp, err := w.client.GetThread(ctx, &quaycrewv1.GetThreadRequest{Id: current.sessionID})
		if err != nil {
			return err
		}
		// The handle points at a conversation the model keeps on its own disk. Lose it and that
		// conversation still exists but can never be reached again.
		if got := resp.GetThread().GetModelSessionId(); got != "conversation-1" {
			return fmt.Errorf("the session holds conversation %q, want conversation-1", got)
		}
		return nil
	})
	sc.Step(`^the workspace has (\d+) sessions$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		resp, err := w.client.ListThreads(ctx, &quaycrewv1.ListThreadsRequest{Workspace: w.workspaceID})
		if err != nil {
			return err
		}
		if got := len(resp.GetThreads()); got != want {
			return fmt.Errorf("the workspace has %d sessions, want %d", got, want)
		}
		return nil
	})
	sc.Step(`^the workspace has (\d+) archived sessions$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		resp, err := w.client.ListThreads(ctx, &quaycrewv1.ListThreadsRequest{
			Workspace: w.workspaceID, Archived: true,
		})
		if err != nil {
			return err
		}
		if got := len(resp.GetThreads()); got != want {
			return fmt.Errorf("the workspace has %d archived sessions, want %d", got, want)
		}
		return nil
	})
	sc.Step(`^the session is reported as (\w+)$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		current, err := w.lastTurn()
		if err != nil {
			return err
		}
		resp, err := w.client.GetThread(ctx, &quaycrewv1.GetThreadRequest{Id: current.sessionID})
		if err != nil {
			return err
		}
		if got := resp.GetThread().GetStatus(); got != want {
			return fmt.Errorf("session status is %q, want %q", got, want)
		}
		return nil
	})
	sc.Step(`^the session's sandbox has been closed$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.provider.Boxes) == 0 {
			return fmt.Errorf("no sandbox was created")
		}
		if !w.provider.Boxes[0].Closed {
			return fmt.Errorf("the session's sandbox is still open")
		}
		return nil
	})

	// Then: the turn's environment.
	sc.Step(`^the turn ran with the subscription token "([^"]*)"$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		last, ok := w.runner.turn(w.runner.count() - 1)
		if !ok {
			return fmt.Errorf("no turn reached the model runner")
		}
		if got := last.Env[model.ClaudeCodeOAuthTokenEnv]; got != want {
			return fmt.Errorf("the turn ran with %s=%q, want %q", model.ClaudeCodeOAuthTokenEnv, got, want)
		}
		return nil
	})
	sc.Step(`^the turn ran with no extra environment$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		last, ok := w.runner.turn(w.runner.count() - 1)
		if !ok {
			return fmt.Errorf("no turn reached the model runner")
		}
		if len(last.Env) != 0 {
			return fmt.Errorf("the turn ran with %v, want no extra environment", last.Env)
		}
		return nil
	})

	// Then: workspaces.
	sc.Step(`^the workspace is listed$`, func(ctx context.Context) error {
		return worldFrom(ctx).workspaceIsListed(ctx, true)
	})
	sc.Step(`^the workspace is no longer listed$`, func(ctx context.Context) error {
		return worldFrom(ctx).workspaceIsListed(ctx, false)
	})
	sc.Step(`^the workspace can be fetched by its id$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		resp, err := w.client.GetWorkspace(ctx, &quaycrewv1.GetWorkspaceRequest{Id: w.workspaceID})
		if err != nil {
			return err
		}
		if resp.GetWorkspace().GetName() != w.workspaceName {
			return fmt.Errorf("fetched workspace is named %q, want %q", resp.GetWorkspace().GetName(), w.workspaceName)
		}
		return nil
	})
	sc.Step(`^the secrets backend holds "([^"]*)" for that workspace$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		got, err := w.secrets.Get(ctx, w.workspaceID, model.ClaudeCodeOAuthTokenEnv)
		if err != nil {
			return fmt.Errorf("read the secret back: %w", err)
		}
		if got != want {
			return fmt.Errorf("the secrets backend holds %q, want %q", got, want)
		}
		return nil
	})
	sc.Step(`^the response carries no secret value$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastSecretResponse == nil {
			return fmt.Errorf("no response was recorded")
		}
		if size := proto.Size(w.lastSecretResponse); size != 0 {
			return fmt.Errorf("the response carries %d bytes, want an empty response that cannot leak the value", size)
		}
		return nil
	})

	// Then: refusals.
	sc.Step(`^the refusal suggests "([^"]*)"$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("nothing was refused")
		}
		// A refusal that only says no leaves the operator guessing. It has to name what would work.
		if !strings.Contains(w.lastErr.Error(), want) {
			return fmt.Errorf("the refusal is %q, want it to suggest %q", w.lastErr.Error(), want)
		}
		return nil
	})
	sc.Step(`^the control plane refuses it as not found$`, func(ctx context.Context) error {
		return refused(worldFrom(ctx), codes.NotFound)
	})
	sc.Step(`^the control plane refuses it as invalid$`, func(ctx context.Context) error {
		return refused(worldFrom(ctx), codes.InvalidArgument)
	})
	sc.Step(`^the control plane refuses it as the wrong state$`, func(ctx context.Context) error {
		return refused(worldFrom(ctx), codes.FailedPrecondition)
	})
}

func (w *world) workspaceIsListed(ctx context.Context, want bool) error {
	resp, err := w.client.ListWorkspaces(ctx, &quaycrewv1.ListWorkspacesRequest{})
	if err != nil {
		return err
	}
	for _, workspace := range resp.GetWorkspaces() {
		if workspace.GetId() == w.workspaceID {
			if !want {
				return fmt.Errorf("workspace %q is still listed", w.workspaceID)
			}
			return nil
		}
	}
	if want {
		return fmt.Errorf("workspace %q is not listed", w.workspaceID)
	}
	return nil
}

func refused(w *world, want codes.Code) error {
	if w.lastErr == nil {
		return fmt.Errorf("the control plane accepted it, expected %s", want)
	}
	if got := status.Code(w.lastErr); got != want {
		return fmt.Errorf("the control plane refused it as %s, want %s", got, want)
	}
	return nil
}
