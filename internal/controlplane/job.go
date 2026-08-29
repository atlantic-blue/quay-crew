package controlplane

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/auth"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CreateJob declares a job and answers with the record the crew kept.
//
// Every rule is checked here, at the moment of the write, while the caller is looking. A refusal
// that arrives hours later, inside a run, has nothing to point back at.
//
// Nothing runs the job. This call records intent, and that is the whole of it: a controller is a
// slice of its own. What this buys on its own is that the intent outlives the caller.
func (s *Server) CreateJob(ctx context.Context, req *quaycrewv1.CreateJobRequest) (*quaycrewv1.CreateJobResponse, error) {
	declaration := job.Declaration{
		Project: req.GetProject(),
		Title:   req.GetTitle(), Brief: req.GetBrief(), Role: req.GetRole(), Mode: req.GetMode(),
		ExpectFile: req.GetExpectFile(), ExpectContains: req.GetExpectContains(),
		After: req.GetAfter(), BudgetTokens: req.GetBudgetTokens(), Labels: req.GetLabels(),
		Requires: req.GetRequires(),
		ID:       req.GetId(), Parent: req.GetParent(),
	}
	if req.GetDeadline() != nil {
		at := req.GetDeadline().AsTime()
		declaration.Deadline = &at
	}
	declared, declaredEvent, err := s.PrepareJob(ctx, "", declaration)
	if err != nil {
		return nil, err
	}
	if err := s.store.CreateJob(ctx, declared, declaredEvent); err != nil {
		return nil, storeError(err, "create job")
	}
	// After the transaction, never inside it. The store is the truth and the log is the copy, so an
	// export that cannot land is dropped and the record stands.
	s.ExportJob(ctx, declaredEvent)
	// Read back rather than answered from memory, so the caller is given what the store holds,
	// stamped with the store's own clock.
	kept, err := s.store.GetJob(ctx, declared.ID)
	if err != nil {
		return nil, storeError(err, "job")
	}
	return &quaycrewv1.CreateJobResponse{Job: asJob(kept)}, nil
}

