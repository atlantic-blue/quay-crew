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

// CloneSpec is what to run inside a sandbox to put a project's repository in front of a session, once.
//
// Three properties matter here, and together they are why this is built rather than formatted into a
// string at the call site.
//
// It happens once per container. A sandbox is adopted across turns, so a command that clones every time
// would either fail on the second turn or throw away whatever the first one did. The check is inside the
// container rather than on the host, because that is the only place that knows what this container has,
// and it is the same shape the setup scripts already use.
//
// The remote is never part of a shell word. It is a positional argument, read back as "$1", so nothing
// in it is ever parsed as syntax. It comes from a person, and a remote interpolated into a command is a
// remote that can end that command and start another one.
//
// The token is never in the argument list. The credential helper reads it from the environment at the
// moment git asks. An argument list is readable by anything that can inspect the container and ends up
// in whatever logs a command, so a token in one is a token published.
func CloneSpec(remote, into string) (Spec, error) {
	if err := UsableRemote(remote); err != nil {
		return Spec{}, err
	}
	name, err := RepositoryName(remote)
	if err != nil {
		return Spec{}, err
	}
	return Spec{
		Argv:    []string{"sh", "-c", CloneScript(), "sh", remote, path.Join(into, name)},
		Workdir: into,
	}, nil
}

// CloneScript is the script a clone runs inside the sandbox, reading the remote as its first argument
// and the directory to clone into as its second.
//
// Exported so a test can run the real thing rather than a copy of it that agrees with it today.
func CloneScript() string {
	// A helper starting with ! is run by a shell, which is what expands the variable. The value is not
	// here, and it is not in this process either.
	helper := `!f() { echo username=x-access-token; echo "password=$` + CredentialEnv + `"; }; f`
	// $1 is the remote and $2 is where it goes. Neither is in the script.
	return `[ -d "$2/.git" ] || git -c 'credential.helper=` + helper + `' clone -- "$1" "$2"`
}

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
