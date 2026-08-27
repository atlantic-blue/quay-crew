package controlplane

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/atlantic-blue/quay-crew/internal/work"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CreateWork declares a piece of work and answers with the record the crew kept.
//
// Every rule is checked here, at the moment of the write, while the caller is looking. A refusal
// that arrives hours later, inside a run, has nothing to point back at.
//
// Nothing runs the work. This call records intent, and that is the whole of it: a controller is a
// slice of its own. What this buys on its own is that the intent outlives the caller.
func (s *Server) CreateWork(ctx context.Context, req *quaycrewv1.CreateWorkRequest) (*quaycrewv1.CreateWorkResponse, error) {
	declaration := work.Declaration{
		Title: req.GetTitle(), Brief: req.GetBrief(), Role: req.GetRole(), Mode: req.GetMode(),
		ExpectFile: req.GetExpectFile(), ExpectContains: req.GetExpectContains(),
		After: req.GetAfter(), BudgetTokens: req.GetBudgetTokens(), Labels: req.GetLabels(),
		ID: req.GetId(), Parent: req.GetParent(),
	}
	if req.GetDeadline() != nil {
		at := req.GetDeadline().AsTime()
		declaration.Deadline = &at
	}
	// Before the project is read, so a caller that got the shape wrong is told what is wrong with
	// the shape rather than being sent to look for a project.
	if err := declaration.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if req.GetProject() == "" {
		return nil, status.Error(codes.InvalidArgument,
			"work needs a project to run in: say where with an address, for example quay work create me/house-bills")
	}
	project, err := s.store.GetProject(ctx, req.GetProject())
	if err != nil {
		return nil, storeError(err, "project")
	}

	tidy := declaration.Tidied()
	declared := &work.Work{
		ID: store.NewID(), Workspace: project.GetWorkspace(), Project: project.GetId(),
		Title: tidy.Title, Brief: tidy.Brief, Mode: tidy.NamedMode(),
		ExpectFile: tidy.ExpectFile, ExpectContains: tidy.ExpectContains,
		After: tidy.After, Deadline: tidy.Deadline, BudgetTokens: tidy.BudgetTokens,
		Labels: tidy.Labels,
		// Every caller is a root in this slice. The parent is read from a work credential, and
		// nothing mints one yet, so depth stays zero and the depth limit has nothing to bound.
		Version: 1, Phase: work.PhasePending,
	}
	s.traceWork(ctx, declared, nil)
	if err := s.pinRole(ctx, declared, tidy.Role); err != nil {
		return nil, err
	}
	if err := s.checkAfter(ctx, declared); err != nil {
		return nil, err
	}

	declaredEvent := s.workEvent(ctx, declared, work.EventDeclared, declared.Title)
	if err := s.store.CreateWork(ctx, declared, declaredEvent); err != nil {
		return nil, storeError(err, "create work")
	}
	// After the transaction, never inside it. The store is the truth and the log is the copy, so an
	// export that cannot land is dropped and the record stands.
	s.ExportWork(ctx, declaredEvent)
	// Read back rather than answered from memory, so the caller is given what the store holds,
	// stamped with the store's own clock.
	kept, err := s.store.GetWork(ctx, declared.ID)
	if err != nil {
		return nil, storeError(err, "work")
	}
	return &quaycrewv1.CreateWorkResponse{Work: asWork(kept)}, nil
}

// pinRole attaches the role at the version the workspace holds now.
//
// The version is pinned the way a run pins its graph: editing a role tomorrow cannot change work
// that is already declared. A role the workspace does not hold is refused by name here, while
// somebody is looking, rather than when a session that cannot be built is asked for.
func (s *Server) pinRole(ctx context.Context, declared *work.Work, named string) error {
	if named == "" {
		return nil
	}
	held, err := s.roleFor(ctx, declared.Workspace, named)
	if err != nil {
		return err
	}
	declared.Role, declared.RoleVersion = held.Name, held.Version
	return nil
}

// checkAfter refuses an ordering that could never come due: work that waits for something the crew
// does not hold, or a loop of work waiting on itself.
func (s *Server) checkAfter(ctx context.Context, declared *work.Work) error {
	for _, id := range declared.After {
		if _, err := s.store.GetWork(ctx, id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return status.Errorf(codes.InvalidArgument,
					"this work waits for %s, which the crew does not hold: read the identifier off quay work list, "+
						"or declare that work first", id)
			}
			return storeError(err, "work")
		}
	}
	// A caller cannot reach this today, because the crew assigns every identifier and what a piece
	// of work waits for must already exist. It is the guard for the first thing that rewrites the
	// ordering, and a loop would otherwise sit pending forever with nothing saying why.
	dependsOn := func(id string) []string {
		found, err := s.store.GetWork(ctx, id)
		if err != nil {
			return nil
		}
		return found.After
	}
	if from, to, found := work.Cycle(declared.ID, declared.After, dependsOn); found {
		return status.Errorf(codes.InvalidArgument,
			"this ordering closes a loop: %s waits for %s, which waits back round to it. Work in a loop never comes due, "+
				"so break the chain and declare one of them without the other", from, to)
	}
	return nil
}

// GetWork reads one piece of work back, whole, its answer included.
func (s *Server) GetWork(ctx context.Context, req *quaycrewv1.GetWorkRequest) (*quaycrewv1.GetWorkResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "which piece of work: give the identifier quay work list prints")
	}
	found, err := s.store.GetWork(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound,
				"the crew holds no work %s: quay work list says what there is", req.GetId())
		}
		return nil, storeError(err, "work")
	}
	return &quaycrewv1.GetWorkResponse{Work: asWork(found)}, nil
}

