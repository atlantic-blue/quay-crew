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
	"io"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/manual"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/name"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/skill"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	// Secrets is where a workspace's credentials are kept, for example "postgres, sealed".
	Secrets string
	// SandboxBuild is the build of the crew the sandbox image was made from. Empty means the image
	// does not say, and nothing is then claimed about it.
	SandboxBuild string
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
	// Events is the log every turn is written to. Nil means nowhere, which is a stack with no broker
	// configured rather than an error: turns run, and nothing records that they did.
	Events messaging.EventLog
	// Info describes the three above in words, for the console's status block.
	Info Info
	// Skills are the capabilities the crew has been given, read from files, and every session gets them.
	// A skill imported into the store and attached to a workspace reaches that workspace's sessions as
	// well, which is the other half of the same idea. See docs/SKILLS.md.
	Skills []skill.Skill
	// SkillsHost is the skills directory as the host daemon sees it, which is what a bind mount needs.
	// Empty means skills are not mounted, the same way an unset data directory means state is not kept.
	SkillsHost string
	// SandboxImage is named in the refusal when a skill needs a binary that is not in it.
	SandboxImage string
	// GitAuthor is who a commit made inside a sandbox is by: both a name and an address, or neither.
	// Without it git refuses to commit rather than guessing.
	GitAuthor Identity
	// SandboxSecrets are the workspace secrets a session's sandbox is given, by name. The model's own
	// token is always carried and does not need naming here.
	//
	// Named rather than all of them, because a sandbox holds a value for the life of its container
	// and the model can read it.
	//
	// A skill names its own secrets and those reach a session that holds it without being listed here.
	// This is the crew wide list: what every session may reach whatever it holds.
	SandboxSecrets []string
	// Reachable is the address a session should dial to reach this control plane, put into every
	// sandbox as QC_GRPC_ADDR so `quay` inside one drives the crew without being told where it is.
	//
	// Empty means a session cannot reach the crew at all, which is the default: a session that can
	// drive the crew can also stop other sessions, so it is turned on rather than assumed.
	Reachable string
}

// Identity is who a commit is by.
type Identity struct {
	Name  string
	Email string
}

// Complete says whether this is enough to commit with. Half of one is worse than none: git refuses
// either way, and a half identity looks configured.
func (i Identity) Complete() bool {
	return strings.TrimSpace(i.Name) != "" && strings.TrimSpace(i.Email) != ""
}

// Server implements quaycrewv1.ControlPlaneServiceServer.
type Server struct {
	quaycrewv1.UnimplementedControlPlaneServiceServer
	store    store.Store
	secrets  secrets.Store
	runner   model.Runner
	provider sandbox.Provider
	storage  sandbox.Storage
	// reachable is where a session dials to reach this control plane. Empty means it cannot.
	reachable string
	// sandboxSecrets are the workspace secrets a sandbox is given, by name.
	sandboxSecrets []string
	// gitAuthor is who a commit made inside a sandbox is by.
	gitAuthor Identity
	// skills are the capabilities a session is given, and where they are on the host.
	skills       []skill.Skill
	skillsHost   string
	sandboxImage string
	events       messaging.EventLog
	info         Info

	mu        sync.Mutex
	sandboxes map[string]sandbox.Sandbox // one per session, created lazily, closed on stop
}

// NewServer builds a control plane over a durable store, a model runner (the Claude Code adapter by
// default), a sandbox provider (one sandbox per session) and a secrets store.
func NewServer(cfg Config) *Server {
	return &Server{
		store:          cfg.Store,
		secrets:        cfg.Secrets,
		runner:         cfg.Runner,
		provider:       cfg.Provider,
		storage:        cfg.Storage,
		events:         eventsOr(cfg.Events),
		info:           cfg.Info,
		reachable:      cfg.Reachable,
		sandboxSecrets: cfg.SandboxSecrets,
		gitAuthor:      cfg.GitAuthor,
		skills:         cfg.Skills,
		skillsHost:     cfg.SkillsHost,
		sandboxImage:   cfg.SandboxImage,
		sandboxes:      make(map[string]sandbox.Sandbox),
	}
}

// eventsOr is the log to publish on, and Discard when there is none, so nothing downstream has to
// ask whether there is a broker before writing a record.
func eventsOr(log messaging.EventLog) messaging.EventLog {
	if log == nil {
		return messaging.Discard{}
	}
	return log
}

// ListTurns returns a session's history, oldest first, from the read model the projection writes.
//
// It reads the store rather than the log: the log is the write side and replaying it on every
// request would make a listing cost more the longer a crew has been running.
func (s *Server) ListTurns(ctx context.Context, req *quaycrewv1.ListTurnsRequest) (*quaycrewv1.ListTurnsResponse, error) {
	if req.GetSession() == "" {
		return nil, status.Error(codes.InvalidArgument, "session is required")
	}
	if _, err := s.store.GetSession(ctx, req.GetSession()); err != nil {
		return nil, storeError(err, "session")
	}
	turns, err := s.store.ListTurns(ctx, req.GetSession(), int(req.GetLimit()))
	if err != nil {
		return nil, storeError(err, "list turns")
	}
	return &quaycrewv1.ListTurnsResponse{Turns: turns}, nil
}

// GetInfo reports what this control plane is running.
func (s *Server) GetInfo(_ context.Context, _ *quaycrewv1.GetInfoRequest) (*quaycrewv1.GetInfoResponse, error) {
	return &quaycrewv1.GetInfoResponse{
		Model:        s.info.Model,
		Sandbox:      s.info.Sandbox,
		Store:        s.info.Store,
		State:        s.info.State,
		Events:       s.info.Events,
		Secrets:      s.info.Secrets,
		SandboxBuild: s.info.SandboxBuild,
	}, nil
}

