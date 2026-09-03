package main

import (
	"context"
	"io"
	"regexp"
	"strings"
	"testing"
)

// The guard over the whole class, rather than a case per word.
//
// A word removed one at a time gets a test one at a time, and the one that is forgotten is the one
// that matters. These run over the table itself, so a word added to it is covered the moment it is
// added and a word taken out of the table but left in the command switch fails here.

// Every removed word is genuinely gone from the tool, and every one of them refuses.
func TestEveryRemovedWordIsRefused(t *testing.T) {
	client := testClient(t)
	if len(removedCommands) == 0 {
		t.Fatal("the removed table is empty, so this test proves nothing")
	}

	for word := range removedCommands {
		err := run(context.Background(), client, []string{word}, io.Discard, "")
		if err == nil {
			t.Errorf("krewe %s was accepted, so a caller cannot tell it is gone", word)
			continue
		}
		// An unknown command is not a refusal, it is the absence of one: it says the tool does not
		// have the word and nothing about what took its place.
		if strings.Contains(err.Error(), "unknown command") {
			t.Errorf("krewe %s reads as an unknown word rather than a removed one: %v", word, err)
		}
	}
}

// kreweCommand finds the commands a refusal tells the operator to type instead.
var kreweCommand = regexp.MustCompile(`krewe ([a-z]+)`)

// Advice that names another removed word sends the operator around in a circle. This is the failure
// this repository has already had once: the turns refusal named krewe tasks, which is itself gone.
func TestEveryRemovedWordNamesSomethingTheToolStillHas(t *testing.T) {
	client := testClient(t)

	for word, instead := range removedCommands {
		// Some advice is a command with a word after it and some is `krewe` on its own, which is what
		// the panel became. Either is something to type; nothing is not.
		if !strings.Contains(instead, "krewe") {
			t.Errorf("the refusal for krewe %s names nothing to type instead: %s", word, instead)
			continue
		}
		for _, match := range kreweCommand.FindAllStringSubmatch(instead, -1) {
			pointsAt := match[1]
			if _, alsoGone := removedCommands[pointsAt]; alsoGone {
				t.Errorf("krewe %s sends the operator to krewe %s, which is gone too", word, pointsAt)
			}
			// Typed on its own it may well be refused, for want of an argument. What it must never
			// be is a word this tool does not have.
			err := run(context.Background(), client, []string{pointsAt}, io.Discard, "")
			if err != nil && strings.Contains(err.Error(), "unknown command") {
				t.Errorf("krewe %s sends the operator to krewe %s, which this tool does not have",
					word, pointsAt)
			}
		}
	}
}

// The same guard over the flags, because the two tables carry the same kind of advice and the flag
// table is where the advice went stale first.
func TestEveryRemovedFlagNamesSomethingTheToolStillHas(t *testing.T) {
	client := testClient(t)
	if len(removedFlags) == 0 {
		t.Fatal("the removed flag table is empty, so this test proves nothing")
	}

	for flag, instead := range removedFlags {
		for _, match := range kreweCommand.FindAllStringSubmatch(instead, -1) {
			pointsAt := match[1]
			if _, gone := removedCommands[pointsAt]; gone {
				t.Errorf("%s sends the operator to krewe %s, which is gone", flag, pointsAt)
			}
			err := run(context.Background(), client, []string{pointsAt}, io.Discard, "")
			if err != nil && strings.Contains(err.Error(), "unknown command") {
				t.Errorf("%s sends the operator to krewe %s, which this tool does not have", flag, pointsAt)
			}
		}
	}
}

// A flag in both tables is accepted and does nothing, which is the exact defect the removed table
// exists to prevent: the command succeeds and the caller believes the flag took effect. It reads as
// a thing nobody would do until a rename removes a flag from one place and forgets the other.
func TestNoFlagIsBothTakenAndRemoved(t *testing.T) {
	for command, taken := range takenFlags {
		for flag := range taken {
			if _, gone := removedFlags[flag]; gone {
				t.Errorf("krewe %s still takes %s and the removed table says it is gone, so it is accepted "+
					"and quietly ignored", command, flag)
			}
		}
	}
}

