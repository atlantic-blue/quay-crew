package console

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/model"
	tea "github.com/charmbracelet/bubbletea"
)

// The wizard makes one thing. It asks what first, then only the questions that thing needs, and where
// a question needs a parent it offers what the system already has rather than a blank name.
//
// It shipped able to make a whole system and nothing else: a workspace and a project were both required
// on the way to anything, so adding a project to a workspace you had, or setting a token on one, meant
// dropping to the command line and knowing what to type.
//
// It is a mode the keyboard is in, drawn where the command bar draws, following the confirmation step
// rather than inventing an overlay. Escape at any point creates nothing at all: a wizard that half
// creates is worse than no wizard, and it is the one behaviour here worth being certain of.
type wizardKind int

const (
	// kindUnchosen is the wizard as it opens, before anything has been said about what to make.
	kindUnchosen wizardKind = iota
	kindWorkspace
	kindProject
	kindSecret
	kindContext
	kindSession
	kindSkill
)

// kinds is every kind in the order they are offered, which is the order they depend on each other:
// nothing later can be made without something earlier.
var kinds = []wizardKind{kindWorkspace, kindProject, kindSecret, kindContext, kindSession, kindSkill}

func (k wizardKind) String() string {
	switch k {
	case kindWorkspace:
		return "workspace"
	case kindProject:
		return "project"
	case kindSecret:
		return "secret"
	case kindContext:
		return "context"
	case kindSession:
		return "session"
	case kindSkill:
		return "skill"
	default:
		return ""
	}
}

// wizardStep is one question.
type wizardStep int

const (
	// stepKind asks what to make. Every other step follows from the answer.
	stepKind wizardStep = iota
	// stepPickWorkspace and stepPickProject choose a parent from what already exists. They never make
	// one: picking "acme" when there is an "acme" must not leave two.
	stepPickWorkspace
	stepPickProject
	// stepName names the new workspace or project.
	stepName
	// stepSecret takes the subscription token, which is what a workspace needs before it can run a
	// task.
	stepSecret
	// stepContext takes what the model should be told about a project.
	stepContext
	// stepMessage takes a first message, which is the only way a session comes into existence.
	stepMessage
	// stepPickSkill chooses a skill the system holds in its store, to attach to the workspace.
	stepPickSkill
	// stepMode chooses what the session's tasks may do without asking. It is asked rather than
	// defaulted, because a sandbox is born with its capabilities: a session started in the wrong mode
	// costs a restart, and one that cannot act is a session that apologises instead of working.
	stepMode
	// stepWorking is the system being asked to make it.
	stepWorking
)

// steps is what this kind asks, in order.
func (k wizardKind) steps() []wizardStep {
	switch k {
	case kindWorkspace:
		return []wizardStep{stepName}
	case kindProject:
		return []wizardStep{stepPickWorkspace, stepName}
	case kindSecret:
		return []wizardStep{stepPickWorkspace, stepSecret}
	case kindContext:
		return []wizardStep{stepPickWorkspace, stepPickProject, stepContext}
	case kindSession:
		return []wizardStep{stepPickWorkspace, stepPickProject, stepMode, stepMessage}
	case kindSkill:
		return []wizardStep{stepPickWorkspace, stepPickSkill}
	default:
		return nil
	}
}

// without is steps with one dropped, for a flow that does not ask that question.
func without(steps []wizardStep, drop wizardStep) []wizardStep {
	kept := make([]wizardStep, 0, len(steps))
	for _, step := range steps {
		if step != drop {
			kept = append(kept, step)
		}
	}
	return kept
}

// wizardChoice is one thing the system already has, offered where a step needs a parent.
type wizardChoice struct {
	id   string
	name string
}

