package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/quay-krewe/internal/skill"
	"github.com/cucumber/godog"
)

// The rule that a piece of infrastructure is not ready until the identity that will apply it has
// been asked whether it may. It is a skill, so most of what holds it up is the skills suite already;
// these are the two questions that are about this rule and not about skills in general.
func initializeDeployIdentitySteps(sc *godog.ScenarioContext) {
	// Read where the session reads it, which is the copy written out of the store and mounted into
	// the sandbox, rather than the file in this repository. A rule the model cannot open is a name in
	// an index and nothing else.
	sc.Step(`^the "([^"]*)" brief the session can open says "([^"]*)"$`,
		func(ctx context.Context, name, said string) error {
			at := filepath.Join(workspaceSkillDir(ctx, name), skill.BriefFile)
			body, err := os.ReadFile(at) //nolint:gosec // the path is the suite's own temporary directory
			if err != nil {
				return fmt.Errorf("the %s brief is not where the session would read it: %w", name, err)
			}
			if !strings.Contains(string(body), said) {
				return fmt.Errorf("the %s brief never says %q:\n%s", name, said, body)
			}
			return nil
		})

	sc.Step(`^the listing does not say the "([^"]*)" skill was left out$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			if w.lastSkills == nil {
				return fmt.Errorf("nothing has been listed")
			}
			for _, one := range w.lastSkills.GetSkills() {
				if one.GetName() != name {
					continue
				}
				if reason := one.GetLeftOut(); reason != "" {
					return fmt.Errorf("the session was not given the %s skill: %s", name, reason)
				}
				return nil
			}
			return fmt.Errorf("the listing does not carry the %s skill at all", name)
		})
}
