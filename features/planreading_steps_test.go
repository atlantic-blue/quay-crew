package features_test

import (
	"context"
	"fmt"
	"strings"
	"sync"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/flow"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/cucumber/godog"
)

// A plan read by several roles, driven the way the system drives it. Each reading is a job of its
// own, so a scenario acts as that reading's session: it holds the credential the system minted for
// that job, and it writes and settles rows over it, which is exactly what a model in the sandbox
// does with `krewe job question` and `krewe job settle`.
//
// The assertions go past the row. What decides whether this works is what the next reading is
// handed and what the person is asked, so the scenarios read the prompt of the reading that came
// after and the question the run put.

// reading is what one reading of the plan does while its task is under way.
type reading struct {
	// writes are the questions this lens could not settle.
	writes []string
	// settles are the rows this lens answered, by the number it was handed.
	settles []settling
	// fails makes the task fail, which is a lens that fell over rather than one that read.
	fails bool
}

type settling struct {
	seq    int
	answer string
}

// readings is what each node of the graph does, keyed by the node, with the tasks it has already
// done so a reading that runs twice does not write its rows twice.
type readings struct {
	mu   sync.Mutex
	does map[string]*reading
	done map[string]bool
}

func initializePlanReadingSteps(sc *godog.ScenarioContext) {
	// The lenses, as roles the workspace holds. A reading names a role, so a workspace that held none
	// would refuse every step and the scenario would be about that instead.
	sc.Step(`^the workspace holds the roles that read a plan$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		for _, name := range []string{"plan-critic", "test-writer"} {
			if _, err := w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
				Files: roleFilesThatMay(name, nil),
			}); err != nil {
				return err
			}
			if _, err := w.client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
				Workspace: w.workspaceID, Name: name,
			}); err != nil {
				return err
			}
		}
		return nil
	})

	// Started about a plan, which is what a run of the shipped graph is: the plan reaches the run and
	// every reading renders it. A step is a new session with an empty working directory, so a reading
	// told to read a plan and handed none reads nothing at all.
	sc.Step(`^the operator starts the flow "([^"]*)" in the project, about this plan:$`,
		func(ctx context.Context, name string, plan *godog.DocString) error {
			w := worldFrom(ctx)
			engine := flow.NewEngine(w.store, planeClient{client: w.client}, nil, w.server)
			run, err := engine.Start(ctx, name, w.workspaceID, w.projectID,
				map[string]string{flow.PlanKey: plan.Content})
			w.flowRun, w.lastErr = run, err
			if err != nil {
				return err
			}
			return driveTheSystem(ctx)
		})

	sc.Step(`^the reading "([^"]*)" writes down "([^"]*)"$`,
		func(ctx context.Context, node, question string) error {
			at := readingsFor(ctx, node)
			at.writes = append(at.writes, question)
			return nil
		})

	sc.Step(`^the reading "([^"]*)" settles row (\d+) with "([^"]*)"$`,
		func(ctx context.Context, node string, seq int, answer string) error {
			at := readingsFor(ctx, node)
			at.settles = append(at.settles, settling{seq: seq, answer: answer})
			return nil
		})

	sc.Step(`^the reading "([^"]*)" fails$`, func(ctx context.Context, node string) error {
		readingsFor(ctx, node).fails = true
		return nil
	})

	sc.Step(`^the run is asking, and the question carries "([^"]*)"$`,
		func(ctx context.Context, phrase string) error {
			w := worldFrom(ctx)
			kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
			if err != nil {
				return err
			}
			if kept.Status != "asking" {
				return fmt.Errorf("the run is %q on node %q, want it asking a person", kept.Status, kept.Node)
			}
			if !strings.Contains(kept.Question, phrase) {
				return fmt.Errorf("the person is asked %q, want the row carrying %q", kept.Question, phrase)
			}
			return nil
		})

	sc.Step(`^the question does not carry "([^"]*)"$`, func(ctx context.Context, phrase string) error {
		w := worldFrom(ctx)
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		if strings.Contains(kept.Question, phrase) {
			return fmt.Errorf("a row a later lens settled reached the person: %q", kept.Question)
		}
		return nil
	})

	sc.Step(`^the plan carries (\d+) questions?, (\d+) of them settled$`,
		func(ctx context.Context, want, settled int) error {
			rows, err := rowsOnThePlan(ctx)
			if err != nil {
				return err
			}
			if len(rows) != want {
				return fmt.Errorf("the plan carries %d rows, want %d", len(rows), want)
			}
			closed := len(rows) - len(job.OpenQuestions(rows))
			if closed != settled {
				return fmt.Errorf("%d of the plan's rows are settled, want %d", closed, settled)
			}
			return nil
		})

	sc.Step(`^the plan carries no questions$`, func(ctx context.Context) error {
		rows, err := rowsOnThePlan(ctx)
		if err != nil {
			return err
		}
		if len(rows) != 0 {
			return fmt.Errorf("the plan carries %d rows, and no lens wrote one", len(rows))
		}
		return nil
	})

	sc.Step(`^nobody was asked anything$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		if kept.Question != "" {
			return fmt.Errorf("the run asked %q, and every row was settled", kept.Question)
		}
		if kept.State[flowQuestionsKey] != "" {
			return fmt.Errorf("the run holds %q as still open", kept.State[flowQuestionsKey])
		}
		return nil
	})

	sc.Step(`^the reading "([^"]*)" was given "([^"]*)"$`, func(ctx context.Context, node, phrase string) error {
		asked, err := whatTheReadingWasAsked(ctx, node)
		if err != nil {
			return err
		}
		if !strings.Contains(asked, phrase) {
			return fmt.Errorf("the reading was given %q, want the open row carrying %q", asked, phrase)
		}
		return nil
	})

	// The trade this makes, stated as a scenario: the rows travel and the reading behind them does
	// not. A reader that cannot see the earlier reading cannot be led by it.
	sc.Step(`^the reading "([^"]*)" was not given the reading before it$`,
		func(ctx context.Context, node string) error {
			w := worldFrom(ctx)
			asked, err := whatTheReadingWasAsked(ctx, node)
			if err != nil {
				return err
			}
			earlier, err := whatTheReadingReplied(ctx, w, "critic")
			if err != nil {
				return err
			}
			if earlier == "" {
				return fmt.Errorf("the reading before it replied nothing, so this proves nothing")
			}
			if strings.Contains(asked, earlier) {
				return fmt.Errorf("the reading was handed the earlier reading whole: %q", asked)
			}
			return nil
		})
}

