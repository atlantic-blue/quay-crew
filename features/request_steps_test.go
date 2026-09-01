package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/cucumber/godog"
)

// A brief is held against the request that produced it.
//
// Two texts stand for the two halves of the measured failure, and both are here rather than in a
// step argument, because a scenario that carries a paragraph in a quoted string is a scenario nobody
// reads.

// aFaithfulBrief carries what was asked for: somebody pastes a link and reads the text back.
const aFaithfulBrief = "Build the page a reader pastes a YouTube link into. It fetches the transcript " +
	"for that link and renders the text back on the same page. Where there is no transcript, say so."

// aDriftedBrief is the design the system was actually given, which every job then built faithfully.
const aDriftedBrief = "Build a page that serves a transcript archive. The address reads " +
	"/videos?id=<video id>, and the video identifier is the key the store is read by. A reader " +
	"supplies the identifier and the page renders the stored transcript for it."

// theWordsTheDriftedBriefNeverSays are the words of that request the design dropped. They are what a
// person reads, so they are what the assertions look for: a number tells nobody what to do next.
var theWordsTheDriftedBriefNeverSays = []string{"paste", "link", "text"}

func initializeRequestSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the caller declares a job asked for as "([^"]*)" with a brief that serves it$`,
		func(ctx context.Context, asked string) error {
			return declareAskedFor(ctx, asked, aFaithfulBrief)
		})

	sc.Step(`^the caller declares a job asked for as "([^"]*)" with a brief about video identifiers$`,
		func(ctx context.Context, asked string) error {
			return declareAskedFor(ctx, asked, aDriftedBrief)
		})

	sc.Step(`^the system says nothing about the brief drifting$`, func(ctx context.Context) error {
		if drifted := jobFrom(ctx).drifted; drifted != "" {
			return fmt.Errorf("the system spoke about a brief that carries what was asked for: %s", drifted)
		}
		return nil
	})

	sc.Step(`^the system names the words the brief never says$`, func(ctx context.Context) error {
		drifted := jobFrom(ctx).drifted
		if drifted == "" {
			return fmt.Errorf("the brief that cost two days was declared in silence")
		}
		return namesEveryWord(drifted, "the answer")
	})

	sc.Step(`^the job carries the request "([^"]*)"$`, func(ctx context.Context, asked string) error {
		one, err := readTheJobBack(ctx)
		if err != nil {
			return err
		}
		if one.GetRequest() != asked {
			return fmt.Errorf("the job was asked for in the words %q, want %q", one.GetRequest(), asked)
		}
		return nil
	})

	sc.Step(`^the session doing that job is told what was asked for$`, func(ctx context.Context) error {
		task, err := whatTheSessionIsAsked(ctx)
		if err != nil {
			return err
		}
		asked, brief := strings.Index(task, "paste a youtube link"), strings.Index(task, "Build a page")
		if asked < 0 {
			return fmt.Errorf("the session is never told what was asked for: %s", task)
		}
		if brief >= 0 && asked > brief {
			return fmt.Errorf("the request came after the brief rather than above it: %s", task)
		}
		return nil
	})

	sc.Step(`^the session doing that job is told which words its brief never says$`,
		func(ctx context.Context) error {
			task, err := whatTheSessionIsAsked(ctx)
			if err != nil {
				return err
			}
			if !strings.Contains(task, "the brief says nothing about") {
				return fmt.Errorf("the session was never told its brief dropped words: %s", task)
			}
			return namesEveryWord(task, "the task")
		})

	sc.Step(`^the caller declares a (drifting|faithful) job through the tool$`,
		func(ctx context.Context, which string) error {
			brief := aFaithfulBrief
			if which == "drifting" {
				brief = aDriftedBrief
			}
			return runTool(ctx, "job", "create", whereTheProjectIs(ctx),
				"--title", "the transcript page", "--brief", brief,
				"--request", "paste a youtube link and get the text back")
		})

	sc.Step(`^the caller reads that job through the tool$`, func(ctx context.Context) error {
		// The identifier the tool printed, read out of what a person would have read.
		id := theIdentifierTheToolDeclared(toolFrom(ctx).stdout)
		if id == "" {
			return fmt.Errorf("the declaration printed no identifier to read back: %s", toolFrom(ctx).stdout)
		}
		return runTool(ctx, "job", "show", id)
	})

	sc.Step(`^standard output names the words the brief never says$`, func(ctx context.Context) error {
		return namesEveryWord(toolFrom(ctx).stdout, "standard output")
	})

	sc.Step(`^standard output says nothing about the brief drifting$`, func(ctx context.Context) error {
		out := toolFrom(ctx).stdout
		if strings.Contains(out, "does not say what the request says") {
			return fmt.Errorf("the tool spoke about a brief that carries what was asked for: %s", out)
		}
		return nil
	})

	sc.Step(`^standard output carries the request as it was asked$`, func(ctx context.Context) error {
		out := toolFrom(ctx).stdout
		if !strings.Contains(out, "paste a youtube link and get the text back") {
			return fmt.Errorf("reading the job back never says what was asked for: %s", out)
		}
		return nil
	})

	sc.Step(`^the tool exits successfully$`, func(ctx context.Context) error {
		if t := toolFrom(ctx); t.exitCode != 0 {
			return fmt.Errorf("the tool exited %d, saying %q", t.exitCode, t.stderr)
		}
		return nil
	})
}

// declareAskedFor declares one job with the request as it was said and the brief somebody wrote from
// it, and keeps what the system answered so a scenario can assert on the silence as well as on the
// speech.
func declareAskedFor(ctx context.Context, asked, brief string) error {
	if err := declareJob(ctx, &quaycrewv1.CreateJobRequest{
		Title: "the transcript page", Brief: brief, Request: asked,
	}); err != nil {
		return err
	}
	return worldFrom(ctx).lastErr
}

// namesEveryWord holds a piece of text to every word the drifted brief dropped. Every one of them,
// because a report that names one of three has told the reader almost nothing.
func namesEveryWord(text, where string) error {
	for _, word := range theWordsTheDriftedBriefNeverSays {
		if !strings.Contains(text, word) {
			return fmt.Errorf("%s does not name %q, which the brief never says: %s", where, word, text)
		}
	}
	return nil
}

// readTheJobBack is the job as the system holds it, rather than as the call that declared it
// answered.
func readTheJobBack(ctx context.Context) (*quaycrewv1.Job, error) {
	declared, err := firstJob(ctx)
	if err != nil {
		return nil, err
	}
	found, err := worldFrom(ctx).client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declared.GetId()})
	if err != nil {
		return nil, err
	}
	return found.GetJob(), nil
}

// whatTheSessionIsAsked is the task a controller would put in front of the session doing this job,
// built off the row the system holds.
func whatTheSessionIsAsked(ctx context.Context) (string, error) {
	one, err := readTheJobBack(ctx)
	if err != nil {
		return "", err
	}
	return job.Asked(&job.Job{
		ID: one.GetId(), Title: one.GetTitle(), Brief: one.GetBrief(),
		Request: one.GetRequest(), Product: one.GetProduct(), Repository: one.GetRepository(),
	}), nil
}

// theIdentifierTheToolDeclared reads the short identifier out of the line the tool prints on a
// declaration, which is the only thing a person has to hand afterwards.
func theIdentifierTheToolDeclared(out string) string {
	for _, line := range strings.Split(out, "\n") {
		rest, found := strings.CutPrefix(line, "declared ")
		if !found {
			continue
		}
		id, _, _ := strings.Cut(rest, " ")
		if display.LooksLikeIdentifier(id) {
			return id
		}
	}
	return ""
}
