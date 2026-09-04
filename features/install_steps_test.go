package features_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"
)

// The first run's scenarios drive the real make, from the repository a directory up, against a system
// directory of their own. Nothing here touches the control plane: what a person types to get a system
// is a behaviour they can see, and the file that says what the product does is this one.
//
// Docker is a double, written as a program on the path, because make calls docker by name and there
// is no other way to answer it. So these prove which commands make runs and in what order, and they
// deliberately do not prove that a real daemon brings the compose file up. That is the containers
// job in continuous integration, which boots the stack and dispatches an exec for real.

type installWorld struct {
	// home is KREWE_HOME, bin is BINDIR, and stubs holds the docker double and the log of its calls.
	home  string
	bin   string
	stubs string
	// said is everything make printed, and failed is whether it exited non zero.
	said   string
	failed bool
	// ran is whether a scenario has run make at all, so a step cannot assert on a run that never
	// happened and read an empty log as a system that was left alone.
	ran bool
	// edited is the configuration file exactly as the operator left it, so a later run is compared
	// against the whole file rather than against one key of it.
	edited string
}

type installKey struct{}

func installFrom(ctx context.Context) *installWorld {
	i, _ := ctx.Value(installKey{}).(*installWorld)
	return i
}

// dockerLog is where the double writes what it was called with.
func (i *installWorld) dockerLog() string { return filepath.Join(i.stubs, "calls") }

// answerDockerWith writes the double. running is what `docker compose ps --status running --quiet`
// answers: one container id per line, and nothing at all when the system is down.
func (i *installWorld) answerDockerWith(running int) error {
	ids := ""
	for n := range running {
		ids += fmt.Sprintf("echo container%d\n", n)
	}
	double := "#!/bin/sh\n" +
		"echo \"docker $*\" >> " + i.dockerLog() + "\n" +
		"for one in \"$@\"; do\n" +
		"  if [ \"$one\" = \"ps\" ]; then\n" + ids + "    exit 0\n  fi\n" +
		"done\n" +
		"exit 0\n"
	return os.WriteFile(filepath.Join(i.stubs, "docker"), []byte(double), 0o755)
}

// run drives one make target, with whatever the operator typed on its standard input.
func (i *installWorld) run(typed string, args ...string) error {
	// One run, one log. Counting over a system that has been run before would read the earlier run's
	// own compose call as this one's, so a refusal would look like a system that had been replaced.
	if err := os.Remove(i.dockerLog()); err != nil && !os.IsNotExist(err) {
		return err
	}

	command := exec.Command("make", append([]string{"-C", "..", "--no-print-directory",
		"KREWE_HOME=" + i.home, "BINDIR=" + i.bin}, args...)...)
	command.Env = append(os.Environ(), "PATH="+i.stubs+string(os.PathListSeparator)+os.Getenv("PATH"))
	command.Stdin = strings.NewReader(typed)

	out, err := command.CombinedOutput()
	i.said = string(out)
	i.failed = err != nil
	i.ran = true
	return nil
}

// broughtUp counts the times the stack was brought up during the last run.
func (i *installWorld) broughtUp() int {
	body, err := os.ReadFile(i.dockerLog())
	if err != nil {
		return 0
	}
	return strings.Count(string(body), "up --build -d")
}

// asArguments splits a quoted make command the way a person typed it, so a scenario reads
// "make install YES=1" rather than a list.
func asArguments(typed string) []string {
	fields := strings.Fields(typed)
	if len(fields) == 0 || fields[0] != "make" {
		return nil
	}
	return fields[1:]
}

func initializeInstallSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		base, err := os.MkdirTemp("", "quaycrew-install")
		if err != nil {
			return ctx, err
		}
		i := &installWorld{
			home:  filepath.Join(base, "system"),
			bin:   filepath.Join(base, "bin"),
			stubs: filepath.Join(base, "stubs"),
		}
		if err := os.MkdirAll(i.stubs, 0o755); err != nil {
			return ctx, err
		}
		return context.WithValue(ctx, installKey{}, i), nil
	})

	sc.Step(`^a machine with no system on it$`, func(ctx context.Context) error {
		return installFrom(ctx).answerDockerWith(0)
	})

	sc.Step(`^a machine with a system already running$`, func(ctx context.Context) error {
		i := installFrom(ctx)
		// It is a system that was installed once and is up now, not a bare machine with containers on
		// it, because the question the refusal answers is what a second run costs.
		if err := i.answerDockerWith(0); err != nil {
			return err
		}
		if err := i.run("", "install"); err != nil {
			return err
		}
		if i.failed {
			return fmt.Errorf("the first run failed, so there is no running system to test against:\n%s", i.said)
		}
		return i.answerDockerWith(2)
	})

	sc.Step(`^the operator ran "([^"]*)"$`, func(ctx context.Context, typed string) error {
		i := installFrom(ctx)
		if err := i.run("", asArguments(typed)...); err != nil {
			return err
		}
		if i.failed {
			return fmt.Errorf("%s failed:\n%s", typed, i.said)
		}
		return nil
	})

	sc.Step(`^the operator runs "([^"]*)"$`, func(ctx context.Context, typed string) error {
		return installFrom(ctx).run("", asArguments(typed)...)
	})

	sc.Step(`^the operator runs "([^"]*)" and types "([^"]*)"$`, func(ctx context.Context, typed, answer string) error {
		return installFrom(ctx).run(answer+"\n", asArguments(typed)...)
	})

	sc.Step(`^the operator edited the configuration file$`, func(ctx context.Context) error {
		i := installFrom(ctx)
		body, err := os.ReadFile(filepath.Join(i.home, "env"))
		if err != nil {
			return err
		}
		// A line the example cannot already hold. Appending a key that deploy/env.example sets would
		// leave the check below passing over a file that had been written over with the example.
		i.edited = string(body) + "\nQC_SANDBOX_IMAGE=the-one-i-chose:local\n"
		return os.WriteFile(filepath.Join(i.home, "env"), []byte(i.edited), 0o644)
	})

	sc.Step(`^it succeeds$`, func(ctx context.Context) error {
		if i := installFrom(ctx); i.failed {
			return fmt.Errorf("it exited non zero:\n%s", i.said)
		}
		return nil
	})

	// A refusal that exits 0 is read as a success by anything that reads an exit code, which is what
	// issue 419 is open about one layer up.
	sc.Step(`^it refuses$`, func(ctx context.Context) error {
		if i := installFrom(ctx); !i.failed {
			return fmt.Errorf("it exited 0, so a caller reads the refusal as a system that came up:\n%s", i.said)
		}
		return nil
	})

	sc.Step(`^the system has a configuration file$`, func(ctx context.Context) error {
		if _, err := os.Stat(filepath.Join(installFrom(ctx).home, "env")); err != nil {
			return fmt.Errorf("there is no configuration file, so compose is pointed at nothing: %w", err)
		}
		return nil
	})

	sc.Step(`^the system has a data directory$`, func(ctx context.Context) error {
		if _, err := os.Stat(filepath.Join(installFrom(ctx).home, "data")); err != nil {
			return fmt.Errorf("there is no data directory, so docker would make it as root: %w", err)
		}
		return nil
	})

	sc.Step(`^the configuration file still says what the operator wrote$`, func(ctx context.Context) error {
		i := installFrom(ctx)
		body, err := os.ReadFile(filepath.Join(i.home, "env"))
		if err != nil {
			return err
		}
		// The whole file, not one key of it. Every key in the example survives the example being
		// copied over the file, so a check for one of them passes against the very failure it is for.
		if string(body) != i.edited {
			return fmt.Errorf("the configuration the operator edited was written over:\n%s", body)
		}
		return nil
	})

	// The binary is run, not looked for. A file of the right name that cannot execute is exactly the
	// failure that a check for the file would report as a pass.
	sc.Step(`^krewe is on the path and says which build it is$`, func(ctx context.Context) error {
		installed := filepath.Join(installFrom(ctx).bin, "krewe")
		reported, err := exec.Command(installed, "version").CombinedOutput()
		if err != nil {
			return fmt.Errorf("the krewe that was installed does not run: %w\n%s", err, reported)
		}
		if !strings.Contains(string(reported), "tool") {
			return fmt.Errorf("it does not say which build it is:\n%s", reported)
		}
		return nil
	})

	sc.Step(`^the stack was brought up once$`, func(ctx context.Context) error {
		i := installFrom(ctx)
		if got := i.broughtUp(); got != 1 {
			return fmt.Errorf("the stack was brought up %d times, want once:\n%s", got, i.said)
		}
		return nil
	})

	sc.Step(`^the stack was never brought up$`, func(ctx context.Context) error {
		i := installFrom(ctx)
		if !i.ran {
			return fmt.Errorf("nothing was run, so an empty log says nothing")
		}
		if got := i.broughtUp(); got != 0 {
			return fmt.Errorf("the stack was brought up %d times:\n%s", got, i.said)
		}
		return nil
	})

	sc.Step(`^it says it cannot mint the model credential$`, func(ctx context.Context) error {
		i := installFrom(ctx)
		if !strings.Contains(i.said, "cannot mint your model credential") {
			return fmt.Errorf("it never says what it cannot do:\n%s", i.said)
		}
		if !strings.Contains(i.said, "claude setup-token") {
			return fmt.Errorf("it never says where a credential comes from:\n%s", i.said)
		}
		return nil
	})

	sc.Step(`^it prints these commands:$`, func(ctx context.Context, listed *godog.Table) error {
		i := installFrom(ctx)
		for _, row := range listed.Rows {
			one := row.Cells[0].Value
			if !strings.Contains(i.said, one) {
				return fmt.Errorf("it never printed %q, so the operator has a system and no next step:\n%s",
					one, i.said)
			}
		}
		return nil
	})

	sc.Step(`^it says the system is already up$`, func(ctx context.Context) error {
		i := installFrom(ctx)
		if !strings.Contains(i.said, "already up") {
			return fmt.Errorf("it never says why it stopped:\n%s", i.said)
		}
		return nil
	})

	sc.Step(`^it offers "([^"]*)"$`, func(ctx context.Context, offered string) error {
		i := installFrom(ctx)
		if !strings.Contains(i.said, offered) {
			return fmt.Errorf("it never offers %q, so there is no way past the refusal:\n%s", offered, i.said)
		}
		return nil
	})
}
