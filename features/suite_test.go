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
	"net"
	"sync"
	"sync/atomic"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/cucumber/godog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
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
}

var _ model.Runner = (*recordingRunner)(nil)

func (r *recordingRunner) Run(_ context.Context, _ sandbox.Sandbox, req model.Request) (model.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
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
	conn       *grpc.ClientConn
	client     quaycrewv1.ControlPlaneServiceClient
	provider   *sandbox.FakeProvider
	runner     *recordingRunner
	secrets    secrets.Store
	store      store.Store

	projectID          string
	projectName        string
	turns              []turn
	lastErr            error
	lastSecretResponse *quaycrewv1.SetSecretResponse
}

type worldKey struct{}

func worldFrom(ctx context.Context) *world {
	w, _ := ctx.Value(worldKey{}).(*world)
	return w
}

// start builds a control plane with doubles behind it and serves it over an in memory listener, so
// the scenarios exercise the real gRPC path without a port.
func (w *world) start() error {
	w.provider = &sandbox.FakeProvider{}
	w.runner = &recordingRunner{}
	w.secrets = secrets.NewMemory()
	w.store = store.NewMemory()
	return w.serve()
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
	w.grpcServer = grpc.NewServer()
	quaycrewv1.RegisterControlPlaneServiceServer(w.grpcServer, controlplane.NewServer(w.store, w.runner, w.provider, w.secrets))
	go func() { _ = w.grpcServer.Serve(listener) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("dial the control plane: %w", err)
	}
	w.conn = conn
	w.client = quaycrewv1.NewControlPlaneServiceClient(conn)
	return nil
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
	resp, err := w.client.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project, ThreadId: thread, Text: text})
	w.lastErr = err
	if err != nil {
		return nil
	}
	w.turns = append(w.turns, turn{sessionID: resp.GetSessionId(), threadID: resp.GetThreadId(), reply: resp.GetReply()})
	return nil
}

func (w *world) createProject(ctx context.Context, name string) error {
	resp, err := w.client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Name: name})
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
	// Tear the control plane down. The scenario's own failure is already recorded, so this returns
	// nil rather than the incoming error, which would be reported a second time as a hook failure.
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		scenariosRun.Add(1)
		if w := worldFrom(ctx); w != nil {
			w.stop()
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
	sc.Step(`^the project has the subscription token "([^"]*)"$`, func(ctx context.Context, token string) error {
		w := worldFrom(ctx)
		_, err := w.client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
			Project: w.projectID, Key: model.ClaudeCodeOAuthTokenEnv, Value: token,
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
	sc.Step(`^the operator dispatches "([^"]*)" to project "([^"]*)"$`, func(ctx context.Context, text, project string) error {
		return worldFrom(ctx).dispatch(ctx, project, "", text)
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
		_, w.lastErr = w.client.StopSession(ctx, &quaycrewv1.StopSessionRequest{Id: current.sessionID})
		return w.lastErr
	})

	// When: projects.
	sc.Step(`^the operator creates a project named "([^"]*)"$`, func(ctx context.Context, name string) error {
		return worldFrom(ctx).createProject(ctx, name)
	})
	sc.Step(`^the operator fetches the project "([^"]*)"$`, func(ctx context.Context, id string) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: id})
		return nil
	})
	sc.Step(`^the operator deletes the project$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.DeleteProject(ctx, &quaycrewv1.DeleteProjectRequest{Id: w.projectID})
		return w.lastErr
	})
	sc.Step(`^the operator attaches a "([^"]*)" channel called "([^"]*)" to project "([^"]*)"$`,
		func(ctx context.Context, kind, id, project string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.AttachChannel(ctx, &quaycrewv1.AttachChannelRequest{Project: project, Id: id, Kind: kind})
			return nil
		})
	sc.Step(`^the operator sets the secret "([^"]*)" to "([^"]*)"$`, func(ctx context.Context, key, value string) error {
		w := worldFrom(ctx)
		w.lastSecretResponse, w.lastErr = w.client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
			Project: w.projectID, Key: key, Value: value,
		})
		return w.lastErr
	})
	sc.Step(`^the operator sets the secret "([^"]*)" to "([^"]*)" on project "([^"]*)"$`,
		func(ctx context.Context, key, value, project string) error {
			w := worldFrom(ctx)
			w.lastSecretResponse, w.lastErr = w.client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
				Project: project, Key: key, Value: value,
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
		if w.provider.Created[0] != current.sessionID {
			return fmt.Errorf("sandbox was created for %q, want the session %q", w.provider.Created[0], current.sessionID)
		}
		return nil
	})
	sc.Step(`^the session still holds the conversation the first turn started$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTurn()
		if err != nil {
			return err
		}
		resp, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: current.sessionID})
		if err != nil {
			return err
		}
		// The handle points at a conversation the model keeps on its own disk. Lose it and that
		// conversation still exists but can never be reached again.
		if got := resp.GetSession().GetModelSessionId(); got != "conversation-1" {
			return fmt.Errorf("the session holds conversation %q, want conversation-1", got)
		}
		return nil
	})
	sc.Step(`^the project has (\d+) sessions$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		resp, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		if got := len(resp.GetSessions()); got != want {
			return fmt.Errorf("the project has %d sessions, want %d", got, want)
		}
		return nil
	})
	sc.Step(`^the session is reported as (\w+)$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		current, err := w.lastTurn()
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

	// Then: projects.
	sc.Step(`^the project is listed$`, func(ctx context.Context) error {
		return worldFrom(ctx).projectIsListed(ctx, true)
	})
	sc.Step(`^the project is no longer listed$`, func(ctx context.Context) error {
		return worldFrom(ctx).projectIsListed(ctx, false)
	})
	sc.Step(`^the project can be fetched by its id$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		resp, err := w.client.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: w.projectID})
		if err != nil {
			return err
		}
		if resp.GetProject().GetName() != w.projectName {
			return fmt.Errorf("fetched project is named %q, want %q", resp.GetProject().GetName(), w.projectName)
		}
		return nil
	})
	sc.Step(`^the secrets backend holds "([^"]*)" for that project$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		got, err := w.secrets.Get(ctx, w.projectID, model.ClaudeCodeOAuthTokenEnv)
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
	sc.Step(`^the control plane refuses it as not found$`, func(ctx context.Context) error {
		return refused(worldFrom(ctx), codes.NotFound)
	})
	sc.Step(`^the control plane refuses it as invalid$`, func(ctx context.Context) error {
		return refused(worldFrom(ctx), codes.InvalidArgument)
	})
}

func (w *world) projectIsListed(ctx context.Context, want bool) error {
	resp, err := w.client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{})
	if err != nil {
		return err
	}
	for _, project := range resp.GetProjects() {
		if project.GetId() == w.projectID {
			if !want {
				return fmt.Errorf("project %q is still listed", w.projectID)
			}
			return nil
		}
	}
	if want {
		return fmt.Errorf("project %q is not listed", w.projectID)
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
