package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// TestTheViewCanOnlyRead is the read only rule, held by the compiler and checked here as a class
// rather than call by call. Anything that changes the system is spelled Create, Set, Delete, Dispatch,
// Attach, Stop, Restart, Archive, Import or Answer, so a Reader that names only List and Get cannot
// hold one. Adding a write call to the interface fails this before it reaches a handler.
func TestTheViewCanOnlyRead(t *testing.T) {
	reader := reflect.TypeOf((*Reader)(nil)).Elem()
	if reader.NumMethod() == 0 {
		t.Fatal("Reader names no calls at all, so this test proves nothing")
	}
	for index := range reader.NumMethod() {
		name := reader.Method(index).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Get") {
			t.Errorf("Reader names %s, which is not a call that reads: the web view may not change the system", name)
		}
	}
}

func TestItServesThisMachineAndRefusesEverywhereElse(t *testing.T) {
	for _, tc := range []struct {
		addr    string
		refused bool
	}{
		{addr: "127.0.0.1:8080"},
		{addr: "localhost:8080"},
		{addr: "127.0.0.1:0"},
		{addr: "[::1]:8080"},
		// Every one of these is reachable from another machine, which is the decision this refuses
		// to make by accident.
		{addr: ":8080", refused: true},
		{addr: "0.0.0.0:8080", refused: true},
		{addr: "192.168.1.5:8080", refused: true},
		{addr: "[::]:8080", refused: true},
		{addr: "not-a-host-and-port", refused: true},
	} {
		t.Run(tc.addr, func(t *testing.T) {
			err := loopbackOnly(tc.addr)
			if tc.refused && err == nil {
				t.Fatalf("%s was allowed, and it is reachable from another machine", tc.addr)
			}
			if !tc.refused && err != nil {
				t.Fatalf("%s is on this machine and was refused: %v", tc.addr, err)
			}
		})
	}
}

// theThreeThingsAWiderDoorNeeds is what the decision of 31 August 2026 requires before anything binds
// past this machine. The system holds none of them.
//
// The list is here as well as in docs/ARCHITECTURE.md on purpose. A wall whose reason lives only in a
// code comment drifts away from the document that decided it, and the scenario in features/web.feature
// fails when the two stop naming the same three.
var theThreeThingsAWiderDoorNeeds = []string{
	"a credential for each device",
	"a way to withdraw one device",
	"a rule about encryption on the path",
}

// TestTheRefusalSaysWhichOfTheThreeIsMissing holds the refusal to naming what a wider front door would
// need. An operator who binds the wrong address gets a decision he can read and argue with, rather
// than a wall that says no. A refusal that only says no sends him to the source to find out why.
func TestTheRefusalSaysWhichOfTheThreeIsMissing(t *testing.T) {
	err := loopbackOnly("0.0.0.0:8080")
	if err == nil {
		t.Fatal("0.0.0.0:8080 was allowed, and every machine on the network can reach it")
	}
	refusal := strings.ToLower(err.Error())

	for _, needed := range theThreeThingsAWiderDoorNeeds {
		if !strings.Contains(refusal, needed) {
			t.Errorf("the refusal does not name %q, so the operator cannot tell what is missing:\n%s", needed, err)
		}
	}
	// The road that was taken, and where the decision is written, so the reader has somewhere to go.
	for _, want := range []string{"chat channel", "docs/architecture.md"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal does not name %q:\n%s", want, err)
		}
	}
	// What he typed and what to type instead. A refusal that names neither is a puzzle.
	for _, want := range []string{"0.0.0.0:8080", DefaultAddress} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q:\n%s", want, err)
		}
	}
}

func TestTheListingShowsEveryLiveConversation(t *testing.T) {
	client := aSystem(t)
	dispatch(t, client, projectOf(t, client), "", "when is the electricity bill due")

	body, status := get(t, client, "/sessions")

	if status != http.StatusOK {
		t.Fatalf("the listing answered %d", status)
	}
	for _, want := range []string{"me/house-bills/", "idle"} {
		if !strings.Contains(body, want) {
			t.Errorf("the listing does not say %q:\n%s", want, body)
		}
	}
}

