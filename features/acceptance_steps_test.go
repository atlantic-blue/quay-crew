package features_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/cucumber/godog"
)

// A job holding for a person to look at a picture of what it built, driven through the same calls
// both sides use: the workers answer with their runs and their pictures, the job holds, and one
// answer from a person is what ends it.

// The three shapes a picture is missing in. Each of them is a report the machine's own three checks
// pass, so each reads as a finished build everywhere else in this system. Each states an outcome,
// because every task asks for one and a worker that states none stops for that instead.
const (
	aBuildWithNoPicture = "I built it.\n\nVertical: 1\nRan: 14\nRed: 0\n" +
		"Passing 1: TestPastingALinkPrintsTheTranscript\nChanged 1: internal/paste.go\n\nOutcome: proved"
	aPictureWithNoLabel = "I built it and drew it.\n\nVertical: 1\nRan: 14\nRed: 0\n" +
		"Passing 1: TestPastingALinkPrintsTheTranscript\nChanged 1: internal/paste.go\n" +
		"Picture: paste.png\n\nOutcome: proved"
	aPictureGeneratedToIllustrate = "I drew what it will look like.\n\nVertical: 1\nRan: 14\nRed: 0\n" +
		"Passing 1: TestPastingALinkPrintsTheTranscript\nChanged 1: internal/paste.go\n" +
		"Picture: paste.png\nTaken: a mockup of the page, drawn with krewe render\n\nOutcome: proved"
)

func initializeAcceptanceStageSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the builder will answer with no picture of what it built$`,
		func(ctx context.Context) error {
			worldFrom(ctx).runner.willAnswer(job.TheBuildAsk, aBuildWithNoPicture)
			return nil
		})

	sc.Step(`^the builder will answer with a picture that carries no label$`,
		func(ctx context.Context) error {
			worldFrom(ctx).runner.willAnswer(job.TheBuildAsk, aPictureWithNoLabel)
			return nil
		})

	sc.Step(`^the builder will answer with a picture it generated to illustrate$`,
		func(ctx context.Context) error {
			worldFrom(ctx).runner.willAnswer(job.TheBuildAsk, aPictureGeneratedToIllustrate)
			return nil
		})

	sc.Step(`^the row carries a picture of every vertical running$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		wanted := job.RequirementsOf(jobAsKept(one))
		for _, vertical := range wanted {
			shot := job.PictureOf(one.GetBuild(), vertical.Number)
			if shot.File == "" {
				return fmt.Errorf("nothing shows vertical %d working: %q", vertical.Number, one.GetBuild())
			}
			if !job.APicture(shot.File) {
				return fmt.Errorf("vertical %d is shown by %q, which is not a picture",
					vertical.Number, shot.File)
			}
		}
		return nil
	})

	sc.Step(`^every picture says where it came from and how to get it again$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			shots := job.PicturesIn(one.GetBuild())
			if len(shots) == 0 {
				return fmt.Errorf("the record carries no picture at all: %q", one.GetBuild())
			}
			for _, shot := range shots {
				if err := job.LabelsIt(shot.File, shot.Taken); err != nil {
					return fmt.Errorf("the picture of vertical %d: %w", shot.Vertical, err)
				}
			}
			return nil
		})

	sc.Step(`^the question names each picture and where to open it$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		for _, shot := range job.PicturesIn(one.GetBuild()) {
			if !strings.Contains(one.GetQuestion(), shot.File) {
				return fmt.Errorf("the question does not name %q: %s", shot.File, one.GetQuestion())
			}
			if !strings.Contains(one.GetQuestion(), shot.Taken) {
				return fmt.Errorf("the question does not say where %q came from: %s",
					shot.File, one.GetQuestion())
			}
		}
		// And where to open them. A file name a person cannot find is a picture they were not shown.
		for _, needed := range []string{"shared folder", "krewe where"} {
			if !strings.Contains(one.GetQuestion(), needed) {
				return fmt.Errorf("the question does not say %q: %s", needed, one.GetQuestion())
			}
		}
		return nil
	})

	sc.Step(`^the controller ticks (\d+) more times with nobody answering$`,
		func(ctx context.Context, times int) error {
			for range times {
				worldFrom(ctx).server.TickJob(ctx)
			}
			return nil
		})

	sc.Step(`^the job is not accepted$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetAccepted() {
			return fmt.Errorf("the job says a person accepted it, and nobody answered")
		}
		return nil
	})

	sc.Step(`^the record says a person accepted it$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if !one.GetAccepted() {
			return fmt.Errorf("nothing on the job says a person accepted it: it is %q, %s",
				one.GetPhase(), one.GetReason())
		}
		return wroteTheEvent(ctx, one.GetId(), job.EventAccepted)
	})

	sc.Step(`^the session finishing the job is told they said the value arrived, and to build `+
		`nothing more$`, func(ctx context.Context) error {
		// What a session is handed decides what it does, and a session given the brief of a job whose
		// verticals are all built reads it as work and builds it again.
		return theSessionWasSent(ctx, "said the value arrived", "Build nothing further")
	})

	sc.Step(`^the row still carries the picture they looked at$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if len(job.PicturesIn(one.GetBuild())) == 0 {
			return fmt.Errorf("landing the job lost the pictures: %q", one.GetBuild())
		}
		return nil
	})

	sc.Step(`^the job is pending, and the row carries nothing built$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhasePending {
			return fmt.Errorf("a job that was not accepted is %q: %s", one.GetPhase(), one.GetReason())
		}
		if one.GetBuild() != "" {
			return fmt.Errorf("a job sent back still carries what was built: %q", one.GetBuild())
		}
		if one.GetAccepted() {
			return fmt.Errorf("a job nobody accepted says it was accepted")
		}
		return wroteTheEvent(ctx, one.GetId(), job.EventSentBack)
	})

	// Read off the row rather than off the wire. What a person told a job is not a field the control
	// plane puts on a job it answers with, so the store is where this reads it.
	sc.Step(`^the row still carries what the person said$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		kept, err := worldFrom(ctx).store.GetJob(ctx, one.GetId())
		if err != nil {
			return err
		}
		if !strings.Contains(kept.Told, "empty page") {
			return fmt.Errorf("what the person said reads back %q, and the next build is held to it",
				kept.Told)
		}
		return nil
	})

	sc.Step(`^a second worker is building that vertical$`, func(ctx context.Context) error {
		workers, err := theBuilders(ctx)
		if err != nil {
			return err
		}
		if len(workers) < 2 {
			return fmt.Errorf("%d workers have built this vertical, want a second after the send back",
				len(workers))
		}
		return nil
	})

	sc.Step(`^the question says nothing shows the vertical working$`, func(ctx context.Context) error {
		return theQuestionSays(ctx, "nothing shows this vertical working")
	})

	sc.Step(`^the question says the picture carries no label$`, func(ctx context.Context) error {
		return theQuestionSays(ctx, "carries no label")
	})

	sc.Step(`^the question says a sample is not a capture$`, func(ctx context.Context) error {
		return theQuestionSays(ctx, "generated to illustrate")
	})

	// Whatever else a person says, the job does not end. The verticals go back to be built and the
	// row is in front of them again, which is the whole of the gate: nothing but the word moves it
	// past the question.
	sc.Step(`^the job did not end, and nobody accepted it$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if job.Terminal(one.GetPhase()) {
			return fmt.Errorf("the job ended %q with nobody having accepted it: %s",
				one.GetPhase(), one.GetReason())
		}
		if one.GetAccepted() {
			return fmt.Errorf("an answer that was not the word accepted the job")
		}
		return nil
	})
}

// wroteTheEvent says whether one kind of record is on a job's history. The kinds are read rather
// than indexed, because the order rows come back in is not a contract.
func wroteTheEvent(ctx context.Context, id, kind string) error {
	records, err := worldFrom(ctx).store.ListJobEvents(ctx, id)
	if err != nil {
		return err
	}
	for _, one := range records {
		if one.Kind == kind {
			return nil
		}
	}
	return fmt.Errorf("nothing on this job's record is a %q", kind)
}
