package auth_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestTokenAtMintsOnceAndReadsItBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "system.token")

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
	path := filepath.Join(t.TempDir(), "system.token")
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
	path := filepath.Join(t.TempDir(), "system.token")
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
	path := filepath.Join(t.TempDir(), "system.token")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.TokenAt(path); err == nil {
		t.Fatal("an empty token file was accepted, want a refusal: an empty token would let an empty call in")
	}
}

// Two kinds of caller reach the system: the operator, who is refused nothing, and the driver, which
// holds its own token so a token that leaks out of a sandbox grants strictly less than the operator's.

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

func TestTheOperatorsTokenIsRefusedNothing(t *testing.T) {
	options := auth.ServerOptions(auth.Policy{
		Token: "the-operator's-token", DriverToken: "the-driver's-token",
		Denied: func(string, any) error {
			return status.Error(codes.PermissionDenied, "the driver may not call this")
		},
	})
	if err := callThrough(t, options, "the-operator's-token"); err != nil {
		t.Fatalf("the operator was refused a call the driver may not make: %v", err)
	}
}

// The driver is judged, and the refusal has to come from the interceptor rather than from the handler,
// because a handler that is reached at all has already been given the call.
func TestTheDriverIsRefusedWhatThePolicySays(t *testing.T) {
	options := auth.ServerOptions(auth.Policy{
		Token: "the-operator's-token", DriverToken: "the-driver's-token",
		Denied: func(string, any) error {
			return status.Error(codes.PermissionDenied, "the driver may not call this")
		},
	})
	err := callThrough(t, options, "the-driver's-token")
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("the driver's call answered %v, want PermissionDenied", status.Code(err))
	}
}

// What the driver exists to do stays open, or the policy would be a system that cannot be driven.
func TestTheDriverMakesTheCallsNoPolicyRefuses(t *testing.T) {
	options := auth.ServerOptions(auth.Policy{
		Token: "the-operator's-token", DriverToken: "the-driver's-token",
		Denied: func(string, any) error { return nil },
	})
	if err := callThrough(t, options, "the-driver's-token"); err != nil {
		t.Fatalf("the driver was refused a call nothing denies: %v", err)
	}
}

func TestATokenThisSystemNeverMintedIsRefused(t *testing.T) {
	options := auth.ServerOptions(auth.Policy{
		Token: "the-operator's-token", DriverToken: "the-driver's-token",
	})
	err := callThrough(t, options, "a-token-from-somewhere-else")
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("a forged token answered %v, want Unauthenticated", status.Code(err))
	}
}

// A guard that fails open is the one behaviour this package exists to prevent, so a system holding no
// token of its own recognises nobody rather than everybody.
func TestASystemWithNoTokensAtAllRecognisesNobody(t *testing.T) {
	err := callThrough(t, auth.ServerOptions(auth.Policy{}), "any-token-at-all")
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("a system holding no token answered %v, want Unauthenticated", status.Code(err))
	}
}

// A call carrying nothing at all is refused for its own reason, so the operator reads which of the two
// went wrong.
func TestACallCarryingNoTokenIsRefusedAndSaysSo(t *testing.T) {
	options := auth.ServerOptions(auth.Policy{Token: "the-operator's-token"})
	err := callThrough(t, options, "")
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("a call carrying no token answered %v, want Unauthenticated", status.Code(err))
	}
	if !strings.Contains(err.Error(), auth.TokenEnv) {
		t.Fatalf("the refusal is %q, and it does not say where a token is read from", err)
	}
}
