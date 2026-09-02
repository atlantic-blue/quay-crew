package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/cucumber/godog"
)

// A job saying what it would build and a person accepting the list, driven through the same calls
// both sides use. The assertions go past the row to the task the system actually sent, because what
// the session is handed next is the half that decides whether this works.

// aListOfPlumbingTheSessionAnswers is the shape the rule exists to refuse: three pieces of required
// work, none of which anybody can be shown working.
const aListOfPlumbingTheSessionAnswers = "Vertical 1: a schema for the transcripts with an index\n" +
	"Shown 1: the migration applies and the index exists\n" +
	"Vertical 2: a queue between the fetcher and the writer\n" +
	"Shown 2: the topic exists and the consumer is subscribed\n" +
	"Vertical 3: a role for the session that fetches\n" +
	"Shown 3: the role directory holds the model and the brief"

// oneVertical is a list of one, which the rule leaves alone: folding plumbing into the vertical it
// serves is how a list of one is arrived at.
const oneVertical = "Vertical 1: a person pastes a link on the command line and gets the text back\n" +
	"Shown 1: the transcript prints in the terminal for a link the person chooses"

// theSecondList is what the session answers once a person has said what was wrong with the first,
// with the vertical they asked for marked as theirs.
const theSecondList = "Vertical 1: a person pastes a link on the command line and gets the text back\n" +
	"Shown 1: the transcript prints in the terminal for a link the person chooses\n" +
	"Yours 2: a person exports that transcript as a file they keep\n" +
	"Shown 2: the file lands in the folder the person chose, named after the link"

