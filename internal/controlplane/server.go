// Package controlplane implements the spine: the ControlPlaneService gRPC API backed by a durable
// store, a secrets store, and a model runner. Channels feed it through the event log; the dashboard
// and CLI drive it through the API.
//
// The server holds no domain state of its own. Projects and sessions live in the store, so a restart
// resumes conversations instead of orphaning them. The one thing it still keeps in the process is
// the map of live sandboxes, which is a handle to a running container rather than a fact worth
// keeping; reattaching those after a restart is its own piece of work.
package controlplane

import (
	"context"
	"errors"
	"sync"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultPermissionMode = "acceptEdits"

// Server implements quaycrewv1.ControlPlaneServiceServer.
type Server struct {
	quaycrewv1.UnimplementedControlPlaneServiceServer
	store    store.Store
	secrets  secrets.Store
	runner   model.Runner
	provider sandbox.Provider

	mu        sync.Mutex
	sandboxes map[string]sandbox.Sandbox // one per session, created lazily, closed on stop
}

// NewServer builds a control plane over the given store, model runner (the Claude Code adapter by
// default), sandbox provider (one sandbox per session) and secrets store.
func NewServer(durable store.Store, runner model.Runner, provider sandbox.Provider, secretStore secrets.Store) *Server {
	return &Server{
		store:     durable,
		secrets:   secretStore,
		runner:    runner,
		provider:  provider,
		sandboxes: make(map[string]sandbox.Sandbox),
	}
}

// storeError maps a store failure onto the status the caller should see.
func storeError(err error, what string) error {
	if errors.Is(err, store.ErrNotFound) {
		return status.Errorf(codes.NotFound, "%s not found", what)
	}
	return status.Errorf(codes.Internal, "%s: %v", what, err)
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
func (s *Server) CreateProject(ctx context.Context, req *quaycrewv1.CreateProjectRequest) (*quaycrewv1.CreateProjectResponse, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	project, err := s.store.CreateProject(ctx, req.GetName())
	if err != nil {
		return nil, storeError(err, "create project")
	}
	return &quaycrewv1.CreateProjectResponse{Project: project}, nil
}

// GetProject returns a project by id.
func (s *Server) GetProject(ctx context.Context, req *quaycrewv1.GetProjectRequest) (*quaycrewv1.GetProjectResponse, error) {
	project, err := s.store.GetProject(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "project")
	}
	return &quaycrewv1.GetProjectResponse{Project: project}, nil
}

// ListProjects lists all projects.
func (s *Server) ListProjects(ctx context.Context, _ *quaycrewv1.ListProjectsRequest) (*quaycrewv1.ListProjectsResponse, error) {
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, storeError(err, "list projects")
	}
	return &quaycrewv1.ListProjectsResponse{Projects: projects}, nil
}

// DeleteProject removes a project.
func (s *Server) DeleteProject(ctx context.Context, req *quaycrewv1.DeleteProjectRequest) (*quaycrewv1.DeleteProjectResponse, error) {
	if err := s.store.DeleteProject(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "project")
	}
	return &quaycrewv1.DeleteProjectResponse{}, nil
}

// AttachChannel attaches a channel to a project.
func (s *Server) AttachChannel(ctx context.Context, req *quaycrewv1.AttachChannelRequest) (*quaycrewv1.AttachChannelResponse, error) {
	if req.GetProject() == "" || req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "project and id are required")
	}
	channel, err := s.store.AttachChannel(ctx, req.GetProject(), req.GetId(), req.GetKind())
	if err != nil {
		return nil, storeError(err, "project")
	}
	return &quaycrewv1.AttachChannelResponse{Channel: channel}, nil
}

// SetSecret stores a project secret in the secrets backend. The value is never returned.
func (s *Server) SetSecret(ctx context.Context, req *quaycrewv1.SetSecretRequest) (*quaycrewv1.SetSecretResponse, error) {
	if req.GetProject() == "" || req.GetKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "project and key are required")
	}
	if _, err := s.store.GetProject(ctx, req.GetProject()); err != nil {
		return nil, storeError(err, "project")
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

	thread := req.GetThreadId()
	if thread == "" {
		thread = store.NewID()
	}
	session, err := s.store.FindOrCreateSession(ctx, req.GetProject(), thread)
	if err != nil {
		return nil, storeError(err, "project")
	}

	box, err := s.sandboxFor(ctx, session.GetId())
	if err != nil {
		s.recordTurn(ctx, session.GetId(), "", "failed")
		return nil, status.Errorf(codes.Internal, "create sandbox: %v", err)
	}

	resp, err := s.runner.Run(ctx, box, model.Request{
		Text:           req.GetText(),
		ModelSessionID: session.GetModelSessionId(),
		PermissionMode: defaultPermissionMode,
		Env:            s.turnEnv(ctx, req.GetProject()),
	})
	if err != nil {
		s.recordTurn(ctx, session.GetId(), "", "failed")
		return nil, status.Errorf(codes.Internal, "run turn: %v", err)
	}
	s.recordTurn(ctx, session.GetId(), resp.ModelSessionID, "idle")

	return &quaycrewv1.DispatchResponse{SessionId: session.GetId(), ThreadId: thread, Reply: resp.Reply}, nil
}

// recordTurn stores the outcome of a turn. A store failure here must not replace the turn's own
// result, which the operator already has, so it is not returned; a later read shows a stale status
// rather than the turn appearing to have failed when it did not.
func (s *Server) recordTurn(ctx context.Context, sessionID, modelSessionID, sessionStatus string) {
	_ = s.store.RecordTurn(ctx, sessionID, modelSessionID, sessionStatus)
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
func (s *Server) ListSessions(ctx context.Context, req *quaycrewv1.ListSessionsRequest) (*quaycrewv1.ListSessionsResponse, error) {
	sessions, err := s.store.ListSessions(ctx, req.GetProject())
	if err != nil {
		return nil, storeError(err, "list sessions")
	}
	return &quaycrewv1.ListSessionsResponse{Sessions: sessions}, nil
}

// GetSession returns a session by id.
func (s *Server) GetSession(ctx context.Context, req *quaycrewv1.GetSessionRequest) (*quaycrewv1.GetSessionResponse, error) {
	session, err := s.store.GetSession(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	return &quaycrewv1.GetSessionResponse{Session: session}, nil
}

// StopSession marks a session stopped and tears down its sandbox.
func (s *Server) StopSession(ctx context.Context, req *quaycrewv1.StopSessionRequest) (*quaycrewv1.StopSessionResponse, error) {
	if err := s.store.StopSession(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "session")
	}
	s.closeSandbox(ctx, req.GetId())
	return &quaycrewv1.StopSessionResponse{}, nil
}
