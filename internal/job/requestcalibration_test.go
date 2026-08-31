package job_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/atlantic-blue/krewe/internal/job"
	"gopkg.in/yaml.v3"
)

// The measurement job.DriftThreshold came from, run again on every build.
//
// A number in a comment is a claim, and a claim nobody re runs is a claim that quietly stops being
// true. So the corpus is one this repository already keeps, and it is the right shape: a sentence
// saying what a thing is, beside the brief written to serve that sentence. Every role and every skill
// in this build is one of those pairs, written by the same hands, about work that was really done.
//
// The one mistake that matters is the detector speaking about a brief that is faithful. It refuses
// nothing, so a missed drift costs a run and a false alarm costs a line, but a line printed on every
// job is a line nobody reads, and then the check is not there at all.
//
// What this corpus is not: it is not a body of requests and the briefs somebody wrote from them,
// because until this ships the system has recorded none. It is the closest thing in the repository,
// exactly as the changelog paragraphs stand in for job attempts in loopcalibration_test.go. What
// replaces it is the same query shape: once fifty jobs carry a request, read where a job whose answer
// the operator kept sits against where a job the operator steered or stopped sits, and put the
// threshold at the fifth percentile of the first.

// enoughPairs is the smallest corpus this measurement means anything on. A test that finds no text
// passes in silence, and a threshold measured on nothing is a threshold nobody measured.
const enoughPairs = 20

func TestAFaithfulBriefStaysWellOverTheThreshold(t *testing.T) {
	corpus := theSummariesAndTheirBriefs(t)

	var scores []float64
	for _, pair := range corpus {
		covered, missing := job.Covered(pair.summary, pair.brief)
		scores = append(scores, covered)
		if covered < job.DriftThreshold {
			t.Errorf("%s: its brief covers %.3f of its own summary and the threshold is %.3f, so the "+
				"system would speak about a brief that is faithful. It does not say: %v",
				pair.name, covered, job.DriftThreshold, missing)
		}
	}
	sort.Float64s(scores)
	lowest, median := scores[0], scores[len(scores)/2]
	t.Logf("%d faithful pairs: lowest %.3f, median %.3f, threshold %.3f",
		len(scores), lowest, median, job.DriftThreshold)

	// Room above the threshold rather than a pass by a hair. A threshold sitting on the lowest faithful
	// pair fires on the next one written.
	if lowest-job.DriftThreshold < 0.05 {
		t.Errorf("the lowest faithful pair covers %.3f and the threshold is %.3f, which leaves no room: "+
			"the next brief written in this repository would be reported as drifted",
			lowest, job.DriftThreshold)
	}
}

// The other side of the measurement. The two briefs that cost real work have to score under the
// threshold, or the check never fires and none of this was worth building.
func TestTheBriefsThatCostRealWorkScoreUnderTheThreshold(t *testing.T) {
	for _, one := range []struct{ name, request, brief string }{
		{
			// Measured on the acceptance project. The design said the address takes a video identifier.
			// The person had asked to paste a link. Two days of work, every check green.
			name:    "the transcript page",
			request: "paste a youtube link and get the text back",
			brief: "Build a page that serves a transcript archive. The address reads /videos?id=<video id>, " +
				"and the video identifier is the key the store is read by. A reader supplies the identifier " +
				"and the page renders the stored transcript for it. Index the store by identifier and return " +
				"not found where no row exists.",
		},
		{
			// A request for an article about what had been built became a diary of throughput.
			name:    "the article",
			request: "write an article about what we built this week",
			brief: "Write a 1500 word post for the engineering blog. Open with the number of pull requests " +
				"merged this week, then a section per day covering how many jobs ran, the token spend, and " +
				"the sessions that failed. Close with the throughput trend.",
		},
	} {
		covered, missing := job.Covered(one.request, one.brief)
		if covered >= job.DriftThreshold {
			t.Errorf("%s: the brief covers %.3f of its request and the threshold is %.3f, so the system "+
				"would have said nothing about it", one.name, covered, job.DriftThreshold)
		}
		t.Logf("%s: covers %.3f, and the brief never says %v", one.name, covered, missing)
	}
}

type summaryAndBrief struct{ name, summary, brief string }

// theSummariesAndTheirBriefs is every role and every skill this build ships, as the pair of the one
// line saying what it is and the brief a session is given.
func theSummariesAndTheirBriefs(t *testing.T) []summaryAndBrief {
	t.Helper()
	var corpus []summaryAndBrief
	for _, where := range []struct{ glob, declaration, brief string }{
		{"../../roles/*", "role.yaml", "ROLE.md"},
		{"../../skills/*", "skill.yaml", "SKILL.md"},
	} {
		dirs, err := filepath.Glob(where.glob)
		if err != nil {
			t.Fatalf("could not read the corpus: %v", err)
		}
		for _, dir := range dirs {
			declared, err := os.ReadFile(filepath.Join(dir, where.declaration))
			if err != nil {
				continue
			}
			var read struct {
				Summary string `yaml:"summary"`
			}
			if err := yaml.Unmarshal(declared, &read); err != nil {
				t.Fatalf("%s: %v", dir, err)
			}
			brief, err := os.ReadFile(filepath.Join(dir, where.brief))
			if err != nil || read.Summary == "" {
				continue
			}
			corpus = append(corpus, summaryAndBrief{filepath.Base(dir), read.Summary, string(brief)})
		}
	}
	if len(corpus) < enoughPairs {
		t.Fatalf("the corpus is %d pairs and this measurement needs %d: a threshold measured on nothing "+
			"is a threshold nobody measured", len(corpus), enoughPairs)
	}
	return corpus
}
