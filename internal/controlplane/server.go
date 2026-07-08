// Package controlplane implements the spine: the ControlPlaneService gRPC API backed by project and
// session stores, a secrets store, and a model runner. Channels feed it through the event log; the
// dashboard and CLI drive it through the API.
package controlplane

import (
	"context"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultPermissionMode = "acceptEdits"

// Server implements quaycrewv1.ControlPlaneServiceServer.
type Server struct {
	quaycrewv1.UnimplementedControlPlaneServiceServer
	projects *projectStore
	sessions *sessionStore
	secrets  secrets.Store
	runner   model.Runner
}

// NewServer builds a control plane backed by in memory stores, the given secrets store, and the
// given model runner (the Claude Code adapter by default).
func NewServer(runner model.Runner, secretStore secrets.Store) *Server {
	return &Server{
		projects: newProjectStore(),
		sessions: newSessionStore(),
		secrets:  secretStore,
		runner:   runner,
	}
}

// CreateProject creates a project at runtime.
func (s *Server) CreateProject(_ context.Context, req *quaycrewv1.CreateProjectRequest) (*quaycrewv1.CreateProjectResponse, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	return &quaycrewv1.CreateProjectResponse{Project: s.projects.create(req.GetName())}, nil
}

// GetProject returns a project by id.
func (s *Server) GetProject(_ context.Context, req *quaycrewv1.GetProjectRequest) (*quaycrewv1.GetProjectResponse, error) {
	project, ok := s.projects.get(req.GetId())
	if !ok {
		return nil, status.Errorf(codes.NotFound, "project %q not found", req.GetId())
	}
	return &quaycrewv1.GetProjectResponse{Project: project}, nil
}

// ListProjects lists all projects.
func (s *Server) ListProjects(_ context.Context, _ *quaycrewv1.ListProjectsRequest) (*quaycrewv1.ListProjectsResponse, error) {
	return &quaycrewv1.ListProjectsResponse{Projects: s.projects.list()}, nil
}

// DeleteProject removes a project.
func (s *Server) DeleteProject(_ context.Context, req *quaycrewv1.DeleteProjectRequest) (*quaycrewv1.DeleteProjectResponse, error) {
	if !s.projects.delete(req.GetId()) {
		return nil, status.Errorf(codes.NotFound, "project %q not found", req.GetId())
	}
	return &quaycrewv1.DeleteProjectResponse{}, nil
}

// AttachChannel attaches a channel to a project.
func (s *Server) AttachChannel(_ context.Context, req *quaycrewv1.AttachChannelRequest) (*quaycrewv1.AttachChannelResponse, error) {
	if req.GetProject() == "" || req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "project and id are required")
	}
	if !s.projects.exists(req.GetProject()) {
		return nil, status.Errorf(codes.NotFound, "project %q not found", req.GetProject())
	}
	return &quaycrewv1.AttachChannelResponse{Channel: s.projects.attachChannel(req.GetProject(), req.GetId(), req.GetKind())}, nil
}

// SetSecret stores a project secret in the secrets backend. The value is never returned.
func (s *Server) SetSecret(ctx context.Context, req *quaycrewv1.SetSecretRequest) (*quaycrewv1.SetSecretResponse, error) {
	if req.GetProject() == "" || req.GetKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "project and key are required")
	}
	if !s.projects.exists(req.GetProject()) {
		return nil, status.Errorf(codes.NotFound, "project %q not found", req.GetProject())
	}
	if err := s.secrets.Set(ctx, req.GetProject(), req.GetKey(), req.GetValue()); err != nil {
		return nil, status.Errorf(codes.Internal, "set secret: %v", err)
	}
	return &quaycrewv1.SetSecretResponse{}, nil
}

// Dispatch starts or continues a thread, running one turn through the model runner.
func (s *Server) Dispatch(ctx context.Context, req *quaycrewv1.DispatchRequest) (*quaycrewv1.DispatchResponse, error) {
	if req.GetProject() == "" {
		return nil, status.Error(codes.InvalidArgument, "project is required")
	}
	if req.GetText() == "" {
		return nil, status.Error(codes.InvalidArgument, "text is required")
	}
	if !s.projects.exists(req.GetProject()) {
		return nil, status.Errorf(codes.NotFound, "project %q not found", req.GetProject())
	}

	thread := req.GetThreadId()
	if thread == "" {
		thread = newID()
	}
	session := s.sessions.findOrCreate(req.GetProject(), thread)

	resp, err := s.runner.Run(ctx, model.Request{
		Text:           req.GetText(),
		ModelSessionID: session.GetModelSessionId(),
		PermissionMode: defaultPermissionMode,
	})
	if err != nil {
		s.sessions.recordTurn(session.GetId(), "", "failed")
		return nil, status.Errorf(codes.Internal, "run turn: %v", err)
	}
	s.sessions.recordTurn(session.GetId(), resp.ModelSessionID, "idle")

	return &quaycrewv1.DispatchResponse{SessionId: session.GetId(), ThreadId: thread, Reply: resp.Reply}, nil
}

// ListSessions lists sessions, optionally filtered by project.
func (s *Server) ListSessions(_ context.Context, req *quaycrewv1.ListSessionsRequest) (*quaycrewv1.ListSessionsResponse, error) {
	return &quaycrewv1.ListSessionsResponse{Sessions: s.sessions.list(req.GetProject())}, nil
}

// GetSession returns a session by id.
func (s *Server) GetSession(_ context.Context, req *quaycrewv1.GetSessionRequest) (*quaycrewv1.GetSessionResponse, error) {
	session, ok := s.sessions.get(req.GetId())
	if !ok {
		return nil, status.Errorf(codes.NotFound, "session %q not found", req.GetId())
	}
	return &quaycrewv1.GetSessionResponse{Session: session}, nil
}

// StopSession marks a session stopped.
func (s *Server) StopSession(_ context.Context, req *quaycrewv1.StopSessionRequest) (*quaycrewv1.StopSessionResponse, error) {
	if !s.sessions.stop(req.GetId()) {
		return nil, status.Errorf(codes.NotFound, "session %q not found", req.GetId())
	}
	return &quaycrewv1.StopSessionResponse{}, nil
}

// HandleInbound routes an inbound channel message to a session, running a turn. The event log
// consumer calls this, so channel messages drive the same path as Dispatch.
func (s *Server) HandleInbound(ctx context.Context, msg *quaycrewv1.InboundMessage) error {
	_, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project:  msg.GetProject(),
		ThreadId: msg.GetThreadId(),
		Text:     msg.GetText(),
	})
	return err
}
