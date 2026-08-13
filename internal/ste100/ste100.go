// Package ste100 checks prose against the parts of Simplified Technical English (ASD-STE100) and this
// crew's own writing rules that a regular expression can decide correctly every time: sentence and
// paragraph length, a dash used as punctuation, a markdown blockquote or table, and the fixed list of
// corporate words VOICE.md section 6 calls "the robot tells".
//
// Acronyms and passive voice are not checked here. Telling a real acronym from a legitimate short form
// ("URL" against "AUT"), or a passive sentence that should be active from one that needs to be passive,
// takes judgement a pattern match does not have. A hook built on this package blocks a real answer on a
// false positive, so the checks stop at what stays correct on every input.
package ste100

import (
	"fmt"
	"regexp"
	"strings"
)

// Violation is one place text fails a rule, carrying enough of the text to act on without rereading
// the whole response.
type Violation struct {
	// Rule names which check failed, so a caller can group or count by it.
	Rule string
	// Detail is what specifically failed and why, worded for whoever has to fix it.
	Detail string
}

// String is a Violation on one line, which is what a Stop hook's reason field wants: something the
// model reads once and acts on.
func (v Violation) String() string {
	return fmt.Sprintf("%s: %s", v.Rule, v.Detail)
}

// Rule names, so a caller filtering or counting by rule is not matching on a string it has to keep in
// sync with the text below by hand.
const (
	RuleSentenceLength  = "sentence length"
	RuleParagraphLength = "paragraph length"
	RuleDash            = "dash"
	RuleBlockquote      = "blockquote"
	RuleTable           = "table"
	RuleBannedWord      = "banned word"
)

// maxSentenceWords is rule 53's cap for a description sentence, which is the longer of its two caps
// (twenty for an instruction, twenty five for a description). Telling the two apart needs the kind of
// judgement this package does not have, so every sentence is held to the looser cap rather than the
// tighter one flagging ordinary description prose.
const maxSentenceWords = 25

// maxParagraphSentences is rule 53's cap on a prose paragraph.
const maxParagraphSentences = 6

// bannedWords is VOICE.md section 6's list of words and phrases that read as generated rather than
// written, checked case insensitively as substrings. A substring match over a whole word match is
// deliberate: "empowerment" is exactly as generated as "empower".
var bannedWords = []string{
	"comprehensive", "robust", "seamless", "leverage", "utilise", "utilize", "delve", "landscape",
	"journey", "realm", "game changer", "unlock", "supercharge", "elevate", "empower",
	"best in class", "best-in-class", "at scale", "powerful", "cutting edge", "cutting-edge",
	"revolutionise", "revolutionize", "testament to",
	"it's important to note", "it is important to note",
	"it's worth mentioning", "it is worth mentioning",
}

var (
	fencedCodeBlock = regexp.MustCompile("(?s)```.*?```")
	inlineCode      = regexp.MustCompile("`[^`\n]+`")
	sentenceEnd     = regexp.MustCompile(`[.!?]+(\s+|$)`)
	blockquoteLine  = regexp.MustCompile(`(?m)^[ \t]*>`)
	listMarker      = regexp.MustCompile(`^(\s*)([-*]|\d+\.)\s+`)
	// tableSeparator is the row a markdown table's header is followed by, for example
	// "|---|---|" or "| :-- | --: |". Matching on this rather than on a line with two pipes in it is
	// what keeps a sentence that happens to use the word "or" between two vertical bars from counting
	// as a table.
	tableSeparator = regexp.MustCompile(`(?m)^[ \t]*\|?[ \t]*:?-{2,}:?[ \t]*(\|[ \t]*:?-{2,}:?[ \t]*)+\|?[ \t]*$`)
)

// Check runs every rule against text and returns what failed, in a fixed order so the same input
// always produces the same report.
func Check(text string) []Violation {
	var out []Violation
	out = append(out, checkBlockquotes(text)...)
	out = append(out, checkTables(text)...)

	prose := stripCode(text)
	out = append(out, checkSentences(prose)...)
	out = append(out, checkDashes(prose)...)
	out = append(out, checkBannedWords(prose)...)
	return out
}

// stripCode removes fenced code blocks and inline code spans before the checks that would otherwise
// flag them: a kebab case flag or branch name is a legitimate hyphen, and a code sample can run past
// twenty five words with nothing wrong in it.
func stripCode(text string) string {
	text = fencedCodeBlock.ReplaceAllString(text, "")
	text = inlineCode.ReplaceAllString(text, "")
	return text
}

// checkBlockquotes flags any line starting with >, which rule 52 bans outright, everywhere, with no
// exceptions.
func checkBlockquotes(text string) []Violation {
	if !blockquoteLine.MatchString(text) {
		return nil
	}
	return []Violation{{
		Rule:   RuleBlockquote,
		Detail: "a line starts with >, and rule 52 bans markdown blockquotes everywhere: use plain prose with a bold label instead",
	}}
}

