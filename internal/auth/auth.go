// Package auth is the crew's token: how it is minted and kept on the host, how a client presents
// it, and how the control plane refuses a call that does not carry it. Client and server both read
// the wire contract from here, so they cannot drift apart.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TokenFile is the file the crew's token lives in, under the crew's data directory. It sits beside
// the key that seals secrets and is kept the same way, because both are things the crew has to read
// back as they are: a value sealed into the store can never reach the operator's tool, by
// construction, so the token cannot live there.
const TokenFile = "crew.token"

// DriverTokenFile is the file the driver's token lives in, beside the crew's. The driver holds its
// own token rather than the operator's so the crew can tell its calls apart and refuse it the ones
// that grant capability, and so a token that leaks out of a driver sandbox grants strictly less
// than the operator holds.
const DriverTokenFile = "driver.token"

// TokenEnv is what a client reads first, and what the crew puts into a sandbox that is allowed to
// drive it.
const TokenEnv = "QC_TOKEN"

// header is the metadata key a call carries the token under, and scheme is how the value is
// prefixed. The names gRPC clients in other languages already use, so a future channel needs
// nothing invented.
const (
	header = "authorization"
	scheme = "Bearer "
)

// tokenBytes is how much randomness a minted token holds.
const tokenBytes = 32

// TokenAt reads the crew's token, minting one the first time.
//
// Made rather than asked for, like the key that seals secrets: a step the operator has to perform
// before anything works is a step that gets skipped. It is written readable only by its owner, and
// a file that exists but says nothing is refused rather than treated as a token, because an empty
// token would let an empty call in.
func TokenAt(path string) (string, error) {
	if raw, err := os.ReadFile(path); err == nil {
		token := strings.TrimSpace(string(raw))
		if token == "" {
			return "", fmt.Errorf("auth: the token at %s is empty: delete the file and a new one is minted", path)
		}
		return token, nil
	}
	minted := make([]byte, tokenBytes)
	if _, err := rand.Read(minted); err != nil {
		return "", fmt.Errorf("auth: minting a token: %w", err)
	}
	token := hex.EncodeToString(minted)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("auth: making room for the token: %w", err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("auth: writing the token: %w", err)
	}
	return token, nil
}

// Deny is a policy over what a recognised driver may still not call. It sees the full method name
// and the request, and returns the refusal or nil. Only driver calls are put through it: the
// operator's token is refused nothing here.
type Deny func(fullMethod string, request any) error

// Grant is what a work credential carries: the piece of work it is bound to, where that work lives,
// what it may call, and when it stops working.
//
// It is minted per piece of work rather than per session, so a credential that leaks out of a
// sandbox grants only what that one piece of work could do, and only until it ends. That is strictly
// less than the driver's token grants.
type Grant struct {
	// Work is the piece of work this credential is bound to. It is the parent of anything the caller
	// declares, which is what keeps a caller from naming its own parent and escaping the depth count.
	Work      string
	Workspace string
	Project   string
	// Verbs are what the work's role declared it may call. Empty may call nothing.
	Verbs []string
	// ExpiresAt is when the credential stops working. Zero never expires, which is what a test wants
	// and what a crew should not have.
	ExpiresAt time.Time
}

// May says whether this grant holds a verb.
func (g Grant) May(verb string) bool {
	if verb == "" {
		return false
	}
	for _, held := range g.Verbs {
		if held == verb {
			return true
		}
	}
	return false
}

// expired says whether a grant has run out.
func (g Grant) expired(now time.Time) bool {
	return !g.ExpiresAt.IsZero() && !g.ExpiresAt.After(now)
}

// Grants is what recognises a work token. The crew holds the minted ones; this package only asks.
type Grants interface {
	Grant(token string) (Grant, bool)
}

// WorkDeny is the policy over what a piece of work may call. It sees the method, the request and the
// grant, and returns the refusal or nil. A nil policy refuses a work caller everything, because a
// credential nobody judges is a credential that grants everything.
type WorkDeny func(fullMethod string, request any, grant Grant) error

// Policy is who the crew recognises, and what each of them may do.
type Policy struct {
	// Token is the operator's own, and it is refused nothing.
	Token string
	// DriverToken is the driver's, judged by Denied.
	DriverToken string
	Denied      Deny
	// Grants recognises a work credential, and DeniedToWork judges it.
	Grants       Grants
	DeniedToWork WorkDeny
}

// grantKey is how a recognised grant travels to the handler.
type grantKey struct{}

// WithGrant puts a grant on a context, which is how a handler learns which piece of work is calling.
func WithGrant(ctx context.Context, grant Grant) context.Context {
	return context.WithValue(ctx, grantKey{}, grant)
}

