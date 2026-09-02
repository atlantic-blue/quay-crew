package flow

import (
	"context"
	"log/slog"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A plan read by several roles, and only what none of them could settle put to a person.
//
// The graph is what makes the readings several: one dispatch node per lens, each already in a
// session of its own, each given the same plan. What the engine adds is the rows travelling between
// them. A reading writes what its own lens could not settle onto its own job; the engine carries
// those rows up onto the job the run is carried by, which is where the plan is; and it hands the
// rows that are still open down onto the next reading, without the earlier reader's answers.
//
// The reader that comes second is the thing most likely to answer the first one's question, so the
// question is put to a person once, at the end, and only where every lens left it open.

// carryUp writes what a reading could not settle onto the plan it read, and is what makes a later
// reading able to settle an earlier one's row.
//
// A failure here is logged and not returned. The reading happened, its rows are on its own job, and
// a run halted over a merge would cost the whole reading to save one row: the person is asked what
// the plan still holds open, which is the worse of the two answers rather than no answer at all.
func (e *Engine) carryUp(ctx context.Context, run *Run, step *job.Job) {
	if step == nil || step.Parent == "" {
		return
	}
	// Read back rather than trusted, because the row a listing carries is the job and not the rows
	// its session wrote on it.
	read, err := e.store.GetJob(ctx, step.ID)
	if err != nil {
		slog.WarnContext(ctx, "the questions this reading wrote could not be read, so the plan carries none of them",
			"run", run.ID, "job", step.ID, "error", err)
		return
	}
	if len(read.Questions) > 0 {
		if _, err := e.store.CarryJobQuestions(ctx, step.Parent, questionsFor(read.Questions, step.Parent)); err != nil {
			slog.WarnContext(ctx, "what this reading could not settle did not reach the plan it read",
				"run", run.ID, "job", step.ID, "plan", step.Parent, "error", err)
		}
	}
	e.holdOpenRows(ctx, run, step.Parent)
}

// holdOpenRows puts the rows nobody has settled into the run's state, so the next reader's prompt and
// the question at the end both render what is still open and nothing else.
func (e *Engine) holdOpenRows(ctx context.Context, run *Run, plan string) {
	carrying, err := e.store.GetJob(ctx, plan)
	if err != nil {
		slog.WarnContext(ctx, "the plan carrying this run could not be read, so the run holds no open rows",
			"run", run.ID, "plan", plan, "error", err)
		return
	}
	if run.State == nil {
		run.State = map[string]string{}
	}
	run.State[QuestionsKey] = job.RenderQuestions(carrying.Questions)
}

// handDown is the rows a reading is given: the ones still open on the plan, and never the earlier
// reader's answer.
//
// It is read off the plan rather than off the run's state, because the state carries the rows as
// prose and a reader settles by number.
func (e *Engine) handDown(ctx context.Context, plan string, declared *job.Job) {
	if plan == "" || declared == nil {
		return
	}
	carrying, err := e.store.GetJob(ctx, plan)
	if err != nil {
		slog.WarnContext(ctx, "the plan this reading is of could not be read, so the reading is handed no open rows",
			"plan", plan, "job", declared.ID, "error", err)
		return
	}
	declared.Questions = job.CarriedQuestions(carrying.Questions, declared.ID)
}

// questionsFor is a reading's rows addressed to another job, which is the plan above it.
func questionsFor(rows []job.Question, onto string) []job.Question {
	carried := make([]job.Question, 0, len(rows))
	for _, one := range rows {
		one.Job = onto
		carried = append(carried, one)
	}
	return carried
}

// thePlanBeingRead puts the plan the readings read into the run's opening state, off the job the run
// hangs under.
//
// Without it a graph whose steps say "read the plan" hands every reading nothing. A step is a new
// session with an empty working directory, and a plan is a column on a row rather than a file, so
// there is nowhere else a reading could get one: the run has to carry it and the prompt has to
// render it.
//
// A run started with a plan in its state keeps that one, because a caller that passed a plan was
// asking for a reading of that plan. A run under nothing, or under a job that carries no plan, gets
// no key at all, and the prompt then renders the template as typed, which is the graph author's
// signal that the run was started with nothing to read.
func (e *Engine) thePlanBeingRead(ctx context.Context, run *Run, above string) {
	if run.State == nil {
		run.State = map[string]string{}
	}
	if above == "" || run.State[PlanKey] != "" {
		return
	}
	held, err := e.store.GetJob(ctx, above)
	if err != nil {
		slog.WarnContext(ctx, "the job this run hangs under could not be read, so its readings are handed no plan",
			"run", run.ID, "job", above, "error", err)
		return
	}
	if held.Plan != "" {
		run.State[PlanKey] = held.Plan
	}
}
