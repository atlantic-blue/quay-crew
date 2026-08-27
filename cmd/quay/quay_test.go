package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// testClient stands up a control plane over an in memory connection and points the configuration
// directory at a temporary one, so a test that moves the current context cannot touch the operator's
// own.
func testClient(t *testing.T) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	return testClientWith(t, controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
}

func testClientWith(t *testing.T, cfg controlplane.Config) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	return testClientFor(t, controlplane.NewServer(cfg))
}

// testClientFor serves a crew somebody else built, for a test that has to do something to the server
// before a client reaches it.
func testClientFor(t *testing.T, srv *controlplane.Server) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	t.Setenv(HomeEnv, t.TempDir())

	lis := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	quaycrewv1.RegisterControlPlaneServiceServer(grpcServer, srv)
	go func() { _ = grpcServer.Serve(lis) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return quaycrewv1.NewControlPlaneServiceClient(conn)
}

// mustRun runs one invocation and fails the test if it errors, for the setup steps that are not what
// the test is about.
func mustRun(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := run(context.Background(), client, args, &out, ""); err != nil {
		t.Fatalf("quay %s: %v", strings.Join(args, " "), err)
	}
	return out.String()
}

func TestWorkspaceCreateAndList(t *testing.T) {
	client := testClient(t)

	created := mustRun(t, client, "workspace", "create", "acme")
	if !strings.Contains(created, "created workspace") || !strings.Contains(created, "(acme)") {
		t.Fatalf("create output: %q", created)
	}
	// Creating something says where you now are, so the next command needs no address.
	if !strings.Contains(created, "now in acme") {
		t.Fatalf("create did not move the operator: %q", created)
	}

	if listed := mustRun(t, client, "workspace", "list"); !strings.Contains(listed, "acme") {
		t.Fatalf("list output: %q", listed)
	}
}

// TestCreateMovesYouAndTalkingFollows is the point of the whole change: say where you are once,
// then talk.
func TestCreateMovesYouAndTalkingFollows(t *testing.T) {
	client := testClient(t)

	mustRun(t, client, "workspace", "create", "me")
	created := mustRun(t, client, "project", "create", "house-bills")
	if !strings.Contains(created, "now in me/house-bills") {
		t.Fatalf("creating a project did not move the operator into it: %q", created)
	}
	if where := mustRun(t, client, "use"); strings.TrimSpace(where) != "me/house-bills" {
		t.Fatalf("quay use says %q, want me/house-bills", where)
	}

	replied := mustRun(t, client, "ask", "hello", "there")
	if !strings.Contains(replied, "ok") {
		t.Fatalf("asking with no address did not run: %q", replied)
	}
	if !strings.Contains(replied, "session ") || !strings.Contains(replied, "handle ") {
		t.Fatalf("dispatch did not show the session and its handle: %q", replied)
	}

	// And the listing names things rather than printing identifiers.
	listed := mustRun(t, client, "sessions")
	if !strings.Contains(listed, "me/house-bills") {
		t.Fatalf("sessions output does not name the workspace and project: %q", listed)
	}
}

// TestAddressOnTheCommandLineDoesNotMoveYou covers the override: reach somewhere else for one
// command without leaving where you are.
func TestAddressOnTheCommandLineDoesNotMoveYou(t *testing.T) {
	client := testClient(t)

	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "project", "create", "gardening")
	mustRun(t, client, "use", "me/house-bills")

	if replied := mustRun(t, client, "ask", "me/gardening", "order the bulbs"); !strings.Contains(replied, "ok") {
		t.Fatalf("asking at an explicit address did not run: %q", replied)
	}
	if where := mustRun(t, client, "use"); strings.TrimSpace(where) != "me/house-bills" {
		t.Fatalf("an address on the command line moved the operator to %q", where)
	}
}

