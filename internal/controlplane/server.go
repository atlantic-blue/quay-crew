// Package controlplane implements the spine: the ControlPlaneService gRPC API backed by a durable
// store, a secrets store, and a model runner. Channels feed it through the event log; the dashboard
// and CLI drive it through the API.
//
// The server holds no domain state of its own. Workspaces and sessions live in the store, so a restart
// resumes conversations instead of orphaning them. The one thing it still keeps in the process is
// the map of live sandboxes, which is a handle to a running container rather than a fact worth
// keeping; reattaching those after a restart is its own piece of work.
package controlplane

import (
	"context"
	"errors"
	"sort"
	"sync"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/name"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Info is what this control plane is running, reported over the API so an operator can see which
// crew they are about to act on. It is configuration: never a secret, and never a health verdict.
type Info struct {
	// Model is the backend a turn runs against, for example "claude-code".
	Model string
	// Sandbox is what a session is isolated in, for example "docker".
	Sandbox string
	// Store is where workspaces and sessions are kept, for example "postgres".
	Store string
	// State is where a conversation and a project's files are kept, for example "host directory".
	// Empty means they live in the container and are destroyed with it.
	State string
	// Events is the event log a turn is recorded on. Empty means nothing is connected to it.
	Events string
}

// Config is everything the control plane is built from. It is a struct rather than a parameter list
// because the list had already reached four and a caller could silently swap two of them.
type Config struct {
	Store    store.Store
	Runner   model.Runner
	Provider sandbox.Provider
	Secrets  secrets.Store
	// Storage is where a workspace's conversation store lives on the host. The control plane reads it
	// to tell a thread whose conversation is still there from one whose handle outlived it.
	Storage sandbox.Storage
	// Info describes the three above in words, for the console's status block.
	Info Info
}

// Server implements quaycrewv1.ControlPlaneServiceServer.
type Server struct {
	quaycrewv1.UnimplementedControlPlaneServiceServer
	store    store.Store
	secrets  secrets.Store
	runner   model.Runner
	provider sandbox.Provider
	storage  sandbox.Storage
	info     Info

	mu        sync.Mutex
	sandboxes map[string]sandbox.Sandbox // one per session, created lazily, closed on stop
}

// NewServer builds a control plane over a durable store, a model runner (the Claude Code adapter by
// default), a sandbox provider (one sandbox per session) and a secrets store.
func NewServer(cfg Config) *Server {
	return &Server{
		store:     cfg.Store,
		secrets:   cfg.Secrets,
		runner:    cfg.Runner,
		provider:  cfg.Provider,
		storage:   cfg.Storage,
		info:      cfg.Info,
		sandboxes: make(map[string]sandbox.Sandbox),
	}
}

// GetInfo reports what this control plane is running.
func (s *Server) GetInfo(_ context.Context, _ *quaycrewv1.GetInfoRequest) (*quaycrewv1.GetInfoResponse, error) {
	return &quaycrewv1.GetInfoResponse{
		Model:   s.info.Model,
		Sandbox: s.info.Sandbox,
		Store:   s.info.Store,
		State:   s.info.State,
		Events:  s.info.Events,
	}, nil
}

// storeError maps a store failure onto the status the caller should see.
func storeError(err error, what string) error {
	if errors.Is(err, store.ErrNotFound) {
		return status.Errorf(codes.NotFound, "%s not found", what)
	}
	return status.Errorf(codes.Internal, "%s: %v", what, err)
}

// sandboxFor returns the session's sandbox, creating it on first use so it is reused across turns.
//
// The sandbox is told which project and workspace the session belongs to, because the state that
// has to outlive it does not all sit at the same level: the conversation store and the workspace's
// context belong to the workspace, the working files and the project's context to the project.
//
// The workspace's environment is set on the sandbox itself, not just on each turn, so anything the
// operator starts inside it later (attaching to the conversation, for instance) is authenticated
// without the tool carrying the credential around. A token set after a session's first turn will not
// reach that session's existing sandbox: stop the session to get a fresh one.
func (s *Server) sandboxFor(ctx context.Context, session *quaycrewv1.Session) (sandbox.Sandbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Always ask the provider, never a remembered handle. What this process believes about containers
	// and what the daemon actually has drift constantly: an upgrade reaps them, a prune removes them,
	// a machine restart is fine but anything that removes one behind the control plane's back leaves
	// a handle here pointing at nothing, and a name is handed to the operator for a container that is
	// not there. Creating is idempotent, so the daemon is the source of truth and this map is only
	// what to close later.
	box, err := s.provider.Create(ctx, sandbox.Config{
		ID:        session.GetId(),
		Workspace: session.GetWorkspace(),
		Project:   session.GetProject(),
		Env:       environ(s.turnEnv(ctx, session.GetWorkspace())),
	})
	if err != nil {
		return nil, err
	}
	s.sandboxes[session.GetId()] = box
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

// CreateWorkspace creates a workspace at runtime.
func (s *Server) CreateWorkspace(ctx context.Context, req *quaycrewv1.CreateWorkspaceRequest) (*quaycrewv1.CreateWorkspaceResponse, error) {
	if err := name.Validate("workspace", req.GetName()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	workspace, err := s.store.CreateWorkspace(ctx, req.GetName())
	if err != nil {
		return nil, storeError(err, "create workspace")
	}
	return &quaycrewv1.CreateWorkspaceResponse{Workspace: workspace}, nil
}

// GetWorkspace returns a workspace by id.
func (s *Server) GetWorkspace(ctx context.Context, req *quaycrewv1.GetWorkspaceRequest) (*quaycrewv1.GetWorkspaceResponse, error) {
	workspace, err := s.store.GetWorkspace(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "workspace")
	}
	return &quaycrewv1.GetWorkspaceResponse{Workspace: workspace}, nil
}

// ListWorkspaces lists all workspaces.
func (s *Server) ListWorkspaces(ctx context.Context, _ *quaycrewv1.ListWorkspacesRequest) (*quaycrewv1.ListWorkspacesResponse, error) {
	workspaces, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return nil, storeError(err, "list workspaces")
	}
	return &quaycrewv1.ListWorkspacesResponse{Workspaces: workspaces}, nil
}

// DeleteWorkspace removes a workspace.
func (s *Server) DeleteWorkspace(ctx context.Context, req *quaycrewv1.DeleteWorkspaceRequest) (*quaycrewv1.DeleteWorkspaceResponse, error) {
	if err := s.store.DeleteWorkspace(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "workspace")
	}
	return &quaycrewv1.DeleteWorkspaceResponse{}, nil
}

