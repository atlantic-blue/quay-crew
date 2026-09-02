package controlplane

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/auth"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/role"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CreateJob declares a job and answers with the record the system kept.
//
// Every rule is checked here, at the moment of the write, while the caller is looking. A refusal
// that arrives hours later, inside a run, has nothing to point back at.
//
// Nothing runs the job. This call records intent, and that is the whole of it: a controller is a
// slice of its own. What this buys on its own is that the intent outlives the caller.
func (s *Server) CreateJob(ctx context.Context, req *quaycrewv1.CreateJobRequest) (*quaycrewv1.CreateJobResponse, error) {
	declaration := job.Declaration{
		Project: req.GetProject(), Request: req.GetRequest(),
		Title: req.GetTitle(), Brief: req.GetBrief(), Role: req.GetRole(), Mode: req.GetMode(),
		ExpectFile: req.GetExpectFile(), ExpectContains: req.GetExpectContains(),
		After: req.GetAfter(), BudgetTokens: req.GetBudgetTokens(), Labels: req.GetLabels(),
		Requires: req.GetRequires(), Repository: req.GetRepository(), Product: req.GetProduct(),
		Claim: req.GetClaim(), Escalation: req.GetEscalation(),
		Ungated: req.GetUngated(),
		ID:      req.GetId(), Parent: req.GetParent(),
	}
	if req.GetDeadline() != nil {
		at := req.GetDeadline().AsTime()
		declaration.Deadline = &at
	}
	// The brief is held to what a job can do, here rather than in PrepareJob, because a flow declares
	// its steps through that call too and a step is not a job somebody wrote. The graph around a step
	// holds the wait, so its last node merges the pull request and means it.
	if err := job.Declared(declaration.Brief); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	declared, declaredEvent, err := s.PrepareJob(ctx, "", declaration)
	if err != nil {
		return nil, err
	}
	if err := s.store.CreateJob(ctx, declared, declaredEvent); err != nil {
		// The one refusal that cannot be decided before the write, because what it depends on is
		// another row that another caller may be writing at the same moment. It names the job holding
		// the work rather than saying the claim is taken: a caller told the claim is taken goes looking
		// for who has it, and a caller told which job has it opens that job.
		var held *job.Held
		if errors.As(err, &held) {
			return nil, status.Error(codes.FailedPrecondition, held.Refusal(time.Now().UTC()))
		}
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
	return &quaycrewv1.CreateJobResponse{
		Job: asJob(kept), LeftOut: s.jobLeftOut(ctx, declared),
		// Said here, at the write, because this is the last moment somebody is looking at the
		// declaration. It is empty for a brief that carries what was asked for, and empty is the point:
		// a check that speaks about every brief puts a person in front of every job.
		//
		// The request this declaration stated, never the one it inherited. A child builds one slice of
		// the work and cannot carry the whole sentence, so measuring an inherited request would fire on
		// ordinary work, and a check that fires on ordinary work is the rule everybody words around.
		// What is measured is the one hop this exists for: a person said a sentence, somebody wrote a
		// brief from it, and the two are here together.
		Drifted: job.Drifted(declaration.Tidied().Request, kept.Brief),
	}, nil
}

// jobLeftOut is every skill the session running this job will be born without, because the workspace
// has not set a secret that skill needs.
//
// The system already knows this at the moment of the write, and until now it said so in one listing
// nobody is required to read. A workspace with no credential accepts a whole tree of jobs, every
// session in it dies on its first clone, and the budget is spent before anything says why.
//
// It is an answer rather than a refusal, because the system cannot know which skill a brief will reach
// for: a job that reads an electricity bill is not wrong to run in a workspace with no forge token,
// and refusing it would make one unset secret enough to stop every job in the workspace. That is the
// same trade withoutUnusable already made for the session itself.
//
// A role that does not receive skills is given none of them by design, so there is nothing missing to
// report. A job that names no role is a session that receives everything, which is what receives says.
func (s *Server) jobLeftOut(ctx context.Context, declared *job.Job) []*quaycrewv1.Skill {
	if declared.Role != "" {
		held, err := s.roleFor(ctx, declared.Workspace, declared.Role)
		if err != nil || !held.Gets(role.MaterialSkills) {
			return nil
		}
	}
	var out []*quaycrewv1.Skill
	for _, one := range s.leftOutIn(ctx, declared.Workspace) {
		carried := skillAsProto(one.Skill)
		carried.LeftOut = one.Why
		out = append(out, carried)
	}
	return out
}

// PrepareJob holds a declaration to every rule and answers with the row to write and the record of
// writing it. It writes nothing: the caller decides which transaction the row lands in, which is what
// lets a flow run declare its step in the same transaction as the movement that asked for it.
//
// `under` names the job this one hangs under, and empty means the parent comes from the
// credential the caller presented. Only the system itself passes one, and only ever an identifier it
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
			"job needs a project to run in: say where with an address, for example krewe job create me/house-bills")
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
		Labels: tidy.Labels, Requires: tidy.Requires, Repository: tidy.Repository,
		Product: tidy.Product, Request: tidy.Request, Claim: tidy.Claim, Escalation: tidy.Escalation,
		Ungated: tidy.Ungated,
		Version: 1, Phase: job.PhasePending,
	}
	// Where the work lands, when the declaration did not say. It is the project's, because a project
	// is a body of work and the repository is where that body of work goes: the acceptance run had a
	// repository the system held no record of, so every job had to be told the address again and a
	// session told to push had nowhere to push to.
	//
	// A job that named its own keeps it. The project's is the default, not a ceiling.
	if declared.Repository == "" {
		declared.Repository = project.GetRepository()
	}
	// The mode against the repository, once the repository is settled. A repository is reached over the
	// network and the narrower modes ask a person before they run a network command, so a job that
	// carries one and cannot reach it spends a session and stops holding work nobody can read. Both
	// facts are here at the moment of the write, and the refusal costs nothing.
	//
	// Held here as well as in the declaration because both halves can arrive without anybody typing
	// them: the repository comes from the project, and the mode comes from the system.
	if err := s.modeReachesTheRepository(declared); err != nil {
		return nil, nil, err
	}
	if err := s.underTheCaller(ctx, under, declared); err != nil {
		return nil, nil, err
	}
	// The parent, read once and used twice: the trace follows it, and so does the one sentence the
	// job serves. Both are read after underTheCaller has set it, because the parent is the
	// credential's to say and never the request's.
	parent := s.parentOf(ctx, declared)
	// One trace covers a whole tree, and a child that minted its own would leave the tree in as many
	// traces as it has nodes.
	s.traceJob(ctx, declared, parent)
	if err := carryTheSentence(declared, parent); err != nil {
		return nil, nil, err
	}
	if err := carryTheRequest(declared, parent); err != nil {
		return nil, nil, err
	}
	if err := s.pinRole(ctx, declared, tidy.Role); err != nil {
		return nil, nil, err
	}
	if err := s.holdsTheEscalationRole(ctx, declared); err != nil {
		return nil, nil, err
	}
	if err := s.checkAfter(ctx, declared); err != nil {
		return nil, nil, err
	}
	return declared, s.jobEvent(ctx, declared, job.EventDeclared, declared.Title), nil
}

