package features_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/cucumber/godog"
)

// How far through the work a job is, on the two surfaces a person reads a job on. The assertions go
// through the tool rather than through the row, because the row already carries every fact the stage
// is read from: what is being specified here is that a person reading a job is told which stage it
// is in without working it out for themselves.

func initializeStageSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the reading says the job is in the "([^"]*)" stage$`,
		func(ctx context.Context, stage string) error {
			want := fmt.Sprintf("stage %d of %d: %s", numberOfStage(stage), len(job.Stages), stage)
			if out := toolFrom(ctx).stdout; !strings.Contains(out, want) {
				return fmt.Errorf("the reading does not say %q: %s", want, out)
			}
			return nil
		})

	sc.Step(`^the reading says the phase is "([^"]*)"$`, func(ctx context.Context, phase string) error {
		if out := toolFrom(ctx).stdout; !strings.Contains(out, "phase "+phase) {
			return fmt.Errorf("the reading does not say the phase is %q: %s", phase, out)
		}
		return nil
	})

	sc.Step(`^the reading says what closed the stage before it and what opens the next one$`,
		func(ctx context.Context) error {
			out := toolFrom(ctx).stdout
			for _, want := range []string{
				"nothing came before it, ideation is the first stage",
				"design opens on your answer to what it understood",
			} {
				if !strings.Contains(out, want) {
					return fmt.Errorf("the reading does not say %q: %s", want, out)
				}
			}
			return nil
		})

	sc.Step(`^the reading says the answer closed ideation$`, func(ctx context.Context) error {
		const want = "ideation closed on your answer to what it understood"
		if out := toolFrom(ctx).stdout; !strings.Contains(out, want) {
			return fmt.Errorf("the reading does not say %q: %s", want, out)
		}
		return nil
	})

	// A stage that is named and does nothing reads exactly like a stage that works, so a reader in
	// one is told, and a reader in ideation is not told about a stage that is built.
	sc.Step(`^the reading says the stage is not built yet$`, func(ctx context.Context) error {
		if out := toolFrom(ctx).stdout; !strings.Contains(out, "is not built yet") {
			return fmt.Errorf("the reading claims an unbuilt stage works: %s", out)
		}
		return nil
	})

	sc.Step(`^the reading does not say the stage is unbuilt$`, func(ctx context.Context) error {
		if out := toolFrom(ctx).stdout; strings.Contains(out, "is not built yet") {
			return fmt.Errorf("a job in a stage that works is told a stage is not built: %s", out)
		}
		return nil
	})

	sc.Step(`^the reading says accepting the list opens the next stage$`, func(ctx context.Context) error {
		const want = "test opens on your acceptance of the list it would build"
		if out := toolFrom(ctx).stdout; !strings.Contains(out, want) {
			return fmt.Errorf("the reading does not say %q: %s", want, out)
		}
		return nil
	})

	sc.Step(`^the reading says a failing test for every requirement opens the next stage$`,
		func(ctx context.Context) error {
			const want = "build opens on a failing test for every requirement on that list"
			if out := toolFrom(ctx).stdout; !strings.Contains(out, want) {
				return fmt.Errorf("the reading does not say %q: %s", want, out)
			}
			return nil
		})

	sc.Step(`^the reading says the acceptance closed design$`, func(ctx context.Context) error {
		const want = "design closed on your acceptance of the list it would build"
		if out := toolFrom(ctx).stdout; !strings.Contains(out, want) {
			return fmt.Errorf("the reading does not say %q: %s", want, out)
		}
		return nil
	})

	sc.Step(`^the reading says the job is in no stage$`, func(ctx context.Context) error {
		out := toolFrom(ctx).stdout
		if !strings.Contains(out, "no stage") || !strings.Contains(out, "errand") {
			return fmt.Errorf("the reading does not say why this job runs no stages: %s", out)
		}
		for _, stage := range job.Stages {
			if strings.Contains(out, "stage 1 of") || strings.Contains(out, ": "+stage+"\n") {
				return fmt.Errorf("the reading puts an errand in %s: %s", stage, out)
			}
		}
		return nil
	})

	sc.Step(`^the listing carries the stage "([^"]*)"$`, func(ctx context.Context, stage string) error {
		row, err := theRowFor(ctx)
		if err != nil {
			return err
		}
		if !strings.Contains(row, stage) {
			return fmt.Errorf("the row does not carry the stage %q: %s", stage, row)
		}
		// Beside the phase rather than in place of it: a listing that dropped the phase would answer
		// a different question from the one it answered yesterday.
		if !strings.Contains(row, job.PhaseAsking) && !strings.Contains(row, job.PhasePending) {
			return fmt.Errorf("the row carries a stage and no phase: %s", row)
		}
		return nil
	})

	sc.Step(`^the listing carries no stage for that job$`, func(ctx context.Context) error {
		row, err := theRowFor(ctx)
		if err != nil {
			return err
		}
		for _, stage := range job.Stages {
			if strings.Contains(row, stage) {
				return fmt.Errorf("the row puts an errand in %s: %s", stage, row)
			}
		}
		if !strings.Contains(row, " - ") {
			return fmt.Errorf("the row carries no dash where the stage would be: %s", row)
		}
		return nil
	})
}

// numberOfStage is which of the four a stage is, counting from one, so a scenario names the stage
// and the step works out the rest.
func numberOfStage(stage string) int {
	for i, one := range job.Stages {
		if one == stage {
			return i + 1
		}
	}
	return 0
}

// theRowFor is the line the listing printed for the job this scenario declared.
func theRowFor(ctx context.Context) (string, error) {
	one, err := readJob(ctx, 0)
	if err != nil {
		return "", err
	}
	short := display.ShortID(one.GetId())
	for _, line := range strings.Split(toolFrom(ctx).stdout, "\n") {
		if strings.Contains(line, short) {
			return line, nil
		}
	}
	return "", fmt.Errorf("the listing carries no row for %s: %s", short, toolFrom(ctx).stdout)
}

// What a job in a stage that is not built is doing instead. The line is a fact about that job rather
// than about the stage, so a job that has just left ideation and a job working to an approved plan
// are told different things.
func initializeStageWorkSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the reading says the job writes its plan next$`, func(ctx context.Context) error {
		const want = "writes its plan next, and a person approves it before any work starts"
		if out := toolFrom(ctx).stdout; !strings.Contains(out, want) {
			return fmt.Errorf("the reading does not say %q: %s", want, out)
		}
		return nil
	})

	// The moment ideation closes there is no plan at all, so a reader told the job is carrying on
	// under one a person approved is being told about a state no job is in yet.
	sc.Step(`^the reading does not claim a plan nobody approved$`, func(ctx context.Context) error {
		out := toolFrom(ctx).stdout
		if strings.Contains(out, "a person approved") {
			return fmt.Errorf("a job that has written no plan is told a person approved one: %s", out)
		}
		if strings.Contains(out, "plan, approved") {
			return fmt.Errorf("the row carries an approved plan, so this scenario proves nothing: %s", out)
		}
		return nil
	})

	sc.Step(`^the reading says the job carries on under the plan a person approved$`,
		func(ctx context.Context) error {
			const want = "carries on under the plan a person approved"
			if out := toolFrom(ctx).stdout; !strings.Contains(out, want) {
				return fmt.Errorf("the reading does not say %q: %s", want, out)
			}
			return nil
		})
}