// GetUsage adds up what every conversation in the crew has cost. Its own call rather than part of
// GetInfo, which answers configuration and is fetched once. Archived threads are counted: a total
// that shrinks when somebody tidies up is worse than no total.
func (s *Server) GetUsage(ctx context.Context, _ *quaycrewv1.GetUsageRequest) (*quaycrewv1.GetUsageResponse, error) {
	var total sandbox.Usage
	var threads int64
	for _, archived := range []bool{false, true} {
		sessions, err := s.store.ListSessions(ctx, store.SessionFilter{Archived: archived})
		if err != nil {
			return nil, storeError(err, "list sessions")
		}
		for _, session := range sessions {
			spent := s.storage.ConversationUsage(session.GetWorkspace(), session.GetModelSessionId())
			if spent.Empty() {
				continue
			}
			total = total.Add(spent)
			threads++
		}
	}
	return &quaycrewv1.GetUsageResponse{
		Total: &quaycrewv1.Usage{
			Input:        total.Input,
			Output:       total.Output,
			CacheRead:    total.CacheRead,
			CacheWritten: total.CacheWritten,
		},
		Threads: threads,
	}, nil
}

// sandboxError maps a sandbox failure onto the status the caller should see.
//
// A refusal that already says what it is keeps saying it. Wrapping everything as Internal turned "the
// github skill needs gh and the image does not have it", which the operator can act on, into a server
// fault, which reads as the crew being broken.
func sandboxError(err error, what string) error {
	if _, said := status.FromError(err); said && status.Code(err) != codes.Unknown {
		return err
	}
	return status.Errorf(codes.Internal, "%s: %v", what, err)
}

// storeError maps a store failure onto the status the caller should see.
func storeError(err error, what string) error {
	if errors.Is(err, store.ErrNotFound) {
		return status.Errorf(codes.NotFound, "%s not found", what)
	}
	return status.Errorf(codes.Internal, "%s: %v", what, err)
}

// sandboxFor returns the session's sandbox, creating it on first use.
//
// The environment is set on the sandbox itself rather than on each turn, so attaching to the
// conversation is authenticated too. A token set after the first turn does not reach the existing
// sandbox: stop the session to get a fresh one.
func (s *Server) sandboxFor(ctx context.Context, session *quaycrewv1.Session) (sandbox.Sandbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Always ask the provider, never a remembered handle. What this process believes about containers
	// and what the daemon actually has drift constantly: an upgrade reaps them, a prune removes them,
	// a machine restart is fine but anything that removes one behind the control plane's back leaves
	// a handle here pointing at nothing, and a name is handed to the operator for a container that is
	// not there. Creating is idempotent, so the daemon is the source of truth and this map is only
	// what to close later.
	s.syncContext(ctx, session)
	// What the session is missing, said before a container exists rather than discovered inside one.
	// A capability that silently does not work is worse than one that is absent, because the model
	// improvises around it and the operator reads the improvisation as the answer.
	if err := s.skillsAreUsable(ctx, session); err != nil {
		return nil, err
	}
	box, err := s.provider.Create(ctx, sandbox.Config{
		ID:        session.GetId(),
		Workspace: session.GetWorkspace(),
		Project:   session.GetProject(),
		Env:       environ(s.turnEnv(ctx, session)),
		Mounts:    s.skillMounts(ctx, session),
		Driver:    session.GetDriver(),
	})
	if err != nil {
		return nil, err
	}
	if err := s.readySkills(ctx, session, box); err != nil {
		return nil, err
	}
	if err := s.cloneRemote(ctx, session, box); err != nil {
		return nil, err
	}
	s.sandboxes[session.GetId()] = box
	return box, nil
}

// cloneRemote puts the project's repository in front of the session, once.
//
// A session's working directory starts empty, so a git skill had nothing to work in: the crew could
// describe how to commit and there was nowhere to commit. This is where the code comes from.
//
// Once per container, and the command itself is what makes that true: it clones only when the checkout
// is not already there. A sandbox is adopted across turns, so a command that cloned every time would
// either fail on the second turn or throw away whatever the first one did. The check is inside the
// container because that is the only place that knows what this container has, which is the same reason
// a skill's setup checks its own marker there.
//
// It clones into a directory under the working directory rather than into it. The memory file the model
// reads is written there before the sandbox exists, and git refuses to clone into somewhere that is not
// empty.
//
// A failure fails the turn, unlike context. Being asked to work in a repository that is not there is
// worse than being told the clone did not work: the model improvises, and an improvised repository looks
// like an answer.
func (s *Server) cloneRemote(ctx context.Context, session *quaycrewv1.Session, box sandbox.Sandbox) error {
	project, err := s.store.GetProject(ctx, session.GetProject())
	if err != nil || project.GetRemote() == "" {
		return nil
	}
	spec, err := sandbox.CloneSpec(project.GetRemote(), sandbox.WorkingPath)
	if err != nil {
		return status.Errorf(codes.FailedPrecondition, "%v", err)
	}
	proc, err := box.Exec(ctx, spec)
	if err != nil {
		return status.Errorf(codes.Internal, "clone %s: %v", project.GetRemote(), err)
	}
	_, _ = io.Copy(io.Discard, proc.Stdout())
	if err := proc.Wait(); err != nil {
		// The remote and what git said, and never the credential: it is not in the command, and what
		// comes back on the error stream is git's own words.
		return status.Errorf(codes.FailedPrecondition,
			"could not clone %s into this session: %v: %s. The crew reads the credential from the "+
				"workspace secret %s, so check that is set with quay secret set %s %s <value>",
			project.GetRemote(), err, proc.Stderr(), sandbox.CredentialEnv,
			session.GetWorkspace(), sandbox.CredentialEnv)
	}
	return nil
}