// checkTables flags a markdown table separator row. VOICE.md's no tables preference wants a plain
// list instead, one line per item.
func checkTables(text string) []Violation {
	if !tableSeparator.MatchString(text) {
		return nil
	}
	return []Violation{{
		Rule:   RuleTable,
		Detail: "a markdown table separator row: use one line per item instead, per the no tables preference",
	}}
}

// checkDashes flags an em dash, an en dash, or a hyphen with a space on each side used as a
// connector, all of which rule 24 bans as punctuation. A list marker's leading hyphen is stripped
// first, so "- like this" at the start of a bullet is not mistaken for one.
func checkDashes(prose string) []Violation {
	if strings.ContainsRune(prose, '—') || strings.ContainsRune(prose, '–') {
		return []Violation{{
			Rule:   RuleDash,
			Detail: "an em dash or en dash: rule 24 bans both as punctuation, reword with a comma, colon, parentheses, or two sentences",
		}}
	}
	for _, line := range strings.Split(prose, "\n") {
		if strings.Contains(listMarker.ReplaceAllString(line, "$1"), " - ") {
			return []Violation{{
				Rule:   RuleDash,
				Detail: `a hyphen with a space on each side, used as a dash: rule 24 bans that too, reword instead of using " - "`,
			}}
		}
	}
	return nil
}

// checkBannedWords flags the first word from the list that appears, rather than every occurrence: one
// instance already says the paragraph needs a rewrite, and a report naming all of them reads as noise
// once the first has made the point.
func checkBannedWords(prose string) []Violation {
	lower := strings.ToLower(prose)
	for _, word := range bannedWords {
		if strings.Contains(lower, word) {
			return []Violation{{
				Rule:   RuleBannedWord,
				Detail: fmt.Sprintf("%q is on the list of words that read as generated rather than written", word),
			}}
		}
	}
	return nil
}

// checkSentences flags a sentence over rule 53's word cap, and a prose paragraph over its sentence
// cap. A block of lines that looks like a list, each line starting with a bullet or number marker, is
// exempted from the paragraph cap: rule 53 caps a paragraph of prose, not the count of items in a list
// that exists to be scanned rather than read as sentences.
func checkSentences(prose string) []Violation {
	var out []Violation
	for _, paragraph := range paragraphs(prose) {
		sentences := splitSentences(paragraph)
		if len(sentences) > maxParagraphSentences && !looksLikeList(paragraph) && !looksLikeHeading(paragraph) {
			out = append(out, Violation{
				Rule: RuleParagraphLength,
				Detail: fmt.Sprintf("%d sentences in one paragraph, rule 53 caps a paragraph at %d",
					len(sentences), maxParagraphSentences),
			})
		}
		for _, sentence := range sentences {
			words := strings.Fields(sentence)
			if len(words) > maxSentenceWords {
				out = append(out, Violation{
					Rule: RuleSentenceLength,
					Detail: fmt.Sprintf("%d words, over rule 53's cap of %d: %q",
						len(words), maxSentenceWords, truncate(sentence, 80)),
				})
			}
		}
	}
	return out
}

// paragraphs splits text on blank lines, which is where a paragraph ends in both markdown and in
// rule 53's sense of the word.
func paragraphs(text string) []string {
	var out []string
	for _, block := range regexp.MustCompile(`\n\s*\n`).Split(text, -1) {
		block = strings.TrimSpace(block)
		if block != "" {
			out = append(out, block)
		}
	}
	return out
}

// splitSentences breaks a paragraph on sentence ending punctuation. It is a heuristic rather than a
// parser: an abbreviation or a decimal number can split early, which undercounts a sentence's word
// total and so only ever misses a violation, never invents one.
func splitSentences(paragraph string) []string {
	var out []string
	for _, sentence := range sentenceEnd.Split(paragraph, -1) {
		sentence = strings.TrimSpace(sentence)
		if sentence != "" {
			out = append(out, sentence)
		}
	}
	return out
}

// looksLikeList says whether every non blank line of a paragraph opens with a list marker, which is
// what tells a bullet list apart from a run of ordinary sentences that happen to sit in one block.
func looksLikeList(paragraph string) bool {
	lines := nonBlankLines(paragraph)
	if len(lines) == 0 {
		return false
	}
	for _, line := range lines {
		if !listMarker.MatchString(line) {
			return false
		}
	}
	return true
}

// looksLikeHeading says whether a paragraph is a single markdown heading line, which is a label, not
// a sentence, and should not be held to a sentence's word count.
func looksLikeHeading(paragraph string) bool {
	lines := nonBlankLines(paragraph)
	return len(lines) == 1 && strings.HasPrefix(strings.TrimSpace(lines[0]), "#")
}

func nonBlankLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// truncate cuts a string to a length, so a violation naming an eighty word run on sentence does not
// carry the whole thing into a hook's reason field.
func truncate(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max] + "..."
}