// flowQuestionsKey is where a run holds the rows nobody settled. Spelled here rather than imported,
// because the scenarios read a run as an operator does: through the store.
const flowQuestionsKey = "questions.open"

// readingsFor is what the named reading does, made on first use, with the hook that drives it
// attached to the model double.
//
// The hook is what makes a scenario a session rather than a description of one: the double runs it
// while the task is under way, so the rows are written by the credential the system minted for that
// reading's job, over the same calls a model in a sandbox makes.
func readingsFor(ctx context.Context, node string) *reading {
	w := worldFrom(ctx)
	if w.readings == nil {
		w.readings = &readings{does: map[string]*reading{}, done: map[string]bool{}}
		w.runner.duringTask = func(req model.Request) { readAsTheSession(w, req) }
	}
	if w.readings.does[node] == nil {
		w.readings.does[node] = &reading{}
	}
	return w.readings.does[node]
}

// readAsTheSession is one reading doing its work: it finds which reading this task is, and writes
// and settles rows over that job's own credential.
//
// A task that belongs to no reading does nothing at all, which is every other scenario in the suite.
func readAsTheSession(w *world, req model.Request) {
	ctx := context.Background()
	step, node := theReadingRunning(ctx, w, req)
	if step == nil {
		return
	}
	w.readings.mu.Lock()
	does, already := w.readings.does[node], w.readings.done[node]
	w.readings.done[node] = true
	w.readings.mu.Unlock()
	if does == nil || already {
		return
	}
	if does.fails {
		w.runner.failTheNextTask()
		return
	}
	session := w.readingSession(ctx, step.ID)
	if session == nil {
		return
	}
	for _, settle := range does.settles {
		if _, err := session.SettleJobQuestion(ctx, &quaycrewv1.SettleJobQuestionRequest{
			Seq: int32(settle.seq), Answer: settle.answer,
		}); err != nil {
			// Kept rather than returned: the double answers a task and cannot fail a step this way.
			// The scenario's own assertion is what says the row did not land.
			w.readingErr(fmt.Errorf("settling row %d as %s: %w", settle.seq, node, err))
		}
	}
	for _, asking := range does.writes {
		if _, err := session.RecordJobQuestion(ctx, &quaycrewv1.RecordJobQuestionRequest{
			Text: asking,
		}); err != nil {
			w.readingErr(fmt.Errorf("writing %q as %s: %w", asking, node, err))
		}
	}
}