// wizard is what has been answered so far. It holds no identifiers it made: everything is made in one
// pass at the end, so cancelling has nothing to undo.
type wizard struct {
	kind wizardKind
	// at is how far through this kind's steps the wizard is.
	at    int
	typed string

	// workspace and project are what was picked, so they carry an identifier the system gave us rather
	// than a name to be resolved later.
	workspace wizardChoice
	project   wizardChoice

	name    string
	secret  string
	context string
	message string
	// mode is what the session's tasks may do without asking, in the protocol's words rather than the
	// operator's, because it travels straight into the dispatch that starts the session.
	mode  string
	skill wizardChoice

	// choices is what the current step can offer, and loaded says the system has answered. The two are
	// separate because no workspaces at all is a real answer and needs its own sentence.
	choices []wizardChoice
	loaded  bool

	// cycling says tab has landed on a candidate for this step. cycleBase is the prefix typed by hand
	// before the first tab press, kept apart from typed so a later press still offers every candidate
	// that matched it, rather than narrowing to whatever the previous press filled in. cycleAt is the
	// candidate under the cursor.
	cycling   bool
	cycleBase string
	cycleAt   int

	// guided is the first run flow: the stages run as a chain in the order a system needs them, an
	// empty answer skips a stage, and escape leaves the setup rather than cancelling, because the
	// stages behind it are already made.
	guided bool
	// chain is the stages still to run after this one.
	chain []wizardKind
}

// guidedSetup is the wizard as the first run opens it: every stage a system needs, in dependency
// order. Skills come after the token and the context because attaching one is the only stage that
// can be impossible (a system holding none), and a stage that skips itself should not sit in the
// middle of the questions that cannot.
func guidedSetup() wizard {
	return wizard{kind: kindWorkspace, guided: true,
		chain: []wizardKind{kindProject, kindSecret, kindContext, kindSkill, kindSession}}
}

// skipAnsweredPicks moves past pick steps whose answer the chain already carries, so a stage never
// asks for a workspace the operator named one question ago.
func (w *wizard) skipAnsweredPicks() {
	for {
		switch w.step() {
		case stepPickWorkspace:
			if w.workspace.id == "" {
				return
			}
		case stepPickProject:
			if w.project.id == "" {
				return
			}
		default:
			return
		}
		w.at++
	}
}

// needsProject says this stage has nowhere to go without one, so a skipped project takes it too.
func (k wizardKind) needsProject() bool {
	return k == kindContext || k == kindSession
}

// step is the question being asked right now.
func (w wizard) step() wizardStep {
	if w.kind == kindUnchosen {
		return stepKind
	}
	steps := w.kind.steps()
	// The guided setup does not ask. It is the first run of an empty system, walking somebody through
	// six questions already, and a session it starts keeps the system's default the way every session did
	// before this step existed. Asking is for the wizard the operator opens deliberately.
	if w.guided {
		steps = without(steps, stepMode)
	}
	if w.at >= len(steps) {
		return stepWorking
	}
	return steps[w.at]
}

// picking says this step chooses from what exists rather than taking free text.
func (w wizard) picking() bool {
	step := w.step()
	return step == stepPickWorkspace || step == stepPickProject || step == stepPickSkill
}

// modeOrder is the order the modes are offered, narrowest first, so the most permissive is never the
// one under the cursor. The words themselves live with the model, because the command line, this
// wizard and the system's configuration all have to take the same ones.
var modeOrder = model.PermissionModesOffered()

// prompt is what the step asks, and what pressing enter accepts.
func (w wizard) prompt() string {
	switch w.step() {
	case stepKind:
		return "make what"
	case stepPickWorkspace:
		return "which workspace"
	case stepPickProject:
		return "which project in " + w.workspace.name
	case stepName:
		if w.kind == kindProject {
			return "new project in " + w.workspace.name
		}
		if w.guided {
			return "no workspaces yet. new workspace, lowercase and hyphens"
		}
		return "new workspace, lowercase and hyphens"
	case stepSecret:
		return "subscription token for " + w.workspace.name
	case stepContext:
		return "what the model should know about " + w.where()
	case stepMessage:
		return "first message to " + w.where()
	case stepPickSkill:
		// The reminder rides on the question, because a skill whose secret is unset refuses the task
		// rather than the attach, and attaching is the moment somebody can still act on it.
		return "which skill for " + w.workspace.name + " (set the secrets it names on this workspace)"
	case stepMode:
		// What it may do rather than what the mode is called: an operator picking this is deciding
		// how much room the session gets, not naming a setting.
		return "what may it do without asking (plan, edits, dangerous)"
	default:
		return "making it"
	}
}

