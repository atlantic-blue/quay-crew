package main

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// Requirement 4 of quay-krewe#647: an operator reads a question that fits the terminal, and reaches
// the whole text behind it.
//
// This is the reaching, on the surface a person reaches with. The line above a command and the line
// above the console are narrow and cut what they draw, and the command they name has to answer with
// all of it. A reading that stopped at the row would prove a field holds the text, and nothing about
// whether the person told to go and read it gets to read it.

// theDecisionAJobStoppedOn is what a session writes when the choice in front of it costs money and
// the reasoning is the evidence for the choice. It runs past the ceiling a question used to be held
// to. The decision itself is the last sentence, which is the part a cut takes.
func theDecisionAJobStoppedOn() string {
	said := []string{"I got to the store and the cost decides it, so here is what I read."}
	for range 200 {
		said = append(said, "Aurora Serverless version two bills a minimum capacity continuously.")
	}
	said = append(said, "So: the key value store on demand, or Aurora, and which do you want?")
	return strings.Join(said, " ")
}

// The whole question, on the command the telling sends a person to.
func TestJobShowPrintsTheWholeOfALongQuestion(t *testing.T) {
	said := theDecisionAJobStoppedOn()
	if len(said) <= job.QuestionLimit {
		t.Fatalf("this answer is %d bytes and proves nothing under a ceiling of %d",
			len(said), job.QuestionLimit)
	}

	client, srv := aSystemAnswering(t, said+"\n\n"+job.OutcomeMarker+" "+job.OutcomeDecide, false)
	id := declared(t, client, srv, "choose where the transcripts are stored", job.PhaseAsking)

	shown := mustRun(t, client, "job", "show", id)

	if !strings.Contains(shown, "asking: ") {
		t.Fatalf("krewe job show does not say the job waits on a person:\n%s", shown)
	}
	// The last sentence is the decision. A reading that carries the reasoning and drops the choice
	// leaves a person with the evidence for a question nobody asked them.
	if !strings.Contains(shown, "which do you want?") {
		t.Errorf("krewe job show drops the end of the question, which is the decision:\n...%s",
			lastOf(shown, 200))
	}
	// Every sentence of it, counted, because a reading can carry the first words and the last and
	// still have thrown the middle away.
	if held := strings.Count(shown, "bills a minimum capacity continuously"); held != 200 {
		t.Errorf("krewe job show prints %d of the 200 sentences the session wrote", held)
	}
	if strings.Contains(shown, "cut here") {
		t.Errorf("krewe job show marks the question as cut, so the whole text is nowhere:\n...%s",
			lastOf(shown, 200))
	}
}

// lastOf is the end of a long reading, so a failure says where the text stopped without printing
// four hundred lines above it.
func lastOf(said string, shown int) string {
	if len(said) <= shown {
		return said
	}
	return said[len(said)-shown:]
}
