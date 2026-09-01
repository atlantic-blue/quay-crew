package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/skill"
	"github.com/cucumber/godog"
)

// The skill that says to prove the riskiest assumption where it has to hold. What is proved here is
// that it reaches a job designing something without anybody attaching it, and that the rule is
// readable at the path the session is given, rather than only in the store.
func initializeProvingSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the session doing that job holds the "([^"]*)" skill$`,
		func(ctx context.Context, name string) error {
			session, err := sessionDoingTheJob(ctx)
			if err != nil {
				return err
			}
			listed, err := worldFrom(ctx).client.ListSkills(ctx,
				&quaycrewv1.ListSkillsRequest{Session: session.GetId()})
			if err != nil {
				return err
			}
			held := make([]string, 0, len(listed.GetSkills()))
			for _, one := range listed.GetSkills() {
				held = append(held, one.GetName())
				if one.GetName() != name {
					continue
				}
				if left := one.GetLeftOut(); left != "" {
					return fmt.Errorf("the %s skill was left out of that session: %s", name, left)
				}
				return nil
			}
			return fmt.Errorf("the session doing that job holds %v, and none of them is %s", held, name)
		})

	// Read where the session reads it: the file under the workspace's own skills directory, which is
	// the source of the read only mount into the container. A rule that stops in the store is a rule
	// nothing ever opens.
	sc.Step(`^the "([^"]*)" brief the session reads says "([^"]*)"$`,
		func(ctx context.Context, name, said string) error {
			at := filepath.Join(workspaceSkillDir(ctx, name), skill.BriefFile)
			body, err := os.ReadFile(at)
			if err != nil {
				return fmt.Errorf("the brief the index points at is not there: %w", err)
			}
			if !strings.Contains(string(body), said) {
				return fmt.Errorf("%s never says %q, so a design following it need not carry that at all",
					at, said)
			}
			return nil
		})

	sc.Step(`^the listing says the "([^"]*)" skill was left out of nothing$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			if w.lastSkills == nil {
				return fmt.Errorf("nothing has been listed")
			}
			for _, one := range w.lastSkills.GetSkills() {
				if one.GetName() != name {
					continue
				}
				if left := one.GetLeftOut(); left != "" {
					return fmt.Errorf("the %s skill is left out of this workspace's sessions: %s", name, left)
				}
				return nil
			}
			return fmt.Errorf("the listing does not carry the %s skill at all", name)
		})
}