// where is the address a project step is acting on, the way the operator writes it everywhere else.
func (w wizard) where() string {
	return w.workspace.name + "/" + w.project.name
}

// offers is what this step can be answered with, narrowed by what has been typed. A step that takes
// free text offers nothing, because there is nothing to offer.
func (w wizard) offers() []string {
	var names []string
	switch {
	case w.step() == stepKind:
		for _, kind := range kinds {
			names = append(names, kind.String())
		}
	case w.step() == stepMode:
		names = append(names, modeOrder...)
	case w.picking():
		for _, choice := range w.choices {
			names = append(names, choice.name)
		}
	default:
		return nil
	}
	typed := strings.ToLower(strings.TrimSpace(w.typed))
	matched := make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasPrefix(strings.ToLower(name), typed) {
			matched = append(matched, name)
		}
	}
	return matched
}

// cycle moves the tab cursor to the next (or, given a negative direction, the previous) candidate
// this step offers, and fills it into typed. Pressing tab again keeps offering every candidate that
// matched what was typed by hand, because filling in a full name would otherwise narrow the next
// press down to that one candidate alone.
func (w wizard) cycle(direction int) wizard {
	base := w.typed
	if w.cycling {
		base = w.cycleBase
	}
	probe := w
	probe.typed = base
	offers := probe.offers()
	if len(offers) == 0 {
		return w
	}

	at := 0
	switch {
	case w.cycling:
		at = mod(w.cycleAt+direction, len(offers))
	case direction < 0:
		at = len(offers) - 1
	}
	w.cycling, w.cycleBase, w.cycleAt, w.typed = true, base, at, offers[at]
	return w
}

// currentOffers is what tab is cycling through: the full list matching what was typed by hand,
// rather than the one candidate tab has since filled in.
func (w wizard) currentOffers() []string {
	if !w.cycling {
		return w.offers()
	}
	probe := w
	probe.typed = w.cycleBase
	return probe.offers()
}

func mod(a, n int) int {
	a %= n
	if a < 0 {
		a += n
	}
	return a
}

// shown is what the operator sees of what they have typed. A secret is never echoed: a value on a
// screen is a value in that terminal's scrollback, and this one runs every task the system makes.
func (w wizard) shown() string {
	if w.step() == stepSecret {
		return strings.Repeat("*", len([]rune(w.typed)))
	}
	return w.typed
}

// accept takes what was typed for this step and moves on, or reports what is missing. Every step is
// needed now: the wizard makes one thing, so a step it asks is part of that thing rather than an offer
// alongside it.
func (w wizard) accept() (wizard, error) {
	answer := strings.TrimSpace(w.typed)
	current := w.step()

	switch current {
	case stepKind:
		kind, err := parseKind(answer)
		if err != nil {
			return w, err
		}
		w.kind = kind
	case stepPickWorkspace:
		chosen, err := w.pick(answer, "workspace", "")
		if err != nil {
			return w, err
		}
		w.workspace = chosen
	case stepPickProject:
		chosen, err := w.pick(answer, "project", " in "+w.workspace.name)
		if err != nil {
			return w, err
		}
		w.project = chosen
	case stepPickSkill:
		chosen, err := w.pick(answer, "skill", "")
		if err != nil {
			return w, err
		}
		w.skill = chosen
	case stepName:
		if answer == "" {
			return w, fmt.Errorf("%s: this one is needed", w.prompt())
		}
		w.name = answer
	case stepSecret:
		if answer == "" {
			return w, fmt.Errorf("%s: this one is needed", w.prompt())
		}
		w.secret = w.typed
	case stepContext:
		if answer == "" {
			return w, fmt.Errorf("%s: this one is needed", w.prompt())
		}
		w.context = w.typed
	case stepMode:
		mode, known := model.PermissionModeNamed(answer)
		if !known {
			return w, fmt.Errorf("%q is not one of them: %s", answer, strings.Join(modeOrder, ", "))
		}
		w.mode = mode
	case stepMessage:
		if answer == "" {
			return w, fmt.Errorf("%s: this one is needed", w.prompt())
		}
		w.message = answer
	}

	w.typed = ""
	w.choices, w.loaded = nil, false
	w.cycling, w.cycleBase, w.cycleAt = false, "", 0
	if current != stepKind {
		w.at++
	}
	return w, nil
}

