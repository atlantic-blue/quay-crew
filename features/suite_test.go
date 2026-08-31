// Package features holds the executable specification of Quay System.
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
// lifecycle and error handling, and they deliberately do not prove that a real task executes. That
// is the job of the dispatch smoke in continuous integration, which boots the composed stack and
// runs a task for real, and of the gated test in internal/model that needs a live subscription.
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

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/auth"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/flow"
	"github.com/atlantic-blue/krewe/internal/headroom"
	"github.com/atlantic-blue/krewe/internal/messaging"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/secrets"
	"github.com/atlantic-blue/krewe/internal/session"
	"github.com/atlantic-blue/krewe/internal/skill"
	"github.com/atlantic-blue/krewe/internal/store"
	"github.com/atlantic-blue/krewe/internal/telemetry"
	"github.com/cucumber/godog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
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

// recordingRunner is a model runner double that records every task it was asked to run and hands
// back a distinct conversation id each time, so a scenario can assert which conversation the next
// task resumed.
type recordingRunner struct {
	mu       sync.Mutex
	requests []model.Request
	// failNext makes the next task fail, which is how a scenario gets a session that exists but has
	// no conversation behind it.
	failNext bool
	// takes is how long a task pretends to take. Zero is instant, which is right for almost every
	// scenario and wrong for any scenario about something happening while a task is under way:
	// with an instant model a whole automation finishes before the next step runs, and a scenario
	// about stopping one would be racing rather than specifying.
	takes time.Duration
	// gate holds every task open until it is closed, which is takes without the guesswork: a scenario
	// about what is true *while* a task runs cannot be written against a clock, because the clock is
	// a different length on every machine. Nil runs straight through.
	gate chan struct{}
	// started is closed when the first task begins, so a scenario can know a task is genuinely under
	// way rather than infer it from how long a step took.
	started chan struct{}
	once    sync.Once
	// usage, cost and usageReported are what each task reports having spent. The zero value reports
	// nothing, which is what a backend that does not say looks like.
	usage         sandbox.Usage
	cost          float64
	usageReported bool
	// onTask runs before the double answers, so a scenario can be a model that did the job rather
	// than one that talked about it: wrote the file, left the room as it found it. Nil does nothing.
	onTask func()
	// says is what the double answers, one entry per task, with the last repeating once the queue
	// runs out. Empty echoes the task, which is what almost every scenario wants.
	//
	// A queue rather than one string, because a scenario about a session being asked a second thing
	// has to be able to say what it answers the second time. The last one repeats rather than the
	// queue running dry, so a scenario that means "and it keeps saying that" says it once.
	says []string
	// answers is a phrase against what the double answers a task carrying it, tried before the queue
	// above and first match winning.
	//
	// It exists because more than one conversation can be in flight at once: a job held back until a
	// reviewer and a tester have read its work has three, and a queue by position would make a
	// scenario about the gate into a scenario about the order the system happens to ask in.
	answers [][2]string
}

// failTheNextTask makes the next task the model is asked to run fail. Under the lock, because a
// scenario sets it while a task is already waiting inside the double.
func (r *recordingRunner) failTheNextTask() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failNext = true
}

// willSay adds one answer to the queue, so a scenario builds up what a model says over several tasks.
func (r *recordingRunner) willSay(answer string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.says = append(r.says, answer)
}

// willAnswer says what the double answers a task carrying a phrase, whenever that task arrives.
func (r *recordingRunner) willAnswer(whenAsked, answer string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.answers = append(r.answers, [2]string{whenAsked, answer})
}

// answerFor is what the double says to the nth task, one indexed. The caller holds the lock.
func (r *recordingRunner) answerFor(asked int, text string) string {
	// What was asked wins over how many have been asked, because a scenario naming a phrase is being
	// specific and a queue by position is not.
	for _, pair := range r.answers {
		if strings.Contains(text, pair[0]) {
			return pair[1]
		}
	}
	if len(r.says) == 0 {
		return "you said: " + text
	}
	if asked > len(r.says) {
		asked = len(r.says)
	}
	return r.says[asked-1]
}