// TestUseASessionContinuesThatConversation is the third level: a session is somewhere you can stand.
func TestUseASessionContinuesThatConversation(t *testing.T) {
	client := testClient(t)

	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	first := mustRun(t, client, "dispatch", "hello")
	session := sessionFrom(t, first)

	// The shortened identifier is what a listing prints, so typing that back has to work.
	moved := mustRun(t, client, "use", "me/house-bills/"+session[:8])
	if !strings.Contains(moved, "now in me/house-bills/"+session[:8]) {
		t.Fatalf("use of a session said %q", moved)
	}

	second := mustRun(t, client, "dispatch", "and again")
	if got := sessionFrom(t, second); got != session {
		t.Fatalf("the second task ran in session %s, want the one the context named, %s", got, session)
	}
}

// sessionFrom digs the session's handle out of what a dispatch printed, because the handle is what
// an address carries.
func sessionFrom(t *testing.T, output string) string {
	t.Helper()
	_, after, found := strings.Cut(output, "handle ")
	if !found {
		t.Fatalf("no handle in %q", output)
	}
	return strings.TrimSuffix(strings.TrimSpace(after), ")")
}

func TestDispatchFromAWorkspaceSaysWhichProjectsItHolds(t *testing.T) {
	client := testClient(t)

	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "use", "me")

	err := run(context.Background(), client, []string{"dispatch", "hello"}, io.Discard, "")
	if err == nil {
		t.Fatal("dispatching from a workspace succeeded, want a refusal")
	}
	// A refusal that only says no leaves the operator guessing what to type next.
	if !strings.Contains(err.Error(), "house-bills") {
		t.Fatalf("the refusal is %q, want it to name the projects the workspace holds", err)
	}
}

func TestDispatchWithNowhereToGoExplainsItself(t *testing.T) {
	client := testClient(t)
	err := run(context.Background(), client, []string{"dispatch", "hello"}, io.Discard, "")
	if err == nil {
		t.Fatal("dispatch with no context succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "quay use") {
		t.Fatalf("the refusal is %q, want it to say how to get somewhere", err)
	}
}

// TestAMessageIsNotAnAddress guards the parsing rule: only something carrying a separator is read as
// an address, so an unquoted message keeps working.
func TestAMessageIsNotAnAddress(t *testing.T) {
	client := testClient(t)

	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	replied := mustRun(t, client, "ask", "hello", "there")
	if !strings.Contains(replied, "ok") {
		t.Fatalf("a two word message was not dispatched: %q", replied)
	}
}

func TestUseRefusesAnAddressThatNamesNothing(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")

	err := run(context.Background(), client, []string{"use", "ghost/bills"}, io.Discard, "")
	if err == nil {
		t.Fatal("use of an unknown address succeeded")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("the refusal is %q, want it to quote what was typed", err)
	}
	// And it left the operator where they already were, which creating "me" had made "me".
	if where := mustRun(t, client, "use"); strings.TrimSpace(where) != "me" {
		t.Fatalf("a refused address moved the operator to %q", where)
	}
}

func TestSecretSet(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "acme")

	out := mustRun(t, client, "secret", "set", "CLAUDE_CODE_OAUTH_TOKEN", "tok-123")
	if !strings.Contains(out, "set secret CLAUDE_CODE_OAUTH_TOKEN") {
		t.Fatalf("secret set output: %q", out)
	}
	// The value must never be echoed back.
	if strings.Contains(out, "tok-123") {
		t.Fatalf("secret value was printed: %q", out)
	}

	// A workspace can also be named outright, without moving there.
	if out := mustRun(t, client, "secret", "set", "acme", "OTHER_KEY", "value"); !strings.Contains(out, "OTHER_KEY") {
		t.Fatalf("secret set with an address: %q", out)
	}
}

func TestSecretSetNeedsAKeyAndAValue(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "acme")
	if err := run(context.Background(), client, []string{"secret", "set", "only-key"}, io.Discard, ""); err == nil {
		t.Fatal("secret set without a value = nil error, want error")
	}
}

