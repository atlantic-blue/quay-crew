package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/console"
	"github.com/atlantic-blue/quay-crew/internal/headroom"
	"github.com/cucumber/godog"
)

const megabyte = int64(1 << 20)

// aMachine is a machine the crew was given, so a scenario can be a crew on a full machine without
// filling one. The daemon is the only thing these scenarios stand in for, because a scenario cannot
// make a machine run out of memory on purpose.
type aMachine struct {
	sample headroom.Sample
	err    error
	// read counts how often the crew read it, which is what says the header is not on this path.
	read int
}

func (m *aMachine) Sample(context.Context) (headroom.Sample, error) {
	m.read++
	return m.sample, m.err
}

// headroomWorld is what a scenario said about the machine and what the crew said back.
type headroomWorld struct {
	machine *aMachine
	answer  *quaycrewv1.GetHeadroomResponse
}

type headroomKey struct{}

func headroomFrom(ctx context.Context) *headroomWorld {
	h, _ := ctx.Value(headroomKey{}).(*headroomWorld)
	return h
}

// Steps for the scenarios about what the crew says of the machine it runs on. The fault they close:
// on 27 August 2026 the host ran out of memory, the kernel killed eighteen sandboxes in one event,
// and the console kept drawing a healthy crew. See issue 405.
func initializeHeadroomSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, headroomKey{}, &headroomWorld{}), nil
	})

	sc.Step(`^the machine holds (\d+) megabytes of a (\d+) megabyte limit$`,
		func(ctx context.Context, used, limit int) error {
			h, w := headroomFrom(ctx), worldFrom(ctx)
			h.machine = &aMachine{sample: headroom.Sample{
				Used:  headroom.Measured(int64(used) * megabyte),
				Limit: headroom.Measured(int64(limit) * megabyte),
				Machine: headroom.Machine{
					Name:      "Docker Desktop",
					Total:     headroom.Measured(int64(limit) * megabyte),
					Available: headroom.Measured(int64(limit-used) * megabyte),
				},
				TakenAt: time.Now(),
			}}
			w.machine = h.machine
			return w.restart()
		})

	sc.Step(`^the machine underneath it is using (\d+) megabytes of (\d+) megabytes of swap$`,
		func(ctx context.Context, used, total int) error {
			h, w := headroomFrom(ctx), worldFrom(ctx)
			if h.machine == nil {
				return fmt.Errorf("no machine was given to the crew yet")
			}
			h.machine.sample.Machine.SwapUsed = headroom.Measured(int64(used) * megabyte)
			h.machine.sample.Machine.SwapTotal = headroom.Measured(int64(total) * megabyte)
			return w.restart()
		})

	sc.Step(`^the crew cannot read the machine$`, func(ctx context.Context) error {
		h, w := headroomFrom(ctx), worldFrom(ctx)
		h.machine = &aMachine{err: fmt.Errorf("the daemon is not answering")}
		w.machine = h.machine
		return w.restart()
	})

	sc.Step(`^a sandbox holding (\d+) megabytes for the session that is working$`,
		func(ctx context.Context, held int) error {
			return holdSandbox(ctx, int64(held), true)
		})

	sc.Step(`^a sandbox holding (\d+) megabytes for a session that is idle$`,
		func(ctx context.Context, held int) error {
			return holdSandbox(ctx, int64(held), false)
		})

	sc.Step(`^the crew reads the machine$`, func(ctx context.Context) error {
		worldFrom(ctx).server.SampleHeadroom(ctx)
		return nil
	})

	sc.Step(`^the operator asks how much room there is$`, func(ctx context.Context) error {
		h, w := headroomFrom(ctx), worldFrom(ctx)
		answer, err := w.client.GetHeadroom(ctx, &quaycrewv1.GetHeadroomRequest{})
		w.lastErr = err
		if err != nil {
			return err
		}
		h.answer = answer
		return nil
	})

	sc.Step(`^the crew says it holds "([^"]*)" of "([^"]*)"$`,
		func(ctx context.Context, used, limit string) error {
			h := headroomFrom(ctx)
			if h.answer.GetUsed() != used || h.answer.GetLimit() != limit {
				return fmt.Errorf("the crew says %q of %q, want %q of %q",
					h.answer.GetUsed(), h.answer.GetLimit(), used, limit)
			}
			return nil
		})

	sc.Step(`^the crew says the machine is "([^"]*)"$`, func(ctx context.Context, state string) error {
		if got := headroomFrom(ctx).answer.GetState(); got != state {
			return fmt.Errorf("the crew says the machine is %q, want %q", got, state)
		}
		return nil
	})

	sc.Step(`^the crew says the swap is "([^"]*)" of "([^"]*)"$`,
		func(ctx context.Context, used, total string) error {
			h := headroomFrom(ctx)
			if h.answer.GetSwapUsed() != used || h.answer.GetSwapTotal() != total {
				return fmt.Errorf("the crew says swap is %q of %q, want %q of %q",
					h.answer.GetSwapUsed(), h.answer.GetSwapTotal(), used, total)
			}
			return nil
		})

	sc.Step(`^every figure reads unknown$`, func(ctx context.Context) error {
		h := headroomFrom(ctx)
		figures := map[string]string{
			"what the containers hold": h.answer.GetUsed(),
			"the limit":                h.answer.GetLimit(),
			"what is free":             h.answer.GetFree(),
			"the machine's memory":     h.answer.GetMachineTotal(),
			"the swap":                 h.answer.GetSwapUsed(),
		}
		for what, said := range figures {
			if said != "unknown" {
				return fmt.Errorf("%s reads %q, and nothing measured it", what, said)
			}
		}
		if h.answer.GetUsedBytes() != -1 {
			return fmt.Errorf("the byte count reads %d, and a machine holding nothing is a different answer",
				h.answer.GetUsedBytes())
		}
		return nil
	})

	sc.Step(`^the crew says why it knows nothing$`, func(ctx context.Context) error {
		if said := headroomFrom(ctx).answer.GetFailed(); said == "" {
			return fmt.Errorf("the crew says nothing about why it could not read the machine")
		}
		return nil
	})

	sc.Step(`^the crew still answers everything else$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if _, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{}); err != nil {
			return fmt.Errorf("a crew that could not read its machine stopped serving: %w", err)
		}
		return nil
	})

	sc.Step(`^the largest sandbox is listed first$`, func(ctx context.Context) error {
		boxes := headroomFrom(ctx).answer.GetSandboxes()
		if len(boxes) < 2 {
			return fmt.Errorf("%d sandboxes came back, so there is no order to read", len(boxes))
		}
		if boxes[0].GetHeldBytes() < boxes[1].GetHeldBytes() {
			return fmt.Errorf("the first sandbox holds %s and the second holds %s",
				boxes[0].GetHeld(), boxes[1].GetHeld())
		}
		return nil
	})

	sc.Step(`^each line says what its session is doing and how long since its last task$`,
		func(ctx context.Context) error {
			for _, box := range headroomFrom(ctx).answer.GetSandboxes() {
				if box.GetStatus() == "" {
					return fmt.Errorf("the line for %s does not say what that session is doing",
						box.GetSession())
				}
				if box.GetIdle() == "" {
					return fmt.Errorf("the line for %s does not say how long since its last task",
						box.GetSession())
				}
			}
			return nil
		})

	// The largest sandbox may be the one doing the work, which is why every line says what its
	// session is doing. An operator who stops the largest without reading that stops a task mid
	// sentence.
	sc.Step(`^the listing says the largest one is working$`, func(ctx context.Context) error {
		boxes := headroomFrom(ctx).answer.GetSandboxes()
		if len(boxes) == 0 {
			return fmt.Errorf("no sandboxes came back")
		}
		if boxes[0].GetStatus() != "running" {
			return fmt.Errorf("the largest sandbox reads %q, so nothing warns the operator off it",
				boxes[0].GetStatus())
		}
		return nil
	})

	sc.Step(`^the header carries the figure and the word$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		line, state := console.RoomFrom(ctx, w.client)
		if line == "" {
			return fmt.Errorf("the header carries nothing about the machine")
		}
		if state == "" {
			return fmt.Errorf("the header carries a figure and no word about it")
		}
		view, err := consoleHeader(w, line, state)
		if err != nil {
			return err
		}
		for _, want := range []string{"Memory", strings.Fields(line)[0]} {
			if !strings.Contains(view, want) {
				return fmt.Errorf("the header does not carry %q:\n%s", want, view)
			}
		}
		return nil
	})

	sc.Step(`^the header says the machine is "([^"]*)"$`, func(ctx context.Context, state string) error {
		w := worldFrom(ctx)
		line, got := console.RoomFrom(ctx, w.client)
		if got != state {
			return fmt.Errorf("the header says the machine is %q, want %q", got, state)
		}
		view, err := consoleHeader(w, line, got)
		if err != nil {
			return err
		}
		// Full is drawn so it is readable without reading the number, which is why it is a word and
		// why that word is not written the way the others are.
		want := state
		if state == headroom.StateFull {
			want = strings.ToUpper(state)
		}
		if !strings.Contains(view, want) {
			return fmt.Errorf("the header does not say %q:\n%s", want, view)
		}
		return nil
	})

	sc.Step(`^the crew read the machine once$`, func(ctx context.Context) error {
		if read := headroomFrom(ctx).machine.read; read != 1 {
			return fmt.Errorf("the crew read the machine %d times, and a header redraws every second", read)
		}
		return nil
	})
}

