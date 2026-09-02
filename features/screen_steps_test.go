package features_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/panel"
	"github.com/cucumber/godog"
)

// The scenarios about what the operator is left looking at when a conversation cannot be opened.
//
// These drive a real terminal multiplexer, on a socket of their own so they never touch the screen
// anybody is sitting in front of. Everything else in this suite asserts on the commands the panel
// would run, which is right for a layout, and useless here: the whole of this behaviour is what the
// multiplexer does with a pane whose command has ended.

type screenWorld struct {
	socket       string
	console      string
	conversation string
}

type screenKey struct{}

func screenFrom(ctx context.Context) *screenWorld {
	s, _ := ctx.Value(screenKey{}).(*screenWorld)
	return s
}

const screenTarget = "screen:panel"

func initializeScreenSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, screenKey{}, &screenWorld{}), nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if s := screenFrom(ctx); s != nil && s.socket != "" {
			_ = exec.Command("tmux", "-L", s.socket, "kill-server").Run()
		}
		return ctx, nil
	})

	sc.Step(`^a terminal with the console in it$`, func(ctx context.Context) error {
		s := screenFrom(ctx)
		if err := s.own(); err != nil {
			return err
		}
		if _, err := s.tmux("new-session", "-d", "-s", "screen", "-n", "panel",
			"-x", "120", "-y", "20", "sleep 300"); err != nil {
			return err
		}
		panes, err := s.panes()
		if err != nil {
			return err
		}
		s.console = panes[0]
		return nil
	})

	// The real command, in a real pane, pointed at an address with no system on it. A double that
	// printed something and waited would pass this while the tool exited, which is the defect.
	sc.Step(`^krewe attach is put beside the console and cannot reach the system$`, func(ctx context.Context) error {
		s := screenFrom(ctx)
		built, err := kreweBinary()
		if err != nil {
			return err
		}
		return s.beside(fmt.Sprintf("QC_GRPC_ADDR=%s %s attach ffffffff", nowhere, built))
	})

	// What it used to do, and the measurement the change is built on.
	sc.Step(`^a conversation that says why and exits is put beside the console$`, func(ctx context.Context) error {
		return screenFrom(ctx).beside("echo 'cannot open the conversation in 5ae35d77'; exit 1")
	})

	sc.Step(`^the reason is on the screen$`, func(ctx context.Context) error {
		s := screenFrom(ctx)
		// Both halves of it: what went wrong, and that the screen is being held for them to read it.
		for range 500 {
			screen, err := s.tmux("capture-pane", "-p", "-t", s.conversation)
			if err == nil && strings.Contains(screen, nowhere) && strings.Contains(screen, "Press enter") {
				return nil
			}
			time.Sleep(10 * time.Millisecond)
		}
		screen, _ := s.tmux("capture-pane", "-p", "-t", s.conversation)
		return fmt.Errorf("the conversation half shows %q, want the reason it could not open", screen)
	})

	// The next key. The operator has read it, presses enter, and the pane goes on its own, leaving
	// the console they were using.
	sc.Step(`^pressing enter gives the operator the console back$`, func(ctx context.Context) error {
		s := screenFrom(ctx)
		if _, err := s.tmux("send-keys", "-t", s.conversation, "Enter"); err != nil {
			return err
		}
		for range 300 {
			left, err := s.panes()
			if err != nil {
				return err
			}
			if len(left) == 1 && left[0] == s.console {
				return nil
			}
			time.Sleep(10 * time.Millisecond)
		}
		screen, _ := s.tmux("capture-pane", "-p", "-t", s.conversation)
		return fmt.Errorf("the conversation half is still there after enter, showing %q", screen)
	})

	sc.Step(`^the pane is gone, and the reason with it$`, func(ctx context.Context) error {
		s := screenFrom(ctx)
		for range 200 {
			left, err := s.panes()
			if err != nil {
				return err
			}
			if len(left) == 1 && left[0] == s.console {
				return nil
			}
			time.Sleep(10 * time.Millisecond)
		}
		return fmt.Errorf("the pane outlived its command, so the reason was readable after all")
	})

	// The panel as the tool builds it, on a socket of its own. Everything about this behaviour is
	// what the multiplexer does with the panes, so nothing here stands in for one.
	sc.Step(`^a panel with a console and a conversation$`, func(ctx context.Context) error {
		s := screenFrom(ctx)
		if err := s.own(); err != nil {
			return err
		}
		if err := s.openSystem(panel.Terminal{}); err != nil {
			return err
		}
		return s.readHalves()
	})

	// ctrl-q detaches the client inside the sandbox, the command in the pane ends, and the pane goes
	// with it. Killing the pane is that, without a sandbox to do it in.
	sc.Step(`^the conversation is closed the way leaving it closes it$`, func(ctx context.Context) error {
		s := screenFrom(ctx)
		if _, err := s.tmux("kill-pane", "-t", s.conversation); err != nil {
			return err
		}
		return nil
	})

	sc.Step(`^the operator opens the system again$`, func(ctx context.Context) error {
		s := screenFrom(ctx)
		// The same question the tool asks, through the same function, so a scenario cannot be kinder
		// about a missing half than the tool is.
		return s.openSystem(panel.Terminal{
			AlreadyOpen: true,
			LostAHalf:   !panel.Whole(s.layout(), s.ask),
		})
	})

	sc.Step(`^the panel has a console and a conversation again$`, func(ctx context.Context) error {
		s := screenFrom(ctx)
		panes, err := s.panes()
		if err != nil {
			return err
		}
		if len(panes) != 2 {
			screen, _ := s.tmux("capture-pane", "-p", "-t", screenTarget+".0")
			return fmt.Errorf("the panel has %d pane(s), want a console and a conversation. It shows %q",
				len(panes), screen)
		}
		return nil
	})

	sc.Step(`^the conversation is the one that was already there$`, func(ctx context.Context) error {
		s := screenFrom(ctx)
		was := s.conversation
		if err := s.readHalves(); err != nil {
			return err
		}
		if s.conversation != was {
			return fmt.Errorf("the conversation is now in pane %s, and it was in %s: opening the "+
				"system took the one that was there away", s.conversation, was)
		}
		return nil
	})
}

