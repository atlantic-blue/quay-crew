package controlplane

import (
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DeniedToDriver is the deny policy for the driver's token: the calls that grant capability are the
// operator's to make, so a session that can drive the system can never grant itself anything. Secrets
// because a secret becomes readable inside whatever sandbox it reaches; skills because a skill
// carries mounts, secret names and a setup that executes; a session's permission mode because
// loosening its own is the plainest self grant there is; and the system's context because it is
// injected into every session, including the driver itself.
//
// Importing a flow graph is refused for the same reason importing a skill is: writing an automation
// down is the operator deciding what the system may do on its own. Starting a run of one the operator
// already imported is not refused, because a run is dispatch, which the driver already has: it can
// reach nothing through a flow that it could not reach by dispatching directly.
//
// A role is refused on the same line as a skill. It carries a brief, a model and the material a
// session is allowed to receive, so a session that could import or attach one could write itself a
// way of working nobody approved and then be run as it. Reading what the system already holds stays
// open, because choosing from what the operator attached is the point.
//
// A hook is refused on all four of its calls, and the listing is the one place this differs from a
// skill. A hook is a command that runs on the session's own tool use, so attaching one changes what
// every session in that workspace may do, and reading the list is reading the map of the guard the
// session is under. A skill is a capability a session already holds and uses by name, so choosing
// from the ones the operator attached is the point of listing them.
//
// Approving a design is refused because the word is the operator's. A session may write a design,
// and writing one grants it nothing: the write clears the approval, so a session that wrote a design
// has produced a text somebody still has to read. A session that could approve its own text would be
// agreeing with itself, and the gate would be a step in a script rather than a person's judgement.
//
// Everything the driver exists to do stays open: workspaces, projects, sessions, dispatch, starting
// context at the workspace and project scopes, and reading or writing a design.
func DeniedToDriver(fullMethod string, request any) error {
	switch fullMethod {
	case quaycrewv1.ControlPlaneService_SetSecret_FullMethodName,
		quaycrewv1.ControlPlaneService_ListSecrets_FullMethodName,
		quaycrewv1.ControlPlaneService_ImportSkill_FullMethodName,
		quaycrewv1.ControlPlaneService_AttachSkill_FullMethodName,
		quaycrewv1.ControlPlaneService_DetachSkill_FullMethodName,
		quaycrewv1.ControlPlaneService_ImportHook_FullMethodName,
		quaycrewv1.ControlPlaneService_ListHooks_FullMethodName,
		quaycrewv1.ControlPlaneService_AttachHook_FullMethodName,
		quaycrewv1.ControlPlaneService_DetachHook_FullMethodName,
		quaycrewv1.ControlPlaneService_SetSessionPermissionMode_FullMethodName,
		quaycrewv1.ControlPlaneService_ApproveDesign_FullMethodName:
		return refusedToDriver(fullMethod)
	case quaycrewv1.ControlPlaneService_SetContext_FullMethodName:
		if req, ok := request.(*quaycrewv1.SetContextRequest); ok && req.GetScope() == "system" {
			return refusedToDriver(fullMethod)
		}
	}
	return nil
}

func refusedToDriver(fullMethod string) error {
	name := shortMethod(fullMethod)
	return status.Error(codes.PermissionDenied, fmt.Sprintf(
		"the driver drives the system, it does not widen it: %s grants capability and is the operator's to make", name))
}

// shortMethod is the call's own name, without the service in front of it.
func shortMethod(fullMethod string) string {
	if i := strings.LastIndex(fullMethod, "/"); i >= 0 {
		return fullMethod[i+1:]
	}
	return fullMethod
}