// consoleHeader draws the real header from what the crew answered, so a scenario reads what the
// operator reads rather than a description of it.
func consoleHeader(w *world, line, state string) (string, error) {
	registry, err := console.NewDefaultRegistry(w.client)
	if err != nil {
		return "", err
	}
	info := console.Info{
		Version: "test", Address: "bufconn", Workspace: w.workspaceName, Project: w.projectName,
		Room: line, RoomState: state,
	}
	lines, err := console.HeaderOnly(registry, console.Default, info, 170, 24)
	if err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

// holdSandbox gives the machine a container for a real session of this crew, working or idle, so the
// listing a scenario reads is joined to rows the crew actually holds.
func holdSandbox(ctx context.Context, held int64, working bool) error {
	h, w := headroomFrom(ctx), worldFrom(ctx)
	if h.machine == nil {
		return fmt.Errorf("no machine was given to the crew yet")
	}
	if working {
		// A task still in the model, so the session's row says running while the listing is read.
		w.release = w.runner.hold()
		resp, err := w.client.Dispatch(ctx, &quaycrewv1.DispatchRequest{
			Project: w.projectID, Text: "read the repository", Detach: true,
		})
		if err != nil {
			return err
		}
		w.tasks = append(w.tasks, task{sessionID: resp.GetId(), handle: resp.GetHandle()})
		if err := w.runner.waitForTask(); err != nil {
			return err
		}
	} else if err := w.dispatch(ctx, w.projectID, "", "when is the electricity bill due"); err != nil {
		return err
	}

	current, err := w.lastTask()
	if err != nil {
		return err
	}
	h.machine.sample.Sandboxes = append(h.machine.sample.Sandboxes, headroom.Sandbox{
		Session:   current.sessionID,
		Held:      headroom.Measured(held * megabyte),
		Processor: headroom.MeasuredShare(float64(held) / 100),
	})
	return nil
}

// initializeRoomViewSteps registers the steps for the room view's own line, above its rows. The
// fault they close: the view was one line per sandbox, and an operator could read eighteen rows of
// megabytes and still not know how close the machine was. See issue 457.
func initializeRoomViewSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator opens the room view$`, func(ctx context.Context) error {
		return consoleFrom(ctx).openModelOn(worldFrom(ctx), "room")
	})

	sc.Step(`^the view says "([^"]*)" of "([^"]*)" is held, with "([^"]*)" left$`,
		func(ctx context.Context, held, limit, left string) error {
			line, err := roomLine(ctx)
			if err != nil {
				return err
			}
			for _, want := range []string{held, limit, left} {
				if !strings.Contains(line, want) {
					return fmt.Errorf("the line above the sandboxes does not carry %q:\n%s", want, line)
				}
			}
			return nil
		})

	// Megabytes divided by the size of a sandbox is exactly the arithmetic this line exists to save.
	sc.Step(`^the view says what is left in sandboxes$`, func(ctx context.Context) error {
		line, err := roomLine(ctx)
		if err != nil {
			return err
		}
		if !strings.Contains(line, "more sandboxes") {
			return fmt.Errorf("the line does not say how many more sandboxes fit in what is left:\n%s", line)
		}
		return nil
	})

	// Above them, because it answers the question the operator asks before they read any of the rows:
	// whether a session has to be stopped at all.
	sc.Step(`^that line is above the sandboxes$`, func(ctx context.Context) error {
		view := consoleFrom(ctx).model.View()
		at, sandboxes := -1, -1
		for index, line := range strings.Split(view, "\n") {
			if at < 0 && strings.Contains(line, roomLineMark) {
				at = index
			}
			if sandboxes < 0 && strings.Contains(line, "1201 MiB") {
				sandboxes = index
			}
		}
		if sandboxes < 0 {
			return fmt.Errorf("the sandbox holding 1201 MiB is not listed:\n%s", view)
		}
		if at > sandboxes {
			return fmt.Errorf("the line about the machine is drawn under the sandboxes:\n%s", view)
		}
		return nil
	})

	sc.Step(`^the view says the machine is "([^"]*)"$`, func(ctx context.Context, word string) error {
		line, err := roomLine(ctx)
		if err != nil {
			return err
		}
		if !strings.Contains(line, word) {
			return fmt.Errorf("the line does not say the machine is %q:\n%s", word, line)
		}
		return nil
	})

	sc.Step(`^the view says another sandbox will not fit$`, func(ctx context.Context) error {
		line, err := roomLine(ctx)
		if err != nil {
			return err
		}
		if !strings.Contains(line, "not enough for another sandbox") {
			return fmt.Errorf("the line does not say another sandbox will not fit:\n%s", line)
		}
		return nil
	})
}

// roomLineMark is what the room view's own line says and no other line does. The header carries the
// same figures, so a case looking for a number alone would pass on the header while the view said
// nothing.
const roomLineMark = "containers hold"

// roomLine is the line the room view draws above its rows, read off the drawn screen rather than
// from the console's state, because what is drawn is what the operator has.
func roomLine(ctx context.Context) (string, error) {
	view := consoleFrom(ctx).model.View()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, roomLineMark) {
			return line, nil
		}
	}
	return "", fmt.Errorf("the room view says nothing about the machine it is listing:\n%s", view)
}