// beside puts a conversation next to the console the way the console does it, with argv standing in
// for the command attach would be.
func (s *screenWorld) beside(argv string) error {
	commands, err := panel.Beside(s.console, []string{"sh", "-c", argv})
	if err != nil {
		return err
	}
	for _, command := range commands {
		if _, err := s.tmux(command[1:]...); err != nil {
			return err
		}
	}
	// Whichever pane is there and is not the console. There may be none: a command that exits takes
	// its pane before anything can list it, and that is what one of these scenarios is about.
	panes, err := s.panes()
	if err != nil {
		return err
	}
	for _, pane := range panes {
		if pane != s.console {
			s.conversation = pane
		}
	}
	return nil
}

// own gives this scenario a multiplexer of its own, so it never touches the screen anybody is
// sitting in front of. A machine without one skips, except in continuous integration: a scenario
// that runs nothing reports exactly what a scenario that passed reports.
func (s *screenWorld) own() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		if os.Getenv("CI") != "" {
			return fmt.Errorf("tmux is not installed, so this scenario proves nothing: %w", err)
		}
		return godog.ErrSkip
	}
	s.socket = fmt.Sprintf("krewe-screen-%d", os.Getpid())
	return nil
}

// layout is the panel this scenario opens. Both halves are commands that stay, because what is being
// checked is the shape of the window rather than what is drawn in it.
func (s *screenWorld) layout() panel.Layout {
	return panel.Layout{
		Session: "screen",
		Left:    []string{"sh", "-c", "sleep 300"},
		Right:   []string{"sh", "-c", "sleep 300"},
		Width:   120,
		Height:  20,
	}
}

// openSystem runs every tmux invocation the layout asks for, except the last one. That one puts a
// person in front of the panel, and a scenario has nobody to put there.
func (s *screenWorld) openSystem(term panel.Terminal) error {
	commands, err := s.layout().Commands(term)
	if err != nil {
		return err
	}
	for _, argv := range commands[:len(commands)-1] {
		if _, err := s.tmux(argv[1:]...); err != nil {
			return err
		}
	}
	return nil
}

// ask is how the layout puts its one question to this scenario's multiplexer.
func (s *screenWorld) ask(argv []string) (string, error) {
	return s.tmux(argv[1:]...)
}

// readHalves is which pane holds the console and which holds the conversation, taken from where each
// one starts rather than from the order they happen to be listed in.
func (s *screenWorld) readHalves() error {
	out, err := s.tmux("list-panes", "-F", "#{pane_left} #{pane_id}", "-t", screenTarget)
	if err != nil {
		return err
	}
	type half struct {
		left int
		pane string
	}
	halves := make([]half, 0, 2)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		left, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		halves = append(halves, half{left: left, pane: parts[1]})
	}
	if len(halves) != 2 {
		return fmt.Errorf("the panel is not two halves: tmux lists %q", strings.TrimSpace(out))
	}
	sort.Slice(halves, func(i, j int) bool { return halves[i].left < halves[j].left })
	s.console, s.conversation = halves[0].pane, halves[1].pane
	return nil
}

// nowhere is an address with no system behind it, which is the failure this scenario needs: everything
// about a session is on the other side of that connection, so attach cannot get past it.
const nowhere = "127.0.0.1:59"

// kreweBinary builds the tool once and answers with the path to it, because a pane runs a command and
// what is being checked is what that command does to the screen.
func kreweBinary() (string, error) {
	builtOnce.Do(func() {
		dir, err := os.MkdirTemp("", "krewe-binary")
		if err != nil {
			builtErr = err
			return
		}
		built = filepath.Join(dir, "krewe")
		// Stamped the way the install target stamps it, so a scenario can say the tool and the system
		// are different builds and have the tool actually report one.
		out, err := exec.Command("go", "build", "-ldflags", "-X main.version="+toolBuild,
			"-o", built, "../cmd/krewe").CombinedOutput()
		if err != nil {
			builtErr = fmt.Errorf("building krewe: %w: %s", err, out)
		}
	})
	return built, builtErr
}

var (
	builtOnce sync.Once
	built     string
	builtErr  error
)

func (s *screenWorld) tmux(argv ...string) (string, error) {
	out, err := exec.Command("tmux", append([]string{"-L", s.socket}, argv...)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(argv, " "), err, out)
	}
	return string(out), nil
}

func (s *screenWorld) panes() ([]string, error) {
	out, err := s.tmux(panel.Opened(screenTarget)[1:]...)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}