// The guard over the whole class of removed flags, so the next flag that goes cannot repeat this. A
// removed flag that is ignored is worse than one that never existed: its value becomes the next
// argument and the command reads as one that worked.
//
// Every flag is driven through `task`, because a flag no command takes is refused the same way
// whichever word carries it, and task is the word with a value after the flag for the refusal to
// swallow. It used to be `job create`, and job is itself refused now, so the refusal under test
// never ran.
func TestEveryRemovedFlagIsRefusedByNameAndNeverSwallowsItsValue(t *testing.T) {
	client := aSystemToWorkIn(t)
	if len(removedFlags) == 0 {
		t.Fatal("the removed flag table is empty, so this test proves nothing")
	}

	// A value nothing else in a refusal could contain, so finding it proves the refusal ate it
	// rather than proving the sentence mentions a material.
	const value = "swallowed-by-the-refusal"
	for flag := range removedFlags {
		err := refused(t, client, "task", flag, value, "read the electricity bill")
		if !strings.Contains(err.Error(), flag) {
			t.Errorf("%s is refused with %q, which does not name the flag", flag, err)
		}
		if !strings.Contains(err.Error(), "is gone") {
			t.Errorf("%s is refused with %q, which does not say the flag is gone", flag, err)
		}
		if strings.Contains(err.Error(), value) {
			t.Errorf("%s took its value with it: %q", flag, err)
		}
	}
}

// A removed word is refused before its flags are, so somebody who typed a whole command that is gone
// is told about the word rather than sent to correct one part of it.
func TestARemovedWordIsRefusedBeforeItsFlagsAre(t *testing.T) {
	client := testClient(t)

	err := refused(t, client, "dispatch", "--project", "default", "remember the number")
	if !strings.Contains(err.Error(), "there is no dispatch command") {
		t.Errorf("a removed word with a removed flag is refused with %q, which blames the flag", err)
	}
	if strings.Contains(err.Error(), "remember the number") {
		t.Errorf("the refusal took the message with it: %q", err)
	}
}

// No removed word is also a live command.
//
// The removed check runs before the command switch, so a word in both tables refuses and the command
// underneath it becomes unreachable with nothing saying so. That is exactly the shape a rename makes:
// the word is added to the removed table and the old case is left in the switch, or the new word is
// added to the switch and the old one is never removed from it. Either way the tool builds, every
// other test passes, and one command quietly stops existing.
func TestNoRemovedWordIsAlsoACommandTheToolStillRuns(t *testing.T) {
	client := testClient(t)
	if len(removedCommands) == 0 {
		t.Fatal("the removed table is empty, so this test proves nothing")
	}

	// A live command is one the usage lists, which is what a caller reads.
	for word := range removedCommands {
		if strings.Contains(usage, "\n  "+word+" ") || strings.Contains(usage, "\n  "+word+"\n") {
			t.Errorf("krewe %s is refused as removed and listed in the usage, so a caller is told to "+
				"type a word the tool then refuses", word)
		}
		// And it is refused rather than run, which is the same defect read from the other end.
		err := run(context.Background(), client, []string{word}, io.Discard, "")
		if err == nil || !strings.Contains(err.Error(), "there is no "+word+" command") {
			t.Errorf("krewe %s answered with %v, want the refusal that says the word is gone", word, err)
		}
	}
}

// The word this rename removed, by name, because the class guard proves every entry refuses and this
// proves the entry says the word to type instead.
//
// It is driven with the flags a person actually had in their fingers, because a refusal that blames
// one of them sends the operator to correct part of a command that is gone whole. And it is driven
// against a real system, so the row count afterwards means something.
func TestTheWorkCommandRefusesAndNamesJob(t *testing.T) {
	client := aSystemToWorkIn(t)

	err := refused(t, client, "work", "create",
		"--title", "read the electricity bill", "--brief", "open it")

	for _, want := range []string{"there is no work command", "krewe job"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("krewe work create is refused with %q, want it to say %q", err, want)
		}
	}
	// The refusal is about the word, not about a flag on it.
	if strings.Contains(err.Error(), "--title") {
		t.Errorf("the refusal blames a flag rather than the word: %q", err)
	}
	// And it took nothing with it, so the declaration cannot read as one that landed.
	if strings.Contains(err.Error(), "read the electricity bill") {
		t.Errorf("the refusal took the title with it: %q", err)
	}
	listed := mustRun(t, client, "job", "list")
	if strings.Contains(listed, "read the electricity bill") {
		t.Errorf("the refused declaration was written anyway: %q", listed)
	}
}

// Every verb the old word carried refuses too, rather than only the noun on its own. Somebody's
// scripts hold `krewe work list`, not `krewe work`.
func TestEveryVerbOfTheWordThatWentIsRefused(t *testing.T) {
	client := testClient(t)

	for _, verb := range []string{"create", "list", "show", "stop"} {
		err := refused(t, client, "work", verb)
		if !strings.Contains(err.Error(), "there is no work command") {
			t.Errorf("krewe work %s is refused with %q, which does not say the word is gone", verb, err)
		}
		if !strings.Contains(err.Error(), "krewe job") {
			t.Errorf("krewe work %s is refused with %q, which names nothing to type instead", verb, err)
		}
	}
}