// carryTheSentence gives a child the one sentence its parent serves, and refuses a child that states
// a different one.
//
// The sentence belongs to the job at the top. Every job under it carries the same one, so a session
// three levels down is given what a person does with what is being built, without anybody typing it
// again. That is the half that makes the sentence worth having: the failure it answers happened
// inside jobs that never saw it.
//
// A root, and a child under a parent that carries none, keep whatever they declared. A tree that
// started without a sentence can still gain one.
func carryTheSentence(declared *job.Job, parent *job.Job) error {
	if parent == nil {
		return nil
	}
	carried, err := job.Inherited(parent.Product, declared.Product)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	declared.Product = carried
	return nil
}

// carryTheRequest gives a child the request its parent was asked in, and refuses a child that states
// a different one.
//
// It is the rule carryTheSentence gives the product, and for the same reason: a tree with two
// requests has none. The half that makes it worth having is that a session three levels down reads
// what was asked for, in the words it was asked in, without anybody typing it again.
func carryTheRequest(declared *job.Job, parent *job.Job) error {
	if parent == nil {
		return nil
	}
	carried, err := job.InheritedRequest(parent.Request, declared.Request)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	declared.Request = carried
	return nil
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

// holdsTheEscalationRole refuses a job that would be handed to a role the workspace does not hold.
//
// Held here, at the write, for the reason every other rule is: a route that names nobody is found the
// moment the job goes in circles, which is the moment there is least to spare. The version is not
// pinned, unlike the role the job runs as, because the handoff happens later and the role it lands on
// should be the one the workspace holds then.
func (s *Server) holdsTheEscalationRole(ctx context.Context, declared *job.Job) error {
	route, err := job.ReadRoute(declared.Escalation)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if route.Word != job.RouteRole {
		return nil
	}
	if _, err := s.roleFor(ctx, declared.Workspace, route.To); err != nil {
		return err
	}
	return nil
}

// checkAfter refuses an ordering that could never come due: job that waits for something the system
// does not hold, or a loop of jobs waiting on one another.
func (s *Server) checkAfter(ctx context.Context, declared *job.Job) error {
	for _, id := range declared.After {
		if _, err := s.store.GetJob(ctx, id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return status.Errorf(codes.InvalidArgument,
					"this job waits for %s, which the system does not hold: read the identifier off krewe job list, "+
						"or declare that job first", id)
			}
			return storeError(err, "job")
		}
	}
	// A caller cannot reach this today, because the system assigns every identifier and what a
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
		return nil, status.Error(codes.InvalidArgument, "which job: give the identifier krewe job list prints")
	}
	found, err := s.store.GetJob(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound,
				"the system holds no job %s: krewe job list says what there is", req.GetId())
		}
		return nil, storeError(err, "job")
	}
	return &quaycrewv1.GetJobResponse{Job: asJob(found)}, nil
}

