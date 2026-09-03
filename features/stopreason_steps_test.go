package features_test

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// The reason a job stopped, on the row of the listing, at the width the row draws it in.
//
// A reason is free text somebody types. A long one pushes the title off the terminal, and a line
// break in one turns a job into two rows that each read as half a job. So the row draws the reason
// in a column of its own width, and the last character says the text goes on.

// theLongStopReason is what an operator types when the cause takes a sentence. It is 87 characters.
const theLongStopReason = "the meter reading the supplier billed on is not the reading on the meter in the hallway"

// theColumnAReasonIsDrawnIn is the width the row gives a reason, counting the one character that
// says the text goes on.
const theColumnAReasonIsDrawnIn = 40

func initializeStopReasonSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the caller stops the first job saying a reason longer than the column$`,
		stopTheFirstJobSaying(theLongStopReason))

	sc.Step(`^the caller stops the first job saying a reason with a line break in it$`,
		stopTheFirstJobSaying("no meter reading\nthe supplier says so"))

	sc.Step(`^the row draws the reason cut to the column, and marks the cut$`, func(ctx context.Context) error {
		row, err := theRowOfTheFirstJob(ctx)
		if err != nil {
			return err
		}
		cut := theLongStopReason[:theColumnAReasonIsDrawnIn-1] + "…"
		if !strings.Contains(row, cut) {
			return fmt.Errorf("the row reads %q, want it to draw the reason as %q", row, cut)
		}
		if beyond := theLongStopReason[:theColumnAReasonIsDrawnIn+1]; strings.Contains(row, beyond) {
			return fmt.Errorf("the row reads %q, and it draws %q, which is past the column", row, beyond)
		}
		return nil
	})

	sc.Step(`^krewe job show prints the whole reason$`, func(ctx context.Context) error {
		one, err := firstJob(ctx)
		if err != nil {
			return err
		}
		if err := runTool(ctx, "job", "show", one.GetId()); err != nil {
			return err
		}
		return says("standard output", toolFrom(ctx).stdout, theLongStopReason)
	})

	sc.Step(`^the listing draws one row for each job, with both halves of the reason on one of them$`,
		func(ctx context.Context) error {
			rows := theRowsOfTheListing(toolFrom(ctx).stdout)
			if len(rows) != 2 {
				return fmt.Errorf("the listing draws %d rows for 2 jobs, so the line break broke a row:\n%s",
					len(rows), toolFrom(ctx).stdout)
			}
			row, err := theRowOfTheFirstJob(ctx)
			if err != nil {
				return err
			}
			for _, half := range []string{"no meter reading", "the supplier says so"} {
				if !strings.Contains(row, half) {
					return fmt.Errorf("the row reads %q, and %q is not on it", row, half)
				}
			}
			return nil
		})
}

// stopTheFirstJobSaying stops the job the scenario declared first, with the words this scenario is
// about.
func stopTheFirstJobSaying(reason string) func(context.Context) error {
	return func(ctx context.Context) error {
		one, err := firstJob(ctx)
		if err != nil {
			return err
		}
		w := worldFrom(ctx)
		stopped, err := w.client.StopJob(ctx, &quaycrewv1.StopJobRequest{Id: one.GetId(), Reason: reason})
		w.lastErr = err
		if err != nil {
			return err
		}
		jobFrom(ctx).declared[0] = stopped.GetJob()
		return nil
	}
}

// theRowOfTheFirstJob is the one line of the drawn listing that belongs to the job the scenario
// stopped. A reason printed under the listing is not on the row, and this is what says so.
func theRowOfTheFirstJob(ctx context.Context) (string, error) {
	one, err := firstJob(ctx)
	if err != nil {
		return "", err
	}
	short := one.GetId()
	if utf8.RuneCountInString(short) > 8 {
		short = short[:8]
	}
	for _, row := range theRowsOfTheListing(toolFrom(ctx).stdout) {
		if strings.HasPrefix(row, short) {
			return row, nil
		}
	}
	return "", fmt.Errorf("no row of this listing belongs to %s:\n%s", short, toolFrom(ctx).stdout)
}

// theRowsOfTheListing is the rows of a drawn listing, which is everything above the blank line that
// carries the count and where the listing looked.
func theRowsOfTheListing(drawn string) []string {
	rows, _, _ := strings.Cut(strings.TrimSpace(drawn), "\n\n")
	if strings.TrimSpace(rows) == "" {
		return nil
	}
	return strings.Split(rows, "\n")
}