// heldSkills is what a session holds, from both places a skill can come from, sorted by name.
//
// The crew's own skills directory reaches every session, which is the crew level. A skill imported into
// the store and attached to a workspace reaches the sessions in that workspace, which is the workspace
// level. A workspace's own wins where the names collide, because the narrower statement of what a
// session should hold is the more deliberate one.
//
// A failure reading the store is not a failure of the turn: the crew's skills still reach the session,
// and a session with one skill instead of two is better than a session that will not start.
func (s *Server) heldSkills(ctx context.Context, session *quaycrewv1.Session) []skill.Held {
	held := make([]skill.Held, 0, len(s.skills))
	attached, err := s.store.WorkspaceSkills(ctx, session.GetWorkspace())
	if err == nil {
		for _, one := range attached {
			held = append(held, skill.Held{
				Skill: one.Skill,
				// Written out of the store into the workspace's own directory, and mounted from there.
				BriefPath: skill.BriefPathIn(sandbox.SkillsPath, one.Name),
			})
		}
	}
	for _, given := range s.skills {
		if slices.ContainsFunc(held, func(one skill.Held) bool { return one.Name == given.Name }) {
			continue
		}
		held = append(held, skill.Held{
			Skill:     given,
			BriefPath: skill.BriefPathIn(sandbox.SkillsPath, given.Name),
		})
	}
	sort.Slice(held, func(i, j int) bool { return held[i].Name < held[j].Name })
	return held
}

// skillMounts is where each of a session's skills appears inside its sandbox, read only.
//
// Read only because a session that can rewrite its own instructions can give itself a capability nobody
// approved. The two sources differ only in which host directory the files are in: the operator's own for
// the crew's skills, and the workspace's own for the ones written out of the store.
func (s *Server) skillMounts(ctx context.Context, session *quaycrewv1.Session) []sandbox.Mount {
	var mounts []sandbox.Mount
	if attached, err := s.store.WorkspaceSkills(ctx, session.GetWorkspace()); err == nil && len(attached) > 0 {
		if host, ok := s.storage.WorkspaceSkillsHost(session.GetWorkspace()); ok {
			for _, one := range attached {
				mounts = append(mounts, sandbox.Mount{
					Source:   path.Join(host, one.Name),
					Target:   skill.DirIn(sandbox.SkillsPath, one.Name),
					ReadOnly: true,
				})
			}
		}
	}
	if s.skillsHost == "" {
		return mounts
	}
	for _, given := range s.skills {
		target := skill.DirIn(sandbox.SkillsPath, given.Name)
		// Two mounts on one target is a container that will not start, so a name the workspace has
		// already claimed is left to the workspace.
		if slices.ContainsFunc(mounts, func(one sandbox.Mount) bool { return one.Target == target }) {
			continue
		}
		mounts = append(mounts, sandbox.Mount{
			Source:   path.Join(s.skillsHost, given.Name),
			Target:   target,
			ReadOnly: true,
		})
	}
	return mounts
}

// skillsAreUsable refuses when a skill names a secret the workspace has not set.
//
// Before the sandbox, because this is the half that can be answered without one, and a refusal that
// arrives before anything is built is cheaper to read and cheaper to fix. The binaries are the other
// half and they can only be answered inside the container, which readySkills does.
func (s *Server) skillsAreUsable(ctx context.Context, session *quaycrewv1.Session) error {
	for _, given := range s.heldSkills(ctx, session) {
		for _, name := range given.SecretNames() {
			value, err := s.secrets.Get(ctx, session.GetWorkspace(), name)
			if err == nil && value != "" {
				continue
			}
			return status.Errorf(codes.FailedPrecondition,
				"the %s skill needs the secret %s, which this workspace has not set: %s. "+
					"Set it with quay secret set %s %s <value>",
				given.Name, name, given.Secrets[name], session.GetWorkspace(), name)
		}
	}
	return nil
}

// readySkills checks the sandbox has what its skills need, and runs each skill's setup once.
//
// Inside the container because that is the only place that knows what the image carries. Once,
// because a sandbox is adopted across turns and a setup script run on every turn is a script whose
// author has to think about being run a thousand times. The marker lives in the container, so a
// replaced container runs setup again, which is right: it is the container that was set up.
func (s *Server) readySkills(ctx context.Context, session *quaycrewv1.Session, box sandbox.Sandbox) error {
	for _, given := range s.heldSkills(ctx, session) {
		for _, binary := range given.Binaries {
			if s.has(ctx, box, binary) {
				continue
			}
			_ = box.Close(ctx)
			return status.Errorf(codes.FailedPrecondition,
				"the %s skill needs %s and the sandbox image does not have it. Add it to %s",
				given.Name, binary, s.imageName())
		}
		if !given.HasSetup {
			continue
		}
		at := path.Join(sandbox.SkillsPath, given.Name)
		marker := path.Join("/tmp", ".quay-setup-"+given.Name)
		proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c",
			"[ -f " + marker + " ] || { " + path.Join(at, skill.SetupFile) + " && touch " + marker + "; }"}})
		if err != nil {
			return status.Errorf(codes.Internal, "set up the %s skill: %v", given.Name, err)
		}
		_, _ = io.Copy(io.Discard, proc.Stdout())
		if err := proc.Wait(); err != nil {
			return status.Errorf(codes.FailedPrecondition,
				"the %s skill could not set itself up in the sandbox: %v: %s",
				given.Name, err, proc.Stderr())
		}
	}
	return nil
}

// has says whether a command is in the sandbox.
func (s *Server) has(ctx context.Context, box sandbox.Sandbox, binary string) bool {
	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c", "command -v " + binary}})
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, proc.Stdout())
	return proc.Wait() == nil
}

