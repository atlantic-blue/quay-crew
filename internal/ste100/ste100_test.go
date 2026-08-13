package ste100

import (
	"strings"
	"testing"
)

func TestCheckPassesCleanProse(t *testing.T) {
	text := "The load test ran for thirteen seconds. It reported a pass, and the number holds.\n\n" +
		"Two write paths still do not stamp it. Those are in the next change."
	if got := Check(text); len(got) != 0 {
		t.Fatalf("clean prose flagged: %v", got)
	}
}

func TestSentenceOverTwentyFiveWordsIsFlagged(t *testing.T) {
	long := "This is a single sentence that keeps going and going with word after word after word " +
		"after word after word after word after word after word until it is well past the cap."
	got := Check(long)
	if !hasRule(got, RuleSentenceLength) {
		t.Fatalf("a %d word sentence was not flagged: %v", len(strings.Fields(long)), got)
	}
}

func TestParagraphOverSixSentencesIsFlagged(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 7; i++ {
		b.WriteString("This is a short sentence. ")
	}
	got := Check(b.String())
	if !hasRule(got, RuleParagraphLength) {
		t.Fatalf("a seven sentence paragraph was not flagged: %v", got)
	}
}

func TestListIsExemptFromParagraphLength(t *testing.T) {
	list := "- one item here.\n- two item here.\n- three item here.\n- four item here.\n" +
		"- five item here.\n- six item here.\n- seven item here."
	got := Check(list)
	if hasRule(got, RuleParagraphLength) {
		t.Fatalf("a seven line bullet list was flagged as a long paragraph: %v", got)
	}
}

func TestEmDashIsFlagged(t *testing.T) {
	got := Check("The fix is simple — do not use dashes.")
	if !hasRule(got, RuleDash) {
		t.Fatalf("an em dash was not flagged: %v", got)
	}
}

func TestEnDashIsFlagged(t *testing.T) {
	got := Check("Read pages 10–20 before the meeting today please.")
	if !hasRule(got, RuleDash) {
		t.Fatalf("an en dash was not flagged: %v", got)
	}
}

func TestSpacedHyphenIsFlagged(t *testing.T) {
	got := Check("The fix is simple - do not use dashes as connectors.")
	if !hasRule(got, RuleDash) {
		t.Fatalf("a spaced hyphen was not flagged: %v", got)
	}
}

func TestListBulletHyphenIsNotFlaggedAsADash(t *testing.T) {
	got := Check("- the first item\n- the second item")
	if hasRule(got, RuleDash) {
		t.Fatalf("a bullet list's leading hyphen was flagged as a dash: %v", got)
	}
}

func TestHyphenInsideCodeSpanIsNotFlagged(t *testing.T) {
	got := Check("Run `git commit --no - verify` is not a real flag, this is only in code: `a - b`.")
	if hasRule(got, RuleDash) {
		t.Fatalf("a hyphen inside a code span was flagged: %v", got)
	}
}

func TestBannedWordIsFlagged(t *testing.T) {
	got := Check("This is a robust and comprehensive solution for the team.")
	if !hasRule(got, RuleBannedWord) {
		t.Fatalf("a banned word was not flagged: %v", got)
	}
}

func TestBlockquoteIsFlagged(t *testing.T) {
	got := Check("Here is a draft:\n\n> The quick brown fox.\n")
	if !hasRule(got, RuleBlockquote) {
		t.Fatalf("a blockquote was not flagged: %v", got)
	}
}

func TestTableIsFlagged(t *testing.T) {
	got := Check("| Name | Value |\n| --- | --- |\n| a | b |\n")
	if !hasRule(got, RuleTable) {
		t.Fatalf("a table was not flagged: %v", got)
	}
}

func TestSentenceEndingPunctuationAloneIsNotATable(t *testing.T) {
	got := Check("Is this the fix? Or should we revert it instead? Both are on the table.")
	if hasRule(got, RuleTable) {
		t.Fatalf("ordinary prose with question marks was flagged as a table: %v", got)
	}
}

func TestCodeBlockContentIsNotChecked(t *testing.T) {
	text := "Here is the change:\n\n```\nfunc f(a - b int) int {\n  return a - b // a comment with more than twenty five words would still not matter because this whole block is code\n}\n```\n"
	got := Check(text)
	if len(got) != 0 {
		t.Fatalf("code block content was checked as prose: %v", got)
	}
}

func TestHeadingIsNotHeldToSentenceCount(t *testing.T) {
	got := Check("# A heading that is not a sentence at all, just a label\n")
	if hasRule(got, RuleParagraphLength) || hasRule(got, RuleSentenceLength) {
		t.Fatalf("a heading was checked as prose: %v", got)
	}
}

func hasRule(violations []Violation, rule string) bool {
	for _, v := range violations {
		if v.Rule == rule {
			return true
		}
	}
	return false
}
