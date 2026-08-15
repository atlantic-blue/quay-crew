package controlplane

import (
	"context"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// SigningKeyEnv is the workspace secret that holds the private key a session signs commits with, in
// OpenSSH format.
//
// An ssh key rather than a gpg one. Signing with ssh needs one private key file and nothing else: no
// agent, no keyring, no pinentry, and no interactive prompt to hang a turn nobody is watching. GitHub
// verifies both formats. The cost is that a commit signed in a sandbox verifies against a different
// key from one signed on the operator's own machine, so both keys have to be on the account.
const SigningKeyEnv = "GIT_SSH_SIGNING_KEY"

// signingKeyPath is where the key is written inside the sandbox. Under the agent's home, because that
// is the only writable place a session keeps across a turn, and beside the ssh configuration where a
// reader expects to find it.
const signingKeyPath = "/home/agent/.ssh/signing_key"

// readySigning makes the sandbox's signing state match the workspace: signing on when the workspace
// holds a key, and off when it does not.
//
// A session commits as the operator. Where a repository requires verified signatures, a session that
// cannot sign produces a branch the operator cannot merge, which is most of the work the session was
// for. So the key travels the way every other credential does, as a workspace secret, and the sandbox
// writes it out once at birth.
//
// Turning it off is the other half, and it is not the same as leaving it alone. An operator's own git
// configuration reaches a session now, and most operators who sign have signing on for everything,
// against a key held by their machine and not by a container. Left as it arrives, that configuration
// fails every commit a session makes, on a key it was never going to have. So a workspace with no
// signing key says so to git rather than saying nothing.
//
// The key is never an argument. The value is already in the container's environment, so the script
// reads it from there: an argument is visible to every process on the host that can list them, and it
// would reach the turn record.
func (s *Server) readySigning(ctx context.Context, session *quaycrewv1.Thread, box sandbox.Sandbox) error {
	value, err := s.secrets.Get(ctx, session.GetWorkspace(), SigningKeyEnv)
	script := signingSetup
	if err != nil || strings.TrimSpace(value) == "" {
		script = signingOff
	}

	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c", script}})
	if err != nil {
		// A sandbox that cannot be configured to sign is a sandbox that cannot sign, which the git
		// skill already tells a session to handle by asking rather than committing unsigned. Failing
		// the turn here would take the whole conversation down over it.
		return nil
	}
	_, _ = io.Copy(io.Discard, proc.Stdout())
	_ = proc.Wait()
	return nil
}

// signingSetup writes the key and points git at it. Every line is idempotent, because a sandbox is
// adopted across turns and this runs again on a replacement.
//
// `umask 077` before the write rather than a chmod after it, so the key is never on disk readable even
// for the moment between the two.
const signingSetup = `set -e
umask 077
mkdir -p /home/agent/.ssh
printf '%s\n' "$` + SigningKeyEnv + `" > ` + signingKeyPath + `
git config --global gpg.format ssh
git config --global user.signingkey ` + signingKeyPath + `
git config --global commit.gpgsign true
git config --global tag.gpgsign true`

// signingOff says out loud that this sandbox does not sign, for the workspace that holds no key.
//
// Written after the include, so it beats whatever the operator's own configuration asked for: git
// takes the last value it reads, and the include sits above these lines in the same file.
const signingOff = `set -e
git config --global commit.gpgsign false
git config --global tag.gpgsign false`
