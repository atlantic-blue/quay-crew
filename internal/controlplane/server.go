// Package controlplane implements the spine: the ControlPlaneService gRPC API backed by project and
// session stores, a secrets store, and a model runner. Channels feed it through the event log; the
// dashboard and CLI drive it through the API.
package controlplane

import (
	"context"
	"sync"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
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
	provider sandbox.Provider

	mu        sync.Mutex
	sandboxes map[string]sandbox.Sandbox // one per session, created lazily, closed on stop
}

// NewServer builds a control plane backed by in memory stores, the given secrets store, the given
// model runner (the Claude Code adapter by default), and the given sandbox provider (one sandbox per
// session).
func NewServer(runner model.Runner, provider sandbox.Provider, secretStore secrets.Store) *Server {
	return &Server{
		projects:  newProjectStore(),
		sessions:  newSessionStore(),
		secrets:   secretStore,
		runner:    runner,
		provider:  provider,
		sandboxes: make(map[string]sandbox.Sandbox),
	}
}

// sandboxFor returns the session's sandbox, creating it on first use so it is reused across turns.
func (s *Server) sandboxFor(ctx context.Context, sessionID string) (sandbox.Sandbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if box, ok := s.sandboxes[sessionID]; ok {
		return box, nil
	}
	box, err := s.provider.Create(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	s.sandboxes[sessionID] = box
	return box, nil
}

// closeSandbox tears down and forgets a session's sandbox.
func (s *Server) closeSandbox(ctx context.Context, sessionID string) {
	s.mu.Lock()
	box, ok := s.sandboxes[sessionID]
	delete(s.sandboxes, sessionID)
	s.mu.Unlock()
	if ok {
		_ = box.Close(ctx)
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

	box, err := s.sandboxFor(ctx, session.GetId())
	if err != nil {
		s.sessions.recordTurn(session.GetId(), "", "failed")
		return nil, status.Errorf(codes.Internal, "create sandbox: %v", err)
	}

	resp, err := s.runner.Run(ctx, box, model.Request{
		Text:           req.GetText(),
		ModelSessionID: session.GetModelSessionId(),
		PermissionMode: defaultPermissionMode,
		Env:            s.turnEnv(ctx, req.GetProject()),
	})
	if err != nil {
		s.sessions.recordTurn(session.GetId(), "", "failed")
		return nil, status.Errorf(codes.Internal, "run turn: %v", err)
	}
	s.sessions.recordTurn(session.GetId(), resp.ModelSessionID, "idle")

	return &quaycrewv1.DispatchResponse{SessionId: session.GetId(), ThreadId: thread, Reply: resp.Reply}, nil
}

// turnEnv gathers the environment a turn runs with from the project's secrets. Right now that is the
// Claude Code subscription token, if one is set. A project that has not set it (or a model backend
// that does not need it) simply runs with no extra env, so the lookup never fails a turn.
func (s *Server) turnEnv(ctx context.Context, project string) map[string]string {
	token, err := s.secrets.Get(ctx, project, model.ClaudeCodeOAuthTokenEnv)
	if err != nil || token == "" {
		return nil
	}
	return map[string]string{model.ClaudeCodeOAuthTokenEnv: token}
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

// StopSession marks a session stopped and tears down its sandbox.
func (s *Server) StopSession(ctx context.Context, req *quaycrewv1.StopSessionRequest) (*quaycrewv1.StopSessionResponse, error) {
	if !s.sessions.stop(req.GetId()) {
		return nil, status.Errorf(codes.NotFound, "session %q not found", req.GetId())
	}
	s.closeSandbox(ctx, req.GetId())
	return &quaycrewv1.StopSessionResponse{}, nil
}
