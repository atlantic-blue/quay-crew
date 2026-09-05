package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// pathWorld is the last path written or read, the warnings that came with the write, and the file a
// path was written from.
type pathWorld struct {
	steps    []*quaycrewv1.Step
	warnings []string
	file     string
}

type pathKey struct{}

func pathFrom(ctx context.Context) *pathWorld {
	p, _ := ctx.Value(pathKey{}).(*pathWorld)
	return p
}

// stepNumbered finds one step of the path last read. It is the read the assertions go through, so a
// missing step reports which numbers are there rather than an index out of range.
func stepNumbered(ctx context.Context, number int32) (*quaycrewv1.Step, error) {
	p := pathFrom(ctx)
	for _, step := range p.steps {
		if step.GetNumber() == number {
			return step, nil
		}
	}
	var held []string
	for _, step := range p.steps {
		held = append(held, fmt.Sprintf("%d", step.GetNumber()))
	}
	return nil, fmt.Errorf("the path has no step %d, it holds %s", number, strings.Join(held, ", "))
}

func initializePathSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, pathKey{}, &pathWorld{}), nil
	})

	sc.Step(`^the operator sets the path to:$`, func(ctx context.Context, document *godog.DocString) error {
		return setPath(ctx, document.Content)
	})

	sc.Step(`^the project's path is:$`, func(ctx context.Context, document *godog.DocString) error {
		return setPath(ctx, document.Content)
	})

	// The driver's own token, which is what a session inside a sandbox presents. A design session is
	// what writes a path, so this call is not one the operator keeps.
	sc.Step(`^the driver sets the path to:$`, func(ctx context.Context, document *godog.DocString) error {
		w := worldFrom(ctx)
		return asDriver(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			_, err := client.SetPath(ctx, &quaycrewv1.SetPathRequest{
				Project: w.projectID, Document: document.Content})
			return err
		})
	})

	sc.Step(`^the operator sets a path without saying which project$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.SetPath(ctx, &quaycrewv1.SetPathRequest{
			Document: "## 1. The store holds a project's brief"})
		return nil
	})

	sc.Step(`^the operator reads the path$`, func(ctx context.Context) error {
		w, p := worldFrom(ctx), pathFrom(ctx)
		resp, err := w.client.ListSteps(ctx, &quaycrewv1.ListStepsRequest{Project: w.projectID})
		w.lastErr = err
		if err != nil {
			return nil
		}
		p.steps = resp.GetSteps()
		return nil
	})

	sc.Step(`^the operator reads the path of a project that does not exist$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.ListSteps(ctx, &quaycrewv1.ListStepsRequest{Project: "no-such-project"})
		return nil
	})

	sc.Step(`^the path holds (\d+) steps$`, func(ctx context.Context, want int) error {
		if got := len(pathFrom(ctx).steps); got != want {
			return fmt.Errorf("the path holds %d steps, want %d", got, want)
		}
		return nil
	})

	// Order is what the control plane promises, so it is asserted as an order and not as a set: a
	// check that every number is present passes against a path drawn in the wrong order.
	sc.Step(`^the path reads ([\d, ]+) in that order$`, func(ctx context.Context, wanted string) error {
		var got []string
		for _, step := range pathFrom(ctx).steps {
			got = append(got, fmt.Sprintf("%d", step.GetNumber()))
		}
		if reads := strings.Join(got, ", "); reads != wanted {
			return fmt.Errorf("the path reads %s, want %s", reads, wanted)
		}
		return nil
	})

	sc.Step(`^the project has no path$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		resp, err := w.client.ListSteps(ctx, &quaycrewv1.ListStepsRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		if got := len(resp.GetSteps()); got != 0 {
			return fmt.Errorf("the project holds %d steps, and the document was refused", got)
		}
		return nil
	})

	sc.Step(`^step (\d+) is titled "([^"]*)"$`, func(ctx context.Context, number int, want string) error {
		step, err := stepNumbered(ctx, int32(number))
		if err != nil {
			return err
		}
		if got := step.GetTitle(); got != want {
			return fmt.Errorf("step %d is titled %q, want %q", number, got, want)
		}
		return nil
	})

	sc.Step(`^step (\d+) says its intention is "([^"]*)"$`, func(ctx context.Context, number int, want string) error {
		step, err := stepNumbered(ctx, int32(number))
		if err != nil {
			return err
		}
		if got := step.GetIntention(); got != unescape(want) {
			return fmt.Errorf("step %d says its intention is %q, want %q", number, got, unescape(want))
		}
		return nil
	})

	// Line breaks and all, because the take reads this field line by line and a field joined into one
	// line would name one file nobody has.
	sc.Step(`^step (\d+) touches "([^"]*)"$`, func(ctx context.Context, number int, want string) error {
		step, err := stepNumbered(ctx, int32(number))
		if err != nil {
			return err
		}
		if got := step.GetTouches(); got != unescape(want) {
			return fmt.Errorf("step %d touches %q, want %q", number, got, unescape(want))
		}
		return nil
	})

	sc.Step(`^step (\d+) says its proof is "([^"]*)"$`, func(ctx context.Context, number int, want string) error {
		step, err := stepNumbered(ctx, int32(number))
		if err != nil {
			return err
		}
		if got := step.GetProof(); got != unescape(want) {
			return fmt.Errorf("step %d says its proof is %q, want %q", number, got, unescape(want))
		}
		return nil
	})

	sc.Step(`^step (\d+) names the scenario "([^"]*)"$`, func(ctx context.Context, number int, want string) error {
		step, err := stepNumbered(ctx, int32(number))
		if err != nil {
			return err
		}
		if got := step.GetProofScenario(); got != want {
			return fmt.Errorf("step %d names the scenario %q, want %q", number, got, want)
		}
		return nil
	})

	sc.Step(`^step (\d+) waits for step (\d+)$`, func(ctx context.Context, number, want int) error {
		step, err := stepNumbered(ctx, int32(number))
		if err != nil {
			return err
		}
		if got := step.GetAfter(); got != int32(want) {
			return fmt.Errorf("step %d waits for step %d, want %d", number, got, want)
		}
		return nil
	})

	sc.Step(`^step (\d+) is ready$`, func(ctx context.Context, number int) error {
		step, err := stepNumbered(ctx, int32(number))
		if err != nil {
			return err
		}
		if got := step.GetState(); got != "ready" {
			return fmt.Errorf("step %d reads as %q, and nobody has taken it", number, got)
		}
		if step.GetSession() != "" || step.GetTakenAt() != nil {
			return fmt.Errorf("step %d names session %q, taken at %v, and nobody has taken it",
				number, step.GetSession(), step.GetTakenAt())
		}
		return nil
	})

	// The warnings. None of them refuses a document, so every one of these reads the path back too.

	sc.Step(`^the path write warns that step (\d+) says nothing under "([^"]*)"$`,
		func(ctx context.Context, number int, label string) error {
			p := pathFrom(ctx)
			want := fmt.Sprintf("step %d says nothing under", number)
			for _, warning := range p.warnings {
				if strings.Contains(warning, want) && strings.Contains(warning, label) {
					return nil
				}
			}
			return fmt.Errorf("the write warned %q, and none of it says step %d left %q empty",
				p.warnings, number, label)
		})

	sc.Step(`^the path write warns "([^"]*)"$`, func(ctx context.Context, want string) error {
		p := pathFrom(ctx)
		for _, warning := range p.warnings {
			if strings.Contains(warning, want) {
				return nil
			}
		}
		return fmt.Errorf("the write warned %q, and none of it says %q", p.warnings, want)
	})

	sc.Step(`^the path write warns about nothing$`, func(ctx context.Context) error {
		if warnings := pathFrom(ctx).warnings; len(warnings) != 0 {
			return fmt.Errorf("the write warned %q about a path that says everything", warnings)
		}
		return nil
	})

	// The refusals. Each one names the line, because a document is a file somebody has to go and fix.

	sc.Step(`^the refusal names line (\d+)$`, func(ctx context.Context, line int) error {
		return refusalNamesLines(ctx, line)
	})

	sc.Step(`^the refusal names lines (\d+) and (\d+)$`, func(ctx context.Context, first, second int) error {
		return refusalNamesLines(ctx, first, second)
	})

	// The steps that drive the real command line tool, as a caller runs it.

	sc.Step(`^a path file saying:$`, func(ctx context.Context, document *godog.DocString) error {
		p := pathFrom(ctx)
		dir, err := os.MkdirTemp("", "krewe-path-")
		if err != nil {
			return err
		}
		p.file = filepath.Join(dir, "path.md")
		return os.WriteFile(p.file, []byte(document.Content), 0o600)
	})

	sc.Step(`^the caller (?:writes|wrote) the path from that file$`, func(ctx context.Context) error {
		return runTool(ctx, "path", "set", whereTheProjectIs(ctx), "--file", pathFrom(ctx).file)
	})

	sc.Step(`^the caller writes the path without naming a file$`, func(ctx context.Context) error {
		return runTool(ctx, "path", "set", whereTheProjectIs(ctx))
	})

	sc.Step(`^the caller reads the path$`, func(ctx context.Context) error {
		return runTool(ctx, "path", whereTheProjectIs(ctx))
	})

	// Counted off the printed lines rather than asked of the system again, because what this proves
	// is what the operator is looking at.
	sc.Step(`^standard output lists (\d+) steps in number order$`, func(ctx context.Context, want int) error {
		var numbers []string
		for _, line := range strings.Split(toolFrom(ctx).stdout, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 || fields[0] == "STEP" {
				continue
			}
			numbers = append(numbers, fields[0])
		}
		if len(numbers) != want {
			return fmt.Errorf("standard output lists %d steps, want %d: %q",
				len(numbers), want, toolFrom(ctx).stdout)
		}
		for at := 1; at < len(numbers); at++ {
			if numbers[at-1] >= numbers[at] {
				return fmt.Errorf("the listing reads %s, which is not number order", strings.Join(numbers, ", "))
			}
		}
		return nil
	})
}

