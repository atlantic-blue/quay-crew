package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/cucumber/godog"
)

// designWorld is the last design read or written, and the file a design was written from.
type designWorld struct {
	design   *quaycrewv1.Design
	warnings []string
	file     string
}

type designKey struct{}

func designFrom(ctx context.Context) *designWorld {
	d, _ := ctx.Value(designKey{}).(*designWorld)
	return d
}

// unescape turns the two character sequence a feature file can hold into the byte it stands for. A
// design body is markdown with blank lines in it, and a scenario has to be able to say so on one
// line without the body stopping being the thing under test.
func unescape(text string) string {
	return strings.NewReplacer(`\n`, "\n", `\t`, "\t").Replace(text)
}

func initializeDesignSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, designKey{}, &designWorld{}), nil
	})

	sc.Step(`^the operator reads the project's design$`, func(ctx context.Context) error {
		w, d := worldFrom(ctx), designFrom(ctx)
		resp, err := w.client.GetDesign(ctx, &quaycrewv1.GetDesignRequest{Project: w.projectID})
		w.lastErr = err
		if err != nil {
			return nil
		}
		d.design = resp.GetDesign()
		return nil
	})

	// A project that exists and holds nothing is the normal state, and it is not an error. The
	// difference matters: an error here would make every fresh project look broken.
	sc.Step(`^the project has no design yet$`, func(ctx context.Context) error {
		d := designFrom(ctx)
		if d.design == nil {
			return fmt.Errorf("the read gave no design back at all")
		}
		if d.design.GetBrief() != "" || d.design.GetBody() != "" {
			return fmt.Errorf("the project answered brief %q and body %q, want both empty",
				d.design.GetBrief(), d.design.GetBody())
		}
		if d.design.GetProject() != worldFrom(ctx).projectID {
			return fmt.Errorf("the design names project %q, want %q",
				d.design.GetProject(), worldFrom(ctx).projectID)
		}
		return nil
	})

	sc.Step(`^the operator (?:sets|set) the project's brief to "([^"]*)"$`,
		func(ctx context.Context, brief string) error {
			return setBrief(ctx, unescape(brief))
		})

	sc.Step(`^the project's brief is "([^"]*)"$`, func(ctx context.Context, brief string) error {
		return setBrief(ctx, unescape(brief))
	})

	sc.Step(`^the brief reads "([^"]*)"$`, func(ctx context.Context, want string) error {
		d := designFrom(ctx)
		if d.design == nil {
			return fmt.Errorf("no design has been read, so there is no brief to check")
		}
		if got := d.design.GetBrief(); got != unescape(want) {
			return fmt.Errorf("the brief reads %q, want %q", got, unescape(want))
		}
		return nil
	})

	sc.Step(`^the operator (?:writes|wrote) the project's design as "([^"]*)"$`,
		func(ctx context.Context, body string) error {
			return setDesign(ctx, unescape(body), "")
		})

	sc.Step(`^the session "([^"]*)" (?:writes|wrote) the project's design as "([^"]*)"$`,
		func(ctx context.Context, session, body string) error {
			return setDesign(ctx, unescape(body), session)
		})

	sc.Step(`^the design body reads "([^"]*)"$`, func(ctx context.Context, want string) error {
		d := designFrom(ctx)
		if d.design == nil {
			return fmt.Errorf("no design has been read, so there is no body to check")
		}
		if got := d.design.GetBody(); got != unescape(want) {
			return fmt.Errorf("the design body reads %q, want %q", got, unescape(want))
		}
		return nil
	})

	sc.Step(`^the design says it was written by "([^"]*)"$`, func(ctx context.Context, want string) error {
		d := designFrom(ctx)
		if d.design == nil {
			return fmt.Errorf("no design has been read, so there is nothing to check")
		}
		if got := d.design.GetWrittenBy(); got != want {
			return fmt.Errorf("the design says it was written by %q, want %q", got, want)
		}
		return nil
	})

	sc.Step(`^the operator reads the design of a project that does not exist$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.GetDesign(ctx, &quaycrewv1.GetDesignRequest{Project: "no-such-project"})
		return nil
	})

	sc.Step(`^the operator reads the design without saying which project$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.GetDesign(ctx, &quaycrewv1.GetDesignRequest{})
		return nil
	})

	// A long brief is kept and reported, never refused. Refusing here would lose text that exists
	// only in the call being made.
	sc.Step(`^the operator sets the project's brief to (\d+) characters$`,
		func(ctx context.Context, length int) error {
			return setBrief(ctx, strings.Repeat("b", length))
		})

	sc.Step(`^the operator writes a design of (\d+) characters$`,
		func(ctx context.Context, length int) error {
			return setDesign(ctx, strings.Repeat("d", length), "")
		})

	sc.Step(`^the write warns about the length$`, func(ctx context.Context) error {
		d := designFrom(ctx)
		if len(d.warnings) == 0 {
			return fmt.Errorf("the write warned about nothing, so nobody is told the text is long")
		}
		for _, warning := range d.warnings {
			if strings.Contains(warning, "characters") {
				return nil
			}
		}
		return fmt.Errorf("the warnings say %q, and none of them says a length", d.warnings)
	})

	sc.Step(`^the write warns about nothing$`, func(ctx context.Context) error {
		if warnings := designFrom(ctx).warnings; len(warnings) != 0 {
			return fmt.Errorf("the write warned %q about text that is not long", warnings)
		}
		return nil
	})

	sc.Step(`^the brief is kept whole$`, func(ctx context.Context) error {
		w, d := worldFrom(ctx), designFrom(ctx)
		if d.design == nil {
			return fmt.Errorf("no design came back from the write")
		}
		want := d.design.GetBrief()
		resp, err := w.client.GetDesign(ctx, &quaycrewv1.GetDesignRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		if got := resp.GetDesign().GetBrief(); got != want {
			return fmt.Errorf("the brief was written at %d characters and reads back at %d",
				len(want), len(got))
		}
		return nil
	})

	// The approval: the operator's word about the design as it stands.

	sc.Step(`^the operator (?:approves|approved) the project's design$`, func(ctx context.Context) error {
		w, d := worldFrom(ctx), designFrom(ctx)
		resp, err := w.client.ApproveDesign(ctx, &quaycrewv1.ApproveDesignRequest{Project: w.projectID})
		w.lastErr = err
		if err != nil {
			return nil
		}
		d.design = resp.GetDesign()
		return nil
	})

	sc.Step(`^the design is approved$`, func(ctx context.Context) error {
		d := designFrom(ctx)
		if d.design == nil {
			return fmt.Errorf("no design has been read, so there is nothing to check")
		}
		if !d.design.GetApproved() {
			return fmt.Errorf("the design reads as not approved")
		}
		if d.design.GetApprovedAt() == nil {
			return fmt.Errorf("the design is approved and says no moment, so nothing records when the word was given")
		}
		return nil
	})

	// The moment goes with the word. A row that read as unapproved while keeping the time of a word
	// since taken back would let a later reader believe the approval still stands.
	sc.Step(`^the design is not approved$`, func(ctx context.Context) error {
		d := designFrom(ctx)
		if d.design == nil {
			return fmt.Errorf("no design has been read, so there is nothing to check")
		}
		if d.design.GetApproved() {
			return fmt.Errorf("the design reads as approved, on %v", d.design.GetApprovedAt().AsTime())
		}
		if d.design.GetApprovedAt() != nil {
			return fmt.Errorf("the design is not approved and keeps the moment %v", d.design.GetApprovedAt().AsTime())
		}
		return nil
	})

	// The call carries the driver's token, which is what a session inside a sandbox presents. The
	// scenario reads the design again afterwards, because a refusal that still wrote the row would
	// leave the gate looking closed and standing open.
	sc.Step(`^the driver asks to approve the project's design$`, func(ctx context.Context) error {
		return asDriver(ctx, func(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) error {
			_, err := client.ApproveDesign(ctx, &quaycrewv1.ApproveDesignRequest{
				Project: worldFrom(ctx).projectID})
			return err
		})
	})
	// The steps that drive the real command line tool, as a caller runs it.

	sc.Step(`^a design file saying "([^"]*)"$`, func(ctx context.Context, body string) error {
		d := designFrom(ctx)
		dir, err := os.MkdirTemp("", "krewe-design-")
		if err != nil {
			return err
		}
		d.file = filepath.Join(dir, "design.md")
		return os.WriteFile(d.file, []byte(unescape(body)), 0o600)
	})

	sc.Step(`^the caller sets the project's brief to "([^"]*)"$`, func(ctx context.Context, brief string) error {
		return runTool(ctx, "design", "brief", whereTheProjectIs(ctx), unescape(brief))
	})

	sc.Step(`^the caller reads the project's design$`, func(ctx context.Context) error {
		return runTool(ctx, "design", whereTheProjectIs(ctx))
	})

	sc.Step(`^the caller writes the design from that file$`, func(ctx context.Context) error {
		return runTool(ctx, "design", "set", whereTheProjectIs(ctx), "--file", designFrom(ctx).file)
	})

	sc.Step(`^the caller writes the design without naming a file$`, func(ctx context.Context) error {
		return runTool(ctx, "design", "set", whereTheProjectIs(ctx))
	})

	sc.Step(`^the caller (?:approves|approved) the design$`, func(ctx context.Context) error {
		return runTool(ctx, "design", "approve", whereTheProjectIs(ctx))
	})

	sc.Step(`^the caller wrote the design from that file$`, func(ctx context.Context) error {
		return runTool(ctx, "design", "set", whereTheProjectIs(ctx), "--file", designFrom(ctx).file)
	})
}

