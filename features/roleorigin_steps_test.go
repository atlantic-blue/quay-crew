package features_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/origin"
	"github.com/atlantic-blue/quay-crew/internal/role"
	"github.com/cucumber/godog"
)

// Where a role came from, driven from real directories on disk.
//
// The directories are real repositories rather than an origin built here, because the question a
// scenario is asking is whether the crew can tell a role somebody could review from a role in a
// folder on one machine, and an origin written by the test would be the test answering it.
func initializeRoleOriginSteps(sc *godog.ScenarioContext) {
	importFrom := func(ctx context.Context, dir string) error {
		w := worldFrom(ctx)
		files, err := roleFilesFrom(dir)
		if err != nil {
			return err
		}
		read := origin.Of(dir)
		_, w.lastErr = w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
			Files: files,
			Origin: &quaycrewv1.RoleOrigin{
				Repository: read.Repository, Commit: read.Commit, Path: read.Path,
				Dirty: read.Dirty, Unpushed: read.Unpushed,
			},
		})
		return w.lastErr
	}

	sc.Step(`^the operator imports a role from a repository$`, func(ctx context.Context) error {
		return importFrom(ctx, aRoleInARepository(ctx, pushedToARemote))
	})

	sc.Step(`^the operator (?:imports|imported) a role from a folder that is not in a repository$`,
		func(ctx context.Context) error {
			return importFrom(ctx, aRoleInAFolder(ctx))
		})

	sc.Step(`^the operator imports a role from a repository nothing was pushed to$`, func(ctx context.Context) error {
		return importFrom(ctx, aRoleInARepository(ctx, committedAndNotPushed))
	})

	sc.Step(`^the operator imports a role edited after its commit$`, func(ctx context.Context) error {
		return importFrom(ctx, aRoleInARepository(ctx, editedAfterTheCommit))
	})

	// The same role, byte for byte, read somewhere a reviewer can open. It is the way out of the
	// warning, so the crew has to notice it.
	sc.Step(`^the operator imports that role again from a repository$`, func(ctx context.Context) error {
		return importFrom(ctx, aRoleInARepository(ctx, pushedToARemote))
	})

	sc.Step(`^the listing says where the role came from$`, func(ctx context.Context) error {
		said, err := whatTheListingSaysAboutOrigin(ctx)
		if err != nil {
			return err
		}
		if !strings.Contains(said, "github.com/atlantic-blue/quay-crew roles/test-writer at ") {
			return fmt.Errorf("it does not name the repository, the directory and the commit: %q", said)
		}
		return nil
	})

	sc.Step(`^the listing says the role is not in a repository$`, func(ctx context.Context) error {
		said, err := whatTheListingSaysAboutOrigin(ctx)
		if err != nil {
			return err
		}
		if !strings.Contains(said, "not in a repository") {
			return fmt.Errorf("it does not say the role is not in a repository: %q", said)
		}
		return nil
	})

	sc.Step(`^the listing says the commit is on no remote branch$`, func(ctx context.Context) error {
		return listingSays(ctx, "on no remote branch")
	})

	sc.Step(`^the listing says the files are uncommitted$`, func(ctx context.Context) error {
		return listingSays(ctx, "with uncommitted changes")
	})

	sc.Step(`^the listing says where the role came from was not recorded$`, func(ctx context.Context) error {
		return listingSays(ctx, "not recorded")
	})

	sc.Step(`^the listing says nobody else can read it$`, func(ctx context.Context) error {
		return listingSays(ctx, "nobody else can read these files")
	})

	sc.Step(`^the listing does not say nobody else can read it$`, func(ctx context.Context) error {
		said, err := whatTheListingSaysAboutOrigin(ctx)
		if err != nil {
			return err
		}
		if strings.Contains(said, "nobody else can read these files") {
			return fmt.Errorf("a role anybody could go and read is called unreadable: %q", said)
		}
		return nil
	})
}

func listingSays(ctx context.Context, want string) error {
	said, err := whatTheListingSaysAboutOrigin(ctx)
	if err != nil {
		return err
	}
	if !strings.Contains(said, want) {
		return fmt.Errorf("it does not say %q: %q", want, said)
	}
	return nil
}

// whatTheListingSaysAboutOrigin is what an operator reads under the one role the crew holds. It
// renders from the listing rather than from GetRole, because a listing is where somebody would
// notice a role nobody can read without going looking for it.
func whatTheListingSaysAboutOrigin(ctx context.Context) (string, error) {
	if err := listRoles(ctx, ""); err != nil {
		return "", err
	}
	held := worldFrom(ctx).lastRoles.GetRoles()
	if len(held) != 1 {
		return "", fmt.Errorf("the crew holds %d roles, and the scenario imported one", len(held))
	}
	from := held[0].GetOrigin()
	return strings.Join(origin.Origin{
		Repository: from.GetRepository(), Commit: from.GetCommit(), Path: from.GetPath(),
		Dirty: from.GetDirty(), Unpushed: from.GetUnpushed(),
	}.Says(), "\n"), nil
}

// How far a role's directory got towards being something somebody else could read.
type gotAsFarAs int

const (
	pushedToARemote gotAsFarAs = iota
	committedAndNotPushed
	editedAfterTheCommit
)

// aRoleInARepository writes the test-writer role into a real repository and takes it as far as the
// scenario asked, so what the crew is told is what git says rather than what this file decided.
func aRoleInARepository(ctx context.Context, got gotAsFarAs) string {
	w := worldFrom(ctx)
	repository := w.tempDir()
	git(repository, "init", "--initial-branch=main")
	git(repository, "config", "user.email", "crew@example.com")
	git(repository, "config", "user.name", "crew")
	git(repository, "remote", "add", "origin", "https://github.com/atlantic-blue/quay-crew.git")

	dir := writeRoleFiles(filepath.Join(repository, "roles", "test-writer"))
	git(repository, "add", "-A")
	git(repository, "commit", "-m", "the roles this build ships")
	switch got {
	case pushedToARemote:
		// What a push leaves behind, which is all any of this reads.
		git(repository, "update-ref", "refs/remotes/origin/main", "HEAD")
	case editedAfterTheCommit:
		git(repository, "update-ref", "refs/remotes/origin/main", "HEAD")
		if err := os.WriteFile(filepath.Join(dir, role.BriefFile),
			[]byte("Write the product yourself."), 0o644); err != nil {
			panic(err)
		}
	case committedAndNotPushed:
	}
	return dir
}

// aRoleInAFolder is the role of the acceptance run: files on one machine, in nothing.
func aRoleInAFolder(ctx context.Context) string {
	return writeRoleFiles(filepath.Join(worldFrom(ctx).tempDir(), "test-writer"))
}

func writeRoleFiles(dir string) string {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	for _, one := range roleFiles("test-writer", 1, roleManifest{
		model: "opus", receives: []string{"job", "context"},
	}) {
		if err := os.WriteFile(filepath.Join(dir, one.GetPath()), one.GetBody(), 0o644); err != nil {
			panic(err)
		}
	}
	return dir
}

func git(dir string, args ...string) {
	command := exec.Command("git", args...)
	command.Dir = dir
	if out, err := command.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("git %s: %v: %s", strings.Join(args, " "), err, out))
	}
}

// tempDir is a directory this scenario owns, removed when it ends. A scenario about where files came
// from has to put real files somewhere, and a repository left behind on every run is litter.
func (w *world) tempDir() string {
	dir, err := os.MkdirTemp("", "quaycrew-roleorigin")
	if err != nil {
		panic(err)
	}
	w.scratch = append(w.scratch, dir)
	return dir
}
