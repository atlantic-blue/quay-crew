package controlplane

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/flow"
	"github.com/atlantic-blue/krewe/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ImportFlow stores a graph at the version written in it.
//
// The graph is parsed here rather than trusted, so every refusal a run could otherwise fall off
// happens at the moment somebody imports the file, read by the person who wrote it, instead of
// hours later inside a run with nothing pointing back at the text.
func (s *Server) ImportFlow(ctx context.Context, req *quaycrewv1.ImportFlowRequest) (*quaycrewv1.ImportFlowResponse, error) {
	graph, err := flow.Parse([]byte(req.GetDefinition()))
	if err != nil {
		// The flow package's own sentence names what is wrong and what to do about it; wrapping it
		// in something vaguer would lose the only useful part.
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.store.ImportFlowGraph(ctx, graph.Name, graph.Version, req.GetDefinition()); err != nil {
		if strings.Contains(err.Error(), "already imported") {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, storeError(err, "import flow")
	}
	return &quaycrewv1.ImportFlowResponse{Name: graph.Name, Version: int32(graph.Version)}, nil
}

// StartFlow begins a run of the newest version of a graph and answers with the run, rather than
// waiting for it.
//
// A run dispatches tasks, and a task takes as long as the model takes, so a call that blocked until
// the run ended would be a command line that hangs for ten minutes. The run advances behind this
// answer, and GetFlowRun says where it got to.
func (s *Server) StartFlow(ctx context.Context, req *quaycrewv1.StartFlowRequest) (*quaycrewv1.StartFlowResponse, error) {
	if req.GetGraph() == "" {
		return nil, status.Error(codes.InvalidArgument, "a flow needs a graph to run")
	}
	project, err := s.store.GetProject(ctx, req.GetProject())
	if err != nil {
		return nil, storeError(err, "project")
	}
	// The graph is read before the run is made, so a name nobody imported is refused as not found
	// rather than leaving a run that can never move.
	if _, _, err := s.store.LatestFlowGraph(ctx, req.GetGraph()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound,
				"no flow named %s has been imported; write the graph and import it with krewe flow import <file>", req.GetGraph())
		}
		return nil, storeError(err, "flow")
	}

	run, err := s.flows.Begin(ctx, req.GetGraph(), project.GetWorkspace(), project.GetId(), req.GetState())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "start flow %s: %v", req.GetGraph(), err)
	}
	return &quaycrewv1.StartFlowResponse{Run: s.flowRun(ctx, &run)}, nil
}

// GetFlowRun says where a run got to.
func (s *Server) GetFlowRun(ctx context.Context, req *quaycrewv1.GetFlowRunRequest) (*quaycrewv1.GetFlowRunResponse, error) {
	run, err := s.store.GetFlowRun(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "flow run")
	}
	return &quaycrewv1.GetFlowRunResponse{Run: s.flowRun(ctx, run)}, nil
}

// ListFlowRuns lists runs, narrowed to one project when the request names one.
func (s *Server) ListFlowRuns(ctx context.Context, req *quaycrewv1.ListFlowRunsRequest) (*quaycrewv1.ListFlowRunsResponse, error) {
	runs, err := s.store.ListFlowRuns(ctx, req.GetProject())
	if err != nil {
		return nil, storeError(err, "flow runs")
	}
	out := make([]*quaycrewv1.FlowRun, 0, len(runs))
	for _, run := range runs {
		out = append(out, s.flowRun(ctx, run))
	}
	return &quaycrewv1.ListFlowRunsResponse{Runs: out}, nil
}

// flowRun puts a run on the wire, with the job that carries it.
//
// The job is read here rather than kept on the run, because the reducer holds the run and has no
// business knowing where in the tree it sits. A run whose job cannot be read is answered without it
// rather than refused: where it sits is not why somebody asked.
func (s *Server) flowRun(ctx context.Context, run *flow.Run) *quaycrewv1.FlowRun {
	on := asFlowRun(run)
	if carrier, err := s.store.FlowRunCarrier(ctx, run.ID); err == nil {
		on.Job = carrier
	}
	return on
}

