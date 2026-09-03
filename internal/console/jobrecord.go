package console

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	tea "github.com/charmbracelet/bubbletea"
)

// The whole of one job, read in the console.
//
// A row of the jobs listing is nine cells, and what the job holds is the brief a person wrote, every
// question it asked with the answer it got, the plan and whether anybody approved it, the record of
// what its verticals became, and the answer at the end. None of that fits in a cell, so a reader
// left the console and ran `krewe job show`. This is the same record on the screen they were already
// looking at.

// jobRecordMsg is the job that was read, or why it could not be.
type jobRecordMsg struct {
	job *quaycrewv1.Job
	err error
}

// openJobRecord reads the job under the cursor from the system rather than off the row. The listing
// behind it is up to three seconds old, and the field a person opens a job for is the one that just
// changed.
func openJobRecord(client quaycrewv1.ControlPlaneServiceClient, row Row) tea.Cmd {
	// A part of a job is a run of one of its stages. Nobody declared it, it holds no brief and no
	// answer, and asking the system for a job by a run's identifier would only be refused.
	if row.ID == "" || row.Under != "" {
		return nil
	}
	// A console with no system cannot read a job, so enter goes down into what the job ran, the way
	// it did before this page existed. Taking the key away from the one console that has nothing
	// else to offer would leave a person with a refusal where they had a listing.
	if client == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		resp, err := client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: row.ID})
		if err != nil {
			return jobRecordMsg{err: fmt.Errorf("read job %s: %w", display.ShortID(row.ID), err)}
		}
		if resp.GetJob() == nil {
			return jobRecordMsg{err: fmt.Errorf("the system holds no job %s", display.ShortID(row.ID))}
		}
		return jobRecordMsg{job: resp.GetJob()}
	}
}

// showJobRecord puts the record over the rows. It is drawn by the panel that already draws a reading,
// because it is the same kind of thing, and it is its own mode because the keys are not: a reading
// closes on the next key, and this is a page a person reads to the end.
func (m Model) showJobRecord(one *quaycrewv1.Job) Model {
	m = m.showReading("job "+display.ShortID(one.GetId()), strings.Join(jobRecordLines(one), "\n"))
	m.mode, m.err = modeJob, nil
	return m
}

// updateJobRecordKey scrolls the record and closes it on escape. Every other key is left alone, so a
// key pressed on a page taller than the window cannot lose the page.
func (m Model) updateJobRecordKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode, m.readingTop, m.err = modeBrowse, 0, nil
		m.reading, m.readingTitle = nil, ""
		return m, nil
	case "q":
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		m.readingTop--
	case "down", "j":
		m.readingTop++
	case "pgup", "ctrl+b":
		m.readingTop -= m.bodyHeight()
	case "pgdown", "ctrl+f":
		m.readingTop += m.bodyHeight()
	case "G":
		m.readingTop = len(m.readingLines())
	default:
		return m, nil
	}
	if m.readingTop < 0 {
		m.readingTop = 0
	}
	if most := len(m.readingLines()) - 1; m.readingTop > most {
		m.readingTop = most
	}
	return m, nil
}

// jobRecordLines is the record, in the order the job went through it: what it is, what it understood
// and what a person answered, what it would build, the plan, what was built and whether a person
// accepted it, what it asked last and what it was told, and the answer.
//
// Every field is printed whole. A brief cut to the width of a cell is the fault this page answers.
func jobRecordLines(one *quaycrewv1.Job) []string {
	standing := one.GetPhase()
	if one.GetRole() != "" {
		standing += fmt.Sprintf(", as %s version %d", one.GetRole(), one.GetRoleVersion())
	}
	lines := []string{
		fmt.Sprintf("%s  %s", display.ShortID(one.GetId()), one.GetTitle()),
		standing,
		job.StageOfWire(one).Where(),
	}
	if outcome := one.GetOutcome(); outcome != "" {
		lines = append(lines, fmt.Sprintf("outcome: %s, %s", outcome, job.OutcomeMeans(outcome)))
	}
	if product := one.GetProduct(); product != "" {
		lines = append(lines, "", "for a person: "+product)
	}
	lines = block(lines, "brief:", one.GetBrief())
	lines = block(lines, understoodHeading(one), one.GetIdeation())
	lines = block(lines, "you answered:", one.GetIdeationAnswer())
	lines = block(lines, wouldBuildHeading(one), one.GetDesign())
	lines = block(lines, planHeading(one), one.GetPlan())
	lines = block(lines, "built:", one.GetBuild())
	if said := acceptanceLine(one); said != "" {
		lines = append(lines, "", said)
	}
	lines = block(lines, askedHeading(one), one.GetQuestion())
	lines = block(lines, "told:", one.GetTold())
	if pull := one.GetPullRequest(); pull != "" {
		lines = append(lines, "", "pull request: "+pull)
	}
	lines = block(lines, "answer:", one.GetAnswer())
	return lines
}

// block is one part of the record: a blank line, what the part is, and every line of it. A part with
// nothing in it is not drawn, because a heading over nothing reads as something that failed to load.
func block(lines []string, heading, text string) []string {
	if strings.TrimSpace(text) == "" {
		return lines
	}
	lines = append(lines, "", heading)
	for _, line := range strings.Split(text, "\n") {
		lines = append(lines, "  "+line)
	}
	return lines
}

// The headings that carry a fact as well as a name. A plan a person approved and a plan waiting on
// them are days apart, and the record is the same either way, so the one word is the difference.
func understoodHeading(one *quaycrewv1.Job) string {
	if one.GetIdeationAnswer() == "" {
		return "what it understands, waiting for you to answer in your own words:"
	}
	return "what it understood before it planned:"
}

func wouldBuildHeading(one *quaycrewv1.Job) string {
	if one.GetDesignAccepted() {
		return "what it builds, accepted:"
	}
	return "what it would build, waiting for you to accept the list:"
}

func planHeading(one *quaycrewv1.Job) string {
	if one.GetPlanApproved() {
		return "plan, approved:"
	}
	return "plan, not approved yet:"
}

func askedHeading(one *quaycrewv1.Job) string {
	if one.GetPhase() == job.PhaseAsking {
		return "asking:"
	}
	return "asked:"
}

// acceptanceLine says whether a person looked at what was built and said the value arrived. It is
// the only road into done for a job whose verticals are built, so a reader has to see that it
// happened, and see when it has not.
func acceptanceLine(one *quaycrewv1.Job) string {
	if strings.TrimSpace(one.GetBuild()) == "" {
		return ""
	}
	if one.GetAccepted() {
		return "a person read the build and accepted it"
	}
	return "the build is not accepted yet: a person reads the pictures and says whether the value arrived"
}
