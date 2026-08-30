package features_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// The changelog's scenarios touch the control plane nowhere. What they prove is a property of this
// repository: two changes written at the same time do not write the same file, and a release reads
// what they did write. So they run a real git merge in a repository of their own, and the real
// command the release runs, which is what `make changelog` is an alias for.

type changelogWorld struct {
	// repo is the repository the two changes are made in, and pending is the fragment directory a
	// release reads.
	repo    string
	pending string
	// wrote is what each change put where, so a merged tree can be checked for both of them.
	wrote []leftBehind
	// said is everything the release command printed, and refused is whether it stopped.
	said    string
	refused bool
	// merged is whether the merge went through, and mergeSaid is what git said about it.
	merged    bool
	mergeSaid string
}

// leftBehind is one change's mark: the file it wrote, and a line only that change wrote.
type leftBehind struct {
	file string
	mark string
}

type changelogKey struct{}

func changelogFrom(ctx context.Context) *changelogWorld {
	c, _ := ctx.Value(changelogKey{}).(*changelogWorld)
	return c
}

// git runs one command in the repository. The identity is given here rather than taken from whoever
// is running the tests, and signing is off, because this repository exists for the length of one
// scenario and continuous integration holds no key.
func (c *changelogWorld) git(args ...string) (string, error) {
	command := exec.Command("git", append([]string{
		"-C", c.repo,
		"-c", "user.name=krewe",
		"-c", "user.email=krewe@example.invalid",
		"-c", "commit.gpgsign=false",
	}, args...)...)
	out, err := command.CombinedOutput()
	return string(out), err
}

// baseChangelog is the file both changes are about to write into, in the shape the real one is in.
const baseChangelog = "# Changelog\n\nWhat has actually shipped, newest first.\n\n## 29 August 2026\n\n- **Something that already shipped.** Nothing to do with either change.\n"