// TestFeaturesNeedsNoControlPlane: the question "what does this thing do" is usually asked by
// somebody who has not started a stack yet, so it must not need one.
func TestFeaturesNeedsNoControlPlane(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), nil, []string{"features"}, &out, ""); err != nil {
		t.Fatalf("quay features with no client: %v", err)
	}
	if !strings.Contains(out.String(), "sandbox") {
		t.Fatalf("the feature list says nothing about sandboxes:\n%s", out.String())
	}

	out.Reset()
	if err := run(context.Background(), nil, []string{"features", "address"}, &out, ""); err != nil {
		t.Fatalf("quay features address: %v", err)
	}
	if !strings.Contains(out.String(), "address") && !strings.Contains(out.String(), "Address") {
		t.Fatalf("filtering by a word did not find the feature about it:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Sessions run in isolated sandboxes") {
		t.Fatalf("filtering by a word listed everything anyway:\n%s", out.String())
	}

	out.Reset()
	if err := run(context.Background(), nil, []string{"features", "quantum"}, &out, ""); err != nil {
		t.Fatalf("quay features quantum: %v", err)
	}
	if !strings.Contains(out.String(), "nothing here mentions") {
		t.Fatalf("a word that matches nothing said %q", out.String())
	}
}

// TestTheOldFlagsAreRefusedRatherThanSwallowed is the gap that let a regression through: every test
// written for #69 covered what the address form does, and none covered what happens when somebody
// types the form it replaced. The answer was that `--project default` became the first two words of
// the message, and the operator got an error about their workspace instead.
func TestTheOldFlagsAreRefusedRatherThanSwallowed(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "demo")
	mustRun(t, client, "project", "create", "default")

	for _, invocation := range [][]string{
		{"dispatch", "--project", "default", "remember the number"},
		{"dispatch", "--project=default", "remember the number"},
		{"sessions", "--workspace", "demo"},
		{"dispatch", "--session", "abc123", "hello"},
		{"secret", "set", "--workspace", "demo", "KEY", "value"},
	} {
		err := run(context.Background(), client, invocation, io.Discard, "")
		if err == nil {
			t.Fatalf("quay %s was accepted, want it refused", strings.Join(invocation, " "))
		}
		if !strings.Contains(err.Error(), "is gone") || !strings.Contains(err.Error(), "quay use") {
			t.Fatalf("quay %s said %q, want it to name the flag and how to say it now",
				strings.Join(invocation, " "), err)
		}
	}
}

// TestAnyOtherFlagIsRefusedToo, so the next thing that gets replaced by an address cannot repeat this.
func TestAnyOtherFlagIsRefusedToo(t *testing.T) {
	err := run(context.Background(), testClient(t), []string{"dispatch", "--force", "hello"}, io.Discard, "")
	if err == nil {
		t.Fatal("an unknown flag was accepted")
	}
	if !strings.Contains(err.Error(), "takes no flags") {
		t.Fatalf("an unknown flag said %q, want it to say the tool takes none", err)
	}
}

// TestAMessageIsStillAMessage: refusing flags must not refuse a hyphen in something you are saying.
func TestAMessageIsStillAMessage(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "demo")
	mustRun(t, client, "project", "create", "default")

	if replied := mustRun(t, client, "dispatch", "the well-known -5 degrees case"); !strings.Contains(replied, "handle ") {
		t.Fatalf("a message with hyphens in it was refused: %q", replied)
	}
}

func TestUnknownCommand(t *testing.T) {
	if err := run(context.Background(), testClient(t), []string{"bogus"}, io.Discard, ""); err == nil {
		t.Fatal("unknown command = nil error, want error")
	}
}

// TestAnIdWorksWhereverANameDoes: the levels of an address are references, so nothing forces the
// operator to have named things well.
func TestAnIdWorksWhereverANameDoes(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	created, err := client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: created.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	address := created.GetWorkspace().GetId() + "/" + project.GetProject().GetId()
	if replied := mustRun(t, client, "ask", address, "hello"); !strings.Contains(replied, "ok") {
		t.Fatalf("asking by id: %q", replied)
	}
}