// setBrief writes the brief and keeps what came back, so a later step reads the same answer the
// caller got rather than asking again.
func setBrief(ctx context.Context, brief string) error {
	w, d := worldFrom(ctx), designFrom(ctx)
	resp, err := w.client.SetBrief(ctx, &quaycrewv1.SetBriefRequest{Project: w.projectID, Brief: brief})
	w.lastErr = err
	if err != nil {
		return nil
	}
	d.design, d.warnings = resp.GetDesign(), resp.GetWarnings()
	return nil
}

// setDesign writes the body and keeps what came back, for the same reason.
func setDesign(ctx context.Context, body, writtenBy string) error {
	w, d := worldFrom(ctx), designFrom(ctx)
	resp, err := w.client.SetDesign(ctx, &quaycrewv1.SetDesignRequest{
		Project: w.projectID, Body: body, WrittenBy: writtenBy,
	})
	w.lastErr = err
	if err != nil {
		return nil
	}
	d.design, d.warnings = resp.GetDesign(), resp.GetWarnings()
	return nil
}

// The steps about what reaches the session: the summary in its memory file, and the design itself in
// its working directory.

// designFileAt is where the design body sits in a session's working directory, as this process sees
// it on the host.
func designFileAt(ctx context.Context) (string, error) {
	dir, err := sessionWorkingDir(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".krewe", "design.md"), nil
}