// theReadingRunning is the job this task belongs to and the node it is a reading for. The step is
// named after its node when the engine declares it, which is what ties a task back to the graph.
func theReadingRunning(ctx context.Context, w *world, req model.Request) (*job.Job, string) {
	running, err := w.store.ListJobs(ctx, job.Filter{Phase: job.PhaseRunning})
	if err != nil {
		return nil, ""
	}
	for _, one := range running {
		node := one.Labels["flow.node"]
		if node == "" {
			continue
		}
		// The brief is the prompt this task was handed, which is what tells two readings apart when
		// both are running.
		if !strings.Contains(req.Text, one.Brief) {
			continue
		}
		return one, node
	}
	return nil, ""
}

// readingSession is a client holding the credential the system minted for a reading's job, which is
// what the model in that sandbox holds.
func (w *world) readingSession(ctx context.Context, id string) quaycrewv1.ControlPlaneServiceClient {
	token, minted := w.server.JobCredentialForTest(ctx, id)
	if !minted {
		w.readingErr(fmt.Errorf("the system minted no credential for the reading %s", id))
		return nil
	}
	return w.dialAs(token)
}

// rowsOnThePlan are the rows the job carrying the run holds, which is where every reading's rows
// end up and what a person reading the plan sees.
func rowsOnThePlan(ctx context.Context) ([]job.Question, error) {
	w := worldFrom(ctx)
	if err := w.readingFailure(); err != nil {
		return nil, err
	}
	carrier, err := runCarrier(ctx, w)
	if err != nil {
		return nil, err
	}
	plan, err := w.store.GetJob(ctx, carrier.ID)
	if err != nil {
		return nil, err
	}
	return plan.Questions, nil
}

// whatTheReadingWasAsked is the brief the named reading was handed, read off its own job.
func whatTheReadingWasAsked(ctx context.Context, node string) (string, error) {
	w := worldFrom(ctx)
	if err := w.readingFailure(); err != nil {
		return "", err
	}
	step, err := runStepOn(ctx, w, node)
	if err != nil {
		return "", err
	}
	return step.Brief, nil
}

// whatTheReadingReplied is what the named reading answered, which is the prose the next reading must
// not be handed.
func whatTheReadingReplied(ctx context.Context, w *world, node string) (string, error) {
	step, err := runStepOn(ctx, w, node)
	if err != nil {
		return "", err
	}
	return step.Answer, nil
}

// readingErr keeps the first thing a reading could not do. The double answers a task and cannot fail
// a step, so a call that was refused would otherwise be a scenario that quietly proved nothing.
func (w *world) readingErr(err error) {
	w.readings.mu.Lock()
	defer w.readings.mu.Unlock()
	if w.readingFailed == nil {
		w.readingFailed = err
	}
}

// readingFailure is what a reading could not do, reported by the step that reads the rows back.
func (w *world) readingFailure() error {
	if w.readings == nil {
		return nil
	}
	w.readings.mu.Lock()
	defer w.readings.mu.Unlock()
	return w.readingFailed
}
