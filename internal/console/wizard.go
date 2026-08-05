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

// The wizard makes the five things a crew is made of, in the order they depend on each other. Every
// view could look at what the crew had and almost none of it could be made from here, so making
// anything meant dropping to the command line and knowing what to type.
//
// It is a mode the keyboard is in, drawn where the command bar draws, following the confirmation step
// rather than inventing an overlay. Escape at any point creates nothing at all: a wizard that half
// creates is worse than no wizard, and it is the one behaviour here worth being certain of.
type wizardStep int

const (
	// wizardWorkspace names the workspace, or picks the one already in hand.
	wizardWorkspace wizardStep = iota
	// wizardProject names the body of work inside it.
	wizardProject
	// wizardSecret offers the subscription token, because a workspace without one cannot run a turn
	// and this is where somebody finds that out.
	wizardSecret
	// wizardContext offers what the model should be told about this project.
	wizardContext
	// wizardMessage offers a first message, which is the only way a session comes into existence.
	wizardMessage
	// wizardWorking is the crew being asked to make it all.
	wizardWorking
)

// wizard is what has been answered so far. It holds no identifiers: everything is made in one pass at
// the end, so cancelling has nothing to undo.
type wizard struct {
	step  wizardStep
	typed string

	workspace string
	project   string
	secret    string
	context   string
	message   string
}

// optional says whether a step can be skipped by answering nothing. A workspace and a project are what
// the wizard is for; the rest are offers.
func (w wizard) optional() bool {
	return w.step == wizardSecret || w.step == wizardContext || w.step == wizardMessage
}

// prompt is what the step asks, and what pressing enter accepts.
func (w wizard) prompt() string {
	switch w.step {
	case wizardWorkspace:
		return "new workspace, lowercase and hyphens"
	case wizardProject:
		return "project inside " + w.workspace
	case wizardSecret:
		return "subscription token for " + w.workspace + ", or enter to skip"
	case wizardContext:
		return "what the model should know about " + w.project + ", or enter to skip"
	case wizardMessage:
		return "first message, or enter to skip and make no session"
	default:
		return "making it"
	}
}

// shown is what the operator sees of what they have typed. A secret is never echoed: a value on a
// screen is a value in that terminal's scrollback, and this one runs every turn the crew makes.
func (w wizard) shown() string {
	if w.step == wizardSecret {
		return strings.Repeat("*", len([]rune(w.typed)))
	}
	return w.typed
}

// accept takes what was typed for this step and moves on, or reports what is missing.
func (w wizard) accept() (wizard, error) {
	answer := strings.TrimSpace(w.typed)
	if answer == "" && !w.optional() {
		return w, fmt.Errorf("%s: this one is needed", w.prompt())
	}

	switch w.step {
	case wizardWorkspace:
		w.workspace = answer
	case wizardProject:
		w.project = answer
	case wizardSecret:
		w.secret = w.typed
	case wizardContext:
		w.context = w.typed
	case wizardMessage:
		w.message = answer
	}
	w.typed = ""
	w.step++
	return w, nil
}

// summary says what is about to be made, so the last thing before anything happens is a sentence
// somebody can disagree with.
func (w wizard) summary() string {
	parts := []string{"workspace " + w.workspace, "project " + w.project}
	if w.secret != "" {
		parts = append(parts, "a subscription token")
	}
	if strings.TrimSpace(w.context) != "" {
		parts = append(parts, "its context")
	}
	if w.message != "" {
		parts = append(parts, "a session")
	}
	return strings.Join(parts, ", ")
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
		if m.making.step == wizardWorking {
			return m, makeCmd(m.client, m.making)
		}
		return m, nil
	case "backspace":
		m.making.typed = trimLastRune(m.making.typed)
		return m, nil
	}
	m.making.typed += typedText(msg)
	return m, nil
}

// makeCmd asks the crew to make everything that was answered, in the order the pieces depend on each
// other. It stops at the first refusal and says which piece it was on, because "invalid argument" with
// no idea which of five calls it came from is not something anybody can act on.
func makeCmd(client quaycrewv1.ControlPlaneServiceClient, plan wizard) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		workspace, err := client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: plan.workspace})
		if err != nil {
			return actionDoneMsg{err: fmt.Errorf("workspace %s: %w", plan.workspace, err)}
		}
		project, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
			Workspace: workspace.GetWorkspace().GetId(), Name: plan.project,
		})
		if err != nil {
			return actionDoneMsg{err: fmt.Errorf("project %s: %w", plan.project, err)}
		}

		if plan.secret != "" {
			if _, err := client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
				Workspace: workspace.GetWorkspace().GetId(),
				Key:       model.ClaudeCodeOAuthTokenEnv,
				Value:     plan.secret,
			}); err != nil {
				// Never the value, not even in an error: an error is a thing people paste.
				return actionDoneMsg{err: fmt.Errorf("the subscription token: %w", err)}
			}
		}
		if strings.TrimSpace(plan.context) != "" {
			if _, err := client.SetContext(ctx, &quaycrewv1.SetContextRequest{
				Scope: "project", Owner: project.GetProject().GetId(), Body: plan.context,
			}); err != nil {
				return actionDoneMsg{err: fmt.Errorf("the context for %s: %w", plan.project, err)}
			}
		}
		if plan.message != "" {
			if _, err := client.Dispatch(ctx, &quaycrewv1.DispatchRequest{
				Project: project.GetProject().GetId(), Text: plan.message,
			}); err != nil {
				return actionDoneMsg{err: fmt.Errorf("the first message: %w", err)}
			}
		}
		return actionDoneMsg{}
	}
}