// pick resolves what was typed against what the system already has. It never creates: the whole point of
// the step is that an existing workspace is reused rather than made a second time.
func (w wizard) pick(answer, what, within string) (wizardChoice, error) {
	if !w.loaded {
		return wizardChoice{}, fmt.Errorf("still reading which %ss there are", what)
	}
	if len(w.choices) == 0 {
		return wizardChoice{}, fmt.Errorf("there is no %s%s yet: make one first", what, within)
	}
	if answer == "" {
		return wizardChoice{}, fmt.Errorf("which %s: %s", what, strings.Join(w.offers(), ", "))
	}

	var matched []wizardChoice
	for _, choice := range w.choices {
		if strings.EqualFold(choice.name, answer) {
			return choice, nil
		}
		if strings.HasPrefix(strings.ToLower(choice.name), strings.ToLower(answer)) {
			matched = append(matched, choice)
		}
	}
	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		return wizardChoice{}, fmt.Errorf("there is no %s called %q%s", what, answer, within)
	default:
		return wizardChoice{}, fmt.Errorf("%q could be %s: say which", answer, names(matched))
	}
}

// parseKind resolves what to make. A prefix is enough when only one kind starts with it, and "s" is
// deliberately not enough, because a secret and a session are both a keystroke away from each other.
func parseKind(answer string) (wizardKind, error) {
	offered := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		offered = append(offered, kind.String())
	}
	if answer == "" {
		return kindUnchosen, fmt.Errorf("make what: %s", strings.Join(offered, ", "))
	}

	var matched []wizardKind
	for _, kind := range kinds {
		if strings.EqualFold(kind.String(), answer) {
			return kind, nil
		}
		if strings.HasPrefix(kind.String(), strings.ToLower(answer)) {
			matched = append(matched, kind)
		}
	}
	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		return kindUnchosen, fmt.Errorf("there is nothing called %q to make: %s", answer, strings.Join(offered, ", "))
	default:
		spellings := make([]string, 0, len(matched))
		for _, kind := range matched {
			spellings = append(spellings, kind.String())
		}
		return kindUnchosen, fmt.Errorf("%q could be %s: say which", answer, strings.Join(spellings, " or "))
	}
}

func names(choices []wizardChoice) string {
	spellings := make([]string, 0, len(choices))
	for _, choice := range choices {
		spellings = append(spellings, choice.name)
	}
	return strings.Join(spellings, " or ")
}

// summary says what is about to be made, so the last thing before anything happens is a sentence
// somebody can disagree with.
func (w wizard) summary() string {
	switch w.kind {
	case kindWorkspace:
		return "workspace " + w.name
	case kindProject:
		return "project " + w.name + " in " + w.workspace.name
	case kindSecret:
		return "a subscription token for " + w.workspace.name
	case kindContext:
		return "the context for " + w.where()
	case kindSession:
		return "a session in " + w.where()
	case kindSkill:
		return "the " + w.skill.name + " skill on " + w.workspace.name
	default:
		return ""
	}
}

