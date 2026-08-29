package features_test

import (
	"context"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/role"
	"github.com/cucumber/godog"
)

// Steps for the one word that declares intent, and for the word it replaced.
//
// They run the real tool through the harness in tool_steps_test.go, because what is specified here
// is what a caller receives: which stream a thing went to, and what the exit status was. A refusal
// that exits zero is the failure this specification exists to catch, and neither the stream nor the
// status exists inside the test process.

// theWordThatWent is the word declared intent used to carry. It is named once here so a scenario
// driving it and a scenario asserting on the advice cannot drift apart.
const theWordThatWent = "work"

func initializeJobWordSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the caller declares a job through the tool, titled "([^"]*)"$`,
		func(ctx context.Context, title string) error {
			return runTool(ctx, "job", "create", whereTheProjectIs(ctx),
				"--title", title, "--brief", "open it and say when it is due")
		})

	// The whole command a person would have typed before the rename, flags and all. The flags are
	// what makes it worth its own scenario: a refusal that blames one of them sends the operator to
	// correct part of a command that is gone whole.
	sc.Step(`^the caller declares a job through the tool with the word that went$`,
		func(ctx context.Context) error {
			return runTool(ctx, theWordThatWent, "create", whereTheProjectIs(ctx),
				"--title", "read the electricity bill", "--brief", "open it and say when it is due")
		})

	sc.Step(`^standard output says it is declared$`, func(ctx context.Context) error {
		return says("standard output", toolFrom(ctx).stdout, "declared")
	})

	// The role manifests. A role is a file in somebody's repository, so both halves of the old
	// vocabulary are still written down somewhere and both are refused at the door.
	sc.Step(`^the operator imports a role that receives "([^"]*)"$`,
		func(ctx context.Context, material string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
				Files: roleFilesReceiving("backlog-clearer", material),
			})
			return nil
		})

	sc.Step(`^the operator imports a role that may "([^"]*)"$`,
		func(ctx context.Context, verb string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
				Files: roleFilesThatMay("backlog-clearer", []string{verb}),
			})
			return nil
		})

	sc.Step(`^the crew holds no such role$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListRoles(ctx, &quaycrewv1.ListRolesRequest{})
		if err != nil {
			return err
		}
		if len(listed.GetRoles()) != 0 {
			return fmt.Errorf("%d roles are held, so a refused manifest was imported anyway",
				len(listed.GetRoles()))
		}
		return nil
	})
}

// roleFilesReceiving is a manifest asking for one kind of material and nothing else, which is how a
// manifest written before the rename asks for the job it is given.
func roleFilesReceiving(name, material string) []*quaycrewv1.RoleFile {
	manifest := fmt.Sprintf("name: %s\nversion: 1\nsummary: clears the backlog\nmodel: opus\nreceives:\n  - %s\n",
		name, material)
	return []*quaycrewv1.RoleFile{
		{Path: role.ManifestFile, Body: []byte(manifest)},
		{Path: role.BriefFile, Body: []byte("Read the open pull requests.")},
	}
}