// AttachChannel attaches a channel to a workspace.
func (s *Server) AttachChannel(ctx context.Context, req *quaycrewv1.AttachChannelRequest) (*quaycrewv1.AttachChannelResponse, error) {
	if req.GetWorkspace() == "" || req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace and id are required")
	}
	channel, err := s.store.AttachChannel(ctx, req.GetWorkspace(), req.GetId(), req.GetKind())
	if err != nil {
		return nil, storeError(err, "workspace")
	}
	return &quaycrewv1.AttachChannelResponse{Channel: channel}, nil
}

// SetSecret stores a workspace secret in the secrets backend. The value is never returned.
func (s *Server) SetSecret(ctx context.Context, req *quaycrewv1.SetSecretRequest) (*quaycrewv1.SetSecretResponse, error) {
	if req.GetWorkspace() == "" || req.GetKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace and key are required")
	}
	if _, err := s.store.GetWorkspace(ctx, req.GetWorkspace()); err != nil {
		return nil, storeError(err, "workspace")
	}
	if err := s.secrets.Set(ctx, req.GetWorkspace(), req.GetKey(), req.GetValue()); err != nil {
		return nil, status.Errorf(codes.Internal, "set secret: %v", err)
	}
	return &quaycrewv1.SetSecretResponse{}, nil
}

