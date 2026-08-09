package controlplane

import (
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DeniedToDriver is the deny policy for the driver's token: the calls that grant capability are the
// operator's to make, so a session that can drive the crew can never grant itself anything. Secrets
// because a secret becomes readable inside whatever sandbox it reaches; skills because a skill
// carries mounts, secret names and a setup that executes; a session's permission mode because
// loosening its own is the plainest self grant there is; and the crew's context because it is
// injected into every session, including the driver itself.
//
// Importing a flow graph is refused for the same reason importing a skill is: writing an automation
// down is the operator deciding what the crew may do on its own. Starting a run of one the operator
// already imported is not refused, because a run is dispatch, which the driver already has: it can
// reach nothing through a flow that it could not reach by dispatching directly.
//
// Everything the driver exists to do stays open: workspaces, projects, threads, dispatch, starting
// a flow, and context at the workspace and project scopes.
func DeniedToDriver(fullMethod string, request any) error {
	switch fullMethod {
	case quaycrewv1.ControlPlaneService_SetSecret_FullMethodName,
		quaycrewv1.ControlPlaneService_ListSecrets_FullMethodName,
		quaycrewv1.ControlPlaneService_ImportSkill_FullMethodName,
		quaycrewv1.ControlPlaneService_AttachSkill_FullMethodName,
		quaycrewv1.ControlPlaneService_DetachSkill_FullMethodName,
		quaycrewv1.ControlPlaneService_SetThreadPermissionMode_FullMethodName,
		quaycrewv1.ControlPlaneService_ImportFlow_FullMethodName:
		return refusedToDriver(fullMethod)
	case quaycrewv1.ControlPlaneService_SetContext_FullMethodName:
		if req, ok := request.(*quaycrewv1.SetContextRequest); ok && req.GetScope() == "crew" {
			return refusedToDriver(fullMethod)
		}
	}
	return nil
}

func refusedToDriver(fullMethod string) error {
	name := fullMethod
	if i := strings.LastIndex(fullMethod, "/"); i >= 0 {
		name = fullMethod[i+1:]
	}
	return status.Error(codes.PermissionDenied, fmt.Sprintf(
		"the driver drives the crew, it does not widen it: %s grants capability and is the operator's to make", name))
}
