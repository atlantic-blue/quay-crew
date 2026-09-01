package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A first run used to be four commands, and the order mattered. `make config`, `make sandbox-image`,
// `make up`, `make install`. Miss one and the failure arrived somewhere else: compose reading a file
// that is not there, or a first task refused for a missing image, which reads as a broken system
// rather than a missing step.
//
// These cases hold `make install` to being the only command a first run needs, and hold the four
// underneath it to still working on their own.

// theOrderOfAFirstRun is what install has to do, in the order it has to do it in. Configuration
// before the builds because compose is told the path to a file that has to exist. The builds before
// the stack because the image a session runs in is one of them. up-check before up because a system
// that is already working gets a say before compose replaces the services under it.
var theOrderOfAFirstRun = []string{"config", "rebuild", "up-check", "up"}

// TestAFirstRunIsOneCommand, in the order a first run needs.
//
// Each step is a sub make rather than a prerequisite. Prerequisites are a set, and a parallel make
// runs a set in whatever order it likes, so an order written as prerequisites is an order that holds
// only until somebody types -j.
func TestAFirstRunIsOneCommand(t *testing.T) {
	recipe := target(t, "install")

	at := -1
	for _, step := range theOrderOfAFirstRun {
		found := strings.Index(recipe, "$(MAKE) --no-print-directory "+step+"\n")
		if found < 0 {
			t.Fatalf("a first run never runs %s, so it is not the only command a first run needs:\n%s",
				step, recipe)
		}
		if found < at {
			t.Errorf("a first run runs %s out of order, and the order is %v:\n%s",
				step, theOrderOfAFirstRun, recipe)
		}
		at = found
	}
}

// TestAFirstRunBringsTheStackUpOnce. Twice would replace the services a second time, which ends a
// task that had just started under the first.
func TestAFirstRunBringsTheStackUpOnce(t *testing.T) {
	if got := strings.Count(target(t, "install"), "--no-print-directory up\n"); got != 1 {
		t.Errorf("a first run brings the stack up %d times, want once", got)
	}
}

// TestUpgradeDoesNotRunTheWholeFirstRun.
//
// `rebuild` called `install` and `upgrade` called both, back when install built the tool and nothing
// else. Now that install is the whole first run, either of those would have upgrade bring the stack
// up in the middle of itself and then again at the end, which is two restarts of a system whose
// sessions were just drained for one.
func TestUpgradeDoesNotRunTheWholeFirstRun(t *testing.T) {
	upgrade := target(t, "upgrade")
	if strings.Contains(upgrade, "--no-print-directory install\n") {
		t.Errorf("make upgrade runs the whole first run, so it brings the stack up twice:\n%s", upgrade)
	}
	if !strings.Contains(upgrade, "--no-print-directory tool\n") {
		t.Errorf("make upgrade never builds the tool, so it drains with the build from before:\n%s", upgrade)
	}
	if got := strings.Count(upgrade, "up --build"); got != 1 {
		t.Errorf("make upgrade brings the stack up %d times, want once:\n%s", got, upgrade)
	}

	if built := prerequisites(t, "rebuild"); strings.Contains(built, "install") {
		t.Errorf("rebuild is built from install, which is the whole first run, so building the tool "+
			"would bring the stack up:\n%s", built)
	}
}

// TestEveryPieceIsStillCallableOnItsOwn. Somebody rebuilding one part must not be made to sit
// through the other three and a restart of their system.
func TestEveryPieceIsStillCallableOnItsOwn(t *testing.T) {
	for _, name := range []string{"config", "sandbox-image", "hooks", "up", "tool", "rebuild"} {
		recipe := ""
		if name == "rebuild" {
			recipe = prerequisites(t, name)
		} else {
			recipe = target(t, name)
		}
		if recipe == "" {
			t.Errorf("%s is no longer a target of its own", name)
		}
	}

	// And building leaves a running system where it is. rebuild is what the refusal offers as the way
	// to build without restarting anything, so it must not reach the stack.
	if built := prerequisites(t, "rebuild"); strings.Contains(built, "up") {
		t.Errorf("rebuild brings the stack up, so the way to build without a restart does not exist:\n%s", built)
	}
}

