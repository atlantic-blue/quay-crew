package job

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// A job that states the sentence writes its plan first, and a person approves it before any work
// starts.
//
// The failure it answers is the one the sentence alone could not reach. A person says what they
// want in a sentence. Something turns that sentence into a brief. The crew then executes the brief
// faithfully and fast, and nothing ever holds the brief against the sentence, because reading the
// brief costs nearly as much as reading the result: one of them ran to 1,109 words for a 1,505 word
// result. So a misreading of one sentence becomes two days of correct work in the wrong direction,
// and it looks like progress the whole way.
//
// A request for an article about what the crew had built became a diary of throughput. A product
// built from a design document took the video identifier as its key, when what was wanted was to
// paste a link and get the text. Every check was green in both.
//
// The plan is the thing a person can read instead. It is short enough that reading it costs less
// than reading the result, it is written by the crew rather than by the person, and it is the thing
// the work is then held to.
const (
	// PlanSteps is how many steps a plan may carry.
	//
	// The whole point is that reading the plan is cheaper than reading the result, so the ceiling is
	// low: seven steps at the title's ceiling is about 1,400 bytes against a brief of 16,384. A plan
	// as long as the work is worse than no plan, because it costs the reading and buys nothing.
	//
	// Chosen rather than measured. What replaces it is the distribution of steps a job actually
	// records, which krewe job step already writes down: after fifty jobs, the ninety fifth
	// percentile of steps recorded per job is the number.
	PlanSteps = 7
	// PlanStepLimit is how long one step may be. It is the title's ceiling, because both are one line
	// a person reads in a listing.
	PlanStepLimit = TitleLimit
)

// planLine is the shape the system asks for and the shape it reads back.
//
// One shape it can find, for the reason the base line and the pull request address are read the same
// way: what it finds is then what the session meant to say, rather than a sentence that happened to
// hold the word.
var planLine = regexp.MustCompile(`(?im)^[ \t]*step[ \t]+(\d+)[ \t]*[:.][ \t]*(.+?)[ \t]*$`)

// accountLine is the shape a recorded step carries when it accounts for a step of the plan: the
// number first, then a separator. The separator is required, so "2 factor authentication" is not
// read as an account of step 2.
var accountLine = regexp.MustCompile(`^[ \t]*(?:step[ \t]+)?(\d+)[ \t]*[:.)]`)

// Planned says whether this job writes a plan and waits for a person to approve it.
//
// The sentence is the trigger, and it adds no flag and no noun. A job that states no sentence is an
// errand: there is nothing to write a plan from and nothing to hold the plan against, which is the
// argument the flow engine already makes for refusing a usable node with no sentence. Right against
// what.
//
// A job declared under another is never planned. It is one part of a plan a person already approved,
// and stopping at every job in a tree puts a person back in the loop for all of them, which is the
// cost the whole system exists to remove.
func Planned(one *Job) bool {
	return one != nil && one.Product != "" && one.Parent == ""
}

// WaitingForItsPlan says whether this job still owes a person a plan they approved.
//
// A job that has not said what it understood is not waiting for its plan yet. Ideation stands in
// front of this gate and is held by the same person, so the order is what it understood, then the
// plan written from what the person answered, then the work. A session asked to plan before anybody
// had agreed with its reading would be marking its own reading, which is the gap the two gates
// together close.
func WaitingForItsPlan(one *Job) bool {
	return Planned(one) && Ideated(one) && !one.PlanApproved
}

// Step of a plan: what the crew says it will do, and the number a recorded step accounts for it by.
type PlanStep struct {
	Number int
	Text   string
}

// ReadPlan is the plan a reply carries, and the refusal where it carries none the system can read.
//
// It is read off the reply rather than reported, the way a pull request address is. A reply the
// system cannot read a plan out of is not a plan: it is prose about planning, and putting that in
// front of a person to approve is the same compression fault one level up.
func ReadPlan(reply string) ([]PlanStep, error) {
	found := planLine.FindAllStringSubmatch(reply, -1)
	if len(found) == 0 {
		return nil, fmt.Errorf("this reply carries no plan the system can read: write one line per step, "+
			"each opening with %s, and at most %d of them", `Step 1:`, PlanSteps)
	}
	if len(found) > PlanSteps {
		return nil, fmt.Errorf("this plan has %d steps and a plan may have %d: a plan as long as the work "+
			"costs more to read than the work does, so say the %d steps that matter",
			len(found), PlanSteps, PlanSteps)
	}
	steps := make([]PlanStep, 0, len(found))
	for _, one := range found {
		number, err := strconv.Atoi(one[1])
		if err != nil {
			return nil, fmt.Errorf("step %q is not numbered with a number", one[1])
		}
		text := TidySentence(one[2])
		if text == "" {
			return nil, fmt.Errorf("step %d says nothing: a step says what you will do, in a few words", number)
		}
		if len(text) > PlanStepLimit {
			return nil, fmt.Errorf("step %d is %d bytes and a step may be %d: it is one line a person reads",
				number, len(text), PlanStepLimit)
		}
		steps = append(steps, PlanStep{Number: number, Text: text})
	}
	// Numbered from one, in order, with nothing repeated and nothing missing. The numbers are what the
	// work is accounted for by, so a plan that numbers two steps 3 leaves an account nobody can read.
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Number < steps[j].Number })
	for i, one := range steps {
		if one.Number != i+1 {
			return nil, fmt.Errorf("this plan is numbered %s: number the steps from 1 upwards with none "+
				"missing and none repeated, because the numbers are how the work is accounted for",
				numbersIn(steps))
		}
	}
	return steps, nil
}