func TestAConversationReadsBackInTheOrderItHappened(t *testing.T) {
	client := aSystem(t)
	project := projectOf(t, client)
	first := dispatch(t, client, project, "", "hello")
	dispatch(t, client, project, first.GetHandle(), "and again")

	body, status := get(t, client, "/session/"+first.GetId())

	if status != http.StatusOK {
		t.Fatalf("the conversation answered %d", status)
	}
	hello, again := strings.Index(body, "hello"), strings.Index(body, "and again")
	if hello < 0 || again < 0 {
		t.Fatalf("both prompts should be on the page:\n%s", body)
	}
	if hello > again {
		t.Error("the conversation reads backwards: the second prompt is above the first")
	}
}

// TestASystemWithNoConversationsSaysSo keeps an empty system from rendering an empty page. A blank
// listing and a system nobody has spoken to look identical, and one of them is a bug.
func TestASystemWithNoConversationsSaysSo(t *testing.T) {
	client := aSystem(t)

	body, status := get(t, client, "/sessions")

	if status != http.StatusOK {
		t.Fatalf("the listing answered %d", status)
	}
	if !strings.Contains(body, "no live conversations") {
		t.Errorf("an empty system should say it is empty:\n%s", body)
	}
}

func TestASessionTheSystemDoesNotHaveIsNotFound(t *testing.T) {
	client := aSystem(t)

	body, status := get(t, client, "/session/nothing-by-this-name")

	if status != http.StatusNotFound {
		t.Fatalf("want 404 for a session that is not there, got %d", status)
	}
	if strings.Contains(body, "goroutine") {
		t.Error("a missing session printed a stack trace at the operator")
	}
}

// TestAFailedTaskSaysWhatWentWrong holds the difference between a task that answered with nothing and
// a task that was refused. They must never render the same.
func TestAFailedTaskSaysWhatWentWrong(t *testing.T) {
	runner := &model.FakeRunner{Err: errors.New("the model refused")}
	client := systemWith(t, runner)
	project := projectOf(t, client)
	_, _ = client.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{Project: project, Text: "hello"})

	listed, err := client.ListSessions(context.Background(), &quaycrewv1.ListSessionsRequest{})
	if err != nil || len(listed.GetSessions()) != 1 {
		t.Fatalf("want one thread, got %d (%v)", len(listed.GetSessions()), err)
	}

	body, _ := get(t, client, "/session/"+listed.GetSessions()[0].GetId())
	if !strings.Contains(body, "failure") {
		t.Errorf("a refused task should render as a failure:\n%s", body)
	}
}

// get drives the real handler over the real routes and hands back what a browser would receive.
func get(t *testing.T, reader Reader, path string) (string, int) {
	t.Helper()
	handler, err := Handler(reader)
	if err != nil {
		t.Fatalf("build the handler: %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder.Body.String(), recorder.Code
}

func aSystem(t *testing.T) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	client, _ := systemDoingJobs(t, &model.FakeRunner{Reply: "ok"})
	return client
}

func systemWith(t *testing.T, runner model.Runner) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	client, _ := systemDoingJobs(t, runner)
	return client
}

// systemDoingJobs is a whole control plane in memory, reached over a real connection, so these tests
// drive the thing the operator drives rather than a double of it.
//
// The system itself comes back beside the client because nothing here runs the job controller: a test
// about a job that landed moves it on by ticking, which is what the controller does on its own timer
// in a running system.
func systemDoingJobs(t *testing.T, runner model.Runner) (quaycrewv1.ControlPlaneServiceClient, *controlplane.Server) {
	t.Helper()
	client, system, _ := systemKeeping(t, store.NewMemory(), runner)
	return client, system
}

// systemOver is the same system over a store the caller keeps a handle on, which is how a case that
// needs a job in a phase nothing here reaches writes one.
func systemOver(t *testing.T, held store.Store, runner model.Runner) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	client, _, _ := systemKeeping(t, held, runner)
	return client
}