// ListJob says what the system holds, newest first and without answers.
func (s *Server) ListJobs(ctx context.Context, req *quaycrewv1.ListJobsRequest) (*quaycrewv1.ListJobsResponse, error) {
	if phase := req.GetPhase(); phase != "" && !job.KnownPhase(phase) {
		return nil, status.Errorf(codes.InvalidArgument,
			"%q is not a phase; use one of %s", phase, strings.Join(job.Phases(), ", "))
	}
	// Held to the same four words the session was offered. A filter that took any word would answer
	// nothing for a typo, and a listing that says nothing reads exactly like a system with no such
	// jobs in it.
	if outcome := req.GetOutcome(); outcome != "" && !job.KnownOutcome(outcome) {
		return nil, status.Errorf(codes.InvalidArgument,
			"%q is not an outcome; use one of %s", outcome, strings.Join(job.Outcomes(), ", "))
	}
	if req.GetLimit() < 0 {
		return nil, status.Errorf(codes.InvalidArgument,
			"a listing cannot return %d jobs; leave the limit out for every row, or give a count above zero",
			req.GetLimit())
	}
	filter := job.Filter{
		Workspace: req.GetWorkspace(), Project: req.GetProject(),
		Parent: req.GetParent(), Root: req.GetRootsOnly(), Phase: req.GetPhase(),
		Outcome:  req.GetOutcome(),
		LabelKey: req.GetLabelKey(), LabelValue: req.GetLabelValue(),
		Limit: int(req.GetLimit()),
	}
	if since := req.GetFinishedSince(); since != nil {
		at := since.AsTime()
		filter.FinishedSince = &at
	}
	listed, err := s.store.ListJobs(ctx, filter)
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
		return nil, status.Error(codes.InvalidArgument, "which job: give the identifier krewe job list prints")
	}
	reason := strings.TrimSpace(req.GetReason())
	if reason == "" {
		reason = "stopped by the operator"
	}
	found, err := s.store.GetJob(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound,
				"the system holds no job %s: krewe job list says what there is", req.GetId())
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
	// The job is over, so the credentials minted for it are too. Without this a session whose job an
	// operator stopped would keep calling until its credential ran out on its own.
	s.RevokeJobCredentials(stopped.ID, stopped.Phase)
	return &quaycrewv1.StopJobResponse{Job: asJob(stopped)}, nil
}