// startRepository makes the repository the two changes are made in. keepBoth is whether it carries
// the attribute this repository carries, which tells git to keep both sides of a conflict in the
// changelog rather than stopping on one.
func (c *changelogWorld) startRepository(keepBoth bool) error {
	if err := os.MkdirAll(filepath.Join(c.repo, "changelog.d"), 0o755); err != nil {
		return err
	}
	if out, err := c.git("init", "-q", "-b", "main", "."); err != nil {
		return fmt.Errorf("git init: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(c.repo, "CHANGELOG.md"), []byte(baseChangelog), 0o600); err != nil {
		return err
	}
	if keepBoth {
		if err := os.WriteFile(filepath.Join(c.repo, ".gitattributes"), []byte("CHANGELOG.md merge=union\n"), 0o600); err != nil {
			return err
		}
		if out, err := c.git("add", ".gitattributes"); err != nil {
			return fmt.Errorf("git add: %v: %s", err, out)
		}
	}
	if out, err := c.git("add", "CHANGELOG.md"); err != nil {
		return fmt.Errorf("git add: %v: %s", err, out)
	}
	if out, err := c.git("commit", "-qm", "the repository both changes start from"); err != nil {
		return fmt.Errorf("git commit: %v: %s", err, out)
	}
	return nil
}

// change is one of the two changes: a branch off the same commit, holding one file it wrote.
func (c *changelogWorld) change(branch, file, body, mark string) error {
	if out, err := c.git("switch", "-q", "-C", branch, "main"); err != nil {
		return fmt.Errorf("git switch: %v: %s", err, out)
	}
	at := filepath.Join(c.repo, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(at, []byte(body), 0o600); err != nil {
		return err
	}
	if out, err := c.git("add", file); err != nil {
		return fmt.Errorf("git add: %v: %s", err, out)
	}
	if out, err := c.git("commit", "-qm", branch); err != nil {
		return fmt.Errorf("git commit: %v: %s", err, out)
	}
	c.wrote = append(c.wrote, leftBehind{file: file, mark: mark})
	return nil
}

// entryAtTheTop is the changelog with one more entry written above the last one, which is where every
// change used to write and why every change used to collide.
func entryAtTheTop(entry string) string {
	return strings.Replace(baseChangelog, "- **Something", entry+"\n\n- **Something", 1)
}

// assemble runs the release, which is the command `make changelog` runs.
func (c *changelogWorld) assemble() error {
	command := exec.Command("go", "run", "../cmd/changelog", "-dir", c.pending)
	out, err := command.CombinedOutput()
	c.said = string(out)
	c.refused = err != nil
	return nil
}

func initializeChangelogSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		base, err := os.MkdirTemp("", "quaycrew-changelog")
		if err != nil {
			return ctx, err
		}
		c := &changelogWorld{repo: filepath.Join(base, "repository"), pending: filepath.Join(base, "changelog.d")}
		if err := os.MkdirAll(c.pending, 0o755); err != nil {
			return ctx, err
		}
		return context.WithValue(ctx, changelogKey{}, c), nil
	})

	sc.Step(`^two changes written at the same time$`, func(ctx context.Context) error {
		return changelogFrom(ctx).startRepository(true)
	})

	sc.Step(`^two changes written at the same time, on a repository that does not keep both sides$`, func(ctx context.Context) error {
		return changelogFrom(ctx).startRepository(false)
	})

	sc.Step(`^each one writes its own changelog fragment$`, func(ctx context.Context) error {
		c := changelogFrom(ctx)
		if err := c.change("one", "changelog.d/455-a-console-view-of-jobs.md",
			"**A console view of jobs.** What each job is doing.\n", "**A console view of jobs.**"); err != nil {
			return err
		}
		return c.change("two", "changelog.d/461-a-graph-says-what-mode-it-runs-work-in.md",
			"**A graph says what mode it runs work in.** So a reader knows.\n", "**A graph says what mode it runs work in.**")
	})

	sc.Step(`^each one writes its entry at the top of the changelog$`, func(ctx context.Context) error {
		c := changelogFrom(ctx)
		first := "- **A console view of jobs.** What each job is doing."
		second := "- **A graph says what mode it runs work in.** So a reader knows."
		if err := c.change("one", "CHANGELOG.md", entryAtTheTop(first), first); err != nil {
			return err
		}
		return c.change("two", "CHANGELOG.md", entryAtTheTop(second), second)
	})

	sc.Step(`^the branches merge with nothing to resolve$`, func(ctx context.Context) error {
		c := changelogFrom(ctx)
		if out, err := c.git("switch", "-q", "one"); err != nil {
			return fmt.Errorf("git switch: %v: %s", err, out)
		}
		out, err := c.git("merge", "--no-edit", "two")
		c.mergeSaid, c.merged = out, err == nil
		if !c.merged {
			return fmt.Errorf("the merge stopped, and this is the conflict nobody should be resolving:\n%s", out)
		}
		return nil
	})

	sc.Step(`^both changes are in the tree$`, func(ctx context.Context) error {
		c := changelogFrom(ctx)
		if len(c.wrote) != 2 {
			return fmt.Errorf("%d changes were made, so this proves nothing", len(c.wrote))
		}
		for _, one := range c.wrote {
			body, err := os.ReadFile(filepath.Join(c.repo, filepath.FromSlash(one.file)))
			if err != nil {
				return fmt.Errorf("the merge lost %s: %w", one.file, err)
			}
			if !strings.Contains(string(body), one.mark) {
				return fmt.Errorf("%s came out of the merge without %q:\n%s", one.file, one.mark, body)
			}
		}
		return nil
	})

	sc.Step(`^the merge stops on a conflict in "([^"]*)"$`, func(ctx context.Context, file string) error {
		c := changelogFrom(ctx)
		if out, err := c.git("switch", "-q", "one"); err != nil {
			return fmt.Errorf("git switch: %v: %s", err, out)
		}
		out, err := c.git("merge", "--no-edit", "two")
		c.mergeSaid, c.merged = out, err == nil
		if c.merged {
			return fmt.Errorf("the merge went through, so this scenario is no longer about anything:\n%s", out)
		}
		if !strings.Contains(out, "CONFLICT") || !strings.Contains(out, file) {
			return fmt.Errorf("the merge stopped on something else:\n%s", out)
		}
		return nil
	})

	sc.Step(`^a change waiting for a release in "([^"]*)" saying "([^"]*)"$`, func(ctx context.Context, file, body string) error {
		c := changelogFrom(ctx)
		return os.WriteFile(filepath.Join(c.pending, file), []byte(body+"\n"), 0o600)
	})

	sc.Step(`^nothing is waiting for a release$`, func(ctx context.Context) error {
		c := changelogFrom(ctx)
		entries, err := os.ReadDir(c.pending)
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return fmt.Errorf("something is waiting after all: %d files", len(entries))
		}
		return nil
	})

	sc.Step(`^the release is assembled$`, func(ctx context.Context) error {
		return changelogFrom(ctx).assemble()
	})

	sc.Step(`^it prints a section dated today, holding these lines in this order:$`, func(ctx context.Context, lines *godog.Table) error {
		c := changelogFrom(ctx)
		if c.refused {
			return fmt.Errorf("the release refused:\n%s", c.said)
		}
		today := "## " + time.Now().Format("2 January 2006")
		if !strings.HasPrefix(c.said, today+"\n") {
			return fmt.Errorf("the section is not headed %q:\n%s", today, c.said)
		}
		at := 0
		for _, row := range lines.Rows {
			want := strings.TrimSpace(row.Cells[0].Value)
			found := strings.Index(c.said[at:], want)
			if found < 0 {
				return fmt.Errorf("%q is not in the section, or not after the line above it:\n%s", want, c.said)
			}
			at += found + len(want)
		}
		return nil
	})

	sc.Step(`^it refuses, naming "([^"]*)"$`, func(ctx context.Context, file string) error {
		c := changelogFrom(ctx)
		if !c.refused {
			return fmt.Errorf("the release went ahead:\n%s", c.said)
		}
		if !strings.Contains(c.said, file) {
			return fmt.Errorf("it refused without saying which file:\n%s", c.said)
		}
		return nil
	})

	sc.Step(`^it refuses, saying there is nothing to assemble$`, func(ctx context.Context) error {
		c := changelogFrom(ctx)
		if !c.refused {
			return fmt.Errorf("the release went ahead on nothing:\n%s", c.said)
		}
		if !strings.Contains(c.said, "nothing to assemble") {
			return fmt.Errorf("it refused for some other reason:\n%s", c.said)
		}
		return nil
	})
}
