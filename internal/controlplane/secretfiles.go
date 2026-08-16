package controlplane

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
)

// secretFileEnv is the name the value is carried under for the length of one write. The script reads
// it from there rather than taking it as an argument, because an argument is visible to every process
// on the host that can list them, and it would reach the turn record.
const secretFileEnv = "QC_SECRET_FILE_VALUE"

// readySecretFiles writes the workspace's file projected secrets into the sandbox.
//
// A credential a tool opens by path cannot be an environment variable, and some of the most ordinary
// ones are files: a git configuration, a private key, a cloud credentials file. So a secret says how
// it reaches a sandbox, and this is the half that carries the ones that go in as files.
//
// They land in a memory backed directory the container was created with, one file per secret, named
// after it and readable only by the sandbox user. That is the whole reason to prefer a file over an
// environment variable for a credential: a container's environment is readable through docker inspect
// for the life of the container, and this is not.
//
// Nothing here fails a turn. A workspace that has mounted nothing has nothing to do, and a write that
// fails leaves a session that cannot read one credential rather than a conversation that will not
// start at all.
func (s *Server) readySecretFiles(ctx context.Context, session *quaycrewv1.Thread, box sandbox.Sandbox) error {
	refs, err := s.secrets.List(ctx, session.GetWorkspace())
	if err != nil {
		slog.WarnContext(ctx, "the workspace's secrets could not be listed, so none were mounted",
			"session", session.GetId(), "error", err)
		return nil
	}
	for _, ref := range refs {
		if ref.Projection.Or() != secrets.File {
			continue
		}
		value, err := s.secrets.Get(ctx, session.GetWorkspace(), ref.Name)
		if err != nil || value == "" {
			continue
		}
		if err := writeSecretFile(ctx, box, ref.Name, value); err != nil {
			// Named, because the session will fail at whatever the credential was for and the reason
			// belongs somewhere the operator can find it.
			slog.WarnContext(ctx, "a secret could not be mounted", "session", session.GetId(), "secret", ref.Name, "error", err)
		}
	}
	return nil
}

// writeSecretFile puts one value at its path inside the sandbox.
//
// `umask 077` before the write rather than a change of mode after it, so the file is never on disk
// readable even for the moment between the two. Every line is idempotent, because a sandbox is
// adopted across turns and this runs again on the replacement.
func writeSecretFile(ctx context.Context, box sandbox.Sandbox, name, value string) error {
	proc, err := box.Exec(ctx, sandbox.Spec{
		Argv: []string{"sh", "-c", secretFileScript(name)},
		Env:  []string{secretFileEnv + "=" + value},
	})
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, proc.Stdout())
	if err := proc.Wait(); err != nil {
		return fmt.Errorf("%w: %s", err, proc.Stderr())
	}
	return nil
}

// secretFileScript writes one secret to its own file.
//
// printf rather than echo, because a value beginning with a dash is an argument to one of them and
// content to the other. No trailing newline is added: the bytes stored are the bytes the operator
// gave, and a configuration file that gains a line it did not have is a file that no longer matches
// what they mounted.
func secretFileScript(name string) string {
	return fmt.Sprintf(`set -e
umask 077
mkdir -p %s
printf '%%s' "$%s" > %s`, sandbox.SecretsPath, secretFileEnv, sandbox.SecretFilePath(name))
}
