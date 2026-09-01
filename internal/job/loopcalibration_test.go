package job_test

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The measurement the threshold came from, run again on every build.
//
// A number in a comment is a claim, and a claim nobody re runs is a claim that quietly stops being
// true. So the corpus is a file this repository already keeps: every paragraph of CHANGELOG.md over
// sixty words, which is three hundred pieces of technical prose about work that was really done, by
// the same hands and about the same system. That is as close as this repository gets to a body of job
// attempts before any job has recorded one, and the comment on job.LoopThreshold says so.
//
// Two sides are measured, and they are the two mistakes the detector can make. Different work must
// score under the threshold, or the detector stops work that was going to finish. The same work said
// again must score over it, or the detector never fires at all.

// enoughParagraphs is the smallest corpus this measurement means anything on. A test that finds no
// text passes in silence, and a threshold measured on nothing is a threshold nobody measured.
const enoughParagraphs = 100

// aParagraph is how long a piece of the changelog has to be to stand in for what an attempt says. An
// attempt is a paragraph or several, and two short lines share too little to measure.
const aParagraph = 60

var everyNumber = regexp.MustCompile(`[0-9]+`)

func TestDifferentWorkScoresFarUnderTheThreshold(t *testing.T) {
	corpus := theChangelogParagraphs(t)

	var scores []float64
	reaching := 0
	for i := range corpus {
		for j := i + 1; j < len(corpus); j++ {
			alike := job.Similarity(corpus[i], corpus[j])
			scores = append(scores, alike)
			if alike >= job.LoopThreshold {
				reaching++
			}
		}
	}
	sort.Float64s(scores)
	ninetyNine := scores[len(scores)*99/100]
	t.Logf("%d paragraphs, %d pairs: median %.4f, ninety ninth percentile %.4f, highest %.3f, %d at "+
		"or above the threshold of %.2f",
		len(corpus), len(scores), scores[len(scores)/2], ninetyNine, scores[len(scores)-1],
		reaching, job.LoopThreshold)

	// An order of magnitude of room under the threshold, so the number can move a long way before it
	// starts reading ordinary work as a repeat.
	if ninetyNine >= job.LoopThreshold/10 {
		t.Fatalf("ninety nine per cent of different work scores under %.4f, and the threshold is %.2f: "+
			"there is no room left between the two", ninetyNine, job.LoopThreshold)
	}
	// A handful of the paragraphs really are one paragraph written twice with a word changed, and
	// those are the thing this exists to find rather than a mistake. One in ten thousand allows for
	// them and for nothing like a rate.
	if reaching*10000 > len(scores) {
		t.Fatalf("%d of %d pairs of different paragraphs reach the threshold of %.2f, which is more than "+
			"the near duplicates this corpus holds", reaching, len(scores), job.LoopThreshold)
	}
}

func TestTheSameWorkSaidAgainScoresOverTheThreshold(t *testing.T) {
	corpus := theChangelogParagraphs(t)

	least, under := 1.0, 0
	for _, one := range corpus {
		// The same text with every number in it changed, which is what a session repeating an attempt
		// produces: the same reasoning, a new measurement.
		alike := job.Similarity(one, everyNumber.ReplaceAllString(one, "97"))
		if alike < least {
			least = alike
		}
		if alike < job.LoopThreshold {
			under++
		}
	}
	t.Logf("%d paragraphs said again with their numbers changed: least %.3f, %d under the threshold "+
		"of %.2f", len(corpus), least, under, job.LoopThreshold)

	if under > 0 {
		t.Fatalf("%d of %d paragraphs said again score under the threshold of %.2f, so a session "+
			"repeating itself would not be found", under, len(corpus), job.LoopThreshold)
	}
}

// theChangelogParagraphs is the corpus, and the failure where there is not enough of it to measure
// anything.
func theChangelogParagraphs(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatalf("the corpus this threshold was measured on is not there: %v", err)
	}
	var corpus []string
	for _, block := range strings.Split(string(body), "\n\n") {
		if len(strings.Fields(block)) >= aParagraph {
			corpus = append(corpus, block)
		}
	}
	if len(corpus) < enoughParagraphs {
		t.Fatalf("the corpus is %d paragraphs and this measurement needs %d: a threshold measured on "+
			"nothing is a threshold nobody measured", len(corpus), enoughParagraphs)
	}
	return corpus
}
