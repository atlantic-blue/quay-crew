package auth_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestTokenAtMintsOnceAndReadsItBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crew.token")

	minted, err := auth.TokenAt(path)
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	if len(minted) != 64 {
		t.Fatalf("a minted token is %d characters, want 64", len(minted))
	}

	read, err := auth.TokenAt(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if read != minted {
		t.Fatalf("read back %q, want the minted %q", read, minted)
	}
}

func TestTokenAtWritesForTheOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crew.token")
	if _, err := auth.TokenAt(path); err != nil {
		t.Fatalf("minting: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("the token file is %v, want 0600", got)
	}
}

func TestTokenAtTrimsWhatAnEditorLeaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crew.token")
	if err := os.WriteFile(path, []byte("  a-token-somebody-set-by-hand\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := auth.TokenAt(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if token != "a-token-somebody-set-by-hand" {
		t.Fatalf("read %q, want the trimmed token", token)
	}
}

func TestTokenAtRefusesAnEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crew.token")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.TokenAt(path); err == nil {
		t.Fatal("an empty token file was accepted, want a refusal: an empty token would let an empty call in")
	}
}

// A job credential is the third kind of caller. It is bound to one job, carries only the
// verbs that job's role declared, and is what lets a session declare job while holding strictly
// less than the driver does.

// callThrough puts one real call through the interceptors, which is the only way to know what a
// caller presenting a token is actually allowed to do.
func callThrough(t *testing.T, options []grpc.ServerOption, token string) error {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(options...)
	grpc_health_v1.RegisterHealthServer(server, health.NewServer())
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(auth.Credentials(token)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	_, err = grpc_health_v1.NewHealthClient(conn).Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	return err
}

// grants is a double for what recognises a job token.
type grants map[string]auth.Grant

func (g grants) Grant(token string) (auth.Grant, bool) {
	held, found := g[token]
	return held, found
}

func TestAJobTokenIsRecognisedAndItsGrantReachesThePolicy(t *testing.T) {
	held := auth.Grant{Job: "job-1", Workspace: "workspace-1", Project: "project-1", Verbs: []string{"job.create"}}
	var seen auth.Grant
	var sawJob bool
	options := auth.ServerOptions(auth.Policy{
		Token: "operator", Grants: grants{"job-token": held},
		DeniedToJob: func(_ string, _ any, grant auth.Grant) error {
			seen, sawJob = grant, true
			return nil
		},
	})

	if err := callThrough(t, options, "job-token"); err != nil {
		t.Fatalf("a job token was refused: %v", err)
	}
	if !sawJob {
		t.Fatal("the policy never saw the call, so a job token is judged by nothing")
	}
	if seen.Job != "job-1" || len(seen.Verbs) != 1 || seen.Verbs[0] != "job.create" {
		t.Fatalf("the grant reads %+v, want the job and the verbs the token was minted with", seen)
	}
}

// The refusal is the policy's, so a session is told which verb it lacks rather than that something
// went wrong.
func TestAJobTokenIsRefusedByThePolicyThatJudgesIt(t *testing.T) {
	options := auth.ServerOptions(auth.Policy{
		Token: "operator", Grants: grants{"job-token": {Job: "job-1"}},
		DeniedToJob: func(string, any, auth.Grant) error {
			return status.Error(codes.PermissionDenied, "this job may not do that")
		},
	})

	err := callThrough(t, options, "job-token")
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("the refusal is %v, want the policy's own", err)
	}
}

// A token nobody minted is nobody's, however much it looks like one.
func TestATokenNoGrantHoldsIsStillRefused(t *testing.T) {
	options := auth.ServerOptions(auth.Policy{
		Token: "operator", Grants: grants{"job-token": {Job: "job-1"}},
	})

	if err := callThrough(t, options, "another-token"); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("an unminted token answered %v, want Unauthenticated", err)
	}
}

// Each caller is judged by the policy written for it, and by no other.
func TestTheOperatorIsNotJudgedByTheJobPolicy(t *testing.T) {
	judged := false
	options := auth.ServerOptions(auth.Policy{
		Token: "operator", Grants: grants{"job-token": {Job: "job-1"}},
		DeniedToJob: func(string, any, auth.Grant) error {
			judged = true
			return status.Error(codes.PermissionDenied, "no")
		},
	})

	if err := callThrough(t, options, "operator"); err != nil {
		t.Fatalf("the operator was refused: %v", err)
	}
	if judged {
		t.Fatal("the operator was put through the policy written for a job")
	}
}

// A grant that has run out is not a grant. It expires with the job, so one that leaks out of a
// sandbox stops working.
func TestAGrantThatHasExpiredIsRefused(t *testing.T) {
	options := auth.ServerOptions(auth.Policy{
		Token:  "operator",
		Grants: grants{"job-token": {Job: "job-1", ExpiresAt: time.Now().Add(-time.Second)}},
	})

	if err := callThrough(t, options, "job-token"); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("an expired grant answered %v, want Unauthenticated", err)
	}
}

// A crew that recognises nobody refuses everybody, which is the one behaviour this package exists to
// keep: the guard must never fail open.
func TestACrewWithNoTokensAtAllRefusesAJobToken(t *testing.T) {
	options := auth.ServerOptions(auth.Policy{Grants: grants{"job-token": {Job: "job-1"}}})

	if err := callThrough(t, options, "job-token"); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("a crew holding no token of its own answered %v, want Unauthenticated", err)
	}
}

// What a call is allowed to do is read from the grant it carried, so a handler asks the context
// rather than being told by whoever called it.
func TestTheGrantIsReadableFromTheContextOfTheCall(t *testing.T) {
	ctx := auth.WithGrant(context.Background(), auth.Grant{Job: "job-1", Verbs: []string{"job.create"}})

	got, carried := auth.GrantFrom(ctx)
	if !carried {
		t.Fatal("the context carries no grant")
	}
	if got.Job != "job-1" {
		t.Fatalf("the grant is for %q", got.Job)
	}
	if _, carried := auth.GrantFrom(context.Background()); carried {
		t.Fatal("a context nobody put a grant on carries one")
	}
}

func TestAGrantSaysWhichVerbsItHolds(t *testing.T) {
	held := auth.Grant{Verbs: []string{"job.create", "job.read"}}

	for _, verb := range []string{"job.create", "job.read"} {
		if !held.May(verb) {
			t.Errorf("the grant may not %s, and it holds it", verb)
		}
	}
	for _, verb := range []string{"job.stop", "job.answer", ""} {
		if held.May(verb) {
			t.Errorf("the grant may %s, and it never held it", verb)
		}
	}
}
