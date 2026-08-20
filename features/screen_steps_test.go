package features_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/panel"
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
		if _, err := exec.LookPath("tmux"); err != nil {
			// A missing dependency in continuous integration is not a pass. A scenario that runs
			// nothing reports exactly what a scenario that held reports.
			if os.Getenv("CI") != "" {
				return fmt.Errorf("tmux is not installed, so this scenario proves nothing: %w", err)
			}
			return godog.ErrSkip
		}
		s.socket = fmt.Sprintf("quay-screen-%d", os.Getpid())
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

	// The real command, in a real pane, pointed at an address with no crew on it. A double that
	// printed something and waited would pass this while the tool exited, which is the defect.
	sc.Step(`^quay attach is put beside the console and cannot reach the crew$`, func(ctx context.Context) error {
		s := screenFrom(ctx)
		built, err := quayBinary()
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

// nowhere is an address with no crew behind it, which is the failure this scenario needs: everything
// about a session is on the other side of that connection, so attach cannot get past it.
const nowhere = "127.0.0.1:59"

// quayBinary builds the tool once and answers with the path to it, because a pane runs a command and
// what is being checked is what that command does to the screen.
func quayBinary() (string, error) {
	builtOnce.Do(func() {
		dir, err := os.MkdirTemp("", "quay-binary")
		if err != nil {
			builtErr = err
			return
		}
		built = filepath.Join(dir, "quay")
		out, err := exec.Command("go", "build", "-o", built, "../cmd/quay").CombinedOutput()
		if err != nil {
			builtErr = fmt.Errorf("building quay: %w: %s", err, out)
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
