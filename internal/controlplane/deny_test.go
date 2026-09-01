package controlplane_test

import (
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/auth"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/role"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The two policies point in opposite directions, and that is the design. The driver is refused a
// named list and holds everything else. A job holds a named list and is refused everything
// else, which is what makes its credential the narrowest thing the system hands out.

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
		quaycrewv1.ControlPlaneService_StartFlow_FullMethodName,
		quaycrewv1.ControlPlaneService_ListSkills_FullMethodName,
	} {
		if err := controlplane.DeniedToDriver(method, nil); err != nil {
			t.Errorf("%s was refused to the driver: %v", method, err)
		}
	}
}

// A job may call the verbs its role declared, and the call it needs for each is the one
// the system maps it to.
func TestAJobMayCallWhatItsRoleDeclared(t *testing.T) {
	grant := auth.Grant{Job: "job-1", Verbs: []string{role.VerbJobCreate, role.VerbJobRead}}

	for _, method := range []string{
		quaycrewv1.ControlPlaneService_CreateJob_FullMethodName,
		quaycrewv1.ControlPlaneService_GetJob_FullMethodName,
		quaycrewv1.ControlPlaneService_ListJobs_FullMethodName,
	} {
		if err := controlplane.DeniedToJob(method, nil, grant); err != nil {
			t.Errorf("%s was refused to a job whose role declared it: %v", method, err)
		}
	}
}

// The refusal names the verb, because a session that was refused has to know what to ask for.
func TestAJobIsRefusedTheVerbsItsRoleDidNotDeclare(t *testing.T) {
	grant := auth.Grant{Job: "job-1", Verbs: []string{role.VerbJobRead}}

	err := controlplane.DeniedToJob(quaycrewv1.ControlPlaneService_CreateJob_FullMethodName, nil, grant)

	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("the refusal is %v, want PermissionDenied", status.Code(err))
	}
	if !strings.Contains(err.Error(), role.VerbJobCreate) {
		t.Fatalf("the refusal says %q, want it to name the verb", err)
	}
	if !strings.Contains(err.Error(), "verbs list") {
		t.Fatalf("the refusal says %q, want it to say where the grant is written", err)
	}
}

// Everything outside the four verbs is refused, which is the whole difference from the driver: this
// credential holds a list rather than everything but a list.
func TestAJobIsRefusedEveryCallThatIsNotAJobVerb(t *testing.T) {
	// A grant holding every verb there is, so what is refused here is refused by kind rather than by
	// the grant being narrow.
	grant := auth.Grant{Job: "job-1", Verbs: role.Grantable}

	for _, method := range []string{
		quaycrewv1.ControlPlaneService_Dispatch_FullMethodName,
		quaycrewv1.ControlPlaneService_CreateWorkspace_FullMethodName,
		quaycrewv1.ControlPlaneService_CreateProject_FullMethodName,
		quaycrewv1.ControlPlaneService_SetSecret_FullMethodName,
		quaycrewv1.ControlPlaneService_ListSecrets_FullMethodName,
		quaycrewv1.ControlPlaneService_ImportRole_FullMethodName,
		quaycrewv1.ControlPlaneService_AttachRole_FullMethodName,
		quaycrewv1.ControlPlaneService_ImportHook_FullMethodName,
		quaycrewv1.ControlPlaneService_AttachHook_FullMethodName,
		quaycrewv1.ControlPlaneService_ImportSkill_FullMethodName,
		quaycrewv1.ControlPlaneService_SetWorkspaceLimits_FullMethodName,
		quaycrewv1.ControlPlaneService_GetWorkspaceLimits_FullMethodName,
		quaycrewv1.ControlPlaneService_StartFlow_FullMethodName,
		quaycrewv1.ControlPlaneService_ListSessions_FullMethodName,
	} {
		err := controlplane.DeniedToJob(method, nil, grant)
		if status.Code(err) != codes.PermissionDenied {
			t.Errorf("%s answered %v to a job, want PermissionDenied", method, status.Code(err))
		}
	}
}

// A session that could raise its own ceiling has none, so the limits are the operator's alone.
func TestAJobMayNotRaiseItsOwnCeiling(t *testing.T) {
	grant := auth.Grant{Job: "job-1", Verbs: role.Grantable}

	err := controlplane.DeniedToJob(
		quaycrewv1.ControlPlaneService_SetWorkspaceLimits_FullMethodName, nil, grant)

	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a job may set the limits: %v", err)
	}
}

// The job a task runs for decides which credential the system mints, so a session naming one would be
// minting itself that job's grant.
func TestAJobMayNotNameTheJobATaskRunsFor(t *testing.T) {
	grant := auth.Grant{Job: "job-1", Verbs: role.Grantable}

	err := controlplane.DeniedToJob(quaycrewv1.ControlPlaneService_CreateJob_FullMethodName,
		&quaycrewv1.DispatchRequest{Job: "job-2"}, grant)

	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a session named the job a task runs for: %v", err)
	}
	if !strings.Contains(err.Error(), "credential") {
		t.Fatalf("the refusal says %q, want it to say where the system reads it from", err)
	}
}

// A grant holding nothing calls nothing, which is what a role that declared no verbs list becomes.
func TestAGrantThatHoldsNothingCallsNothing(t *testing.T) {
	grant := auth.Grant{Job: "job-1"}

	for _, method := range []string{
		quaycrewv1.ControlPlaneService_CreateJob_FullMethodName,
		quaycrewv1.ControlPlaneService_GetJob_FullMethodName,
		quaycrewv1.ControlPlaneService_ListJobs_FullMethodName,
		quaycrewv1.ControlPlaneService_StopJob_FullMethodName,
	} {
		if err := controlplane.DeniedToJob(method, nil, grant); status.Code(err) != codes.PermissionDenied {
			t.Errorf("%s answered %v to a grant holding nothing", method, status.Code(err))
		}
	}
}