// jobEvent builds one record of what happened, redacted.
//
// The detail can carry whatever a caller typed, and everything recorded here is persisted, so it
// goes through the same redactor a task's reply does. What the system can know is every value the
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
		Requires: from.Requires, Repository: from.Repository, PullRequest: from.PullRequest, Claim: from.Claim,
		Product: from.Product, Request: from.Request, Steers: int32(from.Steers),
		Ungated: from.Ungated, Reviewed: from.Reviewed, Tested: from.Tested,
		Plan: from.Plan, PlanApproved: from.PlanApproved,
		Ideation: from.Ideation, IdeationAnswer: from.IdeationAnswer,
		Parent: from.Parent, Depth: int32(from.Depth), Version: int32(from.Version),
		Phase: from.Phase, Session: from.Session, Attempts: int32(from.Attempts),
		Answer: from.Answer, Outcome: from.Outcome,
		Reason: from.Reason, Question: from.Question, Resuming: from.Resuming,
		Escalation: from.Escalation, LoopedStep: int32(from.LoopedStep), EscalatedTo: from.EscalatedTo,
		Attempted:   asJobAttempts(from.Attempted),
		Steps:       asJobSteps(from.Steps),
		Handoffs:    asJobHandoffs(from.Handoffs),
		Questions:   asJobQuestions(from.Questions),
		SpentTokens: from.SpentTokens, ObservedVersion: int32(from.ObservedVersion),
		TraceId: from.TraceID, ParentSpanId: from.ParentSpanID,
		CreatedAt: timestamppb.New(from.CreatedAt), UpdatedAt: timestamppb.New(from.UpdatedAt),
	}
	// The two moments the telling is measured between. Left unset where the moment has not happened,
	// so a job nobody asked anything of and one asked at the start of the epoch never read the same.
	if from.AskedAt != nil {
		on.AskedAt = timestamppb.New(*from.AskedAt)
	}
	if from.RaisedAt != nil {
		on.RaisedAt = timestamppb.New(*from.RaisedAt)
	}
	// What the forge last said about the pull request, as words rather than as a shape a caller has to
	// interpret. The moment is left unset where nothing has read it, so a caller can tell a reading of
	// unknown from no reading at all.
	reading := from.PullRequestState.Or()
	on.PullRequestStatus, on.PullRequestChecks = reading.Status, reading.Checks
	on.PullRequestCheck, on.PullRequestReview = reading.FailedCheck, reading.Review
	on.PullRequestFailed = reading.Failed
	if !reading.ReadAt.IsZero() {
		on.PullRequestReadAt = timestamppb.New(reading.ReadAt)
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

// asJobAttempts puts what each attempt at this job said on the wire, with how like the ones before it
// each was.
func asJobAttempts(from []job.Attempt) []*quaycrewv1.JobAttempt {
	if len(from) == 0 {
		return nil
	}
	attempts := make([]*quaycrewv1.JobAttempt, 0, len(from))
	for _, one := range from {
		attempts = append(attempts, &quaycrewv1.JobAttempt{
			Task: one.Task, Seq: int32(one.Seq), Step: int32(one.Step), Session: one.Session,
			Said: one.Said, Similarity: one.Similarity, OccurredAt: timestamppb.New(one.OccurredAt),
		})
	}
	return attempts
}

// asJobSteps puts what a job's session said it finished on the wire.
func asJobSteps(from []job.Step) []*quaycrewv1.JobStep {
	if len(from) == 0 {
		return nil
	}
	steps := make([]*quaycrewv1.JobStep, 0, len(from))
	for _, one := range from {
		steps = append(steps, &quaycrewv1.JobStep{
			Seq: int32(one.Seq), Summary: one.Summary, FinishedAt: timestamppb.New(one.FinishedAt),
		})
	}
	return steps
}

// asJobHandoffs puts what each session left behind on the wire.
func asJobHandoffs(from []job.Handoff) []*quaycrewv1.JobHandoff {
	if len(from) == 0 {
		return nil
	}
	handoffs := make([]*quaycrewv1.JobHandoff, 0, len(from))
	for _, one := range from {
		handoffs = append(handoffs, &quaycrewv1.JobHandoff{
			Seq: int32(one.Seq), Left: one.Left, Tried: one.Tried, Session: one.Session,
			WrittenAt: timestamppb.New(one.WrittenAt),
		})
	}
	return handoffs
}

// RunJobController makes reality match the job the system holds, until ctx is done. It blocks, so
// the caller runs it in a goroutine and owns its lifetime.
//
// Declared job is a row rather than a call somebody is holding, which is what makes it survive a
// restart: this reads the rows on the way up, so a system restarted onto job declared while it was
// down starts that job now rather than losing it.
func (s *Server) RunJobController(ctx context.Context) {
	s.jobController.Run(ctx)
}

// TickJob moves the job the system holds on by one step. Exported so a test and a scenario drive one
// tick rather than waiting for a ticker, which would be slow when it passed and flaky when it did not.
func (s *Server) TickJob(ctx context.Context) {
	s.jobController.Tick(ctx)
}

// RedactFor removes anything the workspace keeps sealed from a line the system is about to write down.
// What a model says can carry a value somebody pasted into a conversation, and everything recorded
// here is persisted.
func (s *Server) RedactFor(ctx context.Context, workspace, text string) string {
	return model.Redact(text, s.sealedForWorkspace(ctx, workspace))
}

// JobLease is how long a controller holds a job, from the system's configuration.
//
// The default is derived from what a tick costs rather than chosen, so a system that says nothing gets
// a measured number. A value that is not a duration is refused by falling back to that default and
// saying so, because a system that will not start over a misspelled interval is worse than a system
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

// ControllerName is what this system writes on the leases its controller takes.
//
// The host name, because an investigator reading a released record wants to know which machine
// stopped. A system that cannot say answers with nothing, and the controller mints itself a name.
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
	// The system declaring job for something it is already running, which today is one thing: a step
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
				"Raise it with krewe limits <workspace> --max-depth %d, which an operator does deliberately: "+
				"a session that could raise its own ceiling has none",
			limits.MaxDepth, declared.Depth, declared.Depth)
	}
	return nil
}

