package job

import (
	"strings"
	"testing"
)

// Requirement 4 of quay-krewe#647: an operator reads a question that fits the terminal, and reaches
// the whole text behind it.
//
// The line above a command and the line above the console are narrow, so both cut what they draw.
// This is the other half of that pairing: the text they cut into has to still exist. A question the
// record cut before it stored it leaves nothing behind the cut, and `krewe job show` can only print
// what a narrow surface already threw away.
//
// The answer under the outcome line is the question. A session that stops for a person writes what
// it needs decided, at whatever length that decision takes, and the last paragraph of it is usually
// the choice itself.

// theLongDecision is a session's answer that runs well past the ceiling a question used to be held
// to. The last sentence names the decision, which is what makes the tail of it worth keeping.
func theLongDecision() string {
	said := []string{"The store choice is not settled and the cost is the whole reason."}
	for range 200 {
		said = append(said, "Aurora Serverless version two bills a minimum capacity continuously.")
	}
	said = append(said, "So: the key value store, or Aurora, and which do you want?")
	return strings.Join(said, " ")
}

// The whole answer reaches the record, however long it is, so there is something behind the cut for
// a person to reach.
func TestTheQuestionPutToAPersonKeepsTheWholeAnswer(t *testing.T) {
	said := theLongDecision()
	if len(said) <= QuestionLimit {
		t.Fatalf("this answer is %d bytes and proves nothing under a ceiling of %d", len(said), QuestionLimit)
	}

	asked := TheDecisionPutToAPerson(said+"\n\n"+OutcomeMarker+" "+OutcomeDecide, "session-1")

	if !strings.HasSuffix(asked, "and which do you want?") {
		t.Errorf("the question loses its last words, which are the decision:\n...%s", tailOf(asked))
	}
	if len(asked) != len(said) {
		t.Errorf("the question is %d bytes and the session wrote %d, so the record holds part of it",
			len(asked), len(said))
	}
}

// Nothing tells a person to go and read a conversation. The whole text is on the row, so the place
// to send them is the row, and a marker pointing elsewhere is a marker that outlived its reason.
func TestTheQuestionOnTheRowIsNotMarkedAsCut(t *testing.T) {
	asked := TheDecisionPutToAPerson(theLongDecision()+"\n\n"+OutcomeMarker+" "+OutcomeDecide, "session-1")

	if strings.Contains(asked, "cut here") {
		t.Errorf("the record cut the question it stored:\n...%s", tailOf(asked))
	}
	if strings.Contains(asked, "the rest is in the conversation") {
		t.Errorf("the record sends a person to a conversation for text it could hold:\n...%s", tailOf(asked))
	}
}

// tailOf is the end of a long question, because a failure printing four hundred lines is a failure
// nobody reads to the point.
func tailOf(said string) string {
	const shown = 120
	if len(said) <= shown {
		return said
	}
	return said[len(said)-shown:]
}
