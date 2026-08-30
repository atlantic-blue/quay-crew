package job

import (
	"fmt"
	"regexp"
	"strings"
)

// A job cannot wait, so a brief that asks it to is refused where it is written.
//
// A job runs once and answers. Nothing wakes it again, so a brief that says "watch the checks and
// merge on green" asks for something the runtime does not have. The session is left with two bad
// moves: hold a container open through a five minute pipeline and pay for it, or answer and stop. It
// takes a third. It reports that it will wait, and nothing ever wakes it.
//
// The crew already has the wait, and it is not in a job. A flow has a wait node, so shipping is a
// graph: a dispatch that pushes and opens the pull request, a wait, then a choice on the check
// result. The refusal names that graph.
//
// The rule reads the brief, so it is a guess about English. It is held narrow for that reason. A
// waiting word has to point at a forge pipeline, and a merge has to point at a pull request or at
// the result of one, so "merge origin/main into your branch" is ordinary work and stays legal. A
// refusal that fires on ordinary work is the rule everybody learns to word around.

// waitWindow and mergeWindow are how far a verb may sit from the thing it acts on, in bytes. About a
// short clause. The merge window is the tighter of the two because the words that follow a merge are
// ordinary ones: a brief merging a branch and then checking something must not read as a merge of
// the checks.
const (
	waitWindow  = 40
	mergeWindow = 30
)

// waitsForAPipeline is a brief that asks the job to hold until a forge reports.
//
// The nouns are the ones only a forge has. "Wait for the tests" is left alone on purpose: a session
// that runs the suite and waits for it is doing the work, not waiting on the world.
var waitsForAPipeline = regexp.MustCompile(
	`(?i)\b(wait|waits|waiting|watch|watches|watching|poll|polls|polling|monitor|monitors|monitoring)\b` +
		`[^.!?]{0,` + fmt.Sprint(waitWindow) + `}?` +
		`\b(checks|continuous integration|ci|pipeline|pipelines|workflow|workflows|green)\b`)

// mergesOnAResult is a brief that asks the job to merge a pull request, or to merge on what a
// pipeline said. Merging is the gate, and a gate a job walks through on its own is not a gate.
var mergesOnAResult = regexp.MustCompile(
	`(?i)\bmerge[sd]?\b[^.!?]{0,` + fmt.Sprint(mergeWindow) + `}?` +
		`\b(green|pull request|pull requests|pr|checks|continuous integration|ci)\b`)

// negations are the words that turn one of the phrases above into an instruction not to do it. "Do
// not merge the pull request" is the line the crew itself adds, so a brief repeating it back must
// not be refused.
var negations = []string{
	"not", "n't", "never", "no ", "without", "nobody", "somebody else", "someone else",
	"rather than", "instead of", "avoid", "skip", "leave the",
}

// negationWindow is how much of the text before a phrase is read for a negation, in bytes.
const negationWindow = 24

// OnlyAFlowCan is the phrase in a brief that asks a job for something only a flow can do, and empty
// where the brief asks for nothing of the kind.
//
// The phrase is handed back rather than a yes or a no, so the refusal quotes the words that were
// actually typed. A person shown their own sentence sees what to change.
//
// The whitespace is collapsed first, so a sentence that wrapped across two lines reads as one
// sentence. A full stop, a question mark and an exclamation mark still end one.
func OnlyAFlowCan(brief string) string {
	said := strings.Join(strings.Fields(brief), " ")
	for _, shape := range []*regexp.Regexp{waitsForAPipeline, mergesOnAResult} {
		for _, at := range shape.FindAllStringIndex(said, -1) {
			if negated(said, at[0]) {
				continue
			}
			return said[at[0]:at[1]]
		}
	}
	return ""
}

// negated says whether the text just before a phrase turns it into an instruction not to do it.
func negated(said string, at int) bool {
	from := at - negationWindow
	if from < 0 {
		from = 0
	}
	before := strings.ToLower(said[from:at])
	for _, word := range negations {
		if strings.Contains(before, word) {
			return true
		}
	}
	return false
}

// RefusedWait is what the crew says to a brief that asks the job to wait. It names the graph,
// because a refusal a caller cannot act on sends them looking.
func RefusedWait(asked string) error {
	return fmt.Errorf("job's brief says %q, and a job cannot wait: it runs once and answers, so nothing "+
		"wakes it when the checks land. That shape is a flow, and it is three nodes: a dispatch that "+
		"pushes and opens the pull request, a wait, then a choice on the check result. Write it as a "+
		"graph and import it with `quay flow import`, or let this job end at the pull request and leave "+
		"the merge to somebody else", asked)
}