// designSectionOf is what sits under the design mark in the session's memory file, and false when
// there is no such section.
func designSectionOf(ctx context.Context) (string, bool, error) {
	body, err := sessionMemory(ctx)
	if err != nil {
		return "", false, err
	}
	found := sandbox.Decompose(body, []string{sandbox.DesignScope, "project", "session"})
	section, said := found[sandbox.DesignScope]
	return section, said, nil
}

func initializeDesignRenderSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the project's design is "([^"]*)"$`, func(ctx context.Context, body string) error {
		return setDesign(ctx, unescape(body), "")
	})

	sc.Step(`^the project's brief is (\d+) characters$`, func(ctx context.Context, length int) error {
		return setBrief(ctx, strings.Repeat("b", length))
	})

	sc.Step(`^the session's design file reads "([^"]*)"$`, func(ctx context.Context, want string) error {
		at, err := designFileAt(ctx)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(at)
		if err != nil {
			return fmt.Errorf("the session has no design at %s: %w", at, err)
		}
		if string(body) != unescape(want) {
			return fmt.Errorf("the design file reads %q, want %q", body, unescape(want))
		}
		return nil
	})

	sc.Step(`^the session has no design file$`, func(ctx context.Context) error {
		at, err := designFileAt(ctx)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(at)
		if err == nil {
			return fmt.Errorf("the session has a design at %s saying %q, and the store holds none", at, body)
		}
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	})

	sc.Step(`^the session's memory file does not carry "([^"]*)"$`, func(ctx context.Context, unwanted string) error {
		body, err := sessionMemory(ctx)
		if err != nil {
			return err
		}
		if strings.Contains(body, unwanted) {
			return fmt.Errorf("the session's memory file carries %q, and it should not: %q", unwanted, body)
		}
		return nil
	})

	sc.Step(`^the session's memory file carries no design section$`, func(ctx context.Context) error {
		_, said, err := designSectionOf(ctx)
		if err != nil {
			return err
		}
		if said {
			return fmt.Errorf("a project with no design put a design section in the memory file anyway")
		}
		return nil
	})

	sc.Step(`^the design section is under (\d+) characters$`, func(ctx context.Context, cap int) error {
		section, said, err := designSectionOf(ctx)
		if err != nil {
			return err
		}
		if !said {
			return fmt.Errorf("there is no design section to measure")
		}
		if got := utf8.RuneCountInString(section); got >= cap {
			return fmt.Errorf("the design section is %d characters, want under %d: %q", got, cap, section)
		}
		return nil
	})

	// Counted rather than read, because the failure this guards against is the section appearing a
	// second time underneath itself, which a check for the text passes.
	sc.Step(`^the memory file carries one design section$`, func(ctx context.Context) error {
		body, err := sessionMemory(ctx)
		if err != nil {
			return err
		}
		if got := strings.Count(body, "This project is "); got != 1 {
			return fmt.Errorf("the memory file names the project %d times, want 1: %q", got, body)
		}
		return nil
	})

	// The section is rendered state. Swept into the session's own context it would be stored as
	// though a person typed it, and rendered again underneath itself on every exec after that.
	sc.Step(`^the session's context does not carry the design section$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastExec()
		if err != nil {
			return err
		}
		resp, err := w.client.ListContexts(ctx, &quaycrewv1.ListContextsRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		for _, dir := range resp.GetDirs() {
			if dir.GetScope() != "session" && dir.GetOwner() != current.sessionID {
				continue
			}
			if strings.Contains(dir.GetBody(), "This project is ") {
				return fmt.Errorf("the %s level was taught the design section: %q", dir.GetScope(), dir.GetBody())
			}
		}
		return nil
	})
}
