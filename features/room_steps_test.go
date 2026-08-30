package features_test

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing/fstest"

	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/room"
	"github.com/cucumber/godog"
)

// roomWorld is the machine a scenario gives the session, and what the session was told about it.
// Kept beside the shared world so these scenarios do not widen what every other scenario carries.
type roomWorld struct {
	machine fstest.MapFS
	said    string
	err     error
}

type roomKey struct{}

func roomFrom(ctx context.Context) *roomWorld {
	r, _ := ctx.Value(roomKey{}).(*roomWorld)
	return r
}

// killedProcess is a command that was killed. Docker reports the status of the process inside the
// container as its own, so a task taken by signal 9 arrives at the system as exit status 137.
type killedProcess struct{}

func (killedProcess) Error() string { return "exit status 137" }
func (killedProcess) ExitCode() int { return 137 }

func initializeRoomSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, roomKey{}, &roomWorld{machine: fstest.MapFS{}}), nil
	})

	sc.Step(`^a machine with (\d+) kilobytes of memory and (\d+) free$`,
		func(ctx context.Context, total, available int) error {
			roomFrom(ctx).machine["proc/meminfo"] = &fstest.MapFile{Data: []byte(fmt.Sprintf(
				"MemTotal:       %d kB\nMemFree:          208832 kB\nMemAvailable:   %d kB\n",
				total, available))}
			return nil
		})

	sc.Step(`^a machine that keeps no memory accounting$`, func(ctx context.Context) error {
		roomFrom(ctx).machine = fstest.MapFS{}
		return nil
	})

	sc.Step(`^the sandbox has no memory limit$`, func(ctx context.Context) error {
		return controlGroup(ctx, "max", "294182912")
	})

	sc.Step(`^the sandbox may take (\d+) megabytes and holds (\d+)$`,
		func(ctx context.Context, limit, held int) error {
			return controlGroup(ctx, strconv.Itoa(limit<<20), strconv.Itoa(held<<20))
		})

	sc.Step(`^an out of memory killer has taken (\d+) process(?:es)? in this sandbox at no limit of its own$`,
		func(ctx context.Context, kills int) error {
			return events(ctx, kills, 0)
		})

	sc.Step(`^an out of memory killer has taken (\d+) process(?:es)? in this sandbox at its own limit$`,
		func(ctx context.Context, kills int) error {
			return events(ctx, kills, 12)
		})

	sc.Step(`^the session asks how much memory it has$`, func(ctx context.Context) error {
		r := roomFrom(ctx)
		reading, err := room.Read(r.machine)
		if err != nil {
			r.err = err
			return nil
		}
		r.said = room.Say(reading)
		return nil
	})

	sc.Step(`^the session is told the sandbox has no limit of its own$`, func(ctx context.Context) error {
		return itSays(ctx, "no memory limit of its own")
	})

	sc.Step(`^the session is told (\d+) megabytes are advertised and (\d+) are free$`,
		func(ctx context.Context, advertised, free int) error {
			if err := itSays(ctx, fmt.Sprintf("it advertises %d MiB", advertised)); err != nil {
				return err
			}
			return itSays(ctx, fmt.Sprintf("free right now %d MiB", free))
		})

	sc.Step(`^the session is told (\d+) megabytes are free$`, func(ctx context.Context, free int) error {
		return itSays(ctx, fmt.Sprintf("free right now %d MiB", free))
	})

	sc.Step(`^the session is told the machine ran out rather than the session$`, func(ctx context.Context) error {
		return itSays(ctx, "the machine ran out rather than this session")
	})

	sc.Step(`^the session is told it ran out itself$`, func(ctx context.Context) error {
		if err := itSays(ctx, "took 3 processes"); err != nil {
			return err
		}
		return itSays(ctx, "reached its own limit")
	})

	sc.Step(`^the session is told nothing about a kill$`, func(ctx context.Context) error {
		if said := roomFrom(ctx).said; strings.Contains(said, "out of memory killer took") {
			return fmt.Errorf("a sandbox nothing was killed in is told something was:\n%s", said)
		}
		return nil
	})

	sc.Step(`^the session is told to cap the heap, take one worker, and run the gate in parts$`,
		func(ctx context.Context) error {
			for _, want := range []string{
				"NODE_OPTIONS=--max-old-space-size", "GOMEMLIMIT", "--maxWorkers=1", "part of the tree",
			} {
				if err := itSays(ctx, want); err != nil {
					return err
				}
			}
			return nil
		})

	sc.Step(`^the session is told to say what it could not run rather than report a partial check$`,
		func(ctx context.Context) error {
			if err := itSays(ctx, "say what you could not run"); err != nil {
				return err
			}
			return itSays(ctx, "report a partial check as a check")
		})

	sc.Step(`^the session is told this reads a linux sandbox and there is none here$`,
		func(ctx context.Context) error {
			r := roomFrom(ctx)
			if r.err == nil {
				return fmt.Errorf("a machine with no accounting was read as a sandbox, and it said:\n%s", r.said)
			}
			if !strings.Contains(r.err.Error(), "linux sandbox") {
				return fmt.Errorf("the refusal does not say what it wanted: %v", r.err)
			}
			return nil
		})

	sc.Step(`^the sandbox is killed for memory part way through the task$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.provider.ExitErr = killedProcess{}
		w.realRunner = model.NewClaudeCodeRunner()
		return w.restart()
	})
}

// controlGroup writes what the kernel keeps about this sandbox: its limit, which reads "max" when it
// has none, and what it is holding now.
func controlGroup(ctx context.Context, limit, held string) error {
	r := roomFrom(ctx)
	r.machine["sys/fs/cgroup/memory.max"] = &fstest.MapFile{Data: []byte(limit + "\n")}
	r.machine["sys/fs/cgroup/memory.current"] = &fstest.MapFile{Data: []byte(held + "\n")}
	return nil
}

// events writes memory.events, which counts what an out of memory killer took in this sandbox and
// how often the sandbox was held at a limit of its own.
func events(ctx context.Context, kills, atLimit int) error {
	roomFrom(ctx).machine["sys/fs/cgroup/memory.events"] = &fstest.MapFile{Data: []byte(fmt.Sprintf(
		"low 0\nhigh 0\nmax %d\noom %d\noom_kill %d\noom_group_kill 0\n", atLimit, kills, kills))}
	return nil
}

// itSays checks what the session was told, ignoring how the figures are padded into columns. A
// scenario says which number the session gets, never how many spaces sit in front of it.
func itSays(ctx context.Context, want string) error {
	r := roomFrom(ctx)
	if r.err != nil {
		return fmt.Errorf("the session was refused: %v", r.err)
	}
	if !strings.Contains(oneSpace(r.said), oneSpace(want)) {
		return fmt.Errorf("the session is not told %q:\n%s", want, r.said)
	}
	return nil
}

// oneSpace collapses every run of spaces and tabs to one, and leaves the line breaks alone.
func oneSpace(text string) string { return spaces.ReplaceAllString(text, " ") }

var spaces = regexp.MustCompile(`[ \t]+`)