// imageName is the image to go and fix, and something readable when the crew was not told which it is.
func (s *Server) imageName() string {
	if s.sandboxImage == "" {
		return "the sandbox image"
	}
	return s.sandboxImage
}

// syncContext makes the files the model reads agree with the store, in both directions.
//
// The store holds context because a pod has no host directory to mount. The file is a rendering of
// it, and it is read back first: an agent that wrote into its own CLAUDE.md has learned something,
// and overwriting that would make the crew's memory worse than a text file.
//
// A failure here never fails a turn.
func (s *Server) syncContext(ctx context.Context, session *quaycrewv1.Session) {
	s.syncContextExcept(ctx, session, contextLevel{})
}

// syncContextExcept is syncContext with one level exempt from the read back, because somebody has
// just set it.
//
// `quay context set` is the operator saying what a level is now. Reading the file back for that level
// first would hand them back the very body they were replacing, so at that one level the store wins
// and everything else is still read back and kept.
func (s *Server) syncContextExcept(ctx context.Context, session *quaycrewv1.Session, settled contextLevel) {
	dirs := s.storage.MyDirs(sandbox.Config{
		ID: session.GetId(), Workspace: session.GetWorkspace(), Project: session.GetProject(),
	})
	if len(dirs) != 2 {
		return
	}
	for at, levels := range contextFiles(session) {
		// Read back first. Something inside the sandbox writing into its own memory has learned
		// something, and overwriting that on the next turn would make the crew's memory strictly
		// worse than a text file.
		scopes := make([]string, 0, len(levels)+1)
		for _, level := range levels {
			scopes = append(scopes, string(level.scope))
		}
		// The skills index is a section in the same file and is not a level: it is rendered from what the
		// session holds rather than written by anybody. It is named so the read back recognises its mark
		// and leaves it where it is, because text under a mark this build does not know is swept into the
		// innermost level, which would store the index as though the operator had typed it and then render
		// it again underneath itself on the next turn.
		if at == 0 {
			scopes = append(scopes, sandbox.SkillsScope)
		}
		if onDisk, found := sandbox.ReadMemory(dirs[at]); found {
			written := sandbox.Decompose(onDisk, scopes)
			for _, level := range levels {
				if level == settled {
					continue
				}
				body, said := written[string(level.scope)]
				if !said {
					continue
				}
				kept, err := s.store.GetContext(ctx, level.scope, level.owner)
				if err != nil || kept == body {
					continue
				}
				// An unmarked file was never rendered by the crew, so what is in it cannot be an
				// edit of what the store holds: whoever wrote it had never seen the store's body.
				// Treating it as one costs the store's body entirely, which is how a driver taught
				// the manual lost it again on the first sync after being told. Both are kept.
				if !sandbox.Marked(onDisk) {
					body = join(kept, body)
					if body == kept {
						continue
					}
				}
				_ = s.store.SetContext(ctx, level.scope, level.owner, body)
			}
		}
	}
	s.renderContext(ctx, session)
}

// join puts two bodies together, keeping both, and keeps the first as it is when it already says the
// second.
func join(kept, added string) string {
	switch {
	case strings.TrimSpace(kept) == "":
		return added
	case strings.TrimSpace(added) == "", strings.Contains(kept, strings.TrimSpace(added)):
		return kept
	default:
		return strings.TrimRight(kept, "\n") + "\n\n" + added
	}
}

// renderContext writes what the store holds into the files the model reads, and reads nothing back:
// `quay context set` is the operator saying what the context is now, so the store wins.
//
// A conversation already running does not see it. The tool reads its memory at the start, so a change
// lands on the next turn or the next open.
func (s *Server) renderContext(ctx context.Context, session *quaycrewv1.Session) {
	dirs := s.storage.MyDirs(sandbox.Config{
		ID: session.GetId(), Workspace: session.GetWorkspace(), Project: session.GetProject(),
	})
	if len(dirs) != 2 {
		return
	}
	for at, levels := range contextFiles(session) {
		sections := make([]sandbox.Section, 0, len(levels)+1)
		for _, level := range levels {
			body, err := s.store.GetContext(ctx, level.scope, level.owner)
			if err != nil {
				continue
			}
			sections = append(sections, sandbox.Section{Scope: string(level.scope), Body: body})
		}
		// The index goes in the outer file, beside the workspace's own context, because every session in
		// a workspace holds the same skills: the crew's, and the ones attached to that workspace. It is
		// rendered from what the session holds every time and never read back, so a skill edited in its
		// own directory reaches the next sandbox and an edit from inside one does not survive.
		if at == outerFile {
			if index := s.renderSkills(ctx, session, dirs[at]); index != "" {
				sections = append(sections, sandbox.Section{Scope: sandbox.SkillsScope, Body: index})
			}
		}
		_ = sandbox.WriteMemory(dirs[at], sandbox.Compose(sections))
	}
}

// renderSkills writes the workspace's own skills out of the store onto the host, and returns the index
// naming everything the session holds.
//
// The files and the index are written together on purpose. An index naming a brief that is not there
// sends the model to open a file that does not exist, and files with no index are a capability nothing
// ever mentions.
//
// The crew's skills are not written: they are already a directory the operator keeps, and they are
// mounted from it. Only the ones that live in the store have to be put somewhere before they can be.
//
// A failure here does not fail a turn, for the same reason a context failure does not. The model is told
// what it holds rather than made to depend on being told.
func (s *Server) renderSkills(ctx context.Context, session *quaycrewv1.Session, _ string) string {
	held := s.heldSkills(ctx, session)
	if attached, err := s.store.WorkspaceSkills(ctx, session.GetWorkspace()); err == nil {
		if dir, ok := s.storage.WorkspaceSkillsDir(session.GetWorkspace()); ok {
			skills := make([]skill.Skill, 0, len(attached))
			for _, one := range attached {
				skills = append(skills, one.Skill)
			}
			if err := sandbox.WriteSkills(dir, skills); err != nil {
				return ""
			}
		}
	}
	// The paths in the index are the sandbox's, because the model is the reader and its view of these
	// directories is the mount rather than the host path this process just wrote to.
	return skill.Index(held)
}