// hold makes every task wait, and returns the func that lets them go.
func (r *recordingRunner) hold() func() {
	r.mu.Lock()
	r.gate, r.started = make(chan struct{}), make(chan struct{})
	gate := r.gate
	r.mu.Unlock()
	return func() { close(gate) }
}

// waitForTask blocks until a task has reached the runner, so a scenario never asserts on a session
// whose task has not started yet.
func (r *recordingRunner) waitForTask() error {
	r.mu.Lock()
	started := r.started
	r.mu.Unlock()
	if started == nil {
		return fmt.Errorf("no task was held, so there is nothing to wait for")
	}
	select {
	case <-started:
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("no task reached the model runner")
	}
}

var _ model.Runner = (*recordingRunner)(nil)

// Run answers what the double was told to answer, and gives up the moment its context is cancelled.
//
// The cancellation matters as much as the answer. The real runner runs the model as a process under
// this context, so cancelling it ends the task, which is exactly what stopping one session does. A
// double that blocked on regardless would be looser than the thing it stands in for, and a scenario
// about stopping a task would hang against it while the real system stopped the task at once.
func (r *recordingRunner) Run(ctx context.Context, _ sandbox.Sandbox, req model.Request) (model.Response, error) {
	r.mu.Lock()
	takes, gate, started, onTask := r.takes, r.gate, r.started, r.onTask
	// Recorded on arrival rather than on the way out. A scenario about what is true *while* a task
	// runs, which is what attaching to a running session is, cannot read a task that is only written
	// down once it is over.
	r.requests = append(r.requests, req)
	asked := len(r.requests)
	r.mu.Unlock()
	// Outside the lock: what the model does may ask the system something.
	if onTask != nil {
		onTask()
	}
	if started != nil {
		r.once.Do(func() { close(started) })
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return model.Response{}, ctx.Err()
		}
	}
	if takes > 0 {
		select {
		case <-time.After(takes):
		case <-ctx.Done():
			return model.Response{}, ctx.Err()
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failNext {
		r.failNext = false
		return model.Response{}, fmt.Errorf("the model refused this task")
	}
	return model.Response{
		Reply: r.answerFor(asked, req.Text),
		// The conversation it was given comes back, which is what a runtime that honours the name does.
		// A double that answered with a name of its own would be looser than the thing it stands in for:
		// the system would read every task as the runtime ignoring the name it was handed.
		ModelSessionID: conversationOf(req, fmt.Sprintf("conversation-%d", asked)),
		Usage:          r.usage,
		CostUSD:        r.cost,
		UsageReported:  r.usageReported,
	}, nil
}

func (r *recordingRunner) task(i int) (model.Request, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i < 0 || i >= len(r.requests) {
		return model.Request{}, false
	}
	return r.requests[i], true
}

// lastRequest is the task the model was asked to run most recently, which is what a scenario about
// what a task ran as has to look at.
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

// task is what the operator saw come back from one dispatch.
type task struct {
	sessionID string
	handle    string
	reply     string
}

// world is one scenario's state. A fresh one is built before each scenario, so scenarios never leak
// into each other.
type world struct {
	grpcServer *grpc.Server
	// listener is kept so a scenario can dial the same system as a different caller, presenting
	// something other than what the world's own client presents.
	listener *bufconn.Listener
	// token is the system's token, which every caller has to present to be served.
	token string
	// clockAhead is how far ahead of the real clock this system reads a credential's life. It is how a
	// scenario about what a session still holds half an hour into its job runs in a millisecond.
	// Nothing else in the system reads it, so the only thing it moves is the passage of time.
	clockAhead atomic.Int64
	// driverToken is the driver's own token: recognised, and refused the calls that grant capability.
	driverToken string
	conn        *grpc.ClientConn
	client      quaycrewv1.ControlPlaneServiceClient
	// health is the same interface a container health check asks, and lastHealth what it last said.
	health     grpc_health_v1.HealthClient
	lastHealth grpc_health_v1.HealthCheckResponse_ServingStatus
	provider   *sandbox.FakeProvider
	runner     *recordingRunner
	// realRunner replaces the recording double when a scenario is about what the real one does with
	// what came out of the sandbox. A double that hands back a canned error cannot say anything about
	// an explanation built from a stream.
	realRunner model.Runner
	// reachable is the address a session is told to dial for the system, empty when it cannot reach it.
	reachable string
	// gitAuthor is who a commit made inside a sandbox is by.
	gitAuthor controlplane.Identity
	// flowRun is the run the last flow step started.
	flowRun flow.Run
	// trigger is the last trigger something raised, so a Then step can read what became of it.
	trigger flow.Trigger
	// flowRunID is the run the operator surface steps started, and driverErr what the driver was
	// told when it tried something.
	flowRunID string
	driverErr error
	// scratch is every directory a scenario wrote on disk, removed when the scenario ends.
	scratch []string
	// server is the control plane itself, kept so a scenario can drive what main does at startup
	// rather than only what a client can call.
	server *controlplane.Server
	// lastStop is what the last stop of one session came back with, and lastStopReason what the
	// operator said, so a Then step can hold the record to their own words.
	lastStop       *quaycrewv1.StopTaskResponse
	lastStopReason string
	// lastDrain is what the last drain put down, kept so a scenario can ask what went rather than
	// counting sandboxes.
	lastDrain *quaycrewv1.DrainSessionsResponse
	// release lets go of a task a scenario is holding open, and is nil when none is held.
	release func()
	// waited carries what a dispatch the scenario is not watching came back with. A waited dispatch
	// does not return until its task lands, so a scenario about what is true while one runs has to
	// start it behind itself and pick the answer up afterwards.
	waited chan waitedDispatch
	// otherWorkspaceID is a second workspace, for the scenarios about what one workspace's
	// attachment does and does not reach.
	otherWorkspaceID string
	// seedHooks says this scenario is a system that seeds the shipped hooks, so a restart seeds again
	// the way the real main does.
	seedHooks bool
	// skillsDir is where the scenario's skills are written, and skills is what was read from it.
	skillsDir string
	skills    []skill.Skill
	// drivers are the sessions returned by opening the system, so a scenario can say it was the same one.
	drivers []*quaycrewv1.Session
	secrets secrets.Store
	store   store.Store
	// events is the log the control plane publishes tasks to. A scenario asserts on what landed on
	// it. Setting it to nil is how a scenario says the stack has no broker configured.
	events *messaging.Memory
	// eventsRefuse makes the log refuse every record it is given, for the scenarios about what the
	// system says when an export fails.
	eventsRefuse bool
	// eventsStall makes the log take a record and never answer, which is the broker that held a
	// whole system's dispatches inside the call. Refusing and never answering are different faults and
	// only one of them was survivable.
	eventsStall bool
	// storeStalls makes every health probe wait without answering, for the scenarios about a system
	// that reads well and cannot write.
	storeStalls bool
	// machine is what the system reads of the machine it runs on. Nil is a system with no daemon to ask,
	// which reports unknown. A scenario sets it and then asks the system to read it once, because a
	// scenario that waited for the sampler's own timer would be a scenario with a clock in it.
	machine headroom.Source
	// startWait and exportWait are the system's budgets. A scenario about a budget running out sets
	// them short, because a scenario that waits the real minute out is a scenario nobody runs.
	startWait  time.Duration
	exportWait time.Duration
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
	tasks              []task
	lastErr            error
	lastSecretResponse *quaycrewv1.SetSecretResponse
	lastSecrets        *quaycrewv1.ListSecretsResponse
	lastSkills         *quaycrewv1.ListSkillsResponse
	lastRoles          *quaycrewv1.ListRolesResponse
	lastRole           *quaycrewv1.GetRoleResponse
	// mergeGate is what the shipped merge gate answered the last time a scenario fired it.
	mergeGate gateAnswer
	// deployGate is what the shipped deploy identity gate answered the last time a scenario fired it.
	deployGate gateAnswer
	// change is the repository a scenario built to stand for the change a session is opening a pull
	// request for, because the gate reads the change rather than being told about it.
	change string
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
	// Every scenario runs against a system that guards itself, the way a real one does, so the whole
	// suite proves the authenticated path and not a special unguarded one.
	w.token = "the-token-this-scenario-was-minted"
	w.driverToken = "the-driver-token-this-scenario-was-minted"
	return w.serve()
}

// eventLog is the log the control plane is built with. A scenario that unhooks the broker sets
// w.events to nil, and a typed nil pointer handed to an interface is not nil, so it is spelled out
// here rather than left to the caller to get right.
func (w *world) eventLog() messaging.EventLog {
	if w.eventsRefuse {
		return refusingEventLog{}
	}
	if w.eventsStall {
		return stallingEventLog{}
	}
	if w.events == nil {
		return nil
	}
	return w.events
}

// settled waits for every detached task to land, so a scenario asserting on what a task left behind
// is never asserting on a task still running. The same wait the real shutdown does.
func (w *world) settled(ctx context.Context) error {
	waiting, giveUp := context.WithTimeout(ctx, 10*time.Second)
	defer giveUp()
	w.server.WaitForTasks(waiting)
	if waiting.Err() != nil {
		return fmt.Errorf("a detached task never landed")
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
	// The server first, because the interceptors ask it to recognise the credentials it has minted
	// for jobs.
	w.server = controlplane.NewServer(controlplane.Config{
		Store: w.systemStore(), Runner: w.taskRunner(), Provider: w.provider, Secrets: w.secrets,
		Storage: w.storage, Info: w.info, Events: w.eventLog(), Reachable: w.reachable,
		GitAuthor: w.gitAuthor, DriverToken: w.driverToken,
		Skills: w.skills, SkillsHost: w.skillsDir, SandboxImage: "quaycrew-sandbox:test",
		StartWait: w.startWait, ExportWait: w.exportWait,
		Headroom: w.machine, HeadroomEvery: time.Hour,
	})
	// The same options the real main builds the server with, so a scenario about tracing is about
	// what the system does and not about what the harness added.
	w.grpcServer = grpc.NewServer(append(
		telemetry.ServerOptions(),
		auth.ServerOptions(auth.Policy{
			Token: w.token, DriverToken: w.driverToken, Denied: controlplane.DeniedToDriver,
			// The scenarios run against a system that guards itself the way a real one does, job
			// credentials included.
			Grants: w.server.Grants(), DeniedToJob: controlplane.DeniedToJob,
			Now: func() time.Time { return time.Now().Add(time.Duration(w.clockAhead.Load())) },
		})...,
	)...)
	// The way the real main starts: what strayed while the system is down is reaped on the way up, and
	// a session the store still calls running is settled, because its task died with the last process.
	w.server.ReapStrays(context.Background())
	w.server.SettleTasks(context.Background())
	// The way the real main starts, for a scenario about what survives a restart: the shipped hooks
	// are offered again, and a system that already holds some is left exactly as it is.
	if w.seedHooks {
		w.server.SeedHooks(context.Background(), "../hooks",
			slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	quaycrewv1.RegisterControlPlaneServiceServer(w.grpcServer, w.server)
	// The same registration the real main makes, so a scenario asks the system the question a container
	// health check asks it.
	grpc_health_v1.RegisterHealthServer(w.grpcServer, controlplane.NewHealth(w.server))
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
	w.health = grpc_health_v1.NewHealthClient(conn)
	return nil
}

// systemStore is the store the control plane is built over, wrapped when a scenario is about a system
// whose writes do not land.
func (w *world) systemStore() store.Store {
	if w.storeStalls {
		return stallingStore{Store: w.store}
	}
	return w.store
}

// taskRunner is what runs a task: the recording double, unless a scenario asked for the real one.
func (w *world) taskRunner() model.Runner {
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

func (w *world) lastTask() (task, error) {
	if len(w.tasks) == 0 {
		return task{}, fmt.Errorf("no task has been dispatched yet")
	}
	return w.tasks[len(w.tasks)-1], nil
}

// orderedSessions is the workspace's listing as the system hands it over, with the two things an
// ordering case needs guarded: at least two sessions to put in an order, and stamps that are actually
// apart. Two sessions sharing a moment are ordered by their identifiers, which would leave an
// ordering case passing on whichever identifier the system minted first.
func (w *world) orderedSessions(ctx context.Context, archived bool) ([]*quaycrewv1.Session, error) {
	resp, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{
		Workspace: w.workspaceID, Archived: archived,
	})
	if err != nil {
		return nil, err
	}
	listed := resp.GetSessions()
	if len(listed) < 2 {
		return nil, fmt.Errorf("the listing holds %d sessions, and an order needs two", len(listed))
	}
	first, second := session.LastMoved(listed[0]).AsTime(), session.LastMoved(listed[1]).AsTime()
	if first.Equal(second) {
		return nil, fmt.Errorf("the first two sessions share a moment, so the identifier decided this order")
	}
	return listed, nil
}

// conversationOfFirstTask is the conversation the session's first task ran in. The system names a
// conversation before the task starts and the name is a fresh identifier each time, so a scenario
// reads it back from the task rather than expecting a name it could write down.
func (w *world) conversationOfFirstTask() (string, error) {
	first, found := w.runner.task(0)
	if !found {
		return "", fmt.Errorf("no task has reached the model runner, so no conversation was named")
	}
	if first.ModelSessionID == "" {
		return "", fmt.Errorf("the first task ran in no conversation the system could name")
	}
	return first.ModelSessionID, nil
}

// conversationOf is what a model runtime that honours the name it was given reports, falling back to
// a name of its own when it was given none.
func conversationOf(req model.Request, fallback string) string {
	if req.ModelSessionID != "" {
		return req.ModelSessionID
	}
	return fallback
}

// dispatch runs one task and records either the result or the error, so a Then step can assert on
// whichever the scenario is about.
func (w *world) dispatch(ctx context.Context, project, session, text string) error {
	resp, err := w.client.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project, Handle: session, Text: text})
	w.lastErr = err
	if err != nil {
		return nil
	}
	w.tasks = append(w.tasks, task{sessionID: resp.GetId(), handle: resp.GetHandle(), reply: resp.GetReply()})
	return w.keepConversation(ctx, resp.GetId())
}

// keepConversation writes what the real model writes. The recording runner hands back a conversation
// id with nothing behind it, and the control plane now looks in the store before it offers to resume
// one, so the double has to keep what the thing it stands in for keeps.
func (w *world) keepConversation(ctx context.Context, sessionID string) error {
	resp, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: sessionID})
	if err != nil {
		return err
	}
	session := resp.GetSession()
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
	current, err := w.lastTask()
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
	initializeStatsSteps(sc)
	initializeWhatTheSystemDoesSteps(sc)
	initializeConsoleJobsSteps(sc)
	initializeKeysSteps(sc)
	initializeWebSteps(sc)
	initializeFlowSteps(sc)
	initializeFlowSurfaceSteps(sc)
	initializePullRequestReviewSteps(sc)
	initializeFirstRunSteps(sc)
	initializeInstallSteps(sc)
	initializeFrontDoorSteps(sc)
	initializeProjectSteps(sc)
	initializeProjectRepositorySteps(sc)
	initializeDeployTargetSteps(sc)
	initializeAddressSteps(sc)
	initializeInfoSteps(sc)
	initializeEventsSteps(sc)
	initializeSessionEventsSteps(sc)
	initializeObservabilitySteps(sc)
	initializeMetricsSteps(sc)
	initializeTasksSteps(sc)
	initializeToolSteps(sc)
	initializeAnswerSteps(sc)
	initializeTaskWordSteps(sc)
	initializeJobWordSteps(sc)
	initializeHistorySteps(sc)
	initializeLevelWordSteps(sc)
	initializeVersionSteps(sc)
	initializeJobSteps(sc)
	initializeJobMaterialSteps(sc)
	initializeJobControllerSteps(sc)
	initializeJobRepositorySteps(sc)
	initializeJobWaitingSteps(sc)
	initializeFlowStepSteps(sc)
	initializeJobRoleSteps(sc)
	initializeTriggerSteps(sc)
	initializeLifecycleSteps(sc)
	initializeJobEventsSteps(sc)
	initializeJobLeaseSteps(sc)
	initializeAskingSteps(sc)
	initializeResumingSteps(sc)
	initializeSettlingSteps(sc)
	initializeCapabilitySteps(sc)
	initializeProductSteps(sc)
	initializeSteersSteps(sc)
	initializeTasksViewSteps(sc)
	initializeAttachSteps(sc)
	initializeContextSteps(sc)
	initializeContextSizeSteps(sc)
	initializeSandboxEnvSteps(sc)
	initializeAuthSteps(sc)
	initializeWorkspaceSteps(sc)
	initializeWizardSteps(sc)
	initializeReachableSteps(sc)
	initializeDriverSteps(sc)
	initializeDriverContextSteps(sc)
	initializeUsageSteps(sc)
	initializeSkillSteps(sc)
	initializeDeployIdentitySteps(sc)
	initializeDeployIdentityGateSteps(sc)
	initializeSigningSteps(sc)
	initializeSecretFileSteps(sc)
	initializeSystemSecretSteps(sc)
	initializeGitConfigSteps(sc)
	initializeWizardModeSteps(sc)
	initializeDetachSteps(sc)
	initializeDispatchingSteps(sc)
	initializeWaitsSteps(sc)
	initializeDegradedSteps(sc)
	initializeHeadroomSteps(sc)
	initializeRoomViewSteps(sc)
	initializeAdmissionSteps(sc)
	initializeWorkingSteps(sc)
	initializeDrainSteps(sc)
	initializeHookSteps(sc)
	initializeHookSandboxSteps(sc)
	initializeSeededHookSteps(sc)
	initializeMergeGateSteps(sc)
	initializeRoleSteps(sc)
	initializeShippedRoleSteps(sc)
	initializeShippedRoleVerbSteps(sc)
	initializeRoleSkillSteps(sc)
	initializeRoleSessionSteps(sc)
	initializeStoppedReasonSteps(sc)
	initializeHookVersionSteps(sc)
	initializeImportedSkillSteps(sc)
	initializeFailureSteps(sc)
	initializePanelSteps(sc)
	initializeScreenSteps(sc)
	initializeRenderSteps(sc)
	initializeProvingSteps(sc)
	initializeRoomSteps(sc)
	initializeStatusLineSteps(sc)
	initializeIdentifierSteps(sc)
	initializePresenceSteps(sc)
	initializePresenceToolSteps(sc)
	initializePresenceToolReadingSteps(sc)
	initializeToolNameSteps(sc)
	initializeChangelogSteps(sc)
	initializeRoleOriginSteps(sc)
	initializePromisesSteps(sc)
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
			for _, dir := range w.scratch {
				_ = os.RemoveAll(dir)
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
	sc.Step(`^the operator dispatches "([^"]*)" to the same session$`, func(ctx context.Context, text string) error {
		w := worldFrom(ctx)
		previous, err := w.lastTask()
		if err != nil {
			return err
		}
		return w.dispatch(ctx, w.projectID, previous.handle, text)
	})
	sc.Step(`^the operator dispatches "([^"]*)" to a new session$`, func(ctx context.Context, text string) error {
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
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		_, w.lastErr = w.client.StopSession(ctx, &quaycrewv1.StopSessionRequest{Id: current.sessionID})
		return w.lastErr
	})
	// A refusal is the point of two of these scenarios, so the error is recorded rather than returned.
	sc.Step(`^the operator restarts the session$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		_, w.lastErr = w.client.RestartSession(ctx, &quaycrewv1.RestartSessionRequest{Id: current.sessionID})
		return nil
	})
	sc.Step(`^the session is set to permission mode "([^"]*)"$`, func(ctx context.Context, mode string) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		_, w.lastErr = w.client.SetSessionPermissionMode(ctx,
			&quaycrewv1.SetSessionPermissionModeRequest{Id: current.sessionID, Mode: mode})
		return nil
	})
	sc.Step(`^the task ran in permission mode "([^"]*)"$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		got := w.runner.lastRequest().PermissionMode
		if got != want {
			return fmt.Errorf("the task ran as %q, want %q", got, want)
		}
		return nil
	})
	sc.Step(`^the operator archives the session$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		_, w.lastErr = w.client.ArchiveSession(ctx, &quaycrewv1.ArchiveSessionRequest{Id: current.sessionID})
		return nil
	})
	sc.Step(`^the operator dispatches "([^"]*)" to the session started first$`, func(ctx context.Context, text string) error {
		w := worldFrom(ctx)
		if len(w.tasks) == 0 {
			return fmt.Errorf("no session has been started yet")
		}
		return w.dispatch(ctx, w.projectID, w.tasks[0].handle, text)
	})
	// The listing's last column says how long ago each session moved, and the listing is ordered on
	// that same stamp, so the column reads down the page and the session somebody was last in is at
	// the top. It used to be ordered on the created stamp instead, which put a session made a week ago
	// and used an hour ago below one made yesterday and untouched since.
	sc.Step(`^the listing puts the session last worked in at the top$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		listed, err := w.orderedSessions(ctx, false)
		if err != nil {
			return err
		}
		if listed[0].GetId() != current.sessionID {
			return fmt.Errorf("the listing starts with %s, want the session last worked in: %s",
				listed[0].GetId(), current.sessionID)
		}
		// The created stamps have to disagree with the order, or this passed on a listing that is in
		// created order too and says nothing about which clock decided it.
		if !listed[0].GetCreatedAt().AsTime().Before(listed[1].GetCreatedAt().AsTime()) {
			return fmt.Errorf("the session at the top is also the one made last, so this proves nothing")
		}
		return nil
	})
	// Naming a session writes to its row, which moves its touched stamp without bringing it back out
	// of the archive. It is what makes the two stamps disagree, so the archived listing below can only
	// be in the right order if it was ordered by when each session was put away.
	sc.Step(`^the operator names the session archived first "([^"]*)"$`, func(ctx context.Context, label string) error {
		w := worldFrom(ctx)
		if len(w.tasks) == 0 {
			return fmt.Errorf("no session has been started yet")
		}
		_, err := w.client.SetSessionLabel(ctx, &quaycrewv1.SetSessionLabelRequest{
			Id: w.tasks[0].sessionID, Label: label,
		})
		return err
	})
	sc.Step(`^the archived listing puts the session put away last at the top$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		listed, err := w.orderedSessions(ctx, true)
		if err != nil {
			return err
		}
		if listed[0].GetId() != current.sessionID {
			return fmt.Errorf("the archived listing starts with %s, want the session put away last: %s",
				listed[0].GetId(), current.sessionID)
		}
		return nil
	})
	sc.Step(`^the operator restores the session$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		_, w.lastErr = w.client.RestoreSession(ctx, &quaycrewv1.RestoreSessionRequest{Id: current.sessionID})
		return nil
	})
	sc.Step(`^the operator restarts a session that does not exist$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.RestartSession(ctx, &quaycrewv1.RestartSessionRequest{Id: "ghost"})
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
		current, err := w.lastTask()
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
	sc.Step(`^every sandbox the system made is closed$`, func(ctx context.Context) error {
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
			current, err := w.lastTask()
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

	// Then: tasks and replies.
	sc.Step(`^the reply is "([^"]*)"$`, func(ctx context.Context, want string) error {
		current, err := worldFrom(ctx).lastTask()
		if err != nil {
			return err
		}
		if current.reply != want {
			return fmt.Errorf("reply is %q, want %q", current.reply, want)
		}
		return nil
	})
	sc.Step(`^both tasks ran in the same session$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.tasks) != 2 {
			return fmt.Errorf("expected 2 tasks, got %d", len(w.tasks))
		}
		if w.tasks[0].sessionID != w.tasks[1].sessionID {
			return fmt.Errorf("tasks ran in different sessions: %q and %q", w.tasks[0].sessionID, w.tasks[1].sessionID)
		}
		return nil
	})
	sc.Step(`^the tasks ran in different sessions$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if len(w.tasks) != 2 {
			return fmt.Errorf("expected 2 tasks, got %d", len(w.tasks))
		}
		if w.tasks[0].sessionID == w.tasks[1].sessionID {
			return fmt.Errorf("both tasks ran in session %q, expected different sessions", w.tasks[0].sessionID)
		}
		return nil
	})
	sc.Step(`^the second task resumed the conversation the first task started$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.runner.count() != 2 {
			return fmt.Errorf("expected the runner to have run 2 tasks, got %d", w.runner.count())
		}
		first, _ := w.runner.task(0)
		second, _ := w.runner.task(1)
		if first.ModelSessionID == "" {
			return fmt.Errorf("the first task ran in no conversation the system could name")
		}
		if first.ConversationStarted {
			return fmt.Errorf("the first task resumed a conversation nothing had written yet, "+
				"which exits saying there is no conversation found: %q", first.ModelSessionID)
		}
		if second.ModelSessionID != first.ModelSessionID {
			return fmt.Errorf("the second task ran in conversation %q and the first in %q",
				second.ModelSessionID, first.ModelSessionID)
		}
		if !second.ConversationStarted {
			return fmt.Errorf("the second task started conversation %q again rather than continuing it",
				second.ModelSessionID)
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
		current, err := w.lastTask()
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
	sc.Step(`^the session still holds the conversation the first task started$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		resp, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: current.sessionID})
		if err != nil {
			return err
		}
		// The name points at a conversation the model keeps on its own disk. Lose it and that
		// conversation still exists but can never be reached again.
		ran, err := w.conversationOfFirstTask()
		if err != nil {
			return err
		}
		if got := resp.GetSession().GetModelSessionId(); got != ran {
			return fmt.Errorf("the session holds conversation %q and its first task ran in %q", got, ran)
		}
		return nil
	})
	sc.Step(`^the workspace has (\d+) sessions$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		resp, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Workspace: w.workspaceID})
		if err != nil {
			return err
		}
		if got := len(resp.GetSessions()); got != want {
			return fmt.Errorf("the workspace has %d sessions, want %d", got, want)
		}
		return nil
	})
	sc.Step(`^the workspace has (\d+) archived sessions$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		resp, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{
			Workspace: w.workspaceID, Archived: true,
		})
		if err != nil {
			return err
		}
		if got := len(resp.GetSessions()); got != want {
			return fmt.Errorf("the workspace has %d archived sessions, want %d", got, want)
		}
		return nil
	})
	sc.Step(`^the session is reported as (\w+)$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		resp, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: current.sessionID})
		if err != nil {
			return err
		}
		if got := resp.GetSession().GetStatus(); got != want {
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

	// Then: the task's environment.
	sc.Step(`^the task ran with the subscription token "([^"]*)"$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		last, ok := w.runner.task(w.runner.count() - 1)
		if !ok {
			return fmt.Errorf("no task reached the model runner")
		}
		if got := last.Env[model.ClaudeCodeOAuthTokenEnv]; got != want {
			return fmt.Errorf("the task ran with %s=%q, want %q", model.ClaudeCodeOAuthTokenEnv, got, want)
		}
		return nil
	})
	// Its own identifier is not extra: every session is told which session it is, so it can name what
	// it puts in the volume it shares with the sessions beside it. Anything else here is a credential
	// or an address nobody asked for.
	sc.Step(`^the task ran with nothing but the session's own identifier$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		last, ok := w.runner.task(w.runner.count() - 1)
		if !ok {
			return fmt.Errorf("no task reached the model runner")
		}
		for key, value := range last.Env {
			if key == sandbox.SessionIDEnv && value != "" {
				continue
			}
			// What the system is already tracing, carried so anything in the container joins that trace
			// rather than starting a second one. It names no credential and no address.
			if key == telemetry.TraceparentEnv {
				continue
			}
			return fmt.Errorf("the task ran with %s=%q, which it was not given", key, value)
		}
		if last.Env[sandbox.SessionIDEnv] == "" {
			return fmt.Errorf("the task ran without %s, so it cannot name a working tree of its own", sandbox.SessionIDEnv)
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

// dialAs is a second client presenting somebody else's token, which is how a scenario makes a call
// as a session rather than as the operator. The guard is the real one: the call goes through the
// same interceptors the operator's does and is judged by the policy written for whoever it is from.
func (w *world) dialAs(token string) quaycrewv1.ControlPlaneServiceClient {
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return w.listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(auth.Credentials(token)),
	)
	if err != nil {
		// A dial that cannot be built is a defect in the harness rather than a behaviour, and the
		// step that follows says so plainly when every call fails.
		return w.client
	}
	return quaycrewv1.NewControlPlaneServiceClient(conn)
}
