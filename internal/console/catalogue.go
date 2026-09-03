package console

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
)

// The three listings of what the system holds: the roles a job can be run as, the skills a session is
// given, and the hooks a session runs under. They change rarely, and they belong on the screen an
// operator lives in for the reason the secrets view does: the question "what does this system hold"
// had one answer, and it was on the command line.
//
// Each one lists the whole catalogue, which is what `krewe role list` and its two neighbours answer
// with no address. What a single workspace holds is a narrower question, and the console has no way
// to ask it yet: these views are opened by name rather than descended into, so nothing hands them a
// workspace to scope by.

// Roles lists what a job can be run as: the name, the model that role uses, and the material it is
// given.
func Roles(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "roles",
		Aliases: []string{"role"},
		Columns: []Column{
			{Title: "role", Width: 22, Colour: colourOfName},
			{Title: "version", Width: 7, Colour: dim},
			{Title: "reaches", Width: 11, Colour: dim},
			// Which model the work is done on. It gives way first: most of a listing is one model,
			// and by then the summary is worth more than the column is.
			{Title: "model", Width: 14, Give: 1, Colour: dim},
			{Title: "summary", Width: 0},
		},
		SortBy: 0,
		List: func(ctx context.Context, _ string) ([]Row, error) {
			resp, err := client.ListRoles(ctx, &quaycrewv1.ListRolesRequest{})
			if err != nil {
				return nil, err
			}
			rows := make([]Row, 0, len(resp.GetRoles()))
			for _, one := range resp.GetRoles() {
				rows = append(rows, Row{
					ID:    one.GetName(),
					Label: one.GetName(),
					State: StateReady,
					Cells: []string{
						one.GetName(),
						versionCell(one.GetVersion()),
						reachCell(one.GetSystem()),
						one.GetModel(),
						oneLine(one.GetSummary()),
					},
					Detail: one.GetSummary(),
				})
			}
			return rows, nil
		},
	}
}

// Skills lists what a session is given to work with: the name, whether every workspace has it, the
// commands it needs in the sandbox, and what it is for.
func Skills(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "skills",
		Aliases: []string{"skill"},
		Columns: []Column{
			{Title: "skill", Width: 20, Colour: colourOfName},
			{Title: "version", Width: 7, Colour: dim},
			{Title: "reaches", Width: 11, Colour: dim},
			// The commands the sandbox image has to carry. It gives way first: a session missing one
			// is refused before a task runs, and that refusal names the command anyway.
			{Title: "needs", Width: 16, Give: 1, Colour: dim},
			{Title: "summary", Width: 0},
		},
		SortBy: 0,
		List: func(ctx context.Context, _ string) ([]Row, error) {
			resp, err := client.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{})
			if err != nil {
				return nil, err
			}
			rows := make([]Row, 0, len(resp.GetSkills()))
			for _, one := range resp.GetSkills() {
				rows = append(rows, Row{
					ID:    one.GetName(),
					Label: one.GetName(),
					State: StateReady,
					Cells: []string{
						one.GetName(),
						versionCell(one.GetVersion()),
						reachCell(one.GetSystem()),
						strings.Join(one.GetBinaries(), " "),
						oneLine(one.GetSummary()),
					},
					Detail: one.GetSummary(),
				})
			}
			return rows, nil
		},
	}
}

// Hooks lists what a session runs under: the name, what it fires on, and what it is for.
func Hooks(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "hooks",
		Aliases: []string{"hook"},
		Columns: []Column{
			{Title: "hook", Width: 20, Colour: colourOfName},
			{Title: "version", Width: 7, Colour: dim},
			{Title: "reaches", Width: 11, Colour: dim},
			// What the runtime calls it on. It gives way first, because a hook nobody can see the
			// summary of is a hook nobody knows the purpose of either.
			{Title: "fires on", Width: 22, Give: 1, Colour: dim},
			{Title: "summary", Width: 0},
		},
		SortBy: 0,
		List: func(ctx context.Context, _ string) ([]Row, error) {
			resp, err := client.ListHooks(ctx, &quaycrewv1.ListHooksRequest{})
			if err != nil {
				return nil, err
			}
			rows := make([]Row, 0, len(resp.GetHooks()))
			for _, one := range resp.GetHooks() {
				rows = append(rows, Row{
					ID:    one.GetName(),
					Label: one.GetName(),
					State: StateReady,
					Cells: []string{
						one.GetName(),
						versionCell(one.GetVersion()),
						reachCell(one.GetSystem()),
						firesOn(one),
						oneLine(one.GetSummary()),
					},
					Detail: one.GetSummary(),
				})
			}
			return rows, nil
		},
	}
}

// versionCell is the version as a person writes it, so the cell reads the way `krewe skill list`
// prints it rather than as a bare number in a column of names.
func versionCell(version int32) string {
	return fmt.Sprintf("v%d", version)
}

// reachCell says how far a held thing gets. The system holds some of them, and every workspace has
// those without attaching anything; the rest reach a session only where somebody attached them.
//
// It is the question an operator actually has in front of this listing, and the one the row cannot
// answer with a name and a version alone: a workspace nobody attached anything to still has four
// skills, and this is why.
func reachCell(system bool) string {
	if system {
		return everyWorkspace
	}
	return onAttachment
}

const (
	everyWorkspace = "everywhere"
	onAttachment   = "on attach"
)

// No row here says a skill is held and not given. That reason belongs to a workspace or to a session,
// because what leaves a skill out is a secret one of them has not set, and these views ask for the
// system's own catalogue, which has no workspace to answer for. The control plane says so itself: it
// fills `left_out` on a workspace listing and on a session listing, and never on this one. A cell
// reading it here would be empty on every row, and a test proving it would need a double that answers
// what the system never says.
//
// `krewe skill list <workspace>` and `krewe skill list <session>` are where that question is answered
// today. The console can ask it the day one of these views is scoped to a workspace.

// firesOn is the events a hook answers to, with the tools each one is narrowed to. A hook bound to
// every tool says the event alone, because "PreToolUse()" reads as a matcher that failed to print.
func firesOn(one *quaycrewv1.Hook) string {
	events := make([]string, 0, len(one.GetEvents()))
	for _, binding := range one.GetEvents() {
		if binding.GetMatcher() == "" {
			events = append(events, binding.GetOn())
			continue
		}
		events = append(events, binding.GetOn()+"("+binding.GetMatcher()+")")
	}
	return strings.Join(events, " ")
}