// asFlowRun puts a run on the wire.
func asFlowRun(run *flow.Run) *quaycrewv1.FlowRun {
	return &quaycrewv1.FlowRun{
		Id:           run.ID,
		Workspace:    run.Workspace,
		Project:      run.Project,
		GraphName:    run.GraphName,
		GraphVersion: int32(run.GraphVersion),
		Node:         run.Node,
		Status:       run.Status,
		State:        run.State,
		Transitions:  int32(run.Transitions),
		Spent:        run.Spent,
		Reason:       run.Reason,
		Question:     run.Question,
		UpdatedAt:    timestamppb.Now(),
	}
}

// StopFlowRun halts a run in flight, keeping the reason so a run somebody stopped and a run that
// went quiet never read the same.
//
// The stop is cooperative rather than a kill: a run waiting on a task finishes that task, because
// the model is already working and abandoning it would leave a sandbox mid sentence for no gain.
// What it cannot do is take another step, which is what stops the spending.
func (s *Server) StopFlowRun(ctx context.Context, req *quaycrewv1.StopFlowRunRequest) (*quaycrewv1.StopFlowRunResponse, error) {
	reason := strings.TrimSpace(req.GetReason())
	if reason == "" {
		reason = "stopped by the operator"
	}
	run, err := s.store.StopFlowRun(ctx, req.GetId(), reason)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "there is no run %s", req.GetId())
		}
		if strings.Contains(err.Error(), "already ended") {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, storeError(err, "flow run")
	}
	return &quaycrewv1.StopFlowRunResponse{Run: s.flowRun(ctx, run)}, nil
}

// AnswerFlowRun tells a run what the operator decided.
//
// The only thing that moves a run waiting on a person. There is deliberately no timeout and no
// default: an automation nobody answered must never carry on and do the thing it was asking
// permission for.
func (s *Server) AnswerFlowRun(ctx context.Context, req *quaycrewv1.AnswerFlowRunRequest) (*quaycrewv1.AnswerFlowRunResponse, error) {
	run, err := s.store.GetFlowRun(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "flow run")
	}
	if run.Status != flow.StatusAsking {
		return nil, status.Errorf(codes.FailedPrecondition,
			"run %s is %s and is not asking anything, so there is nothing to answer", run.ID, run.Status)
	}
	answered, err := s.flows.Answer(ctx, *run, req.GetAnswer())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "answer run %s: %v", run.ID, err)
	}
	return &quaycrewv1.AnswerFlowRunResponse{Run: s.flowRun(ctx, &answered)}, nil
}

// ScheduleFlow starts a graph running on its own in one project, at the interval the graph declares.
//
// The interval is read from the graph rather than taken as an argument, so how often an automation
// runs is versioned and reviewable alongside what it does. Where it runs is the operator's to say,
// because a run needs a project to dispatch into.
func (s *Server) ScheduleFlow(ctx context.Context, req *quaycrewv1.ScheduleFlowRequest) (*quaycrewv1.ScheduleFlowResponse, error) {
	project, err := s.store.GetProject(ctx, req.GetProject())
	if err != nil {
		return nil, storeError(err, "project")
	}
	_, definition, err := s.store.LatestFlowGraph(ctx, req.GetGraph())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound,
				"no flow named %s has been imported", req.GetGraph())
		}
		return nil, storeError(err, "flow")
	}
	graph, err := flow.Parse([]byte(definition))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if graph.Every <= 0 {
		return nil, status.Errorf(codes.FailedPrecondition,
			"graph %s does not say how often it runs, so there is nothing to schedule; add `on: { every: 24h }` to it and import the next version",
			graph.Name)
	}
	// The first run is one interval away rather than now: scheduling a graph should not be
	// indistinguishable from starting one, or the operator cannot arrange an automation without
	// also running it.
	next := time.Now().UTC().Add(graph.Every)
	if err := s.store.ScheduleFlow(ctx, graph.Name, project.GetId(), graph.Every, next); err != nil {
		return nil, storeError(err, "schedule flow")
	}
	return &quaycrewv1.ScheduleFlowResponse{EverySeconds: int64(graph.Every.Seconds())}, nil
}

