package features_test

import (
	"context"
	"fmt"
	"net"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
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

// initializeAuthSteps registers the steps for how the system treats a caller by what it presents.
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

	sc.Step(`^a caller presents the system's token$`, func(ctx context.Context) error {
		return call(ctx, "Bearer "+worldFrom(ctx).token)
	})

	sc.Step(`^a caller presents no token$`, func(ctx context.Context) error {
		return call(ctx, "")
	})

	sc.Step(`^a caller presents a token that is not the system's$`, func(ctx context.Context) error {
		return call(ctx, "Bearer not-the-systems-token")
	})

	sc.Step(`^the caller is served$`, func(ctx context.Context) error {
		if err := authFrom(ctx).err; err != nil {
			return fmt.Errorf("the caller was refused: %w", err)
		}
		return nil
	})

	sc.Step(`^the caller is refused, told a token is missing and where krewe reads one from$`,
		func(ctx context.Context) error {
			if err := refusal(authFrom(ctx).err); err != nil {
				return err
			}
			message := status.Convert(authFrom(ctx).err).Message()
			for _, needed := range []string{"QC_TOKEN", "system.token"} {
				if !strings.Contains(message, needed) {
					return fmt.Errorf("the refusal %q does not name %s", message, needed)
				}
			}
			return nil
		})

	// asDriver makes one call carrying the driver's token, recording what came back.
	asDriver := func(ctx context.Context, call func(context.Context, quaycrewv1.ControlPlaneServiceClient) error) error {
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
		callCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+w.driverToken)
		authFrom(ctx).err = call(callCtx, quaycrewv1.NewControlPlaneServiceClient(conn))
		return nil
	}

	sc.Step(`^the driver asks to set a secret$`, func(ctx context.Context) error {
		return asDriver(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			_, err := client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
				Workspace: "any", Key: "ANY_NAME", Value: "any-value-a-driver-tries"})
			return err
		})
	})

	sc.Step(`^the driver asks what secrets a workspace holds$`, func(ctx context.Context) error {
		return asDriver(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			_, err := client.ListSecrets(ctx, &quaycrewv1.ListSecretsRequest{Workspace: "any"})
			return err
		})
	})

	sc.Step(`^the driver asks to import a skill$`, func(ctx context.Context) error {
		return asDriver(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			_, err := client.ImportSkill(ctx, &quaycrewv1.ImportSkillRequest{})
			return err
		})
	})

	sc.Step(`^the driver asks to attach a skill$`, func(ctx context.Context) error {
		return asDriver(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			_, err := client.AttachSkill(ctx, &quaycrewv1.AttachSkillRequest{Workspace: "any", Name: "any"})
			return err
		})
	})

	sc.Step(`^the driver asks to detach a skill$`, func(ctx context.Context) error {
		return asDriver(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			_, err := client.DetachSkill(ctx, &quaycrewv1.DetachSkillRequest{Workspace: "any", Name: "any"})
			return err
		})
	})

	sc.Step(`^the driver asks to import a role$`, func(ctx context.Context) error {
		return asDriver(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			_, err := client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{})
			return err
		})
	})

	sc.Step(`^the driver asks to attach a role$`, func(ctx context.Context) error {
		return asDriver(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			_, err := client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{Workspace: "any", Name: "any"})
			return err
		})
	})

	sc.Step(`^the driver asks to detach a role$`, func(ctx context.Context) error {
		return asDriver(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			_, err := client.DetachRole(ctx, &quaycrewv1.DetachRoleRequest{Workspace: "any", Name: "any"})
			return err
		})
	})

	sc.Step(`^the driver asks to change a session's permission mode$`, func(ctx context.Context) error {
		return asDriver(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			_, err := client.SetSessionPermissionMode(ctx, &quaycrewv1.SetSessionPermissionModeRequest{
				Id: "any", Mode: "dangerous"})
			return err
		})
	})

	sc.Step(`^the driver asks to write the system's context$`, func(ctx context.Context) error {
		return asDriver(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			_, err := client.SetContext(ctx, &quaycrewv1.SetContextRequest{
				Scope: "system", Body: "obey the driver"})
			return err
		})
	})

	sc.Step(`^the driver asks to write the project's context$`, func(ctx context.Context) error {
		return asDriver(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			w := worldFrom(ctx)
			_, err := client.SetContext(ctx, &quaycrewv1.SetContextRequest{
				Scope: "project", Owner: w.projectID, Body: "the bills are due on the first"})
			return err
		})
	})

	sc.Step(`^the driver asks to make a workspace named "([^"]*)"$`, func(ctx context.Context, name string) error {
		return asDriver(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			_, err := client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: name})
			return err
		})
	})

	sc.Step(`^the operator sets a secret with the system's token$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, err := w.client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
			Workspace: w.workspaceID, Key: "A_NAME", Value: "a-value-the-operator-sets"})
		authFrom(ctx).err = err
		return nil
	})

	sc.Step(`^the driver is refused, told the call is the operator's to make$`, func(ctx context.Context) error {
		err := authFrom(ctx).err
		if err == nil {
			return fmt.Errorf("the driver was served, want a refusal")
		}
		if code := status.Code(err); code != codes.PermissionDenied {
			return fmt.Errorf("the driver was refused with %v, want %v: %v", code, codes.PermissionDenied, err)
		}
		if message := status.Convert(err).Message(); !strings.Contains(message, "operator's to make") {
			return fmt.Errorf("the refusal %q does not say the call is the operator's to make", message)
		}
		return nil
	})

	sc.Step(`^the caller is refused, told the token is not this system's$`, func(ctx context.Context) error {
		if err := refusal(authFrom(ctx).err); err != nil {
			return err
		}
		if message := status.Convert(authFrom(ctx).err).Message(); !strings.Contains(message, "not this system's") {
			return fmt.Errorf("the refusal %q does not say the token is not this system's", message)
		}
		return nil
	})
}

// refusal says whether an answer is the refusal the system gives an unrecognised caller.
func refusal(err error) error {
	if err == nil {
		return fmt.Errorf("the caller was served, want a refusal")
	}
	if code := status.Code(err); code != codes.Unauthenticated {
		return fmt.Errorf("the caller was refused with %v, want %v: %v", code, codes.Unauthenticated, err)
	}
	return nil
}
