package features_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/role"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/cucumber/godog"
)

// The role that reads a plan before anybody builds it.
//
// The brief is the whole of this role, so the steps read what the session was actually told rather
// than what the manifest says: the file on the host that the model opens, written from the version
// the workspace pinned. A role imported and never rendered proves nothing about what a session runs
// under.

// toldThePlanCritic is a clause the brief has to carry, said the way a scenario says it. The phrase
// is what is checked, so a brief rewritten around it keeps the scenario honest and a brief that
// drops it fails.
var toldThePlanCritic = map[string][]string{
	"the seven classes a finding can be": {
		"Does not serve the sentence", "Duplication", "Ambiguity", "Underspecification",
		"Conflict with the declared standards", "Coverage gap", "Inconsistency",
	},
	"to say where each finding is": {"Where it is.", "the heading or the section number"},
	"to change no file":            {"You change no file"},
	"to report a requirement the sentence does not ask for": {
		"A requirement the plan carries that the sentence does not ask", "the plan and the sentence disagree",
	},
	"to say so in one line where the plan holds up": {"Say so, in one line"},
	"to invent no finding":                          {"Never invent a finding"},
}

func initializePlanCriticSteps(sc *godog.ScenarioContext) {
	// The same role at a version that receives less, attached while the job sits pending. It keeps
	// the shipped summary, model and brief, so the one thing that changed between the two versions is
	// the thing the refusal turns on.
	sc.Step(`^the operator narrows the "([^"]*)" role so it no longer receives the context$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			onDisk, err := role.One(filepath.Join(shippedRoles, name))
			if err != nil {
				return err
			}
			manifest := fmt.Sprintf("name: %s\nversion: %d\nsummary: %s\nmodel: %s\nreceives:\n  - job\n  - skills\n",
				onDisk.Name, onDisk.Version+1, onDisk.Summary, onDisk.Model)
			if _, err := w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
				Files: []*quaycrewv1.RoleFile{
					{Path: role.ManifestFile, Body: []byte(manifest)},
					{Path: role.BriefFile, Body: []byte(onDisk.Brief)},
				},
			}); err != nil {
				return err
			}
			_, err = w.client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
				Workspace: w.workspaceID, Name: name,
			})
			return err
		})

	sc.Step(`^a job titled "([^"]*)" in the role "([^"]*)" saying a person "([^"]*)"$`,
		func(ctx context.Context, title, named, sentence string) error {
			return declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: title, Brief: "read the design, the contracts and the build order",
				Role: named, Requires: []string{role.MaterialContext}, Product: sentence,
			})
		})

	sc.Step(`^that session is told (.+)$`, func(ctx context.Context, clause string) error {
		wanted, known := toldThePlanCritic[clause]
		if !known {
			return fmt.Errorf("no scenario clause is written down for %q", clause)
		}
		body, err := memoryOfTheSessionDoingTheJob(ctx)
		if err != nil {
			return err
		}
		for _, want := range wanted {
			if !strings.Contains(body, want) {
				return fmt.Errorf("the session's memory file does not say %q, so it was not told %s", want, clause)
			}
		}
		return nil
	})

	sc.Step(`^the role comes back with a brief naming its source and its licence$`, func(ctx context.Context) error {
		read := worldFrom(ctx).lastRole
		if read == nil {
			return fmt.Errorf("no role was read back")
		}
		for _, want := range []string{
			"github/spec-kit", "templates/commands/analyze.md", "templates/commands/checklist.md",
			"MIT", "GitHub, Inc.", "https://github.com/github/spec-kit/blob/main/LICENSE",
		} {
			if !strings.Contains(read.GetBrief(), want) {
				return fmt.Errorf("the brief that came back does not record %q, and a reader of the role has "+
					"nowhere else to look", want)
			}
		}
		return nil
	})
}

// memoryOfTheSessionDoingTheJob is the file the model opens, in the container the system built for
// the session that job runs in. The brief is rendered into it every task and never read back, so
// this is where "the session was told the role" is finally true rather than claimed.
//
// It waits, because a tick starts the job and returns: the session, the container and the file it
// holds all arrive after it. Without the wait the step reads a container that is on its way and
// reports the brief as missing.
func memoryOfTheSessionDoingTheJob(ctx context.Context) (string, error) {
	deadline := time.Now().Add(5 * time.Second)
	var last error
	for {
		body, err := renderedBriefOfTheJob(ctx)
		if err == nil {
			return body, nil
		}
		last = err
		if time.Now().After(deadline) {
			return "", last
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func renderedBriefOfTheJob(ctx context.Context) (string, error) {
	session, err := sessionDoingTheJob(ctx)
	if err != nil {
		return "", err
	}
	w := worldFrom(ctx)
	for _, built := range w.provider.Created {
		if built.ID != session.GetId() {
			continue
		}
		dirs := w.storage.MyDirs(built)
		if len(dirs) == 0 {
			return "", fmt.Errorf("the session doing the job has no memory directory")
		}
		body, found := sandbox.ReadMemory(dirs[0])
		if !found {
			return "", fmt.Errorf("nothing was written to the memory file of the session doing the job")
		}
		// An empty file carries no clause at all, and no clause reads exactly like a brief that was
		// never rendered.
		if len(body) < briefIsAtLeast {
			return "", fmt.Errorf("the memory file of the session doing the job is %d bytes, which is too "+
				"short to be a brief", len(body))
		}
		return body, nil
	}
	return "", fmt.Errorf("no sandbox was built for the session doing the job")
}