// numbersIn is the numbering a plan carried, for a refusal that has to show it.
func numbersIn(steps []PlanStep) string {
	said := make([]string, 0, len(steps))
	for _, one := range steps {
		said = append(said, strconv.Itoa(one.Number))
	}
	return strings.Join(said, ", ")
}

// PlanText is a plan as the system keeps it: one line per step, in the shape it reads back.
//
// Kept as the system's own rendering rather than as the reply, so what a person approves and what a
// session is later given are the same lines. A reply carries reasoning around its plan, and the
// reasoning is what makes a plan as expensive to read as the result.
func PlanText(steps []PlanStep) string {
	lines := make([]string, 0, len(steps))
	for _, one := range steps {
		lines = append(lines, fmt.Sprintf("Step %d: %s", one.Number, one.Text))
	}
	return strings.Join(lines, "\n")
}

// PlanIn is the steps a kept plan holds. A plan the system wrote always reads back, so a plan that
// does not is empty rather than an error: nothing downstream of the approval can act on a refusal.
func PlanIn(plan string) []PlanStep {
	steps, err := ReadPlan(plan)
	if err != nil {
		return nil
	}
	return steps
}

// NotAccountedFor is the steps of an approved plan that nothing the session recorded accounts for.
//
// This is what makes the approval worth having. A plan approved and then not followed is the same
// failure as no plan at all, one indirection further along, and it is the one this has to catch.
//
// The measurement is arithmetic over a set of numbers rather than a judgement about prose. The
// session records each step it finishes with krewe job step, which every session already does, and
// the plan's steps are numbered, so an account is a number the record carries. It costs no model
// call, and anybody holding the record can work it out again.
//
// A recorded step that accounts for nothing in the plan is not a fault. Work that was not planned
// and was done anyway is the session being useful, and the plan is a floor rather than a ceiling.
func NotAccountedFor(plan string, recorded []Step) []PlanStep {
	steps := PlanIn(plan)
	if len(steps) == 0 {
		return nil
	}
	accounted := map[int]bool{}
	for _, one := range recorded {
		if found := accountLine.FindStringSubmatch(one.Summary); found != nil {
			if number, err := strconv.Atoi(found[1]); err == nil {
				accounted[number] = true
			}
		}
	}
	var missing []PlanStep
	for _, one := range steps {
		if !accounted[one.Number] {
			missing = append(missing, one)
		}
	}
	return missing
}

// theSecondPlanAsk is the sentence the second ask below is recognised by. It is a constant because
// the ask and the reading of it must not drift: a bound that stops matching asks forever, and every
// ask is a task somebody pays for.
const theSecondPlanAsk = "asked for the plan once already"

// AskedForThePlanAgain says whether a prompt is the second ask for a plan, so a session is asked
// twice and never a third time.
func AskedForThePlanAgain(prompt string) bool {
	return strings.Contains(prompt, theSecondPlanAsk)
}

// WriteThePlan is the first task a planned job's session is given. It asks for the plan and for no
// work.
//
// The sentence goes first, because the plan is written from the sentence rather than from the brief.
// The brief is evidence for the sentence, and a plan written from the brief alone would carry
// whatever misreading the brief carries, which is the whole failure this exists to catch.
func WriteThePlan(one *Job) string {
	said := []string{ServesAPerson(one.Product), one.Brief}
	// What it said it understood, and what the person answered, between the brief and the shape. The
	// plan is written against the answer rather than against the brief again: the answer is the only
	// part of this a human wrote, and a plan that ignored it would put the person back where they
	// started, agreeing with a reading nobody checked.
	if understood := WhatWeUnderstand(one); understood != "" {
		said = append(said, understood)
	}
	return strings.Join(append(said, theShapeOfAPlan()), "\n\n")
}

// theShapeOfAPlan is what the system asks for, in the shape it reads back.
func theShapeOfAPlan() string {
	return fmt.Sprintf("Do no work yet. Write the plan for this job and answer with it, so a person can "+
		"approve it before anything is built. Write at most %d steps, one line each, and each line "+
		"opening with its number in the form \"Step 1: read the design\". Say what you will do, not how "+
		"you will do it, and keep each step under %d bytes. A person reads this instead of reading the "+
		"result, so it is worth nothing if it is as long as the work. Where the brief and the sentence "+
		"above disagree, plan for the sentence and say so.", PlanSteps, PlanStepLimit)
}

