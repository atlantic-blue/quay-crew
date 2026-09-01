package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/name"
	"github.com/cucumber/godog"
)

// Steps for the word that names the level above every workspace, and for the word it replaced.
//
// They run the real tool through the harness in tool_steps_test.go, because what is specified here
// is what a caller receives: which stream the refusal went to, and what the exit status was. A
// refusal that exits zero is the failure this specification exists to catch, and neither the stream
// nor the status exists inside the test process.

// theLevelWordThatWent is the word this level carried before it was called the system. Named once
// here so a scenario driving it and a scenario asserting on the advice cannot drift apart.
const theLevelWordThatWent = "crew"

func initializeLevelWordSteps(sc *godog.ScenarioContext) {
	// The whole command as a person would type it, split the way a shell splits it. A table of whole
	// commands reads as the thing somebody has in their fingers, which is what this is about.
	sc.Step(`^the caller types "([^"]*)" through the tool$`, func(ctx context.Context, typed string) error {
		return runTool(ctx, strings.Fields(typed)...)
	})

	// The same, with the value piped in, which is how a secret is set without the value reaching a
	// shell history.
	sc.Step(`^the caller types "([^"]*)" through the tool, piping in "([^"]*)"$`,
		func(ctx context.Context, typed, in string) error {
			return runToolSaying(ctx, in, strings.Fields(typed)...)
		})

	sc.Step(`^standard output says "([^"]*)"$`, func(ctx context.Context, want string) error {
		return says("standard output", toolFrom(ctx).stdout, want)
	})

	// The refusal itself, word for word, and not a message that merely carries both words.
	//
	// A listing given a workspace it cannot find already answers "this system has no workspace
	// \"crew\"", which says crew and says system and says nothing about the word having moved. An
	// assertion looking for the two words separately passed against exactly that, with the refusal
	// taken out of the code entirely.
	sc.Step(`^standard error says the word is now "([^"]*)"$`, func(ctx context.Context, want string) error {
		return says("standard error", toolFrom(ctx).stderr, fmt.Sprintf("The word is now %q", want))
	})

	sc.Step(`^standard error says the word moved$`, func(ctx context.Context) error {
		return says("standard error", toolFrom(ctx).stderr, name.RefuseRetired(theLevelWordThatWent).Error())
	})

	// The refusal that names the word, rather than the general one about names, whose advice is the
	// typed name lowercased and therefore a name this refuses.
	sc.Step(`^standard error never says "([^"]*)"$`, func(ctx context.Context, never string) error {
		if strings.Contains(toolFrom(ctx).stderr, never) {
			return fmt.Errorf("standard error says %q, which is the refusal that advises typing a reserved word", never)
		}
		return nil
	})
	sc.Step(`^the caller asks for the manual$`, func(ctx context.Context) error {
		return runTool(ctx, "manual")
	})

	sc.Step(`^standard output never says "([^"]*)"$`, func(ctx context.Context, never string) error {
		if strings.Contains(toolFrom(ctx).stdout, never) {
			return fmt.Errorf("standard output still says %q, so it teaches a word that is refused", never)
		}
		return nil
	})

	// Behind the refusal, which is the half a refusal cannot prove about itself.
	sc.Step(`^the system holds no secret called "([^"]*)"$`, func(ctx context.Context, named string) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListSecrets(ctx, &quaycrewv1.ListSecretsRequest{})
		if err != nil {
			return err
		}
		for _, secret := range listed.GetSecrets() {
			if secret.GetName() == named {
				return fmt.Errorf("%q was set anyway, on %q, so the refusal was only words",
					named, theLevelWordThatWent)
			}
		}
		return nil
	})
}
