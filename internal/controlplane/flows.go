package controlplane

import (
	"context"
	"errors"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/flow"
	"github.com/atlantic-blue/quay-crew/internal/store"
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
// A run dispatches turns, and a turn takes as long as the model takes, so a call that blocked until
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
				"no flow named %s has been imported; write the graph and import it with quay flow import <file>", req.GetGraph())
		}
		return nil, storeError(err, "flow")
	}

	run, err := s.flows.Begin(ctx, req.GetGraph(), project.GetWorkspace(), project.GetId(), req.GetState())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "start flow %s: %v", req.GetGraph(), err)
	}
	return &quaycrewv1.StartFlowResponse{Run: asFlowRun(&run)}, nil
}

// GetFlowRun says where a run got to.
func (s *Server) GetFlowRun(ctx context.Context, req *quaycrewv1.GetFlowRunRequest) (*quaycrewv1.GetFlowRunResponse, error) {
	run, err := s.store.GetFlowRun(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "flow run")
	}
	return &quaycrewv1.GetFlowRunResponse{Run: asFlowRun(run)}, nil
}

// ListFlowRuns lists runs, narrowed to one project when the request names one.
func (s *Server) ListFlowRuns(ctx context.Context, req *quaycrewv1.ListFlowRunsRequest) (*quaycrewv1.ListFlowRunsResponse, error) {
	runs, err := s.store.ListFlowRuns(ctx, req.GetProject())
	if err != nil {
		return nil, storeError(err, "flow runs")
	}
	out := make([]*quaycrewv1.FlowRun, 0, len(runs))
	for _, run := range runs {
		out = append(out, asFlowRun(run))
	}
	return &quaycrewv1.ListFlowRunsResponse{Runs: out}, nil
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
		UpdatedAt:    timestamppb.Now(),
	}
}

// StopFlowRun halts a run in flight, keeping the reason so a run somebody stopped and a run that
// went quiet never read the same.
//
// The stop is cooperative rather than a kill: a run waiting on a turn finishes that turn, because
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
	return &quaycrewv1.StopFlowRunResponse{Run: asFlowRun(run)}, nil
}

// RunFlowPoller resumes waiting runs until ctx is done. It blocks, so the caller runs it in a
// goroutine and owns its lifetime.
//
// A wait is a row rather than a timer somebody is holding, which is what makes it survive a
// restart: this reads the rows on the way up, so a crew restarted onto a pile of overdue waits
// resumes them immediately rather than losing them.
func (s *Server) RunFlowPoller(ctx context.Context) {
	s.flowPoller.Run(ctx)
}

// ThreadTokens is what one thread's conversation has cost, which is what a run's ceiling is checked
// against. Zero for a thread that is gone, has no conversation yet, or whose transcript cannot be
// read: a cost that cannot be read is not a reason to stop work that is already under way.
func (s *Server) ThreadTokens(ctx context.Context, thread string) int64 {
	session, err := s.store.GetSession(ctx, thread)
	if err != nil {
		return 0
	}
	return s.storage.ConversationUsage(session.GetWorkspace(), session.GetModelSessionId()).Total()
}

// flowRunner is what the server needs to begin a run. The engine implements it; the indirection is
// here so the server's own tests can watch a run start without one.
type flowRunner interface {
	Begin(ctx context.Context, graph, workspace, project string, state map[string]string) (flow.Run, error)
}
