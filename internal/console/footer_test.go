package console

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// footerOf is the bottom row of the drawn console, which is where the footer is. Reading the whole
// view would pass on a build string that turned up in a listing, and that is the assertion these
// tests exist to avoid.
func footerOf(model Model) string {
	lines := strings.Split(strings.TrimRight(model.View(), "\n"), "\n")
	return lines[len(lines)-1]
}

// withBuild is the console as it runs in front of an operator, which is the only state where the
// footer has a build to carry.
func withBuild(model Model) Model {
	model.info = Info{Version: "b8712fa"}
	return model
}

// The address a person could type, at each level of the walk down. It is not the breadcrumb: the
// breadcrumb reads a job by its title and this addresses it by the identifier the listing prints.
func TestTheFooterSaysTheAddressAtEveryLevel(t *testing.T) {
	model := withBuild(openedOnTheTree(t, aSystemWithOneOfEverything()))

	// The top is above every workspace, so there is nothing to address yet and the chip says the view.
	if got := model.Position(); got != "" {
		t.Fatalf("the top of the tree has an address %q, and there is nothing above a workspace", got)
	}
	screenSays(t, model, "<workspaces>")

	for _, want := range []struct {
		level   string
		address string
	}{
		{"projects", "acme"},
		{"jobs", "acme/house-bills"},
		{"exec", "acme/house-bills/33333333"},
	} {
		model = walk(t, model, enter())
		if got := model.Position(); got != want.address {
			t.Fatalf("at %s the address is %q, want %q", want.level, got, want.address)
		}
		// On the row itself, not merely in the model: a position the console knows and does not draw
		// is a console that makes a person guess.
		if row := footerOf(model); !strings.Contains(row, want.address) {
			t.Fatalf("the footer at %s does not carry %q:\n%s", want.level, want.address, row)
		}
	}
}

// The way back, read on the footer rather than in the stack. Escape from the deepest level has to
// come all the way home, and the row has to follow it every step.
func TestTheFooterFollowsTheWayBackFromTheDeepestLevel(t *testing.T) {
	model := withBuild(openedOnTheTree(t, aSystemWithOneOfEverything()))
	for range 3 {
		model = walk(t, model, enter())
	}
	if got := model.Position(); got != "acme/house-bills/33333333" {
		t.Fatalf("the walk down did not reach the running work, it reached %q", got)
	}

	for _, want := range []string{"acme/house-bills", "acme", ""} {
		model = walk(t, model, escape())
		if got := model.Position(); got != want {
			t.Fatalf("escape left the address at %q, want %q", got, want)
		}
		row := footerOf(model)
		if want != "" && !strings.Contains(row, want) {
			t.Fatalf("the footer does not carry %q on the way back:\n%s", want, row)
		}
	}
	// Home again, so there is nothing left to go back to and the row stops offering it.
	if row := footerOf(model); strings.Contains(row, "esc to go back") {
		t.Fatalf("the top of the tree still offers a way back:\n%s", row)
	}
}

// The way out is offered wherever there is one, which is the half of "one key each" that is easy to
// leave untested: a console can go down four levels and say nothing about coming back.
func TestEveryLevelBelowTheTopSaysHowToLeave(t *testing.T) {
	model := withBuild(openedOnTheTree(t, aSystemWithOneOfEverything()))
	for _, level := range []string{"projects", "jobs", "exec"} {
		model = walk(t, model, enter())
		if row := footerOf(model); !strings.Contains(row, "esc to go back") {
			t.Fatalf("the %s level does not say how to leave:\n%s", level, row)
		}
	}
}

// What the header carried, on the row that replaced it.
func TestTheFooterCarriesTheBuildTheHelpKeyAndTheProduct(t *testing.T) {
	model := withBuild(openedOnTheTree(t, aSystemWithOneOfEverything()))
	row := footerOf(model)
	for _, want := range []string{"Version:", "b8712fa", "<?> Help", "Krewe"} {
		if !strings.Contains(row, want) {
			t.Fatalf("the footer does not carry %q:\n%s", want, row)
		}
	}
}