// updateWizardKey handles the wizard: type, enter to accept, escape to cancel everything.
func (m Model) updateWizardKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Nothing has been made yet, by construction, so there is nothing to undo. Escape works while
		// the system is being asked too, so a system that never answers is not a trap.
		m.mode, m.making = modeBrowse, wizard{}
		return m, nil
	}
	if m.making.step() == stepWorking {
		// The system is making it and the wizard is asking nothing, so a key is not an answer to
		// anything. Taken as an empty answer to this step, enter is refused as "making it: this one is
		// needed", naming no question anybody was asked.
		return m, nil
	}

	switch msg.String() {
	case "enter":
		// In the guided setup an empty answer skips the stage: every question there is an offer.
		// Skipping the workspace leaves the setup, because nothing later can exist without one.
		if m.making.guided && strings.TrimSpace(m.making.typed) == "" {
			if m.making.kind == kindWorkspace {
				m.mode, m.making = modeBrowse, wizard{}
				return m, nil
			}
			return m.advanceGuided()
		}
		next, err := m.making.accept()
		if err != nil {
			m.err = err
			return m, nil
		}
		m.making, m.err = next, nil
		if m.making.step() == stepWorking {
			return m, makeCmd(m.client, m.making)
		}
		return m, m.wizardChoicesCmd()
	case "backspace":
		m.making.typed = trimLastRune(m.making.typed)
		m.making.cycling = false
		return m, nil
	case "tab":
		m.making = m.making.cycle(1)
		return m, nil
	case "shift+tab":
		m.making = m.making.cycle(-1)
		return m, nil
	}
	m.making.typed += typedText(msg)
	m.making.cycling = false
	return m, nil
}

// advanceGuided moves the setup to its next stage, carrying what earlier stages made and dropping
// stages whose parent was skipped. When nothing is left it closes, and the refreshed listing is the
// answer to what the setup built.
func (m Model) advanceGuided() (Model, tea.Cmd) {
	prior := m.making
	chain := prior.chain
	for len(chain) > 0 {
		next := chain[0]
		chain = chain[1:]
		if next.needsProject() && prior.project.id == "" {
			continue
		}
		making := wizard{kind: next, guided: true, chain: chain,
			workspace: prior.workspace, project: prior.project}
		making.skipAnsweredPicks()
		m.making, m.err = making, nil
		return m, m.wizardChoicesCmd()
	}
	m.mode, m.making = modeBrowse, wizard{}
	return m, listCmd(m.active, m.parent)
}

// wizardChoicesCmd asks the system what the step it has just arrived at can offer, and nothing at all
// when the step takes free text.
func (m Model) wizardChoicesCmd() tea.Cmd {
	step := m.making.step()
	if !m.making.picking() {
		return nil
	}
	client, workspace := m.client, m.making.workspace.id
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var choices []wizardChoice
		switch step {
		case stepPickWorkspace:
			listed, err := client.ListWorkspaces(ctx, &quaycrewv1.ListWorkspacesRequest{})
			if err != nil {
				return wizardChoicesMsg{step: step, err: fmt.Errorf("the workspaces: %w", err)}
			}
			for _, workspace := range listed.GetWorkspaces() {
				choices = append(choices, wizardChoice{id: workspace.GetId(), name: workspace.GetName()})
			}
		case stepPickProject:
			listed, err := client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{Workspace: workspace})
			if err != nil {
				return wizardChoicesMsg{step: step, err: fmt.Errorf("the projects: %w", err)}
			}
			for _, project := range listed.GetProjects() {
				choices = append(choices, wizardChoice{id: project.GetId(), name: project.GetName()})
			}
		case stepPickSkill:
			// The store's skills, because attaching is what this stage does and only imported
			// skills attach. The system's own directory reaches every session without being asked.
			listed, err := client.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{})
			if err != nil {
				return wizardChoicesMsg{step: step, err: fmt.Errorf("the skills: %w", err)}
			}
			for _, one := range listed.GetSkills() {
				choices = append(choices, wizardChoice{id: one.GetName(), name: one.GetName()})
			}
		}
		return wizardChoicesMsg{step: step, choices: choices}
	}
}

