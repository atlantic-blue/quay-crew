package console

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/model"
	tea "github.com/charmbracelet/bubbletea"
)

// The wizard makes one thing. It asks what first, then only the questions that thing needs, and where
// a question needs a parent it offers what the crew already has rather than a blank name.
//
// It shipped able to make a whole crew and nothing else: a workspace and a project were both required
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
)

// kinds is every kind in the order they are offered, which is the order they depend on each other:
// nothing later can be made without something earlier.
var kinds = []wizardKind{kindWorkspace, kindProject, kindSecret, kindContext, kindSession}

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
	// turn.
	stepSecret
	// stepContext takes what the model should be told about a project.
	stepContext
	// stepMessage takes a first message, which is the only way a session comes into existence.
	stepMessage
	// stepWorking is the crew being asked to make it.
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
		return []wizardStep{stepPickWorkspace, stepPickProject, stepMessage}
	default:
		return nil
	}
}

// wizardChoice is one thing the crew already has, offered where a step needs a parent.
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

	// workspace and project are what was picked, so they carry an identifier the crew gave us rather
	// than a name to be resolved later.
	workspace wizardChoice
	project   wizardChoice

	name    string
	secret  string
	context string
	message string

	// choices is what the current step can offer, and loaded says the crew has answered. The two are
	// separate because no workspaces at all is a real answer and needs its own sentence.
	choices []wizardChoice
	loaded  bool
}

// step is the question being asked right now.
func (w wizard) step() wizardStep {
	if w.kind == kindUnchosen {
		return stepKind
	}
	steps := w.kind.steps()
	if w.at >= len(steps) {
		return stepWorking
	}
	return steps[w.at]
}

// picking says this step chooses from what exists rather than taking free text.
func (w wizard) picking() bool {
	return w.step() == stepPickWorkspace || w.step() == stepPickProject
}

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
		return "new workspace, lowercase and hyphens"
	case stepSecret:
		return "subscription token for " + w.workspace.name
	case stepContext:
		return "what the model should know about " + w.where()
	case stepMessage:
		return "first message to " + w.where()
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

// shown is what the operator sees of what they have typed. A secret is never echoed: a value on a
// screen is a value in that terminal's scrollback, and this one runs every turn the crew makes.
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
	case stepMessage:
		if answer == "" {
			return w, fmt.Errorf("%s: this one is needed", w.prompt())
		}
		w.message = answer
	}

	w.typed = ""
	w.choices, w.loaded = nil, false
	if current != stepKind {
		w.at++
	}
	return w, nil
}

// pick resolves what was typed against what the crew already has. It never creates: the whole point of
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
	default:
		return ""
	}
}

// updateWizardKey handles the wizard: type, enter to accept, escape to cancel everything.
func (m Model) updateWizardKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		// Nothing has been made yet, by construction, so there is nothing to undo.
		m.mode, m.making = modeBrowse, wizard{}
		return m, nil
	case "enter":
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
		return m, nil
	}
	m.making.typed += typedText(msg)
	return m, nil
}

// wizardChoicesCmd asks the crew what the step it has just arrived at can offer, and nothing at all
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

// makeCmd asks the crew to make the one thing that was answered. It touches exactly one call, so a
// wizard opened to add a project cannot leave a workspace behind it.
func makeCmd(client quaycrewv1.ControlPlaneServiceClient, plan wizard) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		switch plan.kind {
		case kindWorkspace:
			if _, err := client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{
				Name: plan.name,
			}); err != nil {
				return actionDoneMsg{err: fmt.Errorf("workspace %s: %w", plan.name, err)}
			}
		case kindProject:
			if _, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
				Workspace: plan.workspace.id, Name: plan.name,
			}); err != nil {
				return actionDoneMsg{err: fmt.Errorf("project %s: %w", plan.name, err)}
			}
		case kindSecret:
			if _, err := client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
				Workspace: plan.workspace.id,
				Key:       model.ClaudeCodeOAuthTokenEnv,
				Value:     plan.secret,
			}); err != nil {
				// Never the value, not even in an error: an error is a thing people paste.
				return actionDoneMsg{err: fmt.Errorf("the subscription token for %s: %w", plan.workspace.name, err)}
			}
		case kindContext:
			if _, err := client.SetContext(ctx, &quaycrewv1.SetContextRequest{
				Scope: "project", Owner: plan.project.id, Body: plan.context,
			}); err != nil {
				return actionDoneMsg{err: fmt.Errorf("the context for %s: %w", plan.where(), err)}
			}
		case kindSession:
			if _, err := client.Dispatch(ctx, &quaycrewv1.DispatchRequest{
				Project: plan.project.id, Text: plan.message,
			}); err != nil {
				return actionDoneMsg{err: fmt.Errorf("a session in %s: %w", plan.where(), err)}
			}
		default:
			return actionDoneMsg{err: fmt.Errorf("the wizard was asked to make nothing in particular")}
		}
		return actionDoneMsg{}
	}
}
