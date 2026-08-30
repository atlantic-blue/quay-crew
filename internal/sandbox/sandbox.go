// Package sandbox provides isolated execution environments for agent sessions. Each session runs in
// its own Sandbox, created by a Provider and reused across the session's tasks, then closed when the
// session ends.
package sandbox

import (
	"context"
	"io"
	"path"

	"github.com/atlantic-blue/quay-crew/internal/capacity"
)

// Spec describes a command to run inside a Sandbox.
type Spec struct {
	Argv    []string
	Workdir string
	// Env is extra environment, each entry "KEY=value".
	Env []string
}

// Process is a running command with a streaming stdout.
type Process interface {
	Stdout() io.Reader
	Wait() error
	// Stderr is the tail of the error stream, available once Wait has returned. A reader nobody
	// drains stops the command dead as soon as the pipe fills, and nothing reads this until the
	// command has already failed.
	Stderr() string
}

// Sandbox is one session's isolated environment.
type Sandbox interface {
	Exec(ctx context.Context, spec Spec) (Process, error)
	Close(ctx context.Context) error
}

// Config describes the sandbox a session needs.
//
// The provider is told the workspace and the project as well as the session, because a session's
// state does not all sit at one level: the conversation store belongs to the workspace and the
// working files to the project.
type Config struct {
	ID        string
	Workspace string
	Project   string
	// Env is set on the sandbox itself, so anything started inside it later inherits the values. They
	// are readable for the life of the container, through docker inspect among other things, so pass
	// only what the session needs.
	Env []string
	// Mounts are directories this session gets on top of the state the crew keeps for it.
	Mounts []Mount
	// Driver joins the control plane's network and gets the host paths handed to the driver. An
	// ordinary session gets neither.
	Driver bool
	// Request is what this sandbox asks the machine for. The crew admits a sandbox only where its
	// runtime still has this much unallocated, and the container carries the processor half of it as
	// a share, so the runtime shares its processors out in the proportions the crew reserved rather
	// than equally between a compile and a session waiting on a model.
	//
	// It is a request and not a limit. Nothing here stops a sandbox taking more than it asked for
	// when the machine is idle, which is what a limit does, and that is issue 477.
	Request capacity.Request
	// Role is the role the session runs as, empty for a session that runs as nobody in particular. It
	// decides where the session's conversation is kept, because a role that must not see the code must
	// not be able to read the transcript of the session that wrote it.
	Role string
}

// Provider mints a Sandbox per session.
type Provider interface {
	Create(ctx context.Context, cfg Config) (Sandbox, error)
	// Remove tears down the session's sandbox whether or not this process holds a handle to it. The
	// handles are a process map and the containers are not, so after a restart the map is empty while
	// every container runs on; removing by name is what makes stopping a session mean something then.
	// A sandbox that is not there is a remove that already happened, not an error.
	Remove(ctx context.Context, sessionID string) error
	// Stranded lists the sessions whose sandboxes this provider still holds, so the crew can reap the
	// ones whose sessions no longer want one.
	Stranded(ctx context.Context) ([]string, error)
	// Attached says whether somebody has this session's conversation open inside its sandbox.
	//
	// It is asked by name rather than through a handle, the way Remove and Stranded are, because the
	// handles are a map in one process and the containers are not: after a restart the map is empty
	// while every container runs on, and a crew that could only answer for the ones it remembers
	// would say nobody is attached to a conversation somebody is typing into.
	//
	// A failure is not "nobody is attached": it is the crew being unable to tell, and the caller must
	// read it that way. Absent is different, and answers false: a session with no container has
	// nobody in it.
	Attached(ctx context.Context, sessionID string) (bool, error)
	// RuntimeRunning says whether a model runtime is up inside this session's sandbox.
	//
	// It sits beside Attached rather than inside it because they are different states and only one of
	// them was ever visible. A conversation somebody opened, worked in, and detached from leaves the
	// runtime answering with nobody watching it, and the crew read that as an empty container: on 28
	// August 2026, six of eighteen sandboxes held a running runtime and every one of them listed as
	// idle.
	//
	// Asked by name, for the same reason Attached is: the handles are a map in one process and the
	// containers are not, so a question that built a sandbox to answer would start the very container
	// it is asked about taking away.
	//
	// A failure is not "nothing is running": it is the crew being unable to tell, and the caller must
	// read it that way. Absent is different, and answers false: a session with no container is running
	// nothing.
	RuntimeRunning(ctx context.Context, sessionID string) (bool, error)
}

