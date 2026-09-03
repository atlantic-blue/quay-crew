package controlplane_test

import (
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The driver is refused a named list and holds everything else, so a call added tomorrow is open to it
// unless somebody says otherwise. The list is the capability grants: what a session may not do is give
// itself or anybody else more capability than it was dispatched with.

// A hook is a command that runs on a session's own tool use, so a session that could attach one
// would change what every session in that workspace may do.
func TestTheDriverMayNotTouchAHook(t *testing.T) {
	for _, method := range []string{
		quaycrewv1.ControlPlaneService_ImportHook_FullMethodName,
		quaycrewv1.ControlPlaneService_ListHooks_FullMethodName,
		quaycrewv1.ControlPlaneService_AttachHook_FullMethodName,
		quaycrewv1.ControlPlaneService_DetachHook_FullMethodName,
	} {
		err := controlplane.DeniedToDriver(method, nil)
		if status.Code(err) != codes.PermissionDenied {
			t.Errorf("%s answered %v, want PermissionDenied", method, status.Code(err))
		}
	}
}

// What the driver exists to do stays open, or the deny list would be a system that cannot be driven.
func TestTheDriverStillDoesWhatItExistsToDo(t *testing.T) {
	for _, method := range []string{
		quaycrewv1.ControlPlaneService_Dispatch_FullMethodName,
		quaycrewv1.ControlPlaneService_CreateWorkspace_FullMethodName,
		quaycrewv1.ControlPlaneService_CreateProject_FullMethodName,
		quaycrewv1.ControlPlaneService_ListSkills_FullMethodName,
	} {
		if err := controlplane.DeniedToDriver(method, nil); err != nil {
			t.Errorf("%s was refused to the driver: %v", method, err)
		}
	}
}