// CreateProject adds a body of work to a workspace.
func (s *Server) CreateProject(ctx context.Context, req *quaycrewv1.CreateProjectRequest) (*quaycrewv1.CreateProjectResponse, error) {
	if req.GetWorkspace() == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace is required")
	}
	if err := name.Validate("project", req.GetName()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	project, err := s.store.CreateProject(ctx, req.GetWorkspace(), req.GetName())
	if err != nil {
		return nil, storeError(err, "workspace")
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

// ListProjects lists projects, optionally within one workspace.
func (s *Server) ListProjects(ctx context.Context, req *quaycrewv1.ListProjectsRequest) (*quaycrewv1.ListProjectsResponse, error) {
	projects, err := s.store.ListProjects(ctx, req.GetWorkspace())
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

	box, err := s.sandboxFor(ctx, session)
	if err != nil {
		s.recordTurn(ctx, session.GetId(), "", "failed")
		return nil, status.Errorf(codes.Internal, "create sandbox: %v", err)
	}

	resp, err := s.runner.Run(ctx, box, model.Request{
		Text:           req.GetText(),
		ModelSessionID: session.GetModelSessionId(),
		PermissionMode: permissionModeOf(session),
		Env:            s.turnEnv(ctx, session.GetWorkspace()),
	})
	if err != nil {
		s.recordTurn(ctx, session.GetId(), "", "failed")
		return nil, status.Errorf(codes.Internal, "run turn: %v", err)
	}
	s.recordTurn(ctx, session.GetId(), resp.ModelSessionID, "idle")

	return &quaycrewv1.DispatchResponse{SessionId: session.GetId(), ThreadId: thread, Reply: resp.Reply}, nil
}

// permissionModeOf is the mode a thread's turns run in. A thread from before the mode was written
// down has none, and every one of those has been running acceptEdits, so that is what it keeps.
func permissionModeOf(session *quaycrewv1.Session) string {
	if mode := session.GetPermissionMode(); model.KnownPermissionMode(mode) {
		return mode
	}
	return model.PermissionAcceptEdits
}

// SetSessionPermissionMode changes what a thread's turns may do without asking.
//
// The mode belongs to the thread rather than to a turn, so a thread started to plan something keeps
// planning instead of being re armed on every dispatch. An unknown mode is refused here rather than
// handed to the model, which would take it as far as its own argument parser and no further.
func (s *Server) SetSessionPermissionMode(ctx context.Context, req *quaycrewv1.SetSessionPermissionModeRequest) (*quaycrewv1.SetSessionPermissionModeResponse, error) {
	if !model.KnownPermissionMode(req.GetMode()) {
		return nil, status.Errorf(codes.InvalidArgument,
			"%q is not a permission mode: use %s, %s or %s",
			req.GetMode(), model.PermissionPlan, model.PermissionAcceptEdits, model.PermissionBypass)
	}
	// The one place where skipping every permission means the host rather than a container. The local
	// backend is a stopgap for running without Docker, and arming a turn there gives the model the
	// machine the operator is sitting at.
	if req.GetMode() == model.PermissionBypass && s.info.Sandbox == "local" {
		return nil, status.Errorf(codes.FailedPrecondition,
			"this crew runs turns on the host, not in a container, so %s would give the model your machine",
			model.PermissionBypass)
	}
	if _, err := s.store.GetSession(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "session")
	}
	if err := s.store.SetPermissionMode(ctx, req.GetId(), req.GetMode()); err != nil {
		return nil, storeError(err, "session")
	}
	return &quaycrewv1.SetSessionPermissionModeResponse{Session: s.reread(ctx, req.GetId())}, nil
}

// recordTurn stores the outcome of a turn. A store failure here must not replace the turn's own
// result, which the operator already has, so it is not returned; a later read shows a stale status
// rather than the turn appearing to have failed when it did not.
func (s *Server) recordTurn(ctx context.Context, sessionID, modelSessionID, sessionStatus string) {
	_ = s.store.RecordTurn(ctx, sessionID, modelSessionID, sessionStatus)
}

// environ renders an environment map as the "KEY=value" entries a sandbox expects, sorted so the
// result is stable.
func environ(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]string, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, key+"="+values[key])
	}
	return entries
}

