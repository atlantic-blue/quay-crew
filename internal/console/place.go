package console

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Level is one step of a drill: the view that was open, what it was scoped to, and the row that was
// drilled into. It carries both what that row reads as and what a person would type for it, because
// the breadcrumb wants the first and the position line wants the second.
type Level struct {
	Resource string
	Parent   string
	Row      string
	Into     string
	Typed    string
}

// Place is where the console was standing: the levels drilled through, outermost first, and the view
// on top of them. An empty Place is the top of the tree, which is what a console that has never been
// opened resumes to.
type Place struct {
	View   string
	Parent string
	Levels []Level
}

// Empty says the place names nowhere, so the console opens where it opens.
func (p Place) Empty() bool { return p.View == "" }

// PlaceStore is where a place is kept between two runs of the console. Both halves may be nil, in
// which case the console remembers nothing and opens at the top every time.
type PlaceStore struct {
	Load func() (Place, error)
	Save func(Place) error
}

// Remembering tells the console where to keep the place it is standing in, so the next run opens
// there. Without it the console opens at the top, which is the behaviour it had.
func (m Model) Remembering(store PlaceStore) Model {
	m.places = store
	return m
}

// place is where the console is standing right now, in the form that is written down.
func (m Model) place() Place {
	levels := make([]Level, 0, len(m.stack))
	for _, entry := range m.stack {
		levels = append(levels, Level{
			Resource: entry.resource, Parent: entry.parent,
			Row: entry.row, Into: entry.into, Typed: entry.typed,
		})
	}
	return Place{View: m.active.Name, Parent: m.parent, Levels: levels}
}

// rememberCmd writes down where the console is now. A place that cannot be written is dropped: the
// console is usable without one, and an error screen over a console that works is worse than losing
// where somebody was.
func (m Model) rememberCmd() tea.Cmd {
	if m.places.Save == nil {
		return nil
	}
	save, where := m.places.Save, m.place()
	return func() tea.Msg {
		_ = save(where)
		return nil
	}
}

// resumedMsg is a remembered place that was walked back down and found to still be there, as far as
// it goes. It carries the rows of the level it landed on so the first screen is not blank.
type resumedMsg struct {
	stack  []crumbEntry
	active Resource
	parent string
}

// resumeCmd walks a remembered place back down, one level at a time, and stops at the first level
// whose row is no longer there.
//
// It stops rather than failing, because the thing it is resuming to is a workspace or a project
// somebody may have removed since. Opening on a deleted project lists nothing under a heading that
// promises something, which reads as a broken console rather than as a project that is gone.
func resumeCmd(registry *Registry, where Place) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		landed := resumedMsg{}
		// Where the walk has got to: the view it would be standing on if it stopped here, which is the
		// next level down or, once every level is walked, the view that was on top of them.
		standingOn := func(after int) (string, string) {
			if after < len(where.Levels) {
				return where.Levels[after].Resource, where.Levels[after].Parent
			}
			return where.View, where.Parent
		}
		settle := func(after int) resumedMsg {
			name, parent := standingOn(after)
			view, found := registry.Get(name)
			if !found {
				return resumedMsg{}
			}
			landed.active, landed.parent = view, parent
			return landed
		}
		for at, level := range where.Levels {
			resource, found := registry.Get(level.Resource)
			if !found {
				return settle(at)
			}
			rows, err := resource.List(ctx, level.Parent)
			if err != nil || !holdsRow(rows, level.Row) {
				return settle(at)
			}
			landed.stack = append(landed.stack, crumbEntry{
				resource: level.Resource, parent: level.Parent,
				row: level.Row, into: level.Into, typed: level.Typed,
			})
		}
		return settle(len(where.Levels))
	}
}

func holdsRow(rows []Row, id string) bool {
	for _, one := range rows {
		if one.ID == id {
			return true
		}
	}
	return false
}

// applyResumed installs a walked place. A walk that got nowhere leaves the console where it opened,
// which is the top.
func (m Model) applyResumed(msg resumedMsg) (Model, tea.Cmd) {
	if msg.active.Name == "" {
		return m, nil
	}
	if msg.active.Name == m.active.Name && msg.parent == m.parent && len(msg.stack) == 0 {
		// Already where it was: the console opened on this view and the place names the same one.
		return m, nil
	}
	m.stack, m.active, m.parent = msg.stack, msg.active, msg.parent
	m.rows, m.summary, m.selected, m.top, m.filter, m.err = nil, summary{}, 0, 0, "", nil
	return m, tea.Batch(listCmd(m.active, m.parent), m.publishCmd())
}
