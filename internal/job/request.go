package job

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// A job keeps the request that produced it, word for word, and the brief is held against it.
//
// The failure is one hop wide. A person says a sentence. Something writes a brief from it. The system
// runs the brief faithfully and fast, every check is green, and nothing ever reads the brief back
// against the sentence. So a misreading of one sentence becomes two days of correct work in the
// wrong direction, and it looks like progress the whole way.
//
// Measured twice in one week. A request for an article about what had been built became a brief for
// a diary of throughput: the prose was good and the subject was wrong. A request to paste a link and
// get the text back became a design whose address takes a video identifier, and the product was
// built over two days from that design.
//
// The request is not the sentence in Product. Product says what a person does with what is built and
// what they get back, which is an outcome. The request is the ask. On the article the two do not
// overlap at all: "a reader opens the post" and a diary of throughput agree completely, so nothing
// stated as an outcome could have caught it.

const (
	// RequestLimit is how long a request is expected to be. It is the brief's guide rather than the
	// title's, which every other one line field takes.
	//
	// It refuses nothing, and this is the field where refusing would be worst: a ceiling that makes
	// somebody shorten what was said is the compression this whole file exists to catch.
	RequestLimit = BriefLimit
	// DriftThreshold is how much of a request a brief has to carry before the system says nothing.
	//
	// **Measured on the text this repository holds.** The corpus of the right shape is a sentence
	// beside the brief written to serve it, and there are 27 of those here: the summary and the brief
	// of every role and every skill. The lowest of them covers 0.778 and the median covers 1.000. The
	// two incidents above cover 0.500 and 0.000. Two thirds sits between the two groups with room on
	// each side, and requestcalibration_test.go measures it again on every build.
	//
	// **The direction of the error is chosen.** The check writes a line and refuses nothing, so a
	// false alarm costs a sentence and a missed drift costs a run. On the looser corpus of 121 issue
	// titles held against their bodies, one in eight falls below this, which is the price.
	DriftThreshold = 2.0 / 3.0
)

// Covered is how much of a request a brief carries, and the words of the request it never says.
//
// The measure is the content words, and nothing about the model is read: it costs no call, works
// with any backend, and anybody holding the row can work the number out again, which is the same
// argument Similarity makes in loop.go.
//
// A request nobody stated is covered by everything. Silence is not drift.
func Covered(request, brief string) (float64, []string) {
	says := map[string]bool{}
	for _, word := range contentWords(brief) {
		for _, form := range formsOf(word) {
			says[form] = true
		}
	}
	seen, missing := map[string]bool{}, []string{}
	total, found := 0, 0
	for _, word := range contentWords(request) {
		if seen[word] {
			continue
		}
		seen[word] = true
		total++
		if saidSomewhere(word, says) {
			found++
			continue
		}
		missing = append(missing, word)
	}
	if total == 0 {
		return 1, nil
	}
	return float64(found) / float64(total), missing
}

// Drifted is what the system says to whoever declared a job whose brief does not carry its request,
// and empty where the brief carries it.
//
// Empty is the whole point. A check that speaks about every job is a check nobody reads, and it
// would put a person back in front of every brief, which is the cost this system exists to remove.
func Drifted(request, brief string) string {
	if strings.TrimSpace(request) == "" {
		return ""
	}
	covered, missing := Covered(request, brief)
	if covered >= DriftThreshold || len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("the brief does not say what the request says. The request used %s, and the brief "+
		"says none of them. It is declared anyway, and the session is told the request wins over the "+
		"brief; if the brief is the one that is right, say it in the request too",
		theWords(missing))
}

// AskedInTheseWords is the request as a session is given it, above the brief.
//
// The words go to the session unrewritten, because a summary of what was said is the same
// compression that caused the fault. Where the brief dropped some of them, the session is told
// which, and told to say so rather than to build the brief as written. Building it faithfully is
// what already happened.
func AskedInTheseWords(request, brief string) string {
	tidy := strings.TrimSpace(request)
	if tidy == "" {
		return ""
	}
	said := fmt.Sprintf("This is what was asked for, in the words it was asked in: %s. The brief below was "+
		"written from it. Where the two disagree, this wins: say so in your answer rather than building "+
		"the brief as written.", tidy)
	if covered, missing := Covered(tidy, brief); covered < DriftThreshold && len(missing) > 0 {
		said += fmt.Sprintf(" The system read the two and the brief says nothing about %s, so read the "+
			"request again before you start, and say in your answer what you did about it.", theWords(missing))
	}
	return said
}

// theWords is a list of words as a sentence carries them, so a refusal reads as English rather than
// as a dump of a slice.
func theWords(words []string) string {
	quoted := make([]string, 0, len(words))
	for _, word := range words {
		quoted = append(quoted, fmt.Sprintf("%q", word))
	}
	sort.Strings(quoted)
	if len(quoted) == 1 {
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
}

// saidSomewhere says whether a brief says a word in any of the shapes it takes.
func saidSomewhere(word string, says map[string]bool) bool {
	for _, form := range formsOf(word) {
		if says[form] {
			return true
		}
	}
	return false
}

// formsOf is the shapes one word may be matched in, so a request that says pasting and a brief that
// says paste are saying the same thing.
//
// It folds four endings and nothing else. An irregular verb is not folded, so a request saying built
// and a brief saying build read as two words. That is a word reported as dropped where it was not,
// which costs a line rather than a run.
func formsOf(word string) []string {
	forms := []string{word}
	for _, ending := range []string{"ing", "ed", "es", "s"} {
		if strings.HasSuffix(word, ending) && len(word)-len(ending) >= 3 {
			stem := word[:len(word)-len(ending)]
			forms = append(forms, stem, stem+"e")
		}
	}
	return forms
}

// contentWords is the words of a piece of text that carry its subject, lowercased and with the
// punctuation off.
//
// The words below are dropped because every brief holds nearly all of them, so counting them would
// score a brief about the wrong subject as faithful.
func contentWords(text string) []string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	kept := make([]string, 0, len(words))
	for _, word := range words {
		if len(word) < 2 || everywhereWords[word] {
			continue
		}
		kept = append(kept, word)
	}
	return kept
}

// everywhereWords are the words that say nothing about a subject.
var everywhereWords = func() map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.Fields(`a an the and or but if then than that this these those of in on at to for
from by with without into onto about as is are was were be been being it its i we you they them us our your their
my me do does did done doing have has had having will would can could should may might must shall not no nor so
such only own same too very just also more most other some any each every both few many much one there here when
where what which who whom whose why how all over under again further once out off up down before after while
during because until against between through`) {
		set[word] = true
	}
	return set
}()
