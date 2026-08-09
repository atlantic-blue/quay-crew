package features_test

import (
	"context"
	"fmt"
	"net"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/cucumber/godog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// authWorld is what the last caller was answered with.
type authWorld struct {
	err error
}

type authKey struct{}

func authFrom(ctx context.Context) *authWorld {
	a, _ := ctx.Value(authKey{}).(*authWorld)
	return a
}

// initializeAuthSteps registers the steps for how the crew treats a caller by what it presents.
//
// These steps build the request metadata by hand rather than through the client helper the tool
// uses, so they pin the wire contract itself: a header named authorization carrying Bearer and the
// token.
func initializeAuthSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, authKey{}, &authWorld{}), nil
	})

	call := func(ctx context.Context, header string) error {
		w := worldFrom(ctx)
		conn, err := grpc.NewClient(
			"passthrough:///bufnet",
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return w.listener.DialContext(ctx)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			return fmt.Errorf("dial the control plane: %w", err)
		}
		defer func() { _ = conn.Close() }()
		if header != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", header)
		}
		client := quaycrewv1.NewControlPlaneServiceClient(conn)
		_, callErr := client.ListWorkspaces(ctx, &quaycrewv1.ListWorkspacesRequest{})
		authFrom(ctx).err = callErr
		return nil
	}

	sc.Step(`^a caller presents the crew's token$`, func(ctx context.Context) error {
		return call(ctx, "Bearer "+worldFrom(ctx).token)
	})

	sc.Step(`^a caller presents no token$`, func(ctx context.Context) error {
		return call(ctx, "")
	})

	sc.Step(`^a caller presents a token that is not the crew's$`, func(ctx context.Context) error {
		return call(ctx, "Bearer not-the-crews-token")
	})

	sc.Step(`^the caller is served$`, func(ctx context.Context) error {
		if err := authFrom(ctx).err; err != nil {
			return fmt.Errorf("the caller was refused: %w", err)
		}
		return nil
	})

	sc.Step(`^the caller is refused, told a token is missing and where quay reads one from$`,
		func(ctx context.Context) error {
			if err := refusal(authFrom(ctx).err); err != nil {
				return err
			}
			message := status.Convert(authFrom(ctx).err).Message()
			for _, needed := range []string{"QC_TOKEN", "crew.token"} {
				if !strings.Contains(message, needed) {
					return fmt.Errorf("the refusal %q does not name %s", message, needed)
				}
			}
			return nil
		})

	sc.Step(`^the caller is refused, told the token is not this crew's$`, func(ctx context.Context) error {
		if err := refusal(authFrom(ctx).err); err != nil {
			return err
		}
		if message := status.Convert(authFrom(ctx).err).Message(); !strings.Contains(message, "not this crew's") {
			return fmt.Errorf("the refusal %q does not say the token is not this crew's", message)
		}
		return nil
	})
}

// refusal says whether an answer is the refusal the crew gives an unrecognised caller.
func refusal(err error) error {
	if err == nil {
		return fmt.Errorf("the caller was served, want a refusal")
	}
	if code := status.Code(err); code != codes.Unauthenticated {
		return fmt.Errorf("the caller was refused with %v, want %v: %v", code, codes.Unauthenticated, err)
	}
	return nil
}