func initializeDesignSteps(sc *godog.ScenarioContext) {
	// A job that has said what it would build and is waiting for a person, which is where every
	// scenario about accepting starts.
	sc.Step(`^a job waiting for a person to accept the list it would build$`,
		func(ctx context.Context) error {
			w := worldFrom(ctx)
			if err := declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: "the transcript page", Brief: "build what the design describes",
				Product: "pastes a link and gets the text back",
			}); err != nil {
				return err
			}
			if w.lastErr != nil {
				return w.lastErr
			}
			if err := aPersonAnsweredTheReading(ctx); err != nil {
				return err
			}
			w.server.TickJob(ctx)
			if err := w.settled(ctx); err != nil {
				return err
			}
			w.server.TickJob(ctx)
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if one.GetPhase() != job.PhaseAsking || one.GetDesign() == "" {
				return fmt.Errorf("the job is %q carrying %q, want it waiting for the list to be accepted",
					one.GetPhase(), one.GetDesign())
			}
			return nil
		})

	sc.Step(`^the session will answer with a schema, a queue and a role$`, func(ctx context.Context) error {
		worldFrom(ctx).runner.willSayExactly(aListOfPlumbingTheSessionAnswers)
		return nil
	})

	sc.Step(`^the session will answer with one vertical$`, func(ctx context.Context) error {
		worldFrom(ctx).runner.willSayExactly(oneVertical)
		return nil
	})

	sc.Step(`^the job is asking, and the row carries what it would build$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseAsking {
			return fmt.Errorf("the job is %q, want asking: %s", one.GetPhase(), one.GetReason())
		}
		if !strings.Contains(one.GetDesign(), "Vertical 1:") {
			return fmt.Errorf("what it would build is %q", one.GetDesign())
		}
		// What the session said, rather than what the system asked for. The ask carries the shape of a
		// list, so a row holding the instruction back is a row where nothing was read from a reply.
		if strings.Contains(one.GetDesign(), "what a person can do when this one lands") {
			return fmt.Errorf("the row carries the instruction the system sent, not a list: %q",
				one.GetDesign())
		}
		if one.GetDesignAccepted() {
			return fmt.Errorf("the list reads as accepted, and nobody accepted it")
		}
		return nil
	})

	// The half that says a person gets something. A vertical nobody can be shown is not a vertical.
	sc.Step(`^every vertical says what a person is shown when it lands$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		read := job.DesignIn(one.GetDesign())
		if len(read.Verticals) == 0 {
			return fmt.Errorf("the row carries no list the system can read: %q", one.GetDesign())
		}
		for _, vertical := range read.Verticals {
			if vertical.Shown == "" {
				return fmt.Errorf("vertical %d says nothing about what a person is shown", vertical.Number)
			}
		}
		return nil
	})

	sc.Step(`^the row carries (\d+) vertical$`, func(ctx context.Context, want int) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if held := len(job.DesignIn(one.GetDesign()).Verticals); held != want {
			return fmt.Errorf("the row carries %d verticals, want %d: %q", held, want, one.GetDesign())
		}
		return nil
	})

	sc.Step(`^the question names the sentence and asks whether this list gets it$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			for _, phrase := range []string{one.GetProduct(), "Does this list get that sentence?"} {
				if !strings.Contains(one.GetQuestion(), phrase) {
					return fmt.Errorf("the question is %q, want it to say %q", one.GetQuestion(), phrase)
				}
			}
			return nil
		})

	sc.Step(`^no list reached a person$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetDesign() != "" {
			return fmt.Errorf("a list of plumbing landed on the row: %q", one.GetDesign())
		}
		if one.GetPhase() == job.PhaseAsking {
			return fmt.Errorf("the job is asking about %q", one.GetQuestion())
		}
		return nil
	})

	sc.Step(`^the session is told it listed one vertical with its plumbing inside it$`,
		func(ctx context.Context) error {
			return theSessionWasSent(ctx, "one vertical with its plumbing inside it",
				"required work towards", "name the person")
		})

	// Queued before the answer that sends the list back, rather than inside the assertion after it.
	// The tick that carries the correction dispatches the second ask and the double answers it there
	// and then, so an answer queued afterwards is an answer that arrives too late.
	sc.Step(`^the session will answer with the vertical the person asked for$`,
		func(ctx context.Context) error {
			worldFrom(ctx).runner.willSayExactly(theSecondList)
			return nil
		})

	sc.Step(`^the session is sent the list it wrote and what the person said$`,
		func(ctx context.Context) error {
			return theSessionWasSent(ctx, "was not accepted", "Vertical 1:",
				"the browser one is not needed, an export is", "Do no work yet")
		})

	sc.Step(`^the list is not accepted yet$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetDesignAccepted() {
			return fmt.Errorf("the list reads as accepted, and nobody accepted it")
		}
		return nil
	})

	sc.Step(`^the list is accepted$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if !one.GetDesignAccepted() {
			return fmt.Errorf("the list is not accepted after a person answered yes")
		}
		return nil
	})

	// The mark that survives onto the record. A list a person changed and a list the machine proposed
	// read the same once they are both on the row, and this is where they stop.
	sc.Step(`^the row marks the vertical the person put there as theirs$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		read := job.DesignIn(one.GetDesign())
		if len(read.Verticals) != 2 {
			return fmt.Errorf("the row carries %d verticals, want the two of the second list: %q",
				len(read.Verticals), one.GetDesign())
		}
		if read.Verticals[0].Yours {
			return fmt.Errorf("a vertical the crew proposed reads as the person's")
		}
		if !read.Verticals[1].Yours {
			return fmt.Errorf("the vertical the person asked for does not read as theirs: %q",
				one.GetDesign())
		}
		return nil
	})

	sc.Step(`^the job is still asking, and the list is not accepted yet$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseAsking {
			return fmt.Errorf("a list nobody answered left the job %q: %s", one.GetPhase(), one.GetReason())
		}
		if one.GetDesignAccepted() {
			return fmt.Errorf("a list nobody answered reads as accepted")
		}
		return nil
	})

	sc.Step(`^the session is asked for a plan carrying the list a person accepted$`,
		func(ctx context.Context) error {
			return theSessionWasSent(ctx, "Step 1:", "A person accepted this list", "Yours 2:")
		})

	sc.Step(`^the session is asked again for the list and told what was wrong$`,
		func(ctx context.Context) error {
			return theSessionWasSent(ctx, "asked for the list once already", "Vertical 1:")
		})

	sc.Step(`^that job was never asked what it would build$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetDesign() != "" {
			return fmt.Errorf("it was asked what it would build: %q", one.GetDesign())
		}
		return nil
	})
}
