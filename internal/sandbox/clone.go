package sandbox

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// CredentialEnv is the environment variable a clone reads its password from, inside the sandbox.
//
// The same name `gh` prefers, so one workspace secret serves the clone and anything else that talks to
// the same host.
const CredentialEnv = "GH_TOKEN"

// RepositoriesDir is where a workspace's clones live inside its volume.
const RepositoriesDir = "repositories"

// CloneSpec is what to run inside a sandbox to put a repository in the workspace's volume, once.
//
// Into the volume rather than into the session's own directory, so a workspace clones a repository once
// however many conversations it is having. The difference is one copy of a large checkout against one per
// conversation, and it is what lets two sessions share the history they are both working on.
//
// Three properties matter here, and together they are why this is built rather than formatted into a
// string at the call site.
//
// It happens once. It clones only when the checkout is not already there, because a sandbox is adopted
// across turns and every session in the workspace runs this against the same directory. Two sessions
// starting at the same moment cannot race, because the control plane holds its own lock across the whole
// of building a sandbox, so this is a guard against repeating work rather than against a collision.
//
// The remote is never part of a shell word. It is a positional argument, read back as "$1", so nothing in
// it is ever parsed as syntax. It comes from a person, and a remote interpolated into a command is a
// remote that can end that command and start another one.
//
// The token is never in the argument list. The credential helper reads it from the environment at the
// moment git asks. An argument list is readable by anything that can inspect the container and ends up in
// whatever logs a command, so a token in one is a token published.
func CloneSpec(remote, into string) (Spec, error) {
	if err := UsableRemote(remote); err != nil {
		return Spec{}, err
	}
	name, err := RepositoryName(remote)
	if err != nil {
		return Spec{}, err
	}
	return Spec{
		Argv:    []string{"sh", "-c", CloneScript(), "sh", remote, path.Join(into, RepositoriesDir, name)},
		Workdir: into,
	}, nil
}

// CloneScript is the script a clone runs inside the sandbox, reading the remote as its first argument and
// the directory to clone into as its second.
//
// Exported so a test can run the real thing rather than a copy of it that agrees with it today.
func CloneScript() string {
	// A helper starting with ! is run by a shell, which is what expands the variable. The value is not
	// here, and it is not in this process either.
	helper := `!f() { echo username=x-access-token; echo "password=$` + CredentialEnv + `"; }; f`
	// $1 is the remote and $2 is where it goes. Neither is in the script.
	return `[ -e "$2/.git" ] || git -c 'credential.helper=` + helper + `' clone -- "$1" "$2"`
}

// WorktreesDir is where each session's working tree of a repository lives inside the workspace's volume.
const WorktreesDir = "worktrees"

// WorktreePath is where a session's working tree of a repository sits.
//
// Inside the volume, under the session's own identifier, and not under the session's working directory,
// which is the part that is easy to get wrong. A clone records the path of every working tree cut from
// it, and that record is shared by every session in the workspace. A session's working directory is at
// the same path in every container, so two sessions registering a tree there would register the same
// path twice: the second would prune the first's registration and leave that session holding a working
// tree its clone no longer knows about. Inside the volume the paths differ and mean the same thing from
// every container.
func WorktreePath(session, name string) string {
	return path.Join(SharedPath, WorktreesDir, session, name)
}

// WorktreeSpec gives one session its own working tree of a repository the workspace has cloned.
//
// A worktree rather than a copy, because it shares the objects and the history with the clone in the
// volume and costs only the files that are checked out. A worktree rather than the shared checkout
// itself, because git allows exactly one working tree per branch: two sessions in one directory share an
// index, and the first checkout or rebase moves the ground under the other one.
//
// Its own branch for the same reason. Two worktrees cannot have the same branch checked out, so a session
// gets one named after itself, which is also what makes it obvious afterwards which conversation did
// what.
//
// A symbolic link puts it in the session's working directory as well, because that is where the model
// starts and where it will look, and the volume is somewhere it has no reason to go.
//
// Stale registrations are pruned first, so a session whose directory was cleared away does not leave the
// clone believing a working tree is still there and refusing to make it again.
func WorktreeSpec(clone, at, link, branch string) Spec {
	script := `[ -e "$2/.git" ] || { git -C "$1" worktree prune && git -C "$1" worktree add -B "$4" "$2"; }; ` +
		`[ -e "$3" ] || ln -s "$2" "$3"`
	return Spec{Argv: []string{"sh", "-c", script, "sh", clone, at, link, branch}}
}

// SessionBranch is the branch a session's working tree is on.
func SessionBranch(session string) string { return "quay/" + session }

// UsableRemote refuses a remote that is not one, before it reaches a command.
//
// Https and the scp like ssh form, which is what a repository is handed out as. Anything else is
// refused by name rather than attempted, because the failure otherwise happens inside a container on
// somebody's first turn and says nothing about what was wrong with it.
func UsableRemote(remote string) error {
	trimmed := strings.TrimSpace(remote)
	switch {
	case trimmed == "":
		return fmt.Errorf("sandbox: a remote is required")
	case trimmed != remote:
		return fmt.Errorf("sandbox: remote %q has space around it", remote)
	case strings.ContainsAny(remote, " \t\n\r"):
		return fmt.Errorf("sandbox: remote %q has whitespace in it, so it is not one address", remote)
	}
	if strings.HasPrefix(remote, "https://") {
		parsed, err := url.Parse(remote)
		if err != nil {
			return fmt.Errorf("sandbox: remote %q is not a readable address: %w", remote, err)
		}
		if parsed.Host == "" || strings.Trim(parsed.Path, "/") == "" {
			return fmt.Errorf("sandbox: remote %q names no host and path to clone from", remote)
		}
		// A password already in the address would be a credential in the database and in every listing
		// that prints a project.
		if parsed.User != nil {
			return fmt.Errorf("sandbox: remote %q carries a user in it; the crew supplies the credential from the workspace's secrets, so leave it out", remote)
		}
		return nil
	}
	// The scp like form: user@host:path, no scheme.
	if at := strings.Index(remote, "@"); at > 0 {
		rest := remote[at+1:]
		colon := strings.Index(rest, ":")
		if colon > 0 && strings.Trim(rest[colon+1:], "/") != "" {
			return nil
		}
	}
	return fmt.Errorf("sandbox: remote %q is not a repository address; give an https:// url or the user@host:path form", remote)
}

// RepositoryName is the directory a clone of this remote lands in.
//
// The last part of the path without its .git, which is what git itself would choose. It is checked
// rather than trusted, because it becomes a directory name under the session's working directory and
// the remote came from a person.
func RepositoryName(remote string) (string, error) {
	trimmed := strings.TrimSuffix(strings.TrimRight(remote, "/"), ".git")
	last := trimmed
	if at := strings.LastIndexAny(trimmed, "/:"); at >= 0 {
		last = trimmed[at+1:]
	}
	if last == "" || last == "." || last == ".." || strings.ContainsAny(last, `/\`) {
		return "", fmt.Errorf("sandbox: remote %q gives no directory name to clone into", remote)
	}
	return last, nil
}