// UnscheduleFlow stops a graph running on its own in a project.
func (s *Server) UnscheduleFlow(ctx context.Context, req *quaycrewv1.UnscheduleFlowRequest) (*quaycrewv1.UnscheduleFlowResponse, error) {
	if err := s.store.UnscheduleFlow(ctx, req.GetGraph(), req.GetProject()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound,
				"%s is not scheduled to run on its own here", req.GetGraph())
		}
		return nil, storeError(err, "unschedule flow")
	}
	return &quaycrewv1.UnscheduleFlowResponse{}, nil
}

// RunFlowPoller resumes waiting runs until ctx is done. It blocks, so the caller runs it in a
// goroutine and owns its lifetime.
//
// A wait is a row rather than a timer somebody is holding, which is what makes it survive a
// restart: this reads the rows on the way up, so a system restarted onto a pile of overdue waits
// resumes them immediately rather than losing them.
func (s *Server) RunFlowPoller(ctx context.Context) {
	s.flowPoller.Run(ctx)
}

// TickFlows moves every run the system holds on by one step: a wait that came due, a schedule that
// fired, a step whose job ended. Exported so a test and a scenario drive one tick rather than
// waiting for a ticker, which would be slow when it passed and flaky when it did not.
func (s *Server) TickFlows(ctx context.Context) {
	s.flowPoller.Tick(ctx)
}

// SessionTokens is what one session's conversation has cost, which is what a run's ceiling is checked
// against. Zero for a session that is gone, has no conversation yet, or whose transcript cannot be
// read: a cost that cannot be read is not a reason to stop job that is already under way.
func (s *Server) SessionTokens(ctx context.Context, id string) int64 {
	session, err := s.store.GetSession(ctx, id)
	if err != nil {
		return 0
	}
	return s.storage.ConversationUsage(boxOf(session), session.GetModelSessionId()).Total()
}

// SessionHolds says whether a path is in a session's own working directory, which is how a graph's
// claim about what its task would leave behind is checked by the system rather than by the model.
//
// It reads the directory rather than the sandbox. The working directory is state the system keeps on
// the host and mounts in, so this is the same files the model was looking at, answered without
// starting a container and without a road into one.
//
// A path that cannot be reached is an error rather than a false: a system that keeps no state on disk,
// or a session it does not have, must stop the run rather than quietly satisfy the check.
func (s *Server) SessionHolds(ctx context.Context, id, path string) (bool, error) {
	if id == "" {
		return false, fmt.Errorf("the run has no session yet")
	}
	session, err := s.store.GetSession(ctx, id)
	if err != nil {
		return false, fmt.Errorf("the run's session could not be read: %w", err)
	}
	dir, kept := s.storage.WorkingDir(boxOf(session))
	if !kept {
		return false, fmt.Errorf("this system keeps no working directory on disk to look in")
	}
	// Cleaned and held inside the session's own directory. The parser refuses a path that climbs, and
	// this is the second of the two, because the graph and the system are edited by different hands.
	inside := filepath.Join(dir, filepath.Clean("/"+path))
	if _, err := os.Stat(inside); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("%s could not be read: %w", path, err)
	}
	return true, nil
}

// flowRunner is what the server needs to begin a run. The engine implements it; the indirection is
// here so the server's own tests can watch a run start without one.
type flowRunner interface {
	Begin(ctx context.Context, graph, workspace, project string, state map[string]string) (flow.Run, error)
	Answer(ctx context.Context, run flow.Run, answer string) (flow.Run, error)
}
