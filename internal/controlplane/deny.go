package controlplane

import (
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/auth"
	"github.com/atlantic-blue/krewe/internal/role"
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
// Everything the driver exists to do stays open: workspaces, projects, sessions, dispatch, starting
// a flow, and context at the workspace and project scopes.
func DeniedToDriver(fullMethod string, request any) error {
	switch fullMethod {
	case quaycrewv1.ControlPlaneService_SetSecret_FullMethodName,
		quaycrewv1.ControlPlaneService_ListSecrets_FullMethodName,
		quaycrewv1.ControlPlaneService_ImportSkill_FullMethodName,
		quaycrewv1.ControlPlaneService_AttachSkill_FullMethodName,
		quaycrewv1.ControlPlaneService_DetachSkill_FullMethodName,
		quaycrewv1.ControlPlaneService_ImportRole_FullMethodName,
		quaycrewv1.ControlPlaneService_AttachRole_FullMethodName,
		quaycrewv1.ControlPlaneService_DetachRole_FullMethodName,
		quaycrewv1.ControlPlaneService_ImportHook_FullMethodName,
		quaycrewv1.ControlPlaneService_ListHooks_FullMethodName,
		quaycrewv1.ControlPlaneService_AttachHook_FullMethodName,
		quaycrewv1.ControlPlaneService_DetachHook_FullMethodName,
		quaycrewv1.ControlPlaneService_SetSessionPermissionMode_FullMethodName,
		quaycrewv1.ControlPlaneService_ImportFlow_FullMethodName:
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

// DeniedToJob is the policy over what a job may call.
//
// Default deny, and the difference from the driver's policy is the direction: the driver is refused
// a named list and holds everything else, while a job holds a named list and is refused
// everything else. A credential minted for one job is the narrowest thing the system hands
// out, so it grants what its role declared and nothing beside it.
//
// The refusal names the verb, because a session that was refused has to know what to ask its
// operator for.
func DeniedToJob(fullMethod string, request any, grant auth.Grant) error {
	// Asking is not a verb, so no role has to grant it and none can withhold it. A session puts a
	// question about the job it is itself running, and the credential is already bound to that job,
	// so there is nothing here to check that the call does not check for itself. The alternative to
	// asking is guessing, and no role should be able to leave a session with only that.
	// Recording a step is not a verb either, and for the same shape of reason. A session says what it
	// finished on the job it is itself running, and the credential is already bound to that job. A role
	// that could withhold it would leave a job that can only ever be started again from nothing, which
	// is the second attempt paying for the first.
	// Handing over is the third of the same shape. A session at its workspace's context ceiling is
	// given no new task, and writing down what it leaves behind is the only way out of that: a role
	// that could withhold it would leave the session with nothing to do but carry on badly, which is
	// the failure the ceiling exists to end.
	if fullMethod == quaycrewv1.ControlPlaneService_AskJob_FullMethodName ||
		fullMethod == quaycrewv1.ControlPlaneService_RecordJobStep_FullMethodName ||
		fullMethod == quaycrewv1.ControlPlaneService_RecordJobHandoff_FullMethodName {
		return nil
	}
	verb, known := jobVerbs[fullMethod]
	if !known {
		return status.Errorf(codes.PermissionDenied,
			"a session running a job may call the job verbs and nothing else: %s is not one of them",
			shortMethod(fullMethod))
	}
	if !grant.May(verb) {
		return status.Errorf(codes.PermissionDenied,
			"this job runs as a role that may not %s; a role declares what it may do in its verbs list, "+
				"and an operator widens it by importing the role again and attaching it",
			verb)
	}
	// The job a caller names on a dispatch decides which credential the system mints for that task, so
	// only the operator may name one. A session that could name any job could mint itself
	// that job's grant.
	if named, ok := request.(*quaycrewv1.DispatchRequest); ok && named.GetJob() != "" {
		return status.Error(codes.PermissionDenied,
			"a session may not name the job a task runs for: the system reads that from the credential")
	}
	return nil
}

// jobVerbs is which verb each call needs. A call that is not here is not a job call, and a
// job may not make it.
//
// AnswerJob is deliberately absent. The verb `job.answer` exists and a role can be written that
// holds it, and no call is mapped to it, so what a session may do with that verb today is nothing.
// A question is put to a person, and a run that could answer its own question is a gate that
// decorates rather than holds.
//
// ResumeJob and RefuseJob are absent for the same reason and no verb exists for either. They are the
// two answers to a failure, and a session that could continue its own job would be deciding that its
// own failure was not about the work.
var jobVerbs = map[string]string{
	quaycrewv1.ControlPlaneService_CreateJob_FullMethodName: role.VerbJobCreate,
	quaycrewv1.ControlPlaneService_GetJob_FullMethodName:    role.VerbJobRead,
	quaycrewv1.ControlPlaneService_ListJobs_FullMethodName:  role.VerbJobRead,
	// A history is a digest of jobs, which is strictly less than GetJob already returns, so it needs
	// the verb that exists rather than a second one meaning the same thing.
	quaycrewv1.ControlPlaneService_GetHistory_FullMethodName: role.VerbJobRead,
	quaycrewv1.ControlPlaneService_StopJob_FullMethodName:    role.VerbJobStop,
}

// shortMethod is the call's own name, without the service in front of it.
func shortMethod(fullMethod string) string {
	if i := strings.LastIndex(fullMethod, "/"); i >= 0 {
		return fullMethod[i+1:]
	}
	return fullMethod
}