func systemKeeping(t *testing.T, held store.Store, runner model.Runner) (quaycrewv1.ControlPlaneServiceClient, *controlplane.Server, store.Store) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	system := controlplane.NewServer(controlplane.Config{
		Store: held, Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	quaycrewv1.RegisterControlPlaneServiceServer(server, system)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := quaycrewv1.NewControlPlaneServiceClient(conn)
	mustCreate(t, client)
	return client, system, held
}

func mustCreate(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) {
	t.Helper()
	workspace, err := client.CreateWorkspace(context.Background(), &quaycrewv1.CreateWorkspaceRequest{Name: "me"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := client.CreateProject(context.Background(), &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
}

func projectOf(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) string {
	t.Helper()
	listed, err := client.ListProjects(context.Background(), &quaycrewv1.ListProjectsRequest{})
	if err != nil || len(listed.GetProjects()) != 1 {
		t.Fatalf("want one project, got %d (%v)", len(listed.GetProjects()), err)
	}
	return listed.GetProjects()[0].GetId()
}

func dispatch(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, project, handle, text string) *quaycrewv1.Session {
	t.Helper()
	resp, err := client.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Handle: handle, Text: text,
	})
	if err != nil {
		t.Fatalf("dispatch %q: %v", text, err)
	}
	got, err := client.GetSession(context.Background(), &quaycrewv1.GetSessionRequest{Id: resp.GetId()})
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	return got.GetSession()
}

// A task is written when it starts, so the page draws tasks that have not answered yet. An empty
// reply box under the prompt reads as a task that answered nothing, which is the same defect the
// failure box exists to prevent.
func TestATaskStillRunningSaysSoRatherThanShowingAnEmptyReply(t *testing.T) {
	runner := &model.FakeRunner{Reply: "the electricity bill is due on the ninth",
		Gate: make(chan struct{}), Started: make(chan struct{})}
	client := systemWith(t, runner)
	project := projectOf(t, client)

	// Detached, so the page can be read while the model is still working.
	resp, err := client.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "when is the electricity bill due", Detach: true,
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	select {
	case <-runner.Started:
	case <-time.After(5 * time.Second):
		t.Fatal("the task never reached the model")
	}

	body, status := get(t, client, "/session/"+resp.GetId())
	if status != http.StatusOK {
		t.Fatalf("the conversation answered %d", status)
	}
	if !strings.Contains(body, "when is the electricity bill due") {
		t.Fatalf("the page does not say what the session was asked:\n%s", body)
	}
	if !strings.Contains(body, "still running") {
		t.Fatalf("the page does not say the task is still working:\n%s", body)
	}

	close(runner.Gate)
	waitUntilLanded(t, client, resp.GetId())
	answered, _ := get(t, client, "/session/"+resp.GetId())
	if strings.Contains(answered, "still running") {
		t.Fatalf("the page still calls a landed task running:\n%s", answered)
	}
	if !strings.Contains(answered, "the electricity bill is due on the ninth") {
		t.Fatalf("the page does not carry the answer:\n%s", answered)
	}
}

// waitUntilLanded waits for a detached task to be closed in the history, so a page is never read
// mid task by accident.
//
// It watches the task rather than the session. A session is marked idle before its task record is
// closed, so a page read on the session's status alone can still be drawn from a task that says it
// is running and carries no reply, which is what this waited for and failed on about once in a
// hundred runs.
func waitUntilLanded(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, session string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got, err := client.ListTasks(context.Background(), &quaycrewv1.ListTasksRequest{Session: session})
		if err == nil && len(got.GetTasks()) > 0 {
			last := got.GetTasks()[len(got.GetTasks())-1]
			if last.GetStatus() != "running" {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the task never landed")
}

// The page shows how long ago each session moved and reads it from the same stamp the system orders by,
// so the column runs down the page. It does not order the listing itself: one order decided in the
// system is what keeps this page, the console and the command line saying the same thing.
//
// The waits are real because the system stamps its own rows and takes no stamp from a caller. Without
// them the two sessions would share a moment, the identifier would decide, and this would pass on
// whichever identifier happened to be minted first.
func TestTheListingPutsTheSessionLastWorkedInAtTheTop(t *testing.T) {
	client := aSystem(t)
	project := projectOf(t, client)

	// The handles are chosen so that any order taken from the address runs against the answer: the
	// session that belongs at the top is the one whose address sorts last. Left to generated handles
	// this case passes or fails on which identifier the system happened to mint first.
	early := dispatch(t, client, project, "zzz-older-subject", "the older subject")
	time.Sleep(10 * time.Millisecond)
	late := dispatch(t, client, project, "aaa-newer-subject", "a newer subject")
	time.Sleep(10 * time.Millisecond)
	// Back to the one started first, which makes it the session somebody was last working in.
	dispatch(t, client, project, early.GetHandle(), "carry on")

	body, status := get(t, client, "/sessions")
	if status != http.StatusOK {
		t.Fatalf("the listing answered %d", status)
	}

	first := strings.Index(body, display.ShortID(early.GetHandle()))
	second := strings.Index(body, display.ShortID(late.GetHandle()))
	if first < 0 || second < 0 {
		t.Fatalf("both sessions should be on the page:\n%s", body)
	}
	if first > second {
		t.Errorf("the session worked in last is below the one nobody has touched since:\n%s", body)
	}
}
