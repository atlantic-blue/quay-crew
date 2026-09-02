package job_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The measurement the plumbing rule came from, run again on every build.
//
// A rule that refuses work costs a task and a person's patience every time it is wrong, so the one
// mistake that matters here is refusing a line that really is a deliverable. The corpus is one this
// repository already keeps and it is the right shape: the opening sentence of every changelog entry
// says what somebody can now do that they could not do before, written by the same hands, about work
// that really shipped.
//
// What this corpus is not: it is not a body of proposed verticals, because until this ships the
// system has recorded none. It stands in for one exactly as the changelog paragraphs already stand
// in for job attempts in loopcalibration_test.go. What replaces it is the record itself: once fifty
// lists have been put to a person, read how many lines the rule refused against how many the person
// then sent back themselves, and keep the vocabulary that agrees with them.

// enoughDeliverables is the smallest corpus this measurement means anything on, and enoughNamed is
// the smallest subset that names its person. A test that finds no text passes in silence, and a rule
// measured on nothing is a rule nobody measured.
const (
	enoughDeliverables = 40
	enoughNamed        = 25
)

// theDeliverableSentence is the opening claim of a changelog entry, in both shapes this repository
// writes them in: a fragment waiting for a release, and an entry already in the changelog.
var theDeliverableSentence = regexp.MustCompile(`(?m)^(?:- )?\*\*(.+?)\*\*`)

// The measurement, on the shape the ask demands. A vertical names the person it serves, so what has
// to be true is that a real deliverable written that way is never refused.
//
// Measured on 2 September 2026: 366 sentences of work that shipped, 38 of them naming the person
// they serve, and none of those 38 refused. The other 328 do not name anybody, because a changelog
// headline is written for a reader who already knows who the product is for, and the rule refuses 42
// of them. That is the cost, and it is one line rewritten rather than one deliverable lost: the
// refusal names the line and says to name the person in it.
func TestTheRuleRefusesNoShippedDeliverableThatNamesItsPerson(t *testing.T) {
	corpus := theSentencesOfWorkThatShipped(t)
	if len(corpus) < enoughDeliverables {
		t.Fatalf("the corpus holds %d sentences and this measurement needs %d",
			len(corpus), enoughDeliverables)
	}

	var named, refused, refusedOverall []string
	for _, said := range corpus {
		word := job.OnlyPlumbing(said)
		if word != "" {
			refusedOverall = append(refusedOverall, said)
		}
		if !job.NamesAPerson(said) {
			continue
		}
		named = append(named, said)
		if word != "" {
			refused = append(refused, said+" [refused for "+word+"]")
		}
	}
	t.Logf("%d sentences of work that shipped, %d of them name the person they serve, %d of those "+
		"refused, %d refused across the whole corpus",
		len(corpus), len(named), len(refused), len(refusedOverall))

	if len(named) < enoughNamed {
		t.Fatalf("only %d of the corpus names the person it serves, and this measurement needs %d",
			len(named), enoughNamed)
	}
	if len(refused) > 0 {
		t.Errorf("the rule refuses %d sentences of work that really shipped and named their person: %s",
			len(refused), strings.Join(refused, "; "))
	}
	// And a bound on the whole corpus, which is the sanity check on the vocabulary itself. A rule that
	// refused most sentences about this work would be a rule about how this crew writes rather than a
	// rule about who the work serves.
	if share := float64(len(refusedOverall)) / float64(len(corpus)); share > 0.20 {
		t.Errorf("the rule refuses %.1f%% of every sentence of work that shipped, named or not, "+
			"which is a rule about the vocabulary rather than about the person", share*100)
	}
}

// And the other direction, on the shape the rule exists to catch. A rule that refuses nothing is a
// sentence in a prompt, which is what this replaced.
func TestTheRuleRefusesEveryPieceOfPlumbing(t *testing.T) {
	plumbing := []string{
		"a schema for the transcripts, with an index on the link",
		"a queue between the reader and the writer",
		"a role for the session that writes the list",
		"the migration that adds the two columns",
		"a container image with the tool in it",
		"the protobuf contract for the new call",
		"a cache in front of the store",
		"a bucket for the rendered pages",
		"terraform for the pipeline that deploys it",
		"a library that wraps the client",
	}
	for _, said := range plumbing {
		if word := job.OnlyPlumbing(said); word == "" {
			t.Errorf("%q reads as something a person can be shown working", said)
		}
	}
}

// theSentencesOfWorkThatShipped is the opening claim of every changelog entry this repository holds,
// waiting for a release and already released.
func theSentencesOfWorkThatShipped(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../../changelog.d/*.md")
	if err != nil {
		t.Fatalf("could not read the corpus: %v", err)
	}
	files = append(files, "../../CHANGELOG.md")

	var corpus []string
	for _, where := range files {
		read, err := os.ReadFile(where)
		if err != nil {
			continue
		}
		for _, found := range theDeliverableSentence.FindAllStringSubmatch(string(read), -1) {
			said := strings.TrimSpace(found[1])
			if said != "" {
				corpus = append(corpus, said)
			}
		}
	}
	return corpus
}