// setPath writes the document and keeps what came back, so a later step reads the same answer the
// caller got rather than asking again.
func setPath(ctx context.Context, document string) error {
	w, p := worldFrom(ctx), pathFrom(ctx)
	resp, err := w.client.SetPath(ctx, &quaycrewv1.SetPathRequest{
		Project: w.projectID, Document: document,
	})
	w.lastErr = err
	if err != nil {
		return nil
	}
	p.steps, p.warnings = resp.GetSteps(), resp.GetWarnings()
	return nil
}

// refusalNamesLines holds a refusal to naming every line it has to name. A refusal that named one of
// two duplicate numbers would send a person to the line that is fine.
func refusalNamesLines(ctx context.Context, lines ...int) error {
	err := worldFrom(ctx).lastErr
	if err == nil {
		return fmt.Errorf("nothing was refused")
	}
	for _, line := range lines {
		if !strings.Contains(err.Error(), fmt.Sprintf("line %d", line)) {
			return fmt.Errorf("the refusal is %q, want it to name line %d", err.Error(), line)
		}
	}
	return nil
}

// The steps about what reaches the session: the path document in its working directory.

// pathFileAt is where the path sits in a session's working directory, as this process sees it on the
// host.
func pathFileAt(ctx context.Context) (string, error) {
	dir, err := sessionWorkingDir(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".krewe", "path.md"), nil
}