// WriteThePlanAgain is what a session is given when a person did not approve its plan.
//
// It carries the plan that was refused and what the person said, so the second plan is written from
// the correction rather than from nothing. The person who answered no writes no plan: saying what is
// wrong is the whole of what they owe, and writing the replacement is the crew's job.
func WriteThePlanAgain(one *Job) string {
	said := fmt.Sprintf("The plan you wrote was not approved.\n\nYou wrote:\n\n%s\n\nThe person said: %s\n\n"+
		"Write the plan again from what they said, and answer with it. Do no work yet.",
		one.Plan, one.Told)
	// The understanding travels with the second plan too. What was assumed is still an assumption, and
	// a session rewriting a plan from one correction is the session most likely to drop the rest.
	if understood := WhatWeUnderstand(one); understood != "" {
		said += "\n\n" + understood
	}
	return said + "\n\n" + theShapeOfAPlan()
}

// AskedForAPlanTheSystemCanRead is the second ask, where a reply carried no plan the system could
// read. It carries the refusal, so the session is told what was wrong with what it sent.
func AskedForAPlanTheSystemCanRead(why string) string {
	return fmt.Sprintf("The system %s and could not read one out of your answer: %s\n\nAnswer with the plan "+
		"and nothing else. %s", theSecondPlanAsk, why, theShapeOfAPlan())
}

// NoPlanToApprove is why a planned job stops when its session was asked twice and answered with no
// plan either time.
//
// It stops rather than starting the work. A job whose plan nobody could read is a job nobody
// approved, and running it is running the thing this gate exists to stop, after paying for two tasks
// to find out.
func NoPlanToApprove(why string) string {
	return fmt.Sprintf("this job serves a sentence, so a person approves its plan before any work starts, "+
		"and the session was asked twice and answered with no plan the system could read: %s. Read what it "+
		"said with krewe task list, and declare the job again with a brief that says what to plan", why)
}

// AskingWhetherThisIsThePlan is the one question a planned job puts to a person.
//
// It names the sentence and the plan, and nothing else. A person shown the code and asked whether it
// is right answers about the code, because that is the only thing in front of them, and the same
// trap is here: what is being approved is whether these steps serve that sentence.
func AskingWhetherThisIsThePlan(sentence, plan string) string {
	return fmt.Sprintf("This job has not started. Here is the plan for it, and here is the sentence it "+
		"serves.\n\nThe sentence: %s\n\nThe plan:\n\n%s\n\nDoes this plan get that sentence? Answer %s and "+
		"the work starts against this plan. Answer with what is wrong instead, and the crew writes the plan "+
		"again from what you said: you do not have to write it yourself.",
		sentence, plan, theAnswerThatApproves)
}

// theAnswerThatApproves is the answer that starts the work. Anything else is the correction, which
// is the same rule the first usable path already keeps: one word carries on, and everything else is
// what the person wanted instead.
const theAnswerThatApproves = "yes"

// ApprovesThePlan says whether an answer is the approval.
func ApprovesThePlan(answer string) bool {
	return strings.EqualFold(TidySentence(answer), theAnswerThatApproves)
}

// FollowThePlan is the line a session doing approved work is given, with the plan it is held to.
//
// It replaces the ordinary line about recording steps rather than sitting beside it. Two lines about
// recording steps, saying it two ways, is how a session ends up doing neither.
func FollowThePlan(plan string) string {
	return fmt.Sprintf("A person approved this plan, and it is what this job is held to:\n\n%s\n\nDo these "+
		"steps. As you finish each one, record it with its number: krewe job step \"2: read the design\". "+
		"The job does not end until every step of the plan has one. Where a step turns out to be wrong, "+
		"say so in your answer rather than dropping it in silence.", plan)
}

// PlanNotFollowed is why a job stops when the work drifted off the plan a person approved.
//
// The reason names the steps rather than saying the plan was not followed, because a person reading
// it has to be able to act on it without opening the session. What was answered stays on the row: the
// work is not lost, it is unapproved, and those are different things.
func PlanNotFollowed(missing []PlanStep) string {
	said := make([]string, 0, len(missing))
	for _, one := range missing {
		said = append(said, fmt.Sprintf("step %d, %s", one.Number, one.Text))
	}
	return fmt.Sprintf("a person approved this job's plan and the record accounts for none of %s: "+
		"nothing was recorded against %s. The answer is on the row and the work is in the session, so read "+
		"both before declaring the rest", pluralSteps(len(missing)), strings.Join(said, "; "))
}

// pluralSteps keeps the reason readable for one step and for several.
func pluralSteps(count int) string {
	if count == 1 {
		return "one of its steps"
	}
	return fmt.Sprintf("%d of its steps", count)
}