// renderTo writes a changed context out to every live session that reads it, so a change reaches the
// sandboxes that are already running rather than waiting for each of them to be replaced.
//
// Archived threads are left alone. They are put away, their sandboxes are gone, and the render would
// only create directories for conversations nobody is having.
func (s *Server) renderTo(ctx context.Context, scope store.ContextScope, owner string) {
	sessions, err := s.store.ListSessions(ctx, store.SessionFilter{})
	if err != nil {
		return
	}
	for _, session := range sessions {
		if reads(session, scope, owner) {
			s.syncContextExcept(ctx, session, contextLevel{scope, owner})
		}
	}
}

// reads says whether a session's memory files carry this level of context.
func reads(session *quaycrewv1.Session, scope store.ContextScope, owner string) bool {
	switch scope {
	case store.ContextCrew:
		return true
	case store.ContextWorkspace:
		return session.GetWorkspace() == owner
	case store.ContextProject:
		return session.GetProject() == owner
	case store.ContextSession:
		return session.GetId() == owner
	default:
		return false
	}
}

// The two memory files a session reads: the outer one in the workspace's conversation store, carrying
// the crew's context, the workspace's, and the index of the skills the session holds; the inner one in
// the session's own working directory, carrying the project's context and the session's own.
const (
	outerFile = 0
	innerFile = 1
)

// contextLevel is one level of context and whose it is.
type contextLevel struct {
	scope store.ContextScope
	owner string
}

// contextFiles is what goes in each of a session's two memory files: the outer two levels in the
// conversation store's directory, which every session in the workspace reads, and the inner two in
// this session's own working directory, which only it reads.
func contextFiles(session *quaycrewv1.Session) [][]contextLevel {
	return [][]contextLevel{
		{{store.ContextCrew, ""}, {store.ContextWorkspace, session.GetWorkspace()}},
		{{store.ContextProject, session.GetProject()}, {store.ContextSession, session.GetId()}},
	}
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

// ListSecrets says what each workspace has set, and never what any of it says.
//
// The response has no field for a value, so this cannot leak one by mistake rather than by policy.
// What an operator needs from a list of secrets is whether the thing is there, and everything else
// about it is the crew's business.
func (s *Server) ListSecrets(ctx context.Context, req *quaycrewv1.ListSecretsRequest) (*quaycrewv1.ListSecretsResponse, error) {
	workspaces, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return nil, storeError(err, "list workspaces")
	}

	out := make([]*quaycrewv1.SecretRef, 0, len(workspaces))
	for _, workspace := range workspaces {
		if req.GetWorkspace() != "" && workspace.GetId() != req.GetWorkspace() {
			continue
		}
		names, err := s.secrets.Names(ctx, workspace.GetId())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "list secrets: %v", err)
		}
		for _, name := range names {
			out = append(out, &quaycrewv1.SecretRef{
				Workspace:     workspace.GetId(),
				WorkspaceName: workspace.GetName(),
				Name:          name,
			})
		}
	}
	return &quaycrewv1.ListSecretsResponse{Secrets: out}, nil
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

// SetProjectRemote names the repository this project's sessions work in.
//
// The remote is checked here rather than when a clone runs. A refusal at the moment somebody sets it is
// read by the person who typed it; the same refusal on a first turn arrives at whoever was trying to get
// work done, hours later, with nothing pointing back at where it came from.
//
// Sessions already running keep the checkout they have. A remote is where new work comes from, not an
// instruction to replace what a conversation is in the middle of.
func (s *Server) SetProjectRemote(ctx context.Context, req *quaycrewv1.SetProjectRemoteRequest) (*quaycrewv1.SetProjectRemoteResponse, error) {
	if req.GetRemote() != "" {
		if err := sandbox.UsableRemote(req.GetRemote()); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "%v", err)
		}
		if _, err := sandbox.RepositoryName(req.GetRemote()); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "%v", err)
		}
	}
	if err := s.store.SetProjectRemote(ctx, req.GetProject(), req.GetRemote()); err != nil {
		return nil, storeError(err, "project")
	}
	project, err := s.store.GetProject(ctx, req.GetProject())
	if err != nil {
		return nil, storeError(err, "project")
	}
	return &quaycrewv1.SetProjectRemoteResponse{Project: project}, nil
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
		s.publishTurn(ctx, session, &quaycrewv1.TurnEvent{
			Prompt: req.GetText(), Status: "failed", Failure: "the session's sandbox could not be created",
		})
		return nil, sandboxError(err, "create sandbox")
	}

	resp, err := s.runner.Run(ctx, box, model.Request{
		Text:           req.GetText(),
		ModelSessionID: session.GetModelSessionId(),
		PermissionMode: permissionModeOf(session),
		Env:            s.turnEnv(ctx, session),
	})
	if err != nil {
		s.recordTurn(ctx, session.GetId(), "", "failed")
		s.publishTurn(ctx, session, &quaycrewv1.TurnEvent{
			Prompt: req.GetText(), Status: "failed", Failure: "the model did not complete the turn",
		})
		return nil, status.Errorf(codes.Internal, "run turn: %v", err)
	}
	s.recordTurn(ctx, session.GetId(), resp.ModelSessionID, "idle")
	s.publishTurn(ctx, session, &quaycrewv1.TurnEvent{
		Prompt: req.GetText(), Reply: resp.Reply, Status: "idle",
	})

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