func sessionPathFile(ctx context.Context) (string, error) {
	at, err := pathFileAt(ctx)
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(at)
	if err != nil {
		return "", fmt.Errorf("the session has no path at %s: %w", at, err)
	}
	return string(body), nil
}

// pathHeading matches a step's own line in the rendered document, which is the same line the grammar
// reads.
var pathHeading = regexp.MustCompile(`(?m)^##\s+(\d+)\.`)

func initializePathRenderSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the session's path file carries "([^"]*)"$`, func(ctx context.Context, want string) error {
		body, err := sessionPathFile(ctx)
		if err != nil {
			return err
		}
		if !strings.Contains(body, unescape(want)) {
			return fmt.Errorf("the path file does not carry %q: %q", unescape(want), body)
		}
		return nil
	})

	sc.Step(`^the session's path file does not carry "([^"]*)"$`, func(ctx context.Context, unwanted string) error {
		body, err := sessionPathFile(ctx)
		if err != nil {
			return err
		}
		if strings.Contains(body, unescape(unwanted)) {
			return fmt.Errorf("the path file carries %q, and it should not: %q", unescape(unwanted), body)
		}
		return nil
	})

	// Read as the list of headings rather than as a search for each number, because the failure this
	// guards against is the right steps in the wrong order, which a search for the text passes.
	sc.Step(`^the session's path file lists steps ([\d, ]+) in that order$`,
		func(ctx context.Context, want string) error {
			body, err := sessionPathFile(ctx)
			if err != nil {
				return err
			}
			var numbers []string
			for _, found := range pathHeading.FindAllStringSubmatch(body, -1) {
				numbers = append(numbers, found[1])
			}
			if got := strings.Join(numbers, ", "); got != strings.TrimSpace(want) {
				return fmt.Errorf("the path file lists steps %s, want %s: %q", got, want, body)
			}
			return nil
		})

	sc.Step(`^the session has no path file$`, func(ctx context.Context) error {
		at, err := pathFileAt(ctx)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(at)
		if err == nil {
			return fmt.Errorf("the session has a path at %s saying %q, and the store holds none", at, body)
		}
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	})
}
