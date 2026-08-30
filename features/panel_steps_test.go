package features_test

import (
	"context"
	"fmt"
	"sort"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/panel"
	"github.com/cucumber/godog"
)

// The panel takes over the terminal, so these scenarios assert on the commands it would run rather
// than running them, the same way the attach scenarios do. What is worth saying out here is that the
// conversation in the right half is a real one from the control plane.

type panelWorld struct {
	commands [][]string
	err      error
}

type panelKey struct{}

func panelFrom(ctx context.Context) *panelWorld {
	p, _ := ctx.Value(panelKey{}).(*panelWorld)
	return p
}

func (p *panelWorld) line() string {
	out := make([]string, 0, len(p.commands))
	for _, argv := range p.commands {
		out = append(out, strings.Join(argv, " "))
	}
	return strings.Join(out, "\n")
}

func initializePanelSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, panelKey{}, &panelWorld{}), nil
	})

	sc.Step(`^a session started by dispatching "([^"]*)" on a new session$`, func(ctx context.Context, text string) error {
		w := worldFrom(ctx)
		if err := w.dispatch(ctx, w.projectID, "", text); err != nil {
			return err
		}
		return w.lastErr
	})

	sc.Step(`^the operator opens the panel$`, func(ctx context.Context) error {
		w, p := worldFrom(ctx), panelFrom(ctx)

		listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
		if err != nil {
			return err
		}
		newest, found := newestOpenable(listed.GetSessions())
		if !found {
			p.err = fmt.Errorf("there is no conversation to put beside the console yet: " +
				"start one with `krewe task \"hello\"`, then open the panel again")
			return nil
		}
		p.commands, p.err = panel.Layout{
			Header: []string{"krewe", "header"}, HeaderRows: 10,
			Left:  []string{"krewe", "console"},
			Right: []string{"krewe", "attach", newest},
		}.Commands(panel.Terminal{})
		return nil
	})

	sc.Step(`^the panel puts the console in one half and that conversation in the other$`, func(ctx context.Context) error {
		w, p := worldFrom(ctx), panelFrom(ctx)
		if p.err != nil {
			return fmt.Errorf("the panel refused: %w", p.err)
		}
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		got := p.line()
		if strings.Count(got, "split-window") != 2 {
			return fmt.Errorf("the panel is not a header over two halves:\n%s", got)
		}
		if !strings.Contains(got, "attach "+current.sessionID) {
			return fmt.Errorf("the panel does not open the session the control plane made:\n%s", got)
		}
		return nil
	})

	sc.Step(`^each half is 50% of the width$`, func(ctx context.Context) error {
		got := panelFrom(ctx).line()
		if !strings.Contains(got, "-l 50%") {
			return fmt.Errorf("the halves are not equal:\n%s", got)
		}
		// Stacked would give the console half its rows instead of half its width, which is the
		// layout that was not chosen.
		if !strings.Contains(got, "split-window -h") {
			return fmt.Errorf("the split is not side by side:\n%s", got)
		}
		return nil
	})

	sc.Step(`^the console has the keyboard$`, func(ctx context.Context) error {
		got := panelFrom(ctx).line()
		if !strings.Contains(got, "select-pane") || !strings.Contains(got, ".1") {
			return fmt.Errorf("the panel does not put the keyboard in the console:\n%s", got)
		}
		return nil
	})

	// The header is a pane of its own above the two halves, because a tmux pane is a rectangle and one
	// reaching across both cannot belong to either.
	sc.Step(`^the header spans the whole width above both halves$`, func(ctx context.Context) error {
		got := panelFrom(ctx).line()
		splits := make([]string, 0, 2)
		for _, line := range strings.Split(got, "\n") {
			if strings.Contains(line, "split-window") {
				splits = append(splits, line)
			}
		}
		if len(splits) != 2 {
			return fmt.Errorf("want a full width cut then a side by side one:\n%s", got)
		}
		if !strings.Contains(splits[0], " -v ") {
			return fmt.Errorf("the header does not span both halves:\n%s", splits[0])
		}
		if !strings.Contains(got, "resize-pane") {
			return fmt.Errorf("the header is not given its own rows:\n%s", got)
		}
		return nil
	})

	sc.Step(`^the panel opens the newer conversation$`, func(ctx context.Context) error {
		w, p := worldFrom(ctx), panelFrom(ctx)
		if p.err != nil {
			return fmt.Errorf("the panel refused: %w", p.err)
		}
		newer, err := w.lastTask()
		if err != nil {
			return err
		}
		if !strings.Contains(p.line(), "attach "+newer.sessionID) {
			return fmt.Errorf("the panel does not open the conversation last spoken to:\n%s", p.line())
		}
		return nil
	})

	sc.Step(`^the panel says there is no conversation to put beside the console$`, func(ctx context.Context) error {
		p := panelFrom(ctx)
		if p.err == nil {
			return fmt.Errorf("the panel opened with nothing to put in it:\n%s", p.line())
		}
		if !strings.Contains(p.err.Error(), "no conversation") {
			return fmt.Errorf("the refusal is %q, and does not say what is missing", p.err)
		}
		return nil
	})

	sc.Step(`^it says how to start one$`, func(ctx context.Context) error {
		p := panelFrom(ctx)
		if p.err == nil {
			return fmt.Errorf("nothing was refused")
		}
		if !strings.Contains(p.err.Error(), "krewe task") {
			return fmt.Errorf("the refusal is %q, and never says what to type", p.err)
		}
		return nil
	})
}

// newestOpenable is the conversation last spoken to, skipping the ones that cannot be attached to at
// all. It mirrors what the command does, which is the part worth stating here: the right half holds a
// real session from the control plane rather than a name a scenario made up.
func newestOpenable(sessions []*quaycrewv1.Session) (string, bool) {
	live := make([]*quaycrewv1.Session, 0, len(sessions))
	for _, session := range sessions {
		if session.GetModelSessionId() != "" && session.GetArchivedAt() == nil {
			live = append(live, session)
		}
	}
	if len(live) == 0 {
		return "", false
	}
	sort.Slice(live, func(i, j int) bool {
		return live[i].GetUpdatedAt().AsTime().After(live[j].GetUpdatedAt().AsTime())
	})
	return live[0].GetId(), true
}
