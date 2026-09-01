package main

import (
	"fmt"
	"strings"
)

// The rules. Each one is a thing the standard says that a program can measure exactly, and nothing
// here is a guess at the part of the standard that needs a person.
//
// ASD-STE100 is about nine hundred approved words, each with one meaning, and fifty three writing
// rules. The dictionary is licensed rather than published as a list, so a gate that carried its own
// list would enforce somebody's guess at it. Idiom and metaphor pass every pattern that can be
// written: "fishing in that pond" is six ordinary words. Those two stay in the brief, where a person
// reads them, and this file holds the rest.

// MaxWords is the longest sentence this gate allows.
//
// The standard is twenty words for an instruction and twenty five for a description. Telling those
// two apart is a reading of what the sentence is for, which is exactly the kind of judgement a hook
// must not make, so the wider of the two is the one that is checked and the narrower one stays in
// the brief.
const MaxWords = 25

// MaxSentences is the longest paragraph this gate allows, in sentences.
const MaxSentences = 6

// A Finding is one refusal: where it is, what is wrong, and what to do about it.
//
// The last part is the one that matters. "Too long" is not something a writer can act on. "This
// sentence is 34 words, split it" is, and a session told only that something is wrong writes the
// same sentence again in different words until its budget runs out.
type Finding struct {
	// Where is the file or the argument the prose came from.
	Where string
	// Line is the line of it the paragraph starts on, counting from one.
	Line int
	// Rule is the short name of the rule, so a listing can be counted by rule.
	Rule string
	// What is wrong, in one sentence.
	What string
	// Do is what to do about it, in one sentence.
	Do string
	// Quote is the prose being refused, so the writer does not have to go and find it.
	Quote string
}

func (f Finding) String() string {
	place := f.Where
	if f.Line > 0 {
		place = fmt.Sprintf("%s line %d", f.Where, f.Line)
	}
	return fmt.Sprintf("%s: %s %s\n    %s", place, f.What, f.Do, quote(f.Quote))
}

// quote keeps a refusal to one screen. The whole sentence is what is wrong when a sentence is too
// long, and a writer who can see both ends of it can see where to cut.
func quote(text string) string {
	const limit = 240
	runes := []rune(text)
	if len(runes) <= limit {
		return `"` + text + `"`
	}
	return `"` + string(runes[:limit]) + `..."`
}

// Check reads one piece of prose and answers everything in it the standard refuses.
func Check(where, source string) []Finding {
	var found []Finding
	for _, paragraph := range Paragraphs(source) {
		if len(paragraph.Sentences) > MaxSentences {
			found = append(found, Finding{
				Where: where, Line: paragraph.Line, Rule: "paragraph",
				What: fmt.Sprintf("this paragraph has %d sentences, and the standard allows %d.",
					len(paragraph.Sentences), MaxSentences),
				Do:    "Start a new paragraph.",
				Quote: paragraph.Sentences[0].Text,
			})
		}
		for _, sentence := range paragraph.Sentences {
			found = append(found, checkSentence(where, sentence)...)
		}
	}
	return found
}

func checkSentence(where string, sentence Sentence) []Finding {
	var found []Finding
	at := func(rule, what, do, said string) {
		found = append(found, Finding{
			Where: where, Line: sentence.Line, Rule: rule, What: what, Do: do, Quote: said,
		})
	}
	if len(sentence.Words) > MaxWords {
		at("length",
			fmt.Sprintf("this sentence is %d words, and the standard allows %d.", len(sentence.Words), MaxWords),
			"Split it into two sentences.", sentence.Text)
	}
	if phrase, simple, ok := perfect(sentence.Words); ok {
		at("tense",
			fmt.Sprintf("%q is the %s.", phrase, simple),
			"The standard allows the infinitive, the imperative, the simple present, the simple past and the simple future. Write it in the simple past.",
			sentence.Text)
	}
	if phrase, tense, ok := continuous(sentence.Words); ok {
		at("tense",
			fmt.Sprintf("%q is a continuous tense.", phrase),
			fmt.Sprintf("The standard allows the simple tenses only. Write it in the %s.", tense),
			sentence.Text)
	}
	if phrase, ok := dash(sentence.Text); ok {
		at("dash",
			fmt.Sprintf("%q uses a dash as punctuation.", phrase),
			"Use a comma, a colon, or two sentences.", sentence.Text)
	}
	return found
}

// auxiliaries are the words that make a perfect tense, and which one each makes.
var auxiliaries = map[string]string{
	"has": "present perfect", "have": "present perfect", "had": "past perfect",
}