// PrepareJob holds a declaration to every rule and answers with the row to write and the record of
// writing it. It writes nothing: the caller decides which transaction the row lands in, which is what
// lets a flow run declare its step in the same transaction as the movement that asked for it.
//
// `under` names the job this one hangs under, and empty means the parent comes from the
// credential the caller presented. Only the crew itself passes one, and only ever an identifier it
// read off a row of its own: a caller that could name its own parent could name none and start again
// at the top, which is why a parent in a request is refused rather than ignored.
func (s *Server) PrepareJob(ctx context.Context, under string, declaration job.Declaration) (*job.Job, *job.Event, error) {
	// Before the project is read, so a caller that got the shape wrong is told what is wrong with
	// the shape rather than being sent to look for a project.
	if err := declaration.Validate(); err != nil {
		return nil, nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// Where the job runs, and a session names none: it holds a credential minted for the job
	// it is running, and that credential says which project that job is in. It is the same
	// reading as the parent below, and for the same reason. A session cannot resolve an address
	// anyway, because resolving one means listing workspaces and projects, and the verbs a role
	// grants are the four job verbs and nothing else.
	if declaration.Project == "" {
		if grant, carried := auth.GrantFrom(ctx); carried {
			declaration.Project = grant.Project
		}
	}
	if declaration.Project == "" {
		return nil, nil, status.Error(codes.InvalidArgument,
			"job needs a project to run in: say where with an address, for example quay job create me/house-bills")
	}
	project, err := s.store.GetProject(ctx, declaration.Project)
	if err != nil {
		return nil, nil, storeError(err, "project")
	}

	tidy := declaration.Tidied()
	declared := &job.Job{
		ID: store.NewID(), Workspace: project.GetWorkspace(), Project: project.GetId(),
		Title: tidy.Title, Brief: tidy.Brief, Mode: tidy.NamedMode(),
		ExpectFile: tidy.ExpectFile, ExpectContains: tidy.ExpectContains,
		After: tidy.After, Deadline: tidy.Deadline, BudgetTokens: tidy.BudgetTokens,
		Labels: tidy.Labels, Requires: tidy.Requires,
		Version: 1, Phase: job.PhasePending,
	}
	if err := s.underTheCaller(ctx, under, declared); err != nil {
		return nil, nil, err
	}
	// And the trace follows the same parent, because one trace covers a whole tree. It is read after
	// the parent is known rather than before: a child that minted its own would leave the tree in as
	// many traces as it has nodes.
	s.traceJob(ctx, declared, s.parentOf(ctx, declared))
	if err := s.pinRole(ctx, declared, tidy.Role); err != nil {
		return nil, nil, err
	}
	if err := s.checkAfter(ctx, declared); err != nil {
		return nil, nil, err
	}
	return declared, s.jobEvent(ctx, declared, job.EventDeclared, declared.Title), nil
}

// pinRole attaches the role at the version the workspace holds now, and refuses job the role could
// not be given the material for.
//
// The version is pinned the way a run pins its graph: editing a role tomorrow cannot change job
// that is already declared. A role the workspace does not hold is refused by name here, while
// somebody is looking, rather than when a session that cannot be built is asked for. What the job
// requires is held against what the role receives for the same reason, and it is checked again at
// the dispatch, because a role can be detached, imported again and attached again while job sits
// pending.
//
// Job that names no role requires its material of nobody, so nothing here applies to it.
func (s *Server) pinRole(ctx context.Context, declared *job.Job, named string) error {
	if named == "" {
		return nil
	}
	held, err := s.roleFor(ctx, declared.Workspace, named)
	if err != nil {
		return err
	}
	if material := job.Unreceived(declared.Requires, held); material != "" {
		return status.Error(codes.FailedPrecondition, job.RefusedMaterial(held.Name, material))
	}
	declared.Role, declared.RoleVersion = held.Name, held.Version
	return nil
}

// checkAfter refuses an ordering that could never come due: job that waits for something the crew
// does not hold, or a loop of jobs waiting on one another.
func (s *Server) checkAfter(ctx context.Context, declared *job.Job) error {
	for _, id := range declared.After {
		if _, err := s.store.GetJob(ctx, id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return status.Errorf(codes.InvalidArgument,
					"this job waits for %s, which the crew does not hold: read the identifier off quay job list, "+
						"or declare that job first", id)
			}
			return storeError(err, "job")
		}
	}
	// A caller cannot reach this today, because the crew assigns every identifier and what a
	// job waits for must already exist. It is the guard for the first thing that rewrites the
	// ordering, and a loop would otherwise sit pending forever with nothing saying why.
	dependsOn := func(id string) []string {
		found, err := s.store.GetJob(ctx, id)
		if err != nil {
			return nil
		}
		return found.After
	}
	if from, to, found := job.Cycle(declared.ID, declared.After, dependsOn); found {
		return status.Errorf(codes.InvalidArgument,
			"this ordering closes a loop: %s waits for %s, which waits back round to it. Job in a loop never comes due, "+
				"so break the chain and declare one of them without the other", from, to)
	}
	return nil
}

// GetJob reads one job back, whole, its answer included.
func (s *Server) GetJob(ctx context.Context, req *quaycrewv1.GetJobRequest) (*quaycrewv1.GetJobResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "which job: give the identifier quay job list prints")
	}
	found, err := s.store.GetJob(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound,
				"the crew holds no job %s: quay job list says what there is", req.GetId())
		}
		return nil, storeError(err, "job")
	}
	return &quaycrewv1.GetJobResponse{Job: asJob(found)}, nil
}

// ListJob says what the crew holds, newest first and without answers.
func (s *Server) ListJobs(ctx context.Context, req *quaycrewv1.ListJobsRequest) (*quaycrewv1.ListJobsResponse, error) {
	if phase := req.GetPhase(); phase != "" && !job.KnownPhase(phase) {
		return nil, status.Errorf(codes.InvalidArgument,
			"%q is not a phase; use one of %s", phase, strings.Join(job.Phases(), ", "))
	}
	listed, err := s.store.ListJobs(ctx, job.Filter{
		Workspace: req.GetWorkspace(), Project: req.GetProject(),
		Parent: req.GetParent(), Root: req.GetRootsOnly(), Phase: req.GetPhase(),
		LabelKey: req.GetLabelKey(), LabelValue: req.GetLabelValue(),
	})
	if err != nil {
		return nil, storeError(err, "job")
	}
	out := make([]*quaycrewv1.Job, 0, len(listed))
	for _, one := range listed {
		out = append(out, asJob(one))
	}
	return &quaycrewv1.ListJobsResponse{Jobs: out}, nil
}