// ListContexts says where the files the model reads live: for one project, or for the whole crew.
//
// The tool runs on the operator's machine and knows nothing of where this process keeps data, so the
// paths come from here. Both the console and the command line are clients of this one call rather
// than each working the layout out for itself, which is the only way the two cannot drift.
func (s *Server) ListContexts(ctx context.Context, req *quaycrewv1.ListContextsRequest) (*quaycrewv1.ListContextsResponse, error) {
	projects, err := s.contextProjects(ctx, req.GetProject())
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	workspaces, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return nil, storeError(err, "list workspaces")
	}
	for _, workspace := range workspaces {
		names[workspace.GetId()] = workspace.GetName()
	}

	dirs := make([]*quaycrewv1.ContextDir, 0, len(projects)*2+1)
	// The crew's own context, first, because it is the level everything else sits inside, and however
	// narrow the question it is part of the answer: every session in the crew reads it. It belongs to
	// no directory, being rendered into every workspace's file, so there is no one file to name.
	dirs = append(dirs, s.contextDir(ctx, store.ContextCrew, "", "crew", sandbox.Context{}))
	seenWorkspace := map[string]bool{}
	for _, project := range projects {
		found := s.storage.Contexts(sandbox.Config{
			ID: "listing", Workspace: project.GetWorkspace(), Project: project.GetId(),
		})
		if len(found) != 2 {
			continue
		}
		// One row per workspace however many projects it holds: the workspace's context is one thing,
		// and listing it twice would read as two.
		if !seenWorkspace[project.GetWorkspace()] {
			seenWorkspace[project.GetWorkspace()] = true
			dirs = append(dirs, s.contextDir(ctx, store.ContextWorkspace,
				project.GetWorkspace(), names[project.GetWorkspace()], found[0]))
		}
		dirs = append(dirs, s.contextDir(ctx, store.ContextProject,
			project.GetId(), project.GetName(), found[1]))
	}
	return &quaycrewv1.ListContextsResponse{Dirs: dirs}, nil
}

// SetContext records what the model should be told at a scope, and renders it into every directory
// that already exists for it, so a sandbox already running picks it up on its next turn.
func (s *Server) SetContext(ctx context.Context, req *quaycrewv1.SetContextRequest) (*quaycrewv1.SetContextResponse, error) {
	scope := store.ContextScope(req.GetScope())
	if !store.KnownContextScope(scope) {
		return nil, status.Errorf(codes.InvalidArgument,
			"%q is not a scope: use %s, %s or %s",
			req.GetScope(), store.ContextCrew, store.ContextWorkspace, store.ContextProject)
	}
	if scope != store.ContextCrew && req.GetOwner() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "a %s context needs to say which one", scope)
	}
	if err := s.store.SetContext(ctx, scope, req.GetOwner(), req.GetBody()); err != nil {
		return nil, storeError(err, "context")
	}
	// Out to the sessions that read it. Without this a context only reached a sandbox when that
	// sandbox was created, so telling a running thread something did nothing you could see until it
	// was replaced, which is not a thing an operator would think to do.
	s.renderTo(ctx, scope, req.GetOwner())
	return &quaycrewv1.SetContextResponse{
		Dir: &quaycrewv1.ContextDir{
			Scope: req.GetScope(), Owner: req.GetOwner(),
			Body: req.GetBody(), Written: req.GetBody() != "",
		},
	}, nil
}

// contextProjects is the projects a listing covers: one when asked for, else every one the crew has.
func (s *Server) contextProjects(ctx context.Context, project string) ([]*quaycrewv1.Project, error) {
	if project == "" {
		projects, err := s.store.ListProjects(ctx, "")
		if err != nil {
			return nil, storeError(err, "list projects")
		}
		return projects, nil
	}
	found, err := s.store.GetProject(ctx, project)
	if err != nil {
		return nil, storeError(err, "project")
	}
	return []*quaycrewv1.Project{found}, nil
}

// contextDir describes one scope's context: where its rendering sits, and what the store holds, which
// is the answer to "what is the model actually told here".
func (s *Server) contextDir(ctx context.Context, scope store.ContextScope, owner, name string, found sandbox.Context) *quaycrewv1.ContextDir {
	body, err := s.store.GetContext(ctx, scope, owner)
	if err != nil {
		body = ""
	}
	return &quaycrewv1.ContextDir{
		Scope:   string(scope),
		Name:    name,
		Owner:   owner,
		Host:    found.Host,
		Sandbox: found.Sandbox,
		Memory:  found.Memory,
		Body:    body,
		Written: body != "",
	}
}