// beings are the words that make a continuous tense, and the tense to write instead.
var beings = map[string]string{
	"is": "simple present", "are": "simple present", "am": "simple present",
	"was": "simple past", "were": "simple past",
	"be": "infinitive", "been": "infinitive", "being": "infinitive",
}

// adverbs are the words that can stand between an auxiliary and the verb it carries. Without them
// "has already shipped" reads as an auxiliary followed by nothing.
var adverbs = map[string]bool{
	"not": true, "never": true, "always": true, "already": true, "also": true, "just": true,
	"recently": true, "indeed": true, "only": true, "still": true, "now": true, "yet": true,
	"ever": true, "long": true, "then": true, "since": true, "therefore": true, "thus": true,
	"probably": true, "certainly": true, "quietly": true, "simply": true, "actually": true,
}

// perfect finds the present perfect and the past perfect: an auxiliary carrying a past participle.
//
// The participle is the part that cannot be looked up, because the verbs of English are an open
// class and the standard's own dictionary is not published as a list. So it is read two ways, and
// both are closed: a word ending in "ed", which is every regular verb, and the irregular
// participles, which are a list of about a hundred and twenty words that does not grow.
func perfect(words []string) (string, string, bool) {
	for at, word := range words {
		tense, isAuxiliary := auxiliaries[bare(word)]
		if !isAuxiliary {
			continue
		}
		next := at + 1
		for next < len(words) && adverbs[bare(words[next])] {
			next++
		}
		if next >= len(words) || !participle(bare(words[next])) {
			continue
		}
		return strings.Join(plain(words[at:next+1]), " "), tense, true
	}
	return "", "", false
}

// continuous finds a continuous tense: a form of "be" carrying an "-ing" word.
//
// This is the measurable half of the standard's rule about "-ing" words, and only that half. An
// "-ing" word standing on its own is a gerund, a participle or a technical noun depending on what
// the sentence is doing with it, and telling those apart needs a parser. A form of "be" in front of
// one needs nothing: it is a continuous tense every time.
func continuous(words []string) (string, string, bool) {
	for at, word := range words {
		tense, isBeing := beings[bare(word)]
		if !isBeing {
			continue
		}
		next := at + 1
		for next < len(words) && adverbs[bare(words[next])] {
			next++
		}
		if next >= len(words) {
			continue
		}
		candidate := bare(words[next])
		if !strings.HasSuffix(candidate, "ing") || notAVerb[candidate] || len(candidate) < 5 {
			continue
		}
		return strings.Join(plain(words[at:next+1]), " "), tense, true
	}
	return "", "", false
}

// The two dashes, named rather than typed inline, because the difference between them is one pixel
// and a reader of this file has to be able to tell which is which.
const (
	emDash = "\u2014"
	enDash = "\u2013"
)

// dash finds a dash used as punctuation: the em dash, the en dash, and the hyphen standing as one.
//
// A hyphen inside a word is structural rather than stylistic, so `kebab-case`, a command flag and a
// package name all go through. What is refused is the hyphen with a space beside it, which is the
// only place a hyphen is doing a dash's job. A bare double hyphen is not refused either: it is a
// command flag far more often than it is punctuation, and a gate that refuses a flag is a gate
// somebody turns off.
func dash(text string) (string, bool) {
	for _, mark := range []string{emDash, enDash, " - ", " -- "} {
		at := strings.Index(text, mark)
		if at < 0 {
			continue
		}
		return around(text, at, len(mark)), true
	}
	return "", false
}

// around is the few words either side of a mark, so a refusal about punctuation points at the place
// rather than at the whole sentence.
func around(text string, at, width int) string {
	runes := []rune(text)
	start := len([]rune(text[:at]))
	from := max(start-24, 0)
	to := min(start+width+24, len(runes))
	return strings.TrimSpace(string(runes[from:to]))
}

// plain is the words of a phrase with their punctuation taken off, so a refusal quotes "has shipped"
// rather than "has shipped,".
func plain(words []string) []string {
	out := make([]string, 0, len(words))
	for _, word := range words {
		out = append(out, strings.Trim(word, `.,;:!?"'()[]{}`))
	}
	return out
}

// participle says whether this word can be the past participle an auxiliary is carrying.
//
// A word beginning with "un" is refused as one, and that is a deliberate gap rather than an
// oversight. "The checkout has uncommitted changes" is an adjective in front of a noun, not a
// tense, and so are unread, unfinished, untested and every other one of that shape. A real verb
// beginning with "un" goes through as the cost of it, except the irregular ones, which are checked
// first: "has understood" and "has undone" are still refused.
func participle(word string) bool {
	if irregular[word] {
		return true
	}
	if notAParticiple[word] || len(word) < 4 || strings.HasPrefix(word, "un") {
		return false
	}
	return strings.HasSuffix(word, "ed")
}