// ListWork says what the crew holds, newest first and without answers.
func (s *Server) ListWork(ctx context.Context, req *quaycrewv1.ListWorkRequest) (*quaycrewv1.ListWorkResponse, error) {
	if phase := req.GetPhase(); phase != "" && !work.KnownPhase(phase) {
		return nil, status.Errorf(codes.InvalidArgument,
			"%q is not a phase; use one of %s", phase, strings.Join(work.Phases(), ", "))
	}
	listed, err := s.store.ListWork(ctx, work.Filter{
		Workspace: req.GetWorkspace(), Project: req.GetProject(),
		Parent: req.GetParent(), Root: req.GetRootsOnly(), Phase: req.GetPhase(),
		LabelKey: req.GetLabelKey(), LabelValue: req.GetLabelValue(),
	})
	if err != nil {
		return nil, storeError(err, "work")
	}
	out := make([]*quaycrewv1.Work, 0, len(listed))
	for _, one := range listed {
		out = append(out, asWork(one))
	}
	return &quaycrewv1.ListWorkResponse{Work: out}, nil
}

// StopWork halts work that has not ended, keeping the reason.
//
// Work that already ended is refused rather than overwritten, for the reason a stopped flow run
// gives: how it ended is the useful part, and a second stop erases it.
func (s *Server) StopWork(ctx context.Context, req *quaycrewv1.StopWorkRequest) (*quaycrewv1.StopWorkResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "which piece of work: give the identifier quay work list prints")
	}
	reason := strings.TrimSpace(req.GetReason())
	if reason == "" {
		reason = "stopped by the operator"
	}
	found, err := s.store.GetWork(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound,
				"the crew holds no work %s: quay work list says what there is", req.GetId())
		}
		return nil, storeError(err, "work")
	}

	stoppedEvent := s.workEvent(ctx, found, work.EventStopped, reason)
	stopped, err := s.store.StopWork(ctx, found.ID, reason, stoppedEvent)
	if err != nil {
		if strings.Contains(err.Error(), "already ended") {
			return nil, status.Errorf(codes.FailedPrecondition,
				"work %s is %s already, and work that already ended is not stopped again", found.ID, found.Phase)
		}
		return nil, storeError(err, "stop work")
	}
	s.ExportWork(ctx, stoppedEvent)
	return &quaycrewv1.StopWorkResponse{Work: asWork(stopped)}, nil
}

// workEvent builds one record of what happened, redacted.
//
// The detail can carry whatever a caller typed, and everything recorded here is persisted, so it
// goes through the same redactor a task's reply does. What the crew can know is every value the
// workspace keeps sealed.
func (s *Server) workEvent(ctx context.Context, of *work.Work, kind, detail string) *work.Event {
	return &work.Event{
		ID: store.NewID(), Kind: kind, Work: of.ID,
		Workspace: of.Workspace, Project: of.Project, Parent: of.Parent, Depth: of.Depth,
		Detail:     oneShortLine(model.Redact(detail, s.sealedForWorkspace(ctx, of.Workspace))),
		TraceID:    of.TraceID,
		OccurredAt: timestamppb.Now().AsTime(),
	}
}

// asWork puts a piece of work on the wire.
func asWork(from *work.Work) *quaycrewv1.Work {
	on := &quaycrewv1.Work{
		Id: from.ID, Workspace: from.Workspace, Project: from.Project,
		Title: from.Title, Brief: from.Brief, Role: from.Role, RoleVersion: int32(from.RoleVersion),
		Mode: from.Mode, ExpectFile: from.ExpectFile, ExpectContains: from.ExpectContains,
		After: from.After, BudgetTokens: from.BudgetTokens, Labels: from.Labels,
		Parent: from.Parent, Depth: int32(from.Depth), Version: int32(from.Version),
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

// RunWorkController makes reality match the work the crew holds, until ctx is done. It blocks, so
// the caller runs it in a goroutine and owns its lifetime.
//
// Declared work is a row rather than a call somebody is holding, which is what makes it survive a
// restart: this reads the rows on the way up, so a crew restarted onto work declared while it was
// down starts that work now rather than losing it.
func (s *Server) RunWorkController(ctx context.Context) {
	s.workController.Run(ctx)
}

// TickWork moves the work the crew holds on by one step. Exported so a test and a scenario drive one
// tick rather than waiting for a ticker, which would be slow when it passed and flaky when it did not.
func (s *Server) TickWork(ctx context.Context) {
	s.workController.Tick(ctx)
}

// RedactFor removes anything the workspace keeps sealed from a line the crew is about to write down.
// What a model says can carry a value somebody pasted into a conversation, and everything recorded
// here is persisted.
func (s *Server) RedactFor(ctx context.Context, workspace, text string) string {
	return model.Redact(text, s.sealedForWorkspace(ctx, workspace))
}

// WorkLease is how long a controller holds a piece of work, from the crew's configuration.
//
// The default is derived from what a tick costs rather than chosen, so a crew that says nothing gets
// a measured number. A value that is not a duration is refused by falling back to that default and
// saying so, because a crew that will not start over a misspelled interval is worse than a crew
// running the number it was already running.
func WorkLease(configured string, logger *slog.Logger) time.Duration {
	value := strings.TrimSpace(configured)
	if value == "" {
		return work.DefaultLease
	}
	lease, err := time.ParseDuration(value)
	if err != nil || lease <= 0 {
		if logger != nil {
			logger.Warn("QC_WORK_LEASE is not a length of time, so the measured default is used instead",
				"configured", value, "using", work.DefaultLease)
		}
		return work.DefaultLease
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