// Mount is a directory made available inside a sandbox.
type Mount struct {
	// Source is the directory as the host daemon sees it.
	Source string
	Target string
	// ReadOnly stops a session rewriting what it was given. A session that can edit its own skills
	// can give itself a capability nobody approved.
	ReadOnly bool
}

// These are properties of the sandbox image, so they move with deploy/sandbox/claude.Dockerfile.
const (
	ConversationPath = "/home/agent/.claude"
	WorkingPath      = "/home/agent/workspace"
	SkillsPath       = "/home/agent/skills"
	// HooksPath is where the hooks a session runs under are mounted, read only, together with the
	// settings file that binds them to their events. The runtime is pointed at that file explicitly,
	// which is what lets the crew own this directory outright rather than merging into the
	// conversation directory's own settings.
	HooksPath = "/home/agent/hooks"
	// SharedPath is the workspace's own volume, mounted read write into every session in it. The
	// repositories a workspace works in are cloned here once and shared; anything else a workspace
	// accumulates and wants its sessions to see can live here too.
	SharedPath = "/home/agent/shared"
	// ReposPath is where the one clone of a repository goes, named after the repository. A session
	// that finds it there does not clone again, so a workspace working in one repository across four
	// sessions holds one copy of it rather than four.
	ReposPath = SharedPath + "/repos"
	// WorktreesPath holds a working tree per session, under the session's own identifier. It is in
	// the volume rather than in the session's own directory because a clone records where its working
	// trees are, and every sandbox sees the same paths: two sessions adding a tree at the same path
	// would register one path between them, and the second would prune the first.
	WorktreesPath = SharedPath + "/worktrees"
	// SessionIDEnv is how a session reads its own identifier. It is what a session names anything of
	// its own in the shared volume after, a working tree among them, and it is an identifier the crew
	// already shows rather than a credential.
	SessionIDEnv = "QC_SESSION_ID"
	// SecretsPath is where a file projected secret lands, one file per secret, named after it. The
	// path is Docker's own default for a secret and the conventional mount point for one in
	// Kubernetes, so a session that has met either already knows where to look.
	//
	// Memory backed, so a value never reaches the container's writable layer or the host's disk.
	SecretsPath = "/run/secrets"
	// UserID is the identifier of the image's agent user. A memory backed mount belongs to root
	// unless it is told otherwise, and the crew writes secrets into it as the sandbox's own user, so
	// the mount has to name that user.
	UserID = 1001
	// AttachedSessionName is the operator's open conversation inside the sandbox. One per sandbox, so
	// opening a session twice lands in the one already running.
	AttachedSessionName = "quay"
	// RuntimeBinary is what the model runtime is called inside a sandbox, which is what says a session
	// is holding a conversation rather than sitting empty. A property of the image: the Dockerfile
	// installs it and every task and every attached conversation runs it by this name.
	RuntimeBinary = "claude"
	// MemoryFile is the model's own convention, not ours.
	MemoryFile = "CLAUDE.md"
	// OpenConversation keeps the terminal alive when a conversation ends, so ending one does not take
	// everything running with it.
	OpenConversation = "open-conversation"
	// GitConfigSecret is the workspace secret an operator mounts to give every session their own git
	// configuration. The image's own configuration includes the file it lands in, so identity,
	// aliases and settings reach a session from any shell rather than only the process a task runs.
	GitConfigSecret = "gitconfig"
	// GitConfigPath is the sandbox user's git configuration, the file git reads as global. Shipped by
	// the image holding the include, and written to by the crew at sandbox birth.
	GitConfigPath = "/home/agent/.gitconfig"
)

// SecretFilePath is where a file projected secret lands inside a sandbox. Derived from the name
// rather than stored, so every reader answers the same, and a name that could not be a file name is
// refused before it reaches the store.
func SecretFilePath(name string) string { return path.Join(SecretsPath, name) }

const ContainerPrefix = "quaycrew-"

// SessionIDLength is how many characters a session identifier has, which is what tells a sandbox
// container from every other container the daemon holds. The name alone is not enough: the compose
// project is called quaycrew too, so its own services carry the same prefix.
const SessionIDLength = 24

// ContainerName is derived here rather than rebuilt by everything that needs to reach into a session
// from outside the provider.
func ContainerName(sessionID string) string { return ContainerPrefix + sessionID }
