package features_test

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cucumber/godog"
)

// The console's keys, driven as keys. Every step here presses what an operator presses and reads the
// console back, so a key that routes correctly and leaves the operator somewhere useless still fails.
//
// The console's own table tests cover which key routes where. What these add is the real control
// plane underneath: the rows a jump lands on are sessions the crew actually made.

func initializeKeysSteps(sc *godog.ScenarioContext) {
	// The crew is handed over the way the real console is built, because the key that makes something
	// asks the crew rather than a view, and a console without one refuses it.
	sc.Step(`^the operator is at the console$`, func(ctx context.Context) error {
		c, w := consoleFrom(ctx), worldFrom(ctx)
		if err := c.openModel(w); err != nil {
			return err
		}
		c.model = c.model.WithClient(w.client)
		return nil
	})

	// The command bar is how an operator reaches another view, so this switches through it rather
	// than opening the console on the view and calling that the same thing.
	sc.Step(`^the operator is at the console on the "([^"]*)" view$`, func(ctx context.Context, view string) error {
		c := consoleFrom(ctx)
		if err := c.openModel(worldFrom(ctx)); err != nil {
			return err
		}
		if err := pressKeys(c, ":"+view); err != nil {
			return err
		}
		return c.press(tea.KeyMsg{Type: tea.KeyEnter})
	})

	sc.Step(`^the operator presses "([^"]*)" in the console$`, func(ctx context.Context, keys string) error {
		return pressKeys(consoleFrom(ctx), keys)
	})

	// Filtering and then clearing it, which is the sequence the keys for the next and previous match
	// exist for: every row is back on screen and the word is still what the operator is looking for.
	sc.Step(`^the operator filters for "([^"]*)" and clears the filter$`, func(ctx context.Context, word string) error {
		c := consoleFrom(ctx)
		if err := pressKeys(c, "/"+word); err != nil {
			return err
		}
		return c.press(tea.KeyMsg{Type: tea.KeyEsc})
	})

	sc.Step(`^the console is on the "([^"]*)" view$`, func(ctx context.Context, view string) error {
		drawn := consoleFrom(ctx).model.View()
		if !strings.Contains(drawn, "<"+view+">") {
			return fmt.Errorf("the console is not on the %s view:\n%s", view, drawn)
		}
		return nil
	})

	// The whole point of a key that moved: it says what to press now. A refusal that does not name
	// the new spelling is a dead end with an apology on it.
	sc.Step(`^the console says to press "([^"]*)"$`, func(ctx context.Context, key string) error {
		said := consoleFrom(ctx).model.Reported()
		if said == nil {
			return fmt.Errorf("the console reports nothing, so the key it used to answer to did nothing at all")
		}
		if !strings.Contains(said.Error(), key) {
			return fmt.Errorf("the console says %q, want it to name %q", said, key)
		}
		return nil
	})

	sc.Step(`^the console is asking what to make$`, func(ctx context.Context) error {
		drawn := consoleFrom(ctx).model.View()
		if !strings.Contains(drawn, "make what") {
			return fmt.Errorf("the console is not asking what to make:\n%s", drawn)
		}
		return nil
	})

	sc.Step(`^the console lists that session$`, func(ctx context.Context) error {
		return listedRows(ctx, 1)
	})

	sc.Step(`^the console lists both workspaces again$`, func(ctx context.Context) error {
		return listedRows(ctx, 2)
	})

	sc.Step(`^the cursor is on the (first|last) row$`, func(ctx context.Context, which string) error {
		rows := consoleFrom(ctx).model.Listed()
		at := 0
		if which == "last" {
			at = len(rows) - 1
		}
		return cursorOn(ctx, at)
	})

	sc.Step(`^the cursor is on row (\d+)$`, func(ctx context.Context, number int) error {
		return cursorOn(ctx, number-1)
	})

	sc.Step(`^the cursor is on the row named "([^"]*)"$`, func(ctx context.Context, name string) error {
		row, found := consoleFrom(ctx).model.Selected()
		if !found {
			return fmt.Errorf("nothing is under the cursor")
		}
		if row.Name() != name {
			return fmt.Errorf("the cursor is on %q, want %q", row.Name(), name)
		}
		return nil
	})
}

// pressKeys sends a sequence one key at a time, the way a keyboard does. A named key is one press;
// anything else is its runes in order, so "gg" is two presses of g and "2j" is a count and a move.
func pressKeys(c *consoleWorld, keys string) error {
	if named, isNamed := namedKeys[keys]; isNamed {
		return c.press(tea.KeyMsg{Type: named})
	}
	for _, key := range keys {
		if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}}); err != nil {
			return err
		}
	}
	return nil
}

// namedKeys are the keys that are not a rune. They are spelled in a scenario the way the console's
// own help spells them.
var namedKeys = map[string]tea.KeyType{
	"esc":       tea.KeyEsc,
	"enter":     tea.KeyEnter,
	"backspace": tea.KeyBackspace,
	"tab":       tea.KeyTab,
	"ctrl+d":    tea.KeyCtrlD,
	"ctrl+u":    tea.KeyCtrlU,
	"ctrl+f":    tea.KeyCtrlF,
	"ctrl+b":    tea.KeyCtrlB,
}

// listedRows says how many rows the console is showing, which is what it is showing after the filter
// rather than what the store holds.
func listedRows(ctx context.Context, want int) error {
	if got := len(consoleFrom(ctx).model.Listed()); got != want {
		return fmt.Errorf("the console lists %d rows, want %d", got, want)
	}
	return nil
}

// cursorOn says the cursor is on the row at this position, by the identity of the row rather than by
// the number, so a listing that came back in a different order fails rather than passing by counting.
func cursorOn(ctx context.Context, at int) error {
	c := consoleFrom(ctx)
	rows := c.model.Listed()
	if at < 0 || at >= len(rows) {
		return fmt.Errorf("there is no row %d in a listing of %d", at+1, len(rows))
	}
	row, found := c.model.Selected()
	if !found {
		return fmt.Errorf("nothing is under the cursor")
	}
	if row.ID != rows[at].ID {
		return fmt.Errorf("the cursor is on %q, want row %d, which is %q", row.ID, at+1, rows[at].ID)
	}
	return nil
}
