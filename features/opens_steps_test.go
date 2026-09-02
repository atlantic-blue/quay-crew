package features_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// What `krewe` on its own puts on the screen.
//
// It used to build a window with the console in one half and a conversation in the other. These
// scenarios run the real tool in a real terminal multiplexer, on a socket of its own, because the
// fault they exist to catch is a second pane appearing, and a scenario that reads the commands the
// tool would run cannot see one.

type kreweWorld struct {
	socket string
	pane   string
}

type kreweKey struct{}

func kreweWorldFrom(ctx context.Context) *kreweWorld {
	k, _ := ctx.Value(kreweKey{}).(*kreweWorld)
	return k
}

func initializeOpensSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, kreweKey{}, &kreweWorld{}), nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if k := kreweWorldFrom(ctx); k != nil && k.socket != "" {
			_ = exec.Command("tmux", "-L", k.socket, "kill-server").Run()
		}
		return ctx, nil
	})

	sc.Step(`^a session started by dispatching "([^"]*)" on a new session$`, func(ctx context.Context, text string) error {
		w := worldFrom(ctx)
		if err := w.dispatch(ctx, w.projectID, "", text); err != nil {
			return err
		}
		return w.lastErr
	})

	sc.Step(`^a terminal to type krewe in$`, func(ctx context.Context) error {
		k := kreweWorldFrom(ctx)
		if _, err := exec.LookPath("tmux"); err != nil {
			if os.Getenv("CI") != "" {
				return fmt.Errorf("tmux is not installed, so this scenario proves nothing: %w", err)
			}
			return godog.ErrSkip
		}
		k.socket = fmt.Sprintf("krewe-opens-%d", os.Getpid())
		if _, err := k.tmux("new-session", "-d", "-s", "typing", "-n", "terminal",
			"-x", "120", "-y", "40"); err != nil {
			return err
		}
		panes, err := k.panes()
		if err != nil {
			return err
		}
		k.pane = panes[0]
		return nil
	})

	// The real tool, in a real pane, with a real system to reach. A double would have nothing to say
	// about the shape of the screen, which is the whole question here.
	sc.Step(`^the operator types krewe$`, func(ctx context.Context) error {
		k, t := kreweWorldFrom(ctx), toolFrom(ctx)
		if t.address == "" {
			return fmt.Errorf("the system has no address the tool can dial")
		}
		built, err := kreweBinary()
		if err != nil {
			return err
		}
		home, err := os.MkdirTemp("", "krewe-opens-")
		if err != nil {
			return err
		}
		typed := fmt.Sprintf("QC_GRPC_ADDR=%s QC_TOKEN=%s KREWE_HOME=%s HOME=%s %s",
			t.address, worldFrom(ctx).token, home, home, built)
		if _, err := k.tmux("send-keys", "-t", k.pane, typed, "Enter"); err != nil {
			return err
		}
		// Waited for rather than slept through: the console is what the pane is running once it has
		// taken the terminal, and tmux is asked which command that is.
		for range 600 {
			running, err := k.tmux("display-message", "-p", "-t", k.pane, "#{pane_current_command}")
			if err == nil && strings.Contains(strings.TrimSpace(running), "krewe") {
				return nil
			}
			time.Sleep(10 * time.Millisecond)
		}
		screen, _ := k.tmux("capture-pane", "-p", "-t", k.pane)
		return fmt.Errorf("krewe never took the terminal. The pane shows %q", screen)
	})

	sc.Step(`^the terminal holds one pane$`, func(ctx context.Context) error {
		k := kreweWorldFrom(ctx)
		// Held for a moment: a split that arrives late is the fault this scenario is for, and a
		// single count taken straight away would miss it.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			panes, err := k.panes()
			if err != nil {
				return err
			}
			if len(panes) != 1 {
				screen, _ := k.tmux("capture-pane", "-p", "-t", k.pane)
				return fmt.Errorf("krewe left %d panes on the screen, want the console on its own. "+
					"The first one shows %q", len(panes), screen)
			}
			time.Sleep(50 * time.Millisecond)
		}
		return nil
	})

	sc.Step(`^no second window was built to hold a conversation$`, func(ctx context.Context) error {
		k := kreweWorldFrom(ctx)
		// The tool runs inside this multiplexer, so anything it built is on this socket.
		listed, err := k.tmux("list-sessions", "-F", "#{session_name}")
		if err != nil {
			return err
		}
		for _, name := range strings.Fields(listed) {
			if name != "typing" {
				return fmt.Errorf("krewe built a session called %q, and the operator asked for none", name)
			}
		}
		windows, err := k.tmux("list-windows", "-F", "#{window_name}", "-t", "typing")
		if err != nil {
			return err
		}
		if got := strings.Fields(windows); len(got) != 1 {
			return fmt.Errorf("the terminal holds windows %v, want the one it started with", got)
		}
		return nil
	})

	sc.Step(`^the operator asks the tool to open the panel$`, func(ctx context.Context) error {
		return runTool(ctx, "panel")
	})
}

func (k *kreweWorld) tmux(argv ...string) (string, error) {
	out, err := exec.Command("tmux", append([]string{"-L", k.socket}, argv...)...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("tmux %s: %w: %s", strings.Join(argv, " "), err, out)
	}
	return string(out), nil
}

func (k *kreweWorld) panes() ([]string, error) {
	out, err := k.tmux("list-panes", "-F", "#{pane_id}", "-t", "typing:terminal")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}