// GrantFrom reads the grant a call carried. A call from the operator or the driver carries none, and
// that is how a handler tells an operator's call from a session's.
func GrantFrom(ctx context.Context) (Grant, bool) {
	grant, carried := ctx.Value(grantKey{}).(Grant)
	return grant, carried
}

// ServerOptions returns the interceptors that refuse every call not carrying one of the crew's
// tokens, and put a recognised driver through the deny policy.
//
// A server built with no tokens refuses everything rather than everything getting in: the guard
// failing open is the one behaviour this package exists to prevent.
func ServerOptions(policy Policy) []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
			handler grpc.UnaryHandler) (any, error) {
			who, grant, err := policy.recognise(ctx)
			if err != nil {
				return nil, err
			}
			switch who {
			case callerDriver:
				if policy.Denied != nil {
					if err := policy.Denied(info.FullMethod, req); err != nil {
						return nil, err
					}
				}
			case callerWork:
				if err := policy.judgeWork(info.FullMethod, req, grant); err != nil {
					return nil, err
				}
				// The grant travels with the call, so the handler reads which piece of work is
				// calling rather than being told by whoever called it.
				ctx = WithGrant(ctx, grant)
			}
			return handler(ctx, req)
		}),
		grpc.ChainStreamInterceptor(func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo,
			handler grpc.StreamHandler) error {
			who, grant, err := policy.recognise(stream.Context())
			if err != nil {
				return err
			}
			// A stream carries no single request to judge, so a caller that is not the operator is
			// judged on the method alone.
			switch who {
			case callerDriver:
				if policy.Denied != nil {
					if err := policy.Denied(info.FullMethod, nil); err != nil {
						return err
					}
				}
			case callerWork:
				if err := policy.judgeWork(info.FullMethod, nil, grant); err != nil {
					return err
				}
			}
			return handler(srv, stream)
		}),
	}
}

// judgeWork puts a work credential through the policy written for it. No policy refuses everything:
// a credential nobody judges is a credential that grants everything.
func (p Policy) judgeWork(fullMethod string, request any, grant Grant) error {
	if p.DeniedToWork == nil {
		return status.Error(codes.PermissionDenied,
			"this crew judges no work credential, so a piece of work may call nothing")
	}
	return p.DeniedToWork(fullMethod, request, grant)
}

// caller is who a recognised token says is calling.
type caller int

const (
	callerOperator caller = iota
	callerDriver
	callerWork
)

// recognise says who a call is from by the token it carries, or why it is refused.
//
// The operator first, then the driver, then the work credentials the crew has minted. A crew holding
// no token of its own recognises nobody, because a guard that fails open is the one thing this
// package exists to prevent.
func (p Policy) recognise(ctx context.Context) (caller, Grant, error) {
	if p.Token == "" && p.DriverToken == "" {
		return 0, Grant{}, status.Error(codes.Unauthenticated,
			"this crew holds no token, so it cannot recognise any caller: restart the control plane with a data directory and one is minted")
	}
	presented := presentedToken(ctx)
	if presented == "" {
		return 0, Grant{}, status.Error(codes.Unauthenticated,
			"this call carries no crew token, so the crew cannot tell who is calling: quay reads "+
				TokenEnv+", or crew.token in the crew's data directory")
	}
	if p.Token != "" && subtle.ConstantTimeCompare([]byte(presented), []byte(p.Token)) == 1 {
		return callerOperator, Grant{}, nil
	}
	if p.DriverToken != "" && subtle.ConstantTimeCompare([]byte(presented), []byte(p.DriverToken)) == 1 {
		return callerDriver, Grant{}, nil
	}
	if p.Grants != nil {
		if grant, minted := p.Grants.Grant(presented); minted && !grant.expired(time.Now()) {
			return callerWork, grant, nil
		}
	}
	return 0, Grant{}, status.Error(codes.Unauthenticated, "the token this call carries is not this crew's")
}

// presentedToken reads the token a call carries, or nothing when it carries none.
func presentedToken(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, value := range md.Get(header) {
		if strings.HasPrefix(value, scheme) {
			return strings.TrimPrefix(value, scheme)
		}
	}
	return ""
}

// Credentials returns what a client attaches to every call to present the crew's token.
func Credentials(token string) credentials.PerRPCCredentials {
	return tokenCredentials(token)
}

type tokenCredentials string

func (t tokenCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{header: scheme + string(t)}, nil
}

// RequireTransportSecurity is false because the crew listens on loopback by default and the
// connection is plaintext today. The token is what a caller is recognised by, not what hides the
// conversation; an operator exposing the port beyond the machine owns the transport in front of it.
func (tokenCredentials) RequireTransportSecurity() bool { return false }
