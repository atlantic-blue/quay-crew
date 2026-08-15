package main

import (
	"strings"
	"testing"
)

func TestTheMessageAsTypedIsAlwaysSent(t *testing.T) {
	sent := Ask{Prompt: "fix the flaky test"}.UserMessage()

	if !strings.Contains(sent, "<message>\nfix the flaky test\n</message>") {
		t.Errorf("got:\n%s", sent)
	}
}

// An empty checkout must not send the model a page of empty headings to reason about.
func TestASectionWithNothingInItIsLeftOutEntirely(t *testing.T) {
	sent := Ask{Prompt: "hello"}.UserMessage()

	for _, absent := range []string{"<facts>", "<skills>", "<rules>"} {
		if strings.Contains(sent, absent) {
			t.Errorf("%s was sent with nothing in it:\n%s", absent, sent)
		}
	}
}

func TestEveryFactThatIsKnownIsSent(t *testing.T) {
	sent := Ask{
		Prompt: "fix it",
		Facts: Facts{
			Cwd:    "/home/agent/workspace",
			Repo:   "atlantic-blue/quay-crew",
			Branch: "gun-1620-fix-the-thing",
			Ticket: "GUN-1620",
			State:  "stage: building",
		},
	}.UserMessage()

	for _, want := range []string{
		"working directory: /home/agent/workspace",
		"repository: atlantic-blue/quay-crew",
		"branch: gun-1620-fix-the-thing",
		"ticket: GUN-1620",
		"ticket state:\nstage: building",
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("%q was not sent:\n%s", want, sent)
		}
	}
}

func TestAFactThatIsNotKnownIsNotSentAsAnEmptyLine(t *testing.T) {
	sent := Ask{Prompt: "fix it", Facts: Facts{Cwd: "/home/agent"}}.UserMessage()

	if strings.Contains(sent, "repository:") || strings.Contains(sent, "branch:") {
		t.Errorf("an empty fact was sent:\n%s", sent)
	}
	if !strings.Contains(sent, "working directory: /home/agent") {
		t.Errorf("the one known fact was not sent:\n%s", sent)
	}
}

func TestSkillsRulesAndDocumentsEachGetTheirOwnSection(t *testing.T) {
	sent := Ask{
		Prompt: "fix it",
		Skills: []Skill{{Name: "git", Description: "how work is done"}},
		Rules:  []string{"1. Never commit without permission."},
		Docs:   []Document{{Label: "CLAUDE", Text: "the rules"}},
	}.UserMessage()

	for _, want := range []string{
		"<skills>\ngit: how work is done\n</skills>",
		"<rules>\n1. Never commit without permission.\n</rules>",
		"<CLAUDE>\nthe rules\n</CLAUDE>",
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("%q was not sent:\n%s", want, sent)
		}
	}
}

func TestTheSystemPromptTellsTheModelHowToStayQuiet(t *testing.T) {
	system := SystemPrompt()

	if !strings.Contains(system, Pass) {
		t.Error("the model is never told the word that means a message needs no analysis")
	}
	for _, field := range []string{"goal:", "target:", "unclear:", "skills:", "rules:", "first move:"} {
		if !strings.Contains(system, field) {
			t.Errorf("the model is never asked for %q", field)
		}
	}
}
