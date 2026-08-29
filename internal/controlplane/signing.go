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
// Signing with ssh needs one private key file and nothing else: no agent, no keyring, no pinentry,
// and no interactive prompt to hang a task nobody is watching. GitHub verifies both formats. The
// cost is that a commit signed in a sandbox verifies against a different key from one signed on the
// operator's own machine, so both keys have to be on the account.
const SigningKeySecret = "GIT_SSH_SIGNING_KEY"

// OpenPGPKeySecret is the workspace secret that holds an exported OpenPGP secret key, the shape
// `gpg --armor --export-secret-keys <key>` prints. Mounted, never set, on the same terms as the ssh
// key above.
//
// Here because an operator who already signs with gpg has one key their machine signs with, on their
// account and on their commits, and an ssh key in a sandbox signs the same history with a second
// identity. One key, one identity, whoever made the commit.
//
// What ssh avoids and this brings back is the keyring and the passphrase. The keyring is made at
// sandbox birth in memory and dies with the container. The passphrase is the part that hangs a task:
// gpg asks for one through pinentry, and a task nobody is watching waits forever. So the sandbox
// tells gpg to work in batch, where a key that needs a passphrase and has none fails in a second
// with a message, and an operator who has one mounts it as OpenPGPPassphraseSecret.
const OpenPGPKeySecret = "GPG_SIGNING_KEY"

// OpenPGPPassphraseSecret is the workspace secret holding the passphrase for the key above, for the
// operator who exports theirs as it stands rather than stripping the passphrase off a copy first.
// Optional: a key exported without one signs without this.
//
// Mounted, never set. It guards the key beside it, so it is worth no less than the key is.
const OpenPGPPassphraseSecret = "GPG_SIGNING_KEY_PASSPHRASE"

// mountedOnly is every secret a workspace may only mount. Each one is a private key or the
// passphrase that guards one, and the environment of a container is readable through docker inspect
// for the life of that container.
var mountedOnly = map[string]bool{
	SigningKeySecret:        true,
	OpenPGPKeySecret:        true,
	OpenPGPPassphraseSecret: true,
}

// readySigning makes the sandbox's signing state match the workspace: signing on when the workspace
// mounts a key, and off when it does not.
//
// A session commits as the operator. Where a repository requires verified signatures, a session that
// cannot sign produces a branch the operator cannot merge, which is most of the job the session was
// for.
//
// The key is already in the sandbox by the time this runs, written as a file by readySecretFiles, so
// there is nothing here but pointing git at it. It was written out by hand before the crew could
// mount anything, and that path put the private key in the container's environment for the life of
// the container, where docker inspect reads it.
//
// Turning signing off is the other half, and it is not the same as leaving it alone. An operator's
// own git configuration reaches a session, and most operators who sign have signing on for
// everything, against a key held by their machine and not by a container. Left as it arrives, that
// configuration fails every commit a session makes, on a key it was never going to have. So a
// workspace that mounts no key says so to git rather than saying nothing.
func (s *Server) readySigning(ctx context.Context, session *quaycrewv1.Session, box sandbox.Sandbox) error {
	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c", s.signingScript(ctx, session.GetWorkspace())}})
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

// signingScript is what the workspace's mounted secrets add up to.
//
// A workspace holding both kinds of key signs with the OpenPGP one. Mounting a second key is a
// deliberate act and the ssh key is the one that was already there, so the newer key is the answer
// to the question of what changed.
func (s *Server) signingScript(ctx context.Context, workspace string) string {
	mounted := s.mountedSecrets(ctx, workspace)
	switch {
	case mounted[OpenPGPKeySecret]:
		return openPGPSigningSetup(mounted[OpenPGPPassphraseSecret])
	case mounted[SigningKeySecret]:
		return signingSetup
	default:
		return signingOff
	}
}

// mountedSecrets asks the listing which secrets reach this workspace's sandboxes as files, rather
// than asking for the values, because no value is wanted here and a crew that never handles one
// cannot leak it.
func (s *Server) mountedSecrets(ctx context.Context, workspace string) map[string]bool {
	mounted := map[string]bool{}
	held, err := s.secrets.List(ctx, workspace)
	if err != nil {
		return mounted
	}
	for _, ref := range held {
		if ref.Projection.Or() == secrets.File {
			mounted[ref.Name] = true
		}
	}
	return mounted
}

// signingKeyPath is where the mounted key lands, which is the only place it exists.
var signingKeyPath = sandbox.SecretFilePath(SigningKeySecret)

// openPGPKeyPath and openPGPPassphrasePath are the same for the OpenPGP pair.
var (
	openPGPKeyPath        = sandbox.SecretFilePath(OpenPGPKeySecret)
	openPGPPassphrasePath = sandbox.SecretFilePath(OpenPGPPassphraseSecret)
)

// signingSetup points git at the key. Every line is idempotent, because a sandbox is adopted across
// tasks and this runs again on a replacement.
var signingSetup = `set -e
git config --global gpg.format ssh
git config --global user.signingkey ` + signingKeyPath + `
git config --global commit.gpgsign true
git config --global tag.gpgsign true`

// openPGPSigningSetup imports the mounted key into a keyring and points git at it. Idempotent like
// the ssh setup above: importing a key already in the keyring changes nothing.
//
// The keyring is wherever the image says, which is a memory backed directory it makes for this. An
// image that says nothing gets the home directory, so an older image still signs, with the imported
// key on the container's writable layer rather than in memory.
//
// batch and no-tty are what keep an unattended task from waiting on a passphrase prompt no operator
// is there to answer. With them, a key that needs a passphrase the workspace did not mount fails
// while the commit is being made, which is a message in a transcript rather than a task that never
// ends.
//
// The signing key is named by fingerprint rather than left to gpg's default, which is the first
// secret key in the keyring. A sandbox's keyring holds one key today; naming it costs a line and
// does not go wrong on the day it holds two.
func openPGPSigningSetup(withPassphrase bool) string {
	script := `set -e
home="${GNUPGHOME:-$HOME/.gnupg}"
mkdir -p "$home"
chmod 700 "$home"
printf '%s\n' batch no-tty > "$home/gpg.conf"
`
	if withPassphrase {
		script += `printf '%s\n' 'pinentry-mode loopback' 'passphrase-file ` + openPGPPassphrasePath + `' >> "$home/gpg.conf"
printf '%s\n' allow-loopback-pinentry > "$home/gpg-agent.conf"
`
	}
	return script + `gpg --quiet --import ` + openPGPKeyPath + `
key=$(gpg --list-secret-keys --with-colons | grep '^fpr:' | head -1 | cut -d: -f10)
test -n "$key"
git config --global gpg.format openpgp
git config --global user.signingkey "$key"
git config --global commit.gpgsign true
git config --global tag.gpgsign true`
}

// signingOff says out loud that this sandbox does not sign, for the workspace that mounts no key.
//
// Written after the include, so it beats whatever the operator's own configuration asked for: git
// takes the last value it reads, and the include sits above these lines in the same file.
const signingOff = `set -e
git config --global commit.gpgsign false
git config --global tag.gpgsign false`