// The colour is the terminal's own green rather than a shade of it, so it resolves through whatever
// theme is already there and reads on a light background and a dark one. Asserted as the escape
// sequence the terminal receives, because looking at it proves nothing about anybody else's terminal.
func TestTheRightOfTheFooterIsTheTerminalsOwnGreen(t *testing.T) {
	was := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(was)
	lipgloss.SetColorProfile(termenv.ANSI)

	model := withBuild(openedOnTheTree(t, aSystemWithOneOfEverything()))
	row := footerOf(model)
	// "\x1b[32m" is the basic ANSI green, the one the theme decides. A fixed shade would come through
	// as a 256 colour or a hex triplet here.
	if !strings.Contains(row, "\x1b[32m") {
		t.Fatalf("the right of the footer is not the terminal's own green:\n%q", row)
	}
	for _, fixed := range []string{"\x1b[38;5;", "\x1b[38;2;"} {
		if strings.Contains(row, fixed) {
			t.Fatalf("the footer pins a shade rather than taking the terminal's:\n%q", row)
		}
	}
}

// A terminal that carries no colour at all still gets every word, which is what the repository already
// does everywhere else: lipgloss drops the sequences and the text is untouched.
func TestATerminalWithNoColourStillReadsTheWholeFooter(t *testing.T) {
	was := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(was)
	lipgloss.SetColorProfile(termenv.Ascii)

	model := withBuild(openedOnTheTree(t, aSystemWithOneOfEverything()))
	row := footerOf(model)
	if strings.Contains(row, "\x1b[") {
		t.Fatalf("a terminal with no colour is still sent escape sequences:\n%q", row)
	}
	for _, want := range []string{"<workspaces>", "Version:", "b8712fa", "Krewe"} {
		if !strings.Contains(row, want) {
			t.Fatalf("the footer loses %q without colour:\n%s", want, row)
		}
	}
}

// One row cannot wrap, so something gives. It is never the left: a person who cannot see where they
// are has to guess, and a person who cannot see the build reads it in the help panel.
func TestTheFooterDropsFromTheEndAndNeverGivesUpThePosition(t *testing.T) {
	base := withBuild(openedOnTheTree(t, aSystemWithOneOfEverything()))
	base = walk(t, base, enter())

	for _, tc := range []struct {
		width int
		holds []string
		gone  []string
	}{
		{width: 120, holds: []string{"acme", "Version:", "<?> Help", "Krewe"}},
		{width: 84, holds: []string{"acme", "Version:", "<?> Help", "Krewe"}},
		{width: 66, holds: []string{"acme", "Version:", "<?> Help"}, gone: []string{"Krewe"}},
		{width: 56, holds: []string{"acme", "Version:"}, gone: []string{"Krewe", "<?> Help"}},
		{width: 30, holds: []string{"acme"}, gone: []string{"Krewe", "Help", "Version:"}},
		// The narrowest window the console draws in at all. The position still wins it.
		{width: 1, holds: nil, gone: []string{"Krewe", "Help", "Version:"}},
	} {
		t.Run(fmt.Sprintf("%d columns", tc.width), func(t *testing.T) {
			model := base
			model.width = tc.width
			row := footerOf(model)
			if width := lipgloss.Width(row); width > tc.width {
				t.Fatalf("the footer is %d columns wide in a %d column window, so it wraps:\n%s",
					width, tc.width, row)
			}
			for _, want := range tc.holds {
				if !strings.Contains(row, want) {
					t.Fatalf("the footer dropped %q at %d columns:\n%s", want, tc.width, row)
				}
			}
			for _, unwanted := range tc.gone {
				if strings.Contains(row, unwanted) {
					t.Fatalf("the footer kept %q at %d columns, so something else gave way first:\n%s",
						unwanted, tc.width, row)
				}
			}
		})
	}
}