// SetSessionPermissionMode changes what a thread's turns may do without asking.
//
// The mode belongs to the thread rather than to a turn, so a thread started to plan something keeps
// planning instead of being re armed on every dispatch. An unknown mode is refused here rather than
// handed to the model, which would take it as far as its own argument parser and no further.
// ImportSkill takes a skill into the crew from the files a client read out of its directory.
//
// The files travel and this side validates, because the control plane runs in a container where a path
// on the operator's machine means nothing, and because one validator is one answer: a client that
// checked for itself would be a second, quietly different, set of rules.
func (s *Server) ImportSkill(ctx context.Context, req *quaycrewv1.ImportSkillRequest) (*quaycrewv1.ImportSkillResponse, error) {
	files := make([]skill.File, 0, len(req.GetFiles()))
	for _, file := range req.GetFiles() {
		files = append(files, skill.File{
			Path:       file.GetPath(),
			Body:       file.GetBody(),
			Executable: file.GetExecutable(),
		})
	}
	loaded, err := skill.FromFiles(files)
	if err != nil {
		// The refusal is the skill package's own sentence, which names what is wrong and what to do
		// about it. Wrapping it in something vaguer would lose the only useful part.
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	imported := store.Imported{Skill: loaded}
	if err := s.store.ImportSkill(ctx, imported); err != nil {
		if errors.Is(err, store.ErrSkillChanged) {
			return nil, status.Errorf(codes.FailedPrecondition,
				"%s version %d is already imported and is a different skill. Raise the version in %s: a workspace pins the version it holds, so changing one underneath it would change what a running session can do.",
				loaded.Name, loaded.Version, skill.ManifestFile)
		}
		return nil, storeError(err, "import skill")
	}
	stored, err := s.store.GetSkill(ctx, loaded.Name, loaded.Version)
	if err != nil {
		return nil, storeError(err, "read the imported skill")
	}
	return &quaycrewv1.ImportSkillResponse{Skill: asSkill(stored)}, nil
}

// ListSkills says what the crew can do, or what one workspace holds.
func (s *Server) ListSkills(ctx context.Context, req *quaycrewv1.ListSkillsRequest) (*quaycrewv1.ListSkillsResponse, error) {
	var held []store.Imported
	var err error
	if req.GetWorkspace() != "" {
		held, err = s.store.WorkspaceSkills(ctx, req.GetWorkspace())
	} else {
		held, err = s.store.ListSkills(ctx)
	}
	if err != nil {
		return nil, storeError(err, "list skills")
	}
	out := make([]*quaycrewv1.Skill, 0, len(held))
	for _, one := range held {
		out = append(out, asSkill(one))
	}
	return &quaycrewv1.ListSkillsResponse{Skills: out}, nil
}

// AttachSkill gives a workspace a skill, and puts it in front of the sessions already running there
// rather than waiting for each of them to be replaced.
func (s *Server) AttachSkill(ctx context.Context, req *quaycrewv1.AttachSkillRequest) (*quaycrewv1.AttachSkillResponse, error) {
	attached, err := s.store.AttachSkill(ctx, req.GetWorkspace(), req.GetName())
	if err != nil {
		return nil, storeError(err, "attach skill")
	}
	s.renderTo(ctx, store.ContextWorkspace, req.GetWorkspace())
	return &quaycrewv1.AttachSkillResponse{Skill: asSkill(attached)}, nil
}

// DetachSkill takes a skill away from a workspace, and takes its files off the sessions that held it.
func (s *Server) DetachSkill(ctx context.Context, req *quaycrewv1.DetachSkillRequest) (*quaycrewv1.DetachSkillResponse, error) {
	if err := s.store.DetachSkill(ctx, req.GetWorkspace(), req.GetName()); err != nil {
		return nil, storeError(err, "detach skill")
	}
	s.renderTo(ctx, store.ContextWorkspace, req.GetWorkspace())
	return &quaycrewv1.DetachSkillResponse{}, nil
}

// asSkill renders a skill for a client. The files never travel back: a client asked what the crew can
// do, not for a copy of every script.
func asSkill(one store.Imported) *quaycrewv1.Skill {
	out := &quaycrewv1.Skill{
		Name:     one.Name,
		Version:  int32(one.Version),
		Summary:  one.Summary,
		Binaries: one.Binaries,
	}
	if !one.ImportedAt.IsZero() {
		out.ImportedAt = timestamppb.New(one.ImportedAt)
	}
	// Sorted, because a map has no order and a listing that shuffles between reads is a listing nobody
	// can diff.
	for _, name := range one.SecretNames() {
		out.Secrets = append(out.Secrets, &quaycrewv1.SkillSecret{
			Name:    name,
			Purpose: one.Secrets[name],
		})
	}
	return out
}

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
func (s *Server) turnEnv(ctx context.Context, session *quaycrewv1.Session) map[string]string {
	env := map[string]string{}
	// Where to reach the crew, so `quay` run inside the driver works with nothing to configure. Only
	// the driver is told: an ordinary session has no business driving the crew, and its sandbox is
	// not on a network that could reach it anyway.
	if session.GetDriver() && s.reachable != "" {
		env[grpcAddrEnv] = s.reachable
	}
	// Who a commit is by. All four, because git wants an author and a committer and refuses on either
	// missing: setting the author alone produces "Committer identity unknown" and a wall of advice,
	// halfway through whatever the session was doing. The committer is the author, because a session
	// commits as the operator rather than on behalf of somebody else.
	if s.gitAuthor.Complete() {
		env["GIT_AUTHOR_NAME"] = s.gitAuthor.Name
		env["GIT_AUTHOR_EMAIL"] = s.gitAuthor.Email
		env["GIT_COMMITTER_NAME"] = s.gitAuthor.Name
		env["GIT_COMMITTER_EMAIL"] = s.gitAuthor.Email
	}
	// By name rather than all of them: a sandbox holds a value for the life of its container and the
	// model can read it. The model's own token is always carried, and a name with nothing set against
	// it is skipped rather than refused. A skill contributes the names it declares, so a workspace can
	// hold a token for one capability without every session in the crew being handed it.
	named := append([]string{model.ClaudeCodeOAuthTokenEnv}, s.sandboxSecrets...)
	for _, given := range s.heldSkills(ctx, session) {
		named = append(named, given.SecretNames()...)
	}
	for _, name := range named {
		if _, already := env[name]; already {
			continue
		}
		value, err := s.secrets.Get(ctx, session.GetWorkspace(), name)
		if err != nil || value == "" {
			continue
		}
		env[name] = value
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

// grpcAddrEnv is what the quay command line reads to find a control plane. A session gets it so the
// tool inside a sandbox needs no arguments.
const grpcAddrEnv = "QC_GRPC_ADDR"

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
	for _, session := range sessions {
		s.withUsage(session)
	}
	return &quaycrewv1.ListSessionsResponse{Sessions: sessions}, nil
}

// GetSession returns a session by id.
func (s *Server) GetSession(ctx context.Context, req *quaycrewv1.GetSessionRequest) (*quaycrewv1.GetSessionResponse, error) {
	session, err := s.store.GetSession(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	s.withUsage(session)
	return &quaycrewv1.GetSessionResponse{Session: session}, nil
}

// withUsage puts what a thread's conversation has cost onto it, read from the transcript the model
// keeps rather than from anything the crew recorded.
//
// It has to come from there: the conversations worth counting are the ones held in the panel, and
// those never pass through the control plane at all. A thread nobody has spoken in is left without a
// figure rather than reported as costing nothing, because those are different things.
func (s *Server) withUsage(session *quaycrewv1.Session) {
	spent := s.storage.ConversationUsage(session.GetWorkspace(), session.GetModelSessionId())
	if spent.Empty() {
		return
	}
	session.Usage = &quaycrewv1.Usage{
		Input:        spent.Input,
		Output:       spent.Output,
		CacheRead:    spent.CacheRead,
		CacheWritten: spent.CacheWritten,
	}
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
	// A driver with no conversation yet is opened rather than refused: it is made the moment somebody
	// opens the crew, and telling them to dispatch a turn to the thing they just opened is a loop.
	// Everything below is about a conversation that exists, so it is skipped too.
	if session.GetModelSessionId() == "" && !session.GetDriver() {
		return nil, status.Errorf(codes.FailedPrecondition,
			"thread %s has no conversation yet: send it a message with quay dispatch first",
			display.ShortID(session.GetThreadId()))
	}
	// The crew names the conversation rather than learning what it was called afterwards. A
	// conversation started interactively picks its own identifier and tells nobody, so before this
	// every conversation opened from the panel was one the crew could not name: no history to read
	// back, no tokens to count, and no way to tell one transcript in the workspace from another.
	//
	// Assigned here rather than at creation: this is the moment a conversation starts to exist, and a
	// thread that is only dispatched to gets its identifier from the turn.
	if session.GetModelSessionId() == "" {
		named := store.NewConversationID()
		if err := s.store.RecordTurn(ctx, session.GetId(), named, session.GetStatus()); err != nil {
			return nil, storeError(err, "name the conversation")
		}
		session.ModelSessionId = named
	}
	if session.GetStatus() == "stopped" {
		return nil, status.Errorf(codes.FailedPrecondition,
			"thread %s is stopped: restart it first", display.ShortID(session.GetThreadId()))
	}
	if session.GetArchivedAt() != nil {
		return nil, status.Errorf(codes.FailedPrecondition,
			"thread %s is archived: restore it first", display.ShortID(session.GetThreadId()))
	}
	// A handle can outlive what it points at, and a conversation the crew has just named has no
	// transcript either, so the two cannot be told apart here. The sandbox decides: it resumes a
	// transcript that exists and starts one under the same name when it does not.
	// The live sandboxes are a map in this process, so a restart empties it while the row still says
	// idle. State is on the host, so a fresh container over the same mounts is the same conversation.
	if _, err := s.sandboxFor(ctx, session); err != nil {
		return nil, sandboxError(err, "start sandbox")
	}
	// Inside tmux, so detaching leaves the model running. -A attaches to the session already there
	// rather than starting a second beside it, and the permission mode is the thread's own, or a
	// thread armed to skip permissions asks anyway the moment it is opened.
	return &quaycrewv1.AttachSessionResponse{
		Sandbox: sandbox.ContainerName(session.GetId()),
		Argv: []string{"tmux", "new-session", "-A", "-s", sandbox.AttachedSessionName,
			sandbox.OpenConversation, session.GetModelSessionId(), permissionModeOf(session)},
	}, nil
}

// RestartSession brings a stopped session back to idle, with its sandbox running.
//
// The sandbox is started here rather than on the next turn, so the operator can attach into the
// conversation straight away instead of having to dispatch a turn to make the container exist. That
// is only safe because a session's state lives on the host now: the sandbox this creates is a new
// container over the same conversation store and the same project files.
//
// The sandbox comes first: one that will not start leaves the session stopped rather than idle and
// unreachable.
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
		return nil, sandboxError(err, "create sandbox")
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

// OpenDriver returns the project's driver, the session that drives the crew, creating it the first
// time somebody opens it. It is what `quay` opens beside the console.
func (s *Server) OpenDriver(ctx context.Context, req *quaycrewv1.OpenDriverRequest) (*quaycrewv1.OpenDriverResponse, error) {
	if req.GetProject() == "" {
		return nil, status.Error(codes.InvalidArgument, "a driver belongs to a project, so name one")
	}
	session, err := s.store.FindOrCreateDriver(ctx, req.GetProject())
	if err != nil {
		return nil, storeError(err, "project")
	}
	s.teachDriver(ctx, session)
	return &quaycrewv1.OpenDriverResponse{Session: session}, nil
}

// teachDriver writes what quay is into the driver's own context, so it opens knowing what it is for
// rather than having to be told every time. It is the crew describing itself: the command list the
// tool prints, and the behaviour specification the binary carries.
//
// The driver's own level, not the project's, and written once: overwriting on every open would make
// it the one context nobody can change.
func (s *Server) teachDriver(ctx context.Context, session *quaycrewv1.Session) {
	if existing, err := s.store.GetContext(ctx, store.ContextSession, session.GetId()); err == nil && existing != "" {
		return
	}
	if err := s.store.SetContext(ctx, store.ContextSession, session.GetId(), manual.Text()); err != nil {
		// A driver that has not been told what quay is still opens, and can be told by hand. Refusing
		// to open the crew over it would be worse than opening one that has to ask.
		return
	}
	// Out to the file the driver reads, rather than waiting for its sandbox to be made. Notes it
	// already had are kept: this is the crew adding what it is, not replacing what was there.
	s.syncContext(ctx, session)
}
