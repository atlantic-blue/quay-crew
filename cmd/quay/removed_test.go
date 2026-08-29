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
			t.Errorf("quay %s was accepted, so a caller cannot tell it is gone", word)
			continue
		}
		// An unknown command is not a refusal, it is the absence of one: it says the tool does not
		// have the word and nothing about what took its place.
		if strings.Contains(err.Error(), "unknown command") {
			t.Errorf("quay %s reads as an unknown word rather than a removed one: %v", word, err)
		}
	}
}

// quayCommand finds the commands a refusal tells the operator to type instead.
var quayCommand = regexp.MustCompile(`quay ([a-z]+)`)

// Advice that names another removed word sends the operator around in a circle. This is the failure
// this repository has already had once: the turns refusal named quay tasks, which is itself gone.
func TestEveryRemovedWordNamesSomethingTheToolStillHas(t *testing.T) {
	client := testClient(t)

	for word, instead := range removedCommands {
		// Some advice is a command with a word after it and some is `quay` on its own, which is what
		// the panel became. Either is something to type; nothing is not.
		if !strings.Contains(instead, "quay") {
			t.Errorf("the refusal for quay %s names nothing to type instead: %s", word, instead)
			continue
		}
		for _, match := range quayCommand.FindAllStringSubmatch(instead, -1) {
			pointsAt := match[1]
			if _, alsoGone := removedCommands[pointsAt]; alsoGone {
				t.Errorf("quay %s sends the operator to quay %s, which is gone too", word, pointsAt)
			}
			// Typed on its own it may well be refused, for want of an argument. What it must never
			// be is a word this tool does not have.
			err := run(context.Background(), client, []string{pointsAt}, io.Discard, "")
			if err != nil && strings.Contains(err.Error(), "unknown command") {
				t.Errorf("quay %s sends the operator to quay %s, which this tool does not have",
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
		for _, match := range quayCommand.FindAllStringSubmatch(instead, -1) {
			pointsAt := match[1]
			if _, gone := removedCommands[pointsAt]; gone {
				t.Errorf("%s sends the operator to quay %s, which is gone", flag, pointsAt)
			}
			err := run(context.Background(), client, []string{pointsAt}, io.Discard, "")
			if err != nil && strings.Contains(err.Error(), "unknown command") {
				t.Errorf("%s sends the operator to quay %s, which this tool does not have", flag, pointsAt)
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
				t.Errorf("quay %s still takes %s and the removed table says it is gone, so it is accepted "+
					"and quietly ignored", command, flag)
			}
		}
	}
}

// The guard over the whole class of removed flags, so the next flag that goes cannot repeat this. A
// removed flag that is ignored is worse than one that never existed: its value becomes the next
// argument and the command reads as one that worked.
//
// Every flag is driven through `work create`, because a flag no command takes is refused the same
// way whichever word carries it, and `work create` is the one word with a value after the flag for
// the refusal to swallow.
func TestEveryRemovedFlagIsRefusedByNameAndNeverSwallowsItsValue(t *testing.T) {
	client := aCrewToWorkIn(t)
	if len(removedFlags) == 0 {
		t.Fatal("the removed flag table is empty, so this test proves nothing")
	}

	// A value nothing else in a refusal could contain, so finding it proves the refusal ate it
	// rather than proving the sentence mentions a material.
	const value = "swallowed-by-the-refusal"
	for flag := range removedFlags {
		err := refused(t, client, "work", "create",
			flag, value, "--title", "read the electricity bill", "--brief", "open it")
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

// The flag this rename removed, by name, because the class guard proves every entry refuses and this
// proves the entry says the word to type instead. A caller with --hands in their fingers, their
// scripts and their notes gets sent to --requires and nowhere else.
func TestTheHandsFlagRefusesAndNamesRequires(t *testing.T) {
	client := aCrewToWorkIn(t)

	err := refused(t, client, "work", "create",
		"--title", "read the electricity bill", "--brief", "open it", "--hands", "context")

	for _, want := range []string{"--hands is gone", "--requires"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("--hands is refused with %q, want it to say %q", err, want)
		}
	}
	// And no row was written, so a caller who reads the listing after a refusal finds nothing.
	listed := mustRun(t, client, "work", "list")
	if strings.Contains(listed, "read the electricity bill") {
		t.Errorf("the refused declaration was written anyway: %q", listed)
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