// modeReachesTheRepository refuses a job that works in a repository it cannot reach.
//
// The mode a job runs in is its own where it named one, and the system's where it did not, so the
// answer needs the server rather than the declaration alone. A crew configured to run its jobs in the
// mode that reaches the network admits the same job the default configuration refuses, which is the
// point: the rule reads what this system will actually run, not a constant.
func (s *Server) modeReachesTheRepository(declared *job.Job) error {
	if declared.Mode != "" {
		if err := job.UsableModeFor(declared.Repository, declared.Mode); err != nil {
			return status.Error(codes.InvalidArgument, err.Error())
		}
		return nil
	}
	if err := job.UsableModeBornIn(declared.Repository, model.PermissionModeBornIn(s.birthMode)); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return nil
}

// RunPullRequests reads back the pull requests the crew opened, on its own timer, until ctx is done.
// It blocks, so the caller runs it in a goroutine and owns its lifetime, the way the headroom sampler
// is run.
//
// Nothing calls a forge while a page draws. A page reads the row this writes, which is the rule
// GetHeadroom and GetHealth already hold: reading a service takes as long as that service takes, and
// a view that waited on one would go blank whenever it was slow.
func (s *Server) RunPullRequests(ctx context.Context) { s.pullRequests.Run(ctx) }

// ReadPullRequests reads every unsettled pull request once, now. The timer above calls this too; a
// caller that cannot wait for a tick calls it directly, which is what a test and a scenario do.
func (s *Server) ReadPullRequests(ctx context.Context) { s.pullRequests.Tick(ctx) }

// asJobQuestions puts what the readings of this job's plan could not settle on the wire, with what a
// later reading settled beside each row.
func asJobQuestions(from []job.Question) []*quaycrewv1.JobQuestion {
	if len(from) == 0 {
		return nil
	}
	questions := make([]*quaycrewv1.JobQuestion, 0, len(from))
	for _, one := range from {
		asked := &quaycrewv1.JobQuestion{
			Seq: int32(one.Seq), Text: one.Text, AskedBy: one.AskedBy, Status: one.Status,
			Answer: one.Answer, SettledBy: one.SettledBy, AskedAt: timestamppb.New(one.AskedAt),
		}
		if !one.SettledAt.IsZero() {
			asked.SettledAt = timestamppb.New(one.SettledAt)
		}
		questions = append(questions, asked)
	}
	return questions
}