// applyWizardChoices installs what a step can offer, ignoring an answer for a step already left. The
// wizard can be several keystrokes further on by the time a listing comes back.
func (m Model) applyWizardChoices(msg wizardChoicesMsg) Model {
	if m.mode != modeWizard || m.making.step() != msg.step {
		return m
	}
	if msg.err != nil {
		m.err = msg.err
		return m
	}
	m.making.choices, m.making.loaded = msg.choices, true
	return m
}

// makeCmd asks the system to make the one thing that was answered. It touches exactly one call, so a
// wizard opened to add a project cannot leave a workspace behind it.
func makeCmd(client quaycrewv1.ControlPlaneServiceClient, plan wizard) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		switch plan.kind {
		case kindWorkspace:
			made, err := client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{
				Name: plan.name,
			})
			if err != nil {
				return actionDoneMsg{kind: plan.kind, err: fmt.Errorf("workspace %s: %w", plan.name, err)}
			}
			return actionDoneMsg{kind: plan.kind, made: wizardChoice{
				id: made.GetWorkspace().GetId(), name: made.GetWorkspace().GetName()}}
		case kindProject:
			made, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
				Workspace: plan.workspace.id, Name: plan.name,
			})
			if err != nil {
				return actionDoneMsg{kind: plan.kind, err: fmt.Errorf("project %s: %w", plan.name, err)}
			}
			return actionDoneMsg{kind: plan.kind, made: wizardChoice{
				id: made.GetProject().GetId(), name: made.GetProject().GetName()}}
		case kindSecret:
			if _, err := client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
				Workspace: plan.workspace.id,
				Key:       model.ClaudeCodeOAuthTokenEnv,
				Value:     plan.secret,
			}); err != nil {
				// Never the value, not even in an error: an error is a thing people paste.
				return actionDoneMsg{kind: plan.kind, err: fmt.Errorf("the subscription token for %s: %w", plan.workspace.name, err)}
			}
		case kindContext:
			body, err := contextBody(plan.context)
			if err != nil {
				return actionDoneMsg{kind: plan.kind, err: err}
			}
			if _, err := client.SetContext(ctx, &quaycrewv1.SetContextRequest{
				Scope: "project", Owner: plan.project.id, Body: body,
			}); err != nil {
				return actionDoneMsg{kind: plan.kind, err: fmt.Errorf("the context for %s: %w", plan.where(), err)}
			}
		case kindSession:
			// Detached, because a task takes as long as the job takes and the console has a screen to
			// draw. Waiting for one meant the wizard held every key until the thirty second deadline
			// below killed the task, and the session it left had a container, a row, and no
			// conversation. The row says running until it lands.
			if _, err := client.Dispatch(ctx, &quaycrewv1.DispatchRequest{
				Project: plan.project.id, Text: plan.message, PermissionMode: plan.mode, Detach: true,
			}); err != nil {
				return actionDoneMsg{kind: plan.kind, err: fmt.Errorf("a session in %s: %w", plan.where(), err)}
			}
		case kindSkill:
			if _, err := client.AttachSkill(ctx, &quaycrewv1.AttachSkillRequest{
				Workspace: plan.workspace.id, Name: plan.skill.name,
			}); err != nil {
				return actionDoneMsg{kind: plan.kind, err: fmt.Errorf("the %s skill on %s: %w", plan.skill.name, plan.workspace.name, err)}
			}
		default:
			return actionDoneMsg{err: fmt.Errorf("the wizard was asked to make nothing in particular")}
		}
		return actionDoneMsg{kind: plan.kind}
	}
}

// contextBody is what the context step answers with: the text itself, or the contents of a file when
// the answer names one. Typed answers cannot hold a newline, so a path is any single line an existing
// regular file answers to, and everything else is taken as the context it already is.
func contextBody(typed string) (string, error) {
	path := strings.TrimSpace(typed)
	if path == "" || strings.ContainsAny(path, "\n") {
		return typed, nil
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return typed, nil
	}
	read, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(read), nil
}