// turnEnv gathers the environment a turn runs with from the workspace's secrets. Right now that is the
// Claude Code subscription token, if one is set. A workspace that has not set it (or a model backend
// that does not need it) simply runs with no extra env, so the lookup never fails a turn.
func (s *Server) turnEnv(ctx context.Context, workspace string) map[string]string {
	token, err := s.secrets.Get(ctx, workspace, model.ClaudeCodeOAuthTokenEnv)
	if err != nil || token == "" {
		return nil
	}
	return map[string]string{model.ClaudeCodeOAuthTokenEnv: token}
}

// ListSessions lists sessions, optionally filtered by workspace.
func (s *Server) ListSessions(ctx context.Context, req *quaycrewv1.ListSessionsRequest) (*quaycrewv1.ListSessionsResponse, error) {
	sessions, err := s.store.ListSessions(ctx, store.SessionFilter{
		Workspace: req.GetWorkspace(),
		Project:   req.GetProject(),
		Archived:  req.GetArchived(),
	})
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

// AttachSession describes how to open a session's conversation.
//
// It returns the container and the command, and no credential. The conversation handle is a pointer
// into the model's own store, not a secret, so the operator's environment supplies the subscription
// token when they run it. Keeping the token out of this response is deliberate: a value the secrets
// backend holds should not become readable through the API just because a client asks nicely.
func (s *Server) AttachSession(ctx context.Context, req *quaycrewv1.AttachSessionRequest) (*quaycrewv1.AttachSessionResponse, error) {
	session, err := s.store.GetSession(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	if session.GetModelSessionId() == "" {
		return nil, status.Errorf(codes.FailedPrecondition,
			"thread %s has no conversation yet: send it a message with quay dispatch first",
			display.ShortID(session.GetThreadId()))
	}
	if session.GetStatus() == "stopped" {
		return nil, status.Errorf(codes.FailedPrecondition,
			"thread %s is stopped: restart it first", display.ShortID(session.GetThreadId()))
	}
	if session.GetArchivedAt() != nil {
		return nil, status.Errorf(codes.FailedPrecondition,
			"thread %s is archived: restore it first", display.ShortID(session.GetThreadId()))
	}
	// A handle can outlive what it points at: every conversation from a sandbox created before the
	// conversations were kept on the host died with that container, while the row kept the handle.
	// Resuming one of those prints "No conversation found" and exits, which from the console looks
	// like nothing happening at all, so say it here instead of starting a container to fail inside.
	//
	// In the operator's words, not ours. "Its conversation predates state on the host" is a sentence
	// only somebody who worked on this understands, and it named an identifier twenty four characters
	// long that appears nowhere on their screen.
	if !s.storage.HasConversation(session.GetWorkspace(), session.GetModelSessionId()) {
		return nil, status.Errorf(codes.FailedPrecondition,
			"thread %s has no conversation left: it was saved inside a sandbox that has since been "+
				"removed, from before conversations were kept on this machine. Send this thread a "+
				"message with quay dispatch to start a new one",
			display.ShortID(session.GetThreadId()))
	}
	// Make sure there is something to attach to. The live sandboxes are a map in this process, so a
	// restart empties it while the row still says idle, and answering from the row alone hands the
	// operator a container name the daemon has never heard of. The conversation is on the host, so a
	// fresh container over the same mounts is the same conversation.
	if _, err := s.sandboxFor(ctx, session); err != nil {
		return nil, status.Errorf(codes.Internal, "start sandbox: %v", err)
	}
	// The same mode the thread's turns run in. Without it an attached session runs as whatever the
	// model defaults to, so a thread armed to skip permissions still stops and asks the moment the
	// operator opens it, which reads as the toggle not working.
	return &quaycrewv1.AttachSessionResponse{
		Sandbox: sandbox.ContainerName(session.GetId()),
		Argv: []string{"claude", "--resume", session.GetModelSessionId(),
			"--permission-mode", permissionModeOf(session)},
	}, nil
}

// RestartSession brings a stopped session back to idle, with its sandbox running.
//
// The sandbox is started here rather than on the next turn, so the operator can attach into the
// conversation straight away instead of having to dispatch a turn to make the container exist. That
// is only safe because a session's state lives on the host now: the sandbox this creates is a new
// container over the same conversation store and the same project files.
//
// The sandbox comes first and the status second. A sandbox that will not start leaves the session
// stopped, which is what it is, rather than idle and unreachable.
func (s *Server) RestartSession(ctx context.Context, req *quaycrewv1.RestartSessionRequest) (*quaycrewv1.RestartSessionResponse, error) {
	session, err := s.store.GetSession(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	if session.GetStatus() != "stopped" {
		return nil, status.Errorf(codes.FailedPrecondition,
			"session %s is %s, not stopped, so there is nothing to restart", req.GetId(), session.GetStatus())
	}
	if _, err := s.sandboxFor(ctx, session); err != nil {
		return nil, status.Errorf(codes.Internal, "create sandbox: %v", err)
	}
	if err := s.store.RestartSession(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "session")
	}
	restarted, err := s.store.GetSession(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	return &quaycrewv1.RestartSessionResponse{Session: restarted}, nil
}

// ArchiveSession puts a thread away, stopping it first if it is running.
//
// Nothing is deleted, by anyone, here: the row, the conversation handle, the conversation store on
// the host and the project's files are all untouched. Archiving is a stamp, so restoring is clearing
// it. Deleting a conversation is a separate decision and not something to slip in behind a key.
func (s *Server) ArchiveSession(ctx context.Context, req *quaycrewv1.ArchiveSessionRequest) (*quaycrewv1.ArchiveSessionResponse, error) {
	session, err := s.store.GetSession(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	if session.GetArchivedAt() != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "session %s is already archived", req.GetId())
	}
	// A container left running for a thread nobody can see is exactly the leak this project keeps
	// finding, so put the sandbox away with the thread.
	if session.GetStatus() != "stopped" {
		if err := s.store.StopSession(ctx, req.GetId()); err != nil {
			return nil, storeError(err, "session")
		}
		s.closeSandbox(ctx, req.GetId())
	}
	if err := s.store.ArchiveSession(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "session")
	}
	return &quaycrewv1.ArchiveSessionResponse{Session: s.reread(ctx, req.GetId())}, nil
}

// RestoreSession brings an archived thread back into the default listing. It comes back stopped,
// which is what it is: archiving stopped it, and starting a container again is what restart is for.
func (s *Server) RestoreSession(ctx context.Context, req *quaycrewv1.RestoreSessionRequest) (*quaycrewv1.RestoreSessionResponse, error) {
	session, err := s.store.GetSession(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	if session.GetArchivedAt() == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "session %s is not archived", req.GetId())
	}
	if err := s.store.RestoreSession(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "session")
	}
	return &quaycrewv1.RestoreSessionResponse{Session: s.reread(ctx, req.GetId())}, nil
}

// reread returns the session as it now is, so a caller does not have to ask again. A read that fails
// here is not worth failing the write that already succeeded, so it yields nothing rather than an
// error the caller would misread as the action not having happened.
func (s *Server) reread(ctx context.Context, id string) *quaycrewv1.Session {
	session, err := s.store.GetSession(ctx, id)
	if err != nil {
		return nil
	}
	return session
}

// StopSession marks a session stopped and tears down its sandbox.
func (s *Server) StopSession(ctx context.Context, req *quaycrewv1.StopSessionRequest) (*quaycrewv1.StopSessionResponse, error) {
	if err := s.store.StopSession(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "session")
	}
	s.closeSandbox(ctx, req.GetId())
	return &quaycrewv1.StopSessionResponse{}, nil
}