// StopJob halts job that has not ended, keeping the reason.
//
// Job that already ended is refused rather than overwritten, for the reason a stopped flow run
// gives: how it ended is the useful part, and a second stop erases it.
func (s *Server) StopJob(ctx context.Context, req *quaycrewv1.StopJobRequest) (*quaycrewv1.StopJobResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "which job: give the identifier quay job list prints")
	}
	reason := strings.TrimSpace(req.GetReason())
	if reason == "" {
		reason = "stopped by the operator"
	}
	found, err := s.store.GetJob(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound,
				"the crew holds no job %s: quay job list says what there is", req.GetId())
		}
		return nil, storeError(err, "job")
	}

	stoppedEvent := s.jobEvent(ctx, found, job.EventStopped, reason)
	stopped, err := s.store.StopJob(ctx, found.ID, reason, stoppedEvent)
	if err != nil {
		if strings.Contains(err.Error(), "already ended") {
			return nil, status.Errorf(codes.FailedPrecondition,
				"job %s is %s already, and a job that already ended is not stopped again", found.ID, found.Phase)
		}
		return nil, storeError(err, "stop job")
	}
	s.ExportJob(ctx, stoppedEvent)
	return &quaycrewv1.StopJobResponse{Job: asJob(stopped)}, nil
}

// jobEvent builds one record of what happened, redacted.
//
// The detail can carry whatever a caller typed, and everything recorded here is persisted, so it
// goes through the same redactor a task's reply does. What the crew can know is every value the
// workspace keeps sealed.
func (s *Server) jobEvent(ctx context.Context, of *job.Job, kind, detail string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: kind, Job: of.ID,
		Workspace: of.Workspace, Project: of.Project, Parent: of.Parent, Depth: of.Depth,
		Detail:     oneShortLine(model.Redact(detail, s.sealedForWorkspace(ctx, of.Workspace))),
		TraceID:    of.TraceID,
		OccurredAt: timestamppb.Now().AsTime(),
	}
}

// asJob puts a job on the wire.
func asJob(from *job.Job) *quaycrewv1.Job {
	on := &quaycrewv1.Job{
		Id: from.ID, Workspace: from.Workspace, Project: from.Project,
		Title: from.Title, Brief: from.Brief, Role: from.Role, RoleVersion: int32(from.RoleVersion),
		Mode: from.Mode, ExpectFile: from.ExpectFile, ExpectContains: from.ExpectContains,
		After: from.After, BudgetTokens: from.BudgetTokens, Labels: from.Labels,
		Requires: from.Requires,
		Parent:   from.Parent, Depth: int32(from.Depth), Version: int32(from.Version),
		Phase: from.Phase, Session: from.Session, Attempts: int32(from.Attempts),
		Answer: from.Answer, Reason: from.Reason, Question: from.Question,
		SpentTokens: from.SpentTokens, ObservedVersion: int32(from.ObservedVersion),
		TraceId: from.TraceID, ParentSpanId: from.ParentSpanID,
		CreatedAt: timestamppb.New(from.CreatedAt), UpdatedAt: timestamppb.New(from.UpdatedAt),
	}
	if from.Deadline != nil {
		on.Deadline = timestamppb.New(*from.Deadline)
	}
	if from.StartedAt != nil {
		on.StartedAt = timestamppb.New(*from.StartedAt)
	}
	if from.FinishedAt != nil {
		on.FinishedAt = timestamppb.New(*from.FinishedAt)
	}
	return on
}

// RunJobController makes reality match the job the crew holds, until ctx is done. It blocks, so
// the caller runs it in a goroutine and owns its lifetime.
//
// Declared job is a row rather than a call somebody is holding, which is what makes it survive a
// restart: this reads the rows on the way up, so a crew restarted onto job declared while it was
// down starts that job now rather than losing it.
func (s *Server) RunJobController(ctx context.Context) {
	s.jobController.Run(ctx)
}

// TickJob moves the job the crew holds on by one step. Exported so a test and a scenario drive one
// tick rather than waiting for a ticker, which would be slow when it passed and flaky when it did not.
func (s *Server) TickJob(ctx context.Context) {
	s.jobController.Tick(ctx)
}