// TestAFirstRunSaysWhatItCannotDo. It cannot mint the model credential, so it ends by printing the
// commands that are the operator's. In full: a pointer to a document is a second thing to go and
// find at the moment somebody has a working system and no idea what to type.
func TestAFirstRunSaysWhatItCannotDo(t *testing.T) {
	recipe := target(t, "install")
	for _, said := range []string{
		"claude setup-token",
		"krewe workspace create <name>",
		"krewe project create <name>",
		"krewe secret set CLAUDE_CODE_OAUTH_TOKEN",
		`krewe task \"say pong\"`,
	} {
		if !strings.Contains(recipe, said) {
			t.Errorf("a first run never says %q, so the operator is left with a system and no next step:\n%s",
				said, recipe)
		}
	}
}

// The cases below run the real make against a temporary system directory, so what is proved is what
// make does rather than what the file says.

// aFirstRun is one run of a make target against a system of its own, with docker answered by a double.
type aFirstRun struct {
	home    string
	bin     string
	said    string
	failed  bool
	dockerd string
}

// ran drives one make target. docker is a double written as a program on the path, because make
// calls docker by name and there is no other way to answer it. What a real docker does with the real
// compose file is the containers job in continuous integration, which boots the stack for real.
//
// running is what `docker compose ps --status running --quiet` answers: one container id per line,
// and nothing when the system is down.
func ran(t *testing.T, system *aFirstRun, running int, typed string, args ...string) *aFirstRun {
	t.Helper()
	if system == nil {
		base := t.TempDir()
		system = &aFirstRun{
			home:    filepath.Join(base, "system"),
			bin:     filepath.Join(base, "bin"),
			dockerd: filepath.Join(base, "docker"),
		}
		if err := os.MkdirAll(system.dockerd, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	log := filepath.Join(system.dockerd, "calls")
	// One run, one log. Counting over a system that has been run before would read the first run's own
	// compose call as the second run's, which is a refusal that looks like it brought the stack up.
	if err := os.Remove(log); err != nil && !os.IsNotExist(err) {
		t.Fatalf("clearing the docker log: %v", err)
	}
	double := "#!/bin/sh\necho \"docker $*\" >> " + log + "\n" +
		"for one in \"$@\"; do\n" +
		"  if [ \"$one\" = \"ps\" ]; then\n" +
		"    i=0; while [ $i -lt " + itoa(running) + " ]; do echo container$i; i=$((i+1)); done\n" +
		"    exit 0\n" +
		"  fi\n" +
		"done\nexit 0\n"
	if err := os.WriteFile(filepath.Join(system.dockerd, "docker"), []byte(double), 0o755); err != nil {
		t.Fatalf("writing the docker double: %v", err)
	}

	command := exec.Command("make", append([]string{"-C", "..", "--no-print-directory",
		"KREWE_HOME=" + system.home, "BINDIR=" + system.bin}, args...)...)
	command.Env = append(os.Environ(),
		"PATH="+system.dockerd+string(os.PathListSeparator)+os.Getenv("PATH"))
	command.Stdin = strings.NewReader(typed)
	out, err := command.CombinedOutput()

	system.said = string(out)
	system.failed = err != nil
	return system
}

// itoa keeps the double a string the test can read, rather than a format call in the middle of it.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for ; n > 0; n /= 10 {
		digits = string(rune('0'+n%10)) + digits
	}
	return digits
}

// brought counts the times the stack was brought up during a run.
func (system *aFirstRun) brought(t *testing.T) int {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(system.dockerd, "calls"))
	if err != nil {
		return 0
	}
	return strings.Count(string(body), "up --build -d")
}

