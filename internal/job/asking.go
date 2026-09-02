package job

import (
	"fmt"
	"strings"
)

// Asking is the system stopping for a person.
//
// A session that has to choose between two things a measurement cannot settle has three moves, and
// two of them are bad. It can guess, and the operator finds out when the answer lands, which is how
// a project asking for nothing that bills while idle came to be built on a store that bills a
// minimum capacity continuously. It can stop and say it needs a decision, which ends the job and
// costs the whole run. Or it can ask, and wait, and carry on with what it was told.
//
// The third is what this is. A question is a phase on the row rather than a message in flight, so it
// survives the controller that was holding the job, the process that took the call, and the night in
// between. Nothing but an answer moves the job while it stands, which is the difference between a
// gate and a note.
const (
	// QuestionLimit is how long a question is expected to be, and how much of one a narrow place
	// draws before it cuts.
	//
	// Shorter than a brief on purpose. A question is read by a person in a terminal, usually one who
	// is doing something else. What it has to carry is the decision and what each answer costs.
	//
	// It refuses nothing. A question past it is asked, kept word for word, and read back in full.
	QuestionLimit = 4096
	// TellingLimit is how long an answer is expected to be. It reaches the session as a task, so the
	// guide is the brief's.
	//
	// It refuses nothing. An answer past it is accepted and kept word for word: a person who writes
	// three paragraphs to settle a decision must not be told to write two.
	TellingLimit = BriefLimit
)

// TidyQuestion is a question as the system keeps it, and the refusal where it could not be kept.
//
// Silence is the only refusal left. A long question is kept whole here and cut where it is drawn,
// because a person who cannot read it all on one screen can still read it all.
func TidyQuestion(question string) (string, error) {
	tidy := strings.TrimSpace(question)
	if tidy == "" {
		return "", fmt.Errorf("a question needs to say what is being decided: ask it in a sentence, " +
			"and say what each answer costs, because the person answering has only what you write here")
	}
	return tidy, nil
}

// TidyTelling is an answer as the system keeps it, and the refusal where it could not be kept.
//
// Silence is the only refusal left. An answer is what unblocks a session, so length is the last
// thing that may stand between a person writing one and a job carrying on.
func TidyTelling(answer string) (string, error) {
	tidy := strings.TrimSpace(answer)
	if tidy == "" {
		return "", fmt.Errorf("an answer needs words: the session is waiting to be told what to do, " +
			"and an empty answer would start it again with nothing new")
	}
	return tidy, nil
}

// CarryOn is what the system sends a session whose question has been answered.
//
// The question goes back with the answer. The session has been sitting in a container since it
// asked, and a model reads what it is handed rather than what it remembers, so an answer arriving on
// its own is an answer to a question nobody restated.
func CarryOn(one *Job) string {
	return fmt.Sprintf("You asked: %s\n\nThe answer is: %s\n\nCarry on with the job from there, "+
		"and do not ask this again.", one.Question, one.Told)
}
