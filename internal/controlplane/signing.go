package controlplane

import (
	"context"
	"io"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
)

// SigningKeySecret is the workspace secret that holds the private key a session signs commits with,
// in OpenSSH format. Mounted, never set: it is a file, and it is the most sensitive thing this crew
// carries.
//
// An ssh key rather than a gpg one. Signing with ssh needs one private key file and nothing else: no
// agent, no keyring, no pinentry, and no interactive prompt to hang a task nobody is watching. GitHub
// verifies both formats. The cost is that a commit signed in a sandbox verifies against a different
// key from one signed on the operator's own machine, so both keys have to be on the account.
const SigningKeySecret = "GIT_SSH_SIGNING_KEY"

// readySigning makes the sandbox's signing state match the workspace: signing on when the workspace
// mounts a key, and off when it does not.
//
// A session commits as the operator. Where a repository requires verified signatures, a session that
// cannot sign produces a branch the operator cannot merge, which is most of the work the session was
// for.
//
// The key is already in the sandbox by the time this runs, written as a file by readySecretFiles, so
// there is nothing here but pointing git at it. It was written out by hand before the crew could
// mount anything, and that path put the private key in the container's environment for the life of
// the container, where docker inspect reads it.
//
// Tasking signing off is the other half, and it is not the same as leaving it alone. An operator's
// own git configuration reaches a session, and most operators who sign have signing on for
// everything, against a key held by their machine and not by a container. Left as it arrives, that
// configuration fails every commit a session makes, on a key it was never going to have. So a
// workspace that mounts no key says so to git rather than saying nothing.
func (s *Server) readySigning(ctx context.Context, session *quaycrewv1.Session, box sandbox.Sandbox) error {
	script := signingOff
	if s.mountsASigningKey(ctx, session.GetWorkspace()) {
		script = signingSetup
	}

	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c", script}})
	if err != nil {
		// A sandbox that cannot be configured to sign is a sandbox that cannot sign, which the git
		// skill already tells a session to handle by asking rather than committing unsigned. Failing
		// the task here would take the whole conversation down over it.
		return nil
	}
	_, _ = io.Copy(io.Discard, proc.Stdout())
	_ = proc.Wait()
	return nil
}

// mountsASigningKey asks the listing rather than reading the value, because the value is not wanted
// here and a crew that never handles it cannot leak it.
func (s *Server) mountsASigningKey(ctx context.Context, workspace string) bool {
	held, err := s.secrets.List(ctx, workspace)
	if err != nil {
		return false
	}
	for _, ref := range held {
		if ref.Name == SigningKeySecret && ref.Projection.Or() == secrets.File {
			return true
		}
	}
	return false
}

// signingKeyPath is where the mounted key lands, which is the only place it exists.
var signingKeyPath = sandbox.SecretFilePath(SigningKeySecret)

// signingSetup points git at the key. Every line is idempotent, because a sandbox is adopted across
// tasks and this runs again on a replacement.
var signingSetup = `set -e
git config --global gpg.format ssh
git config --global user.signingkey ` + signingKeyPath + `
git config --global commit.gpgsign true
git config --global tag.gpgsign true`

// signingOff says out loud that this sandbox does not sign, for the workspace that mounts no key.
//
// Written after the include, so it beats whatever the operator's own configuration asked for: git
// takes the last value it reads, and the include sits above these lines in the same file.
const signingOff = `set -e
git config --global commit.gpgsign false
git config --global tag.gpgsign false`