// TestOneCommandLeavesARunningSystemAndAKreweOnThePath.
//
// The whole point, and it asserts on what the operator is left holding rather than on the calls make
// made: a configuration file, a binary that runs and says which build it is, one stack brought up,
// and the next steps in full.
func TestOneCommandLeavesARunningSystemAndAKreweOnThePath(t *testing.T) {
	system := ran(t, nil, 0, "", "install")
	if system.failed {
		t.Fatalf("a first run failed:\n%s", system.said)
	}

	if _, err := os.Stat(filepath.Join(system.home, "env")); err != nil {
		t.Errorf("a first run wrote no configuration file, so compose is pointed at nothing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(system.home, "data")); err != nil {
		t.Errorf("a first run made no data directory, so docker would make it as root: %v", err)
	}
	if got := system.brought(t); got != 1 {
		t.Errorf("a first run brought the stack up %d times, want once:\n%s", got, system.said)
	}

	// The binary is run, not stat'd. A file of the right name that cannot execute is the failure this
	// would otherwise report as a pass.
	installed := filepath.Join(system.bin, "krewe")
	reported, err := exec.Command(installed, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("the krewe a first run installed does not run: %v\n%s", err, reported)
	}
	if !strings.Contains(string(reported), "tool") {
		t.Errorf("the installed krewe does not say which build it is:\n%s", reported)
	}

	// And the name the tool used to have, beside it. Run rather than stat'd for the same reason, and
	// because what makes it worth installing is what it says: a rename that leaves nothing behind
	// answers the old name with "command not found", which reads as a broken install.
	refused, err := exec.Command(filepath.Join(system.bin, "quay"), "sessions").CombinedOutput()
	if err == nil {
		t.Errorf("the quay a first run installed exited 0, so a script carries on as though it worked:\n%s", refused)
	}
	if !strings.Contains(string(refused), "krewe") {
		t.Errorf("the quay a first run installed says %q, and never names krewe", refused)
	}

	for _, next := range []string{
		"claude setup-token",
		"krewe workspace create <name>",
		"krewe project create <name>",
		"krewe secret set CLAUDE_CODE_OAUTH_TOKEN",
		`krewe task "say pong"`,
	} {
		if !strings.Contains(system.said, next) {
			t.Errorf("a first run never printed %q:\n%s", next, system.said)
		}
	}
}

// TestRunningItTwiceKeepsWhatTheOperatorEdited.
//
// The configuration file is the one thing in a system's directory a person edits by hand: it says
// which model and which image to run. A second run that copied the example over it would put the
// system back on the defaults and say nothing.
func TestRunningItTwiceKeepsWhatTheOperatorEdited(t *testing.T) {
	system := ran(t, nil, 0, "", "install")
	if system.failed {
		t.Fatalf("the first run failed:\n%s", system.said)
	}

	edited := filepath.Join(system.home, "env")
	body, err := os.ReadFile(edited)
	if err != nil {
		t.Fatalf("reading the configuration: %v", err)
	}
	mine := string(body) + "\nQC_MODEL=claude-code\n"
	if err := os.WriteFile(edited, []byte(mine), 0o644); err != nil {
		t.Fatalf("editing the configuration: %v", err)
	}

	again := ran(t, system, 0, "", "install")
	if again.failed {
		t.Fatalf("a second run failed:\n%s", again.said)
	}

	after, err := os.ReadFile(edited)
	if err != nil {
		t.Fatalf("reading the configuration back: %v", err)
	}
	if string(after) != mine {
		t.Errorf("a second run overwrote the configuration the operator edited:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(system.home, "data")); err != nil {
		t.Errorf("a second run took the system's data directory with it: %v", err)
	}
}

// TestARunningSystemIsNotReplacedWithoutAWord.
//
// Compose replaces the services whose build moved, and a task in flight is executing through the
// control plane, so it ends with it. Nothing typed is the case that matters, because that is a
// script: it refuses, it exits non zero, and it brings nothing up.
func TestARunningSystemIsNotReplacedWithoutAWord(t *testing.T) {
	system := ran(t, nil, 0, "", "install")
	if system.failed {
		t.Fatalf("the first run failed:\n%s", system.said)
	}

	refused := ran(t, system, 2, "", "install")
	if !refused.failed {
		t.Errorf("a refusal exited 0, so a caller reads it as the system having been brought up:\n%s",
			refused.said)
	}
	if got := refused.brought(t); got != 0 {
		t.Errorf("a refused run brought the stack up %d times anyway:\n%s", got, refused.said)
	}
	for _, said := range []string{"already up", "make rebuild", "make install YES=1"} {
		if !strings.Contains(refused.said, said) {
			t.Errorf("the refusal never says %q, so there is no way past it:\n%s", said, refused.said)
		}
	}
}

// And the two ways past it work, because a refusal nobody can clear is worse than no refusal.
func TestARunningSystemIsReplacedWhenTheOperatorSaysSo(t *testing.T) {
	for _, way := range []struct {
		named string
		typed string
		args  []string
	}{
		{named: "typing the system's name back", typed: "krewe\n", args: []string{"install"}},
		{named: "YES=1", typed: "", args: []string{"install", "YES=1"}},
	} {
		t.Run(way.named, func(t *testing.T) {
			system := ran(t, nil, 0, "", "install")
			if system.failed {
				t.Fatalf("the first run failed:\n%s", system.said)
			}
			again := ran(t, system, 2, way.typed, way.args...)
			if again.failed {
				t.Fatalf("%s did not get past the refusal:\n%s", way.named, again.said)
			}
			if got := again.brought(t); got != 1 {
				t.Errorf("%s brought the stack up %d times, want once:\n%s", way.named, got, again.said)
			}
		})
	}
}

// TestTheWrongWordRefusesAndLeavesTheSystemRunning. Typing anything else is the operator saying no, and
// it must read as no rather than as a retry.
func TestTheWrongWordRefusesAndLeavesTheSystemRunning(t *testing.T) {
	system := ran(t, nil, 0, "", "install")
	if system.failed {
		t.Fatalf("the first run failed:\n%s", system.said)
	}

	refused := ran(t, system, 2, "no thanks\n", "install")
	if !refused.failed {
		t.Errorf("the wrong word was taken as agreement:\n%s", refused.said)
	}
	if got := refused.brought(t); got != 0 {
		t.Errorf("the wrong word brought the stack up %d times:\n%s", got, refused.said)
	}
}

// TestBuildingLeavesARunningSystemAlone. rebuild is what the refusal offers, so it has to be true.
func TestBuildingLeavesARunningSystemAlone(t *testing.T) {
	system := ran(t, nil, 2, "", "rebuild")
	if system.failed {
		t.Fatalf("make rebuild failed:\n%s", system.said)
	}
	if got := system.brought(t); got != 0 {
		t.Errorf("make rebuild brought the stack up %d times, and it is the way to build without "+
			"restarting anything:\n%s", got, system.said)
	}
	if _, err := os.Stat(filepath.Join(system.bin, "krewe")); err != nil {
		t.Errorf("make rebuild did not build the tool: %v", err)
	}
}

// TestAPieceOnItsOwnStartsNothing. Configuration is the one somebody runs to look at the file, and it
// must not bring a system up as a side effect of being asked a question.
func TestAPieceOnItsOwnStartsNothing(t *testing.T) {
	system := ran(t, nil, 0, "", "config")
	if system.failed {
		t.Fatalf("make config failed:\n%s", system.said)
	}
	if _, err := os.Stat(filepath.Join(system.home, "env")); err != nil {
		t.Errorf("make config wrote no configuration file: %v", err)
	}
	if got := system.brought(t); got != 0 {
		t.Errorf("make config brought the stack up %d times:\n%s", got, system.said)
	}
}

// TestAFailedBuildDoesNotReportASystem.
//
// The tool's recipe is one shell command joined with semicolons, and a shell command list exits with
// the status of its last command. So a failed `go build` printed "installed krewe to ..." and exited
// 0, and a first run built on top of that would replace the running services and print "the system is
// up" over a tool that was never built. Issue 419 is the same shape one layer up.
func TestAFailedBuildDoesNotReportASystem(t *testing.T) {
	system := ran(t, nil, 0, "", "tool", "BINDIR=/proc/nowhere/bin")
	if !system.failed {
		t.Errorf("a build that could not be installed exited 0:\n%s", system.said)
	}
	if strings.Contains(system.said, "installed krewe to") {
		t.Errorf("a build that could not be installed said it was installed:\n%s", system.said)
	}
}