// The header's three rows are the list's now. This counts them rather than trusting the deletion:
// a wordmark drawn anywhere, by anything, takes them back.
func TestTheConsoleDrawsNothingAboveTheList(t *testing.T) {
	model := withBuild(openedOnTheTree(t, aSystemWithOneOfEverything()))
	lines := strings.Split(model.View(), "\n")

	if !strings.Contains(lines[0], "╭") {
		t.Fatalf("the first line of the console is not the top of the list:\n%s", lines[0])
	}
	// The block letters the wordmark was drawn in. Written out here rather than read from the package,
	// because a test that read the variable would pass whatever it was changed to, including nothing.
	for _, mark := range []string{"██  ▄█▀", "▀▀   ▀▀"} {
		if strings.Contains(model.View(), mark) {
			t.Fatalf("the wordmark is still drawn:\n%s", model.View())
		}
	}
	// The memory figure went with it, and `krewe room` is where it is read now.
	if strings.Contains(model.View(), "Memory") {
		t.Fatalf("the console still carries the machine, which is krewe room's answer:\n%s", model.View())
	}
}

// A control plane too old to say what it is running leaves every other field blank, so the console
// quietly does less and the one thing worth saying is how to fix it. It outranks all three of the
// things the header carried.
func TestAnOlderControlPlaneTakesTheRightOfTheFooter(t *testing.T) {
	model := openedOnTheTree(t, aSystemWithOneOfEverything())
	model.info = Info{Version: "b8712fa", Behind: true}
	row := footerOf(model)

	if !strings.Contains(row, "make upgrade") {
		t.Fatalf("the footer does not say how to fix an older system:\n%s", row)
	}
	if strings.Contains(row, "Krewe") || strings.Contains(row, "b8712fa") {
		t.Fatalf("the build and the product crowd out the warning:\n%s", row)
	}
	if !strings.Contains(row, "<workspaces>") {
		t.Fatalf("the warning took the position with it:\n%s", row)
	}
}

// The bar takes the row while it is open, so the console still names its scope on the panel above.
// A console that says nothing about where it is, exactly when somebody is typing at it, makes them
// guess. The panel names it the way a person reads it; the footer is where the typeable form lives.
func TestTheAddressSurvivesABarOpeningOverTheFooter(t *testing.T) {
	model := withBuild(openedOnTheTree(t, aSystemWithOneOfEverything()))
	model = walk(t, model, enter())
	model = walk(t, model, enter())

	model.mode, model.input = modeFilter, "elec"
	screenSays(t, model, "jobs(house-bills)")
	if row := footerOf(model); !strings.HasPrefix(strings.TrimSpace(stripped(row)), "/elec") {
		t.Fatalf("the filter bar does not own the footer row:\n%s", row)
	}
}

// stripped removes the escape sequences, so an assertion about what a row says is not defeated by how
// it is coloured.
func stripped(line string) string {
	var out strings.Builder
	for at := 0; at < len(line); at++ {
		if line[at] == '\x1b' {
			for at < len(line) && line[at] != 'm' {
				at++
			}
			continue
		}
		out.WriteByte(line[at])
	}
	return out.String()
}

// A job with no session yet cannot be descended into, and the footer must not claim the level it
// refused to open.
func TestTheFooterDoesNotAddressALevelTheConsoleRefusedToOpen(t *testing.T) {
	client := aSystemWithOneOfEverything()
	client.jobs[0].Session = ""
	model := withBuild(openedOnTheTree(t, client))

	model = walk(t, model, enter())
	model = walk(t, model, enter())
	before := model.Position()
	model = walk(t, model, enter())

	if got := model.Position(); got != before {
		t.Fatalf("the address moved to %q on a job with no session, from %q", got, before)
	}
	if row := footerOf(model); !strings.Contains(row, before) {
		t.Fatalf("the footer does not still say %q:\n%s", before, row)
	}
}