// RedactFor removes anything the workspace keeps sealed from a line the crew is about to write down.
// What a model says can carry a value somebody pasted into a conversation, and everything recorded
// here is persisted.
func (s *Server) RedactFor(ctx context.Context, workspace, text string) string {
	return model.Redact(text, s.sealedForWorkspace(ctx, workspace))
}

// JobLease is how long a controller holds a job, from the crew's configuration.
//
// The default is derived from what a tick costs rather than chosen, so a crew that says nothing gets
// a measured number. A value that is not a duration is refused by falling back to that default and
// saying so, because a crew that will not start over a misspelled interval is worse than a crew
// running the number it was already running.
func JobLease(configured string, logger *slog.Logger) time.Duration {
	value := strings.TrimSpace(configured)
	if value == "" {
		return job.DefaultLease
	}
	lease, err := time.ParseDuration(value)
	if err != nil || lease <= 0 {
		if logger != nil {
			logger.Warn("QC_JOB_LEASE is not a length of time, so the measured default is used instead",
				"configured", value, "using", job.DefaultLease)
		}
		return job.DefaultLease
	}
	return lease
}

// ControllerName is what this crew writes on the leases its controller takes.
//
// The host name, because an investigator reading a released record wants to know which machine
// stopped. A crew that cannot say answers with nothing, and the controller mints itself a name.
func ControllerName(hostname func() (string, error)) string {
	name, err := hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return ""
	}
	return "controlplane-" + strings.TrimSpace(name)
}

// underTheCaller reads the parent from the credential the caller presented, and holds the result to
// the workspace's ceiling.
//
// An operator's call carries no credential of that kind and so declares a root. A session running a
// job carries one, and everything it declares hangs under that job, one level deeper. The
// caller cannot say otherwise, which is why depth bounds anything at all: job at depth d creates at
// depth d+1, so a loop of any shape stops at the limit.
// parentOf is the job this one hangs under, and nil for a root.
//
// Read after underTheCaller has set the parent, because the parent is the credential's to say and
// never the request's. It exists so a child can inherit its parent's trace: one trace covers a whole
// tree, and a child that minted its own would leave the tree in as many traces as it has nodes.
func (s *Server) parentOf(ctx context.Context, declared *job.Job) *job.Job {
	if declared.Parent == "" {
		return nil
	}
	parent, err := s.store.GetJob(ctx, declared.Parent)
	if err != nil {
		// The parent was read a moment ago to set the depth, so this is a store that went away
		// between the two. The job is declared either way and takes a trace of its own, which is a
		// tree in two traces rather than a job nobody has.
		slog.WarnContext(ctx, "the parent of this job could not be read, so it starts a trace of its own",
			"job", declared.ID, "parent", declared.Parent, "error", err)
		return nil
	}
	return parent
}

func (s *Server) underTheCaller(ctx context.Context, under string, declared *job.Job) error {
	// The crew declaring job for something it is already running, which today is one thing: a step
	// of a flow run. The ceiling was checked when the run's own job was declared, against the
	// credential of whoever started it, so a step is not a second chance to cross it. What bounds the
	// steps themselves is the graph: it is a finite set of nodes with a transition cap, and a run
	// started from inside a session is bounded by the check that session already passed.
	if under != "" {
		parent, err := s.store.GetJob(ctx, under)
		if err != nil {
			return storeError(err, "the job this one hangs under")
		}
		declared.Parent, declared.Depth = parent.ID, parent.Depth+1
		return nil
	}
	if grant, carried := auth.GrantFrom(ctx); carried && grant.Job != "" {
		parent, err := s.store.GetJob(ctx, grant.Job)
		if err != nil {
			return storeError(err, "the job this session is running")
		}
		declared.Parent, declared.Depth = parent.ID, parent.Depth+1
	}
	limits, err := s.store.WorkspaceLimits(ctx, declared.Workspace)
	if err != nil {
		return storeError(err, "the workspace's limits")
	}
	if declared.Depth > limits.MaxDepth {
		return status.Errorf(codes.PermissionDenied,
			"this workspace allows job no deeper than %d, and this would be at depth %d. "+
				"Raise it with quay limits <workspace> --max-depth %d, which an operator does deliberately: "+
				"a session that could raise its own ceiling has none",
			limits.MaxDepth, declared.Depth, declared.Depth)
	}
	return nil
}
