package main

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/manual"
)

// The guard is over the whole old surface, rather than over the words somebody remembered.
//
// A refusal written as a list of cases refuses the cases on the list. The word left off it is the one
// an operator types, and it comes back as "command not found", which reads as a broken install. So
// these read the command list the tool actually ships and hold the refusal to all of it.

// theOldCommands is every word the tool lists, read from the manual the tool prints rather than from a
// copy of it. The list moves as the tool moves, and a test holding a remembered copy would go stale.
func theOldCommands(t *testing.T) []string {
	t.Helper()

	var words []string
	listing := false
	for _, line := range strings.Split(manual.Commands, "\n") {
		if strings.HasPrefix(line, "commands:") {
			listing = true
			continue
		}
		// The command column starts each entry at a two space indent and a continuation line is
		// indented further, so the first word of a two space line is a command and nothing else is.
		if !listing || !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
			continue
		}
		if fields := strings.Fields(line); len(fields) > 0 {
			words = append(words, fields[0])
		}
	}
	if len(words) == 0 {
		t.Fatal("the manual listed no commands at all, so this test would pass on anything")
	}
	return words
}

// Every word the tool has is refused under the old name, and the refusal names the new one.
func TestTheOldNameRefusesEveryCommandTheToolHas(t *testing.T) {
	for _, word := range theOldCommands(t) {
		said := refusal([]string{word})
		if !strings.Contains(said, "krewe") {
			t.Errorf("quay %s answers %q, which never says to type krewe", word, said)
		}
		if !strings.Contains(said, "krewe "+word) {
			t.Errorf("quay %s answers %q, and it does not say to type krewe %s", word, said, word)
		}
	}
}

// The shapes that are not a command, which is where a guard written case by case lets something
// through: the tool on its own opens the console, and a flag is not a word in any list.
func TestTheOldNameRefusesWhateverIsTypedAfterIt(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{},
		{"--help"},
		{"-h"},
		{"sideways"},
		{""},
		{"task", "when is the electricity bill due"},
	} {
		said := refusal(args)
		if !strings.Contains(said, "krewe") {
			t.Errorf("quay %s answers %q, which never says to type krewe", strings.Join(args, " "), said)
		}
	}
}

// The tool on its own opens the console, so the old name on its own has to name the new name on its
// own. Naming a command that was never typed sends the operator somewhere they were not going.
func TestTheOldNameOnItsOwnNamesTheNewNameOnItsOwn(t *testing.T) {
	said := refusal(nil)
	if !strings.Contains(said, `"krewe"`) {
		t.Errorf("quay on its own answers %q, want it to say to type krewe on its own", said)
	}
}

// The advice must never be the word that is being refused. This is the failure this repository has
// already had once, one layer down: a refusal named a command that was itself gone.
func TestTheRefusalNeverSendsTheOperatorBackToTheOldName(t *testing.T) {
	for _, word := range append(theOldCommands(t), "") {
		said := refusal([]string{word})
		for _, sent := range strings.Fields(said) {
			if strings.Trim(sent, `"',.`) == "quay" && !strings.HasPrefix(said, "quay is called") {
				t.Errorf("quay %s answers %q, which tells the operator to type quay again", word, said)
			}
		}
	}
}
