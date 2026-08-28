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
