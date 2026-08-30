package console

import (
	"context"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	tea "github.com/charmbracelet/bubbletea"
)

// Default is the resource the console opens on.
const Default = "sessions"

// Registry builds the console's resources against a control plane client. Adding a view to the
// console means adding a Resource here.
func NewDefaultRegistry(client quaycrewv1.ControlPlaneServiceClient) (*Registry, error) {
	if client == nil {
		return nil, fmt.Errorf("console: nil control plane client")
	}
	registry, err := NewRegistry(Sessions(client), Jobs(client), Tasks(client), Archived(client), Projects(client),
		Workspaces(client), Contexts(client), Secrets(client), Stats(client), Room(client))
	if err != nil {
		return nil, err
	}
	// The keys view reads the registry it lives in, so it is added once that exists.
	if err := registry.Add(Keys(registry)); err != nil {
		return nil, err
	}
	return registry, nil
}

// movedViews are the words the console used to open and does not any more, against what to type
// instead. A view that leaves the switcher belongs here in the same change that takes it out of the
// registry: left out of both, the word falls through to the tool and comes back as
// `unknown command "f"`, which reads as the console being broken rather than as a word that moved.
//
// The features view is what put a table here. The whole word is not in it, because the bar runs a
// command and `krewe features` prints the same list: only the short spellings have nowhere to land.
var movedViews = map[string]string{
	"f":            featuresAreACommand,
	"feature":      featuresAreACommand,
	"capabilities": featuresAreACommand,
}

// featuresAreACommand is what to type instead of the view that went.
const featuresAreACommand = "what this build does is a command now, so type features"

// moved says whether a word the console used to open has gone, and what to type instead of it.
func moved(typed string) (string, bool) {
	instead, gone := movedViews[cleanToken(typed)]
	return instead, gone
}

// RoomFrom asks the system what the machine has left: one figure and one word.
//
// The system answers from its own last sample rather than from the daemon, so this may be asked every
// second. A system too old to answer, or one that could not read its machine, leaves both empty and
// the header says nothing rather than claiming room nobody measured.
func RoomFrom(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) (line, state string) {
	answer, err := client.GetHeadroom(ctx, &quaycrewv1.GetHeadroomRequest{})
	if err != nil {
		return "", ""
	}
	return answer.GetUsed() + " of " + answer.GetLimit(), answer.GetState()
}

// InfoFrom asks the control plane what it is running and folds the answer into what the caller
// already knows: which build this tool is, where it dialled, and where the operator is standing.
// None of those three are the control plane's to say.
func InfoFrom(client quaycrewv1.ControlPlaneServiceClient, known Info) InfoSource {
	return func(ctx context.Context) (Info, error) {
		resp, err := client.GetInfo(ctx, &quaycrewv1.GetInfoRequest{})
		if err != nil {
			return Info{}, err
		}
		known.Model, known.Sandbox = resp.GetModel(), resp.GetSandbox()
		known.Store, known.State, known.Events = resp.GetStore(), resp.GetState(), resp.GetEvents()
		known.Secrets = resp.GetSecrets()
		known.SandboxBuild = resp.GetSandboxBuild()
		// What the system has cost, which is a running total rather than configuration, so it comes
		// from its own call. A system that cannot answer still has a header worth drawing, so this
		// failure is swallowed where the one above is not.
		if spent, err := client.GetUsage(ctx, &quaycrewv1.GetUsageRequest{}); err == nil {
			known.Spent = sandbox.Usage{
				Input:        spent.GetTotal().GetInput(),
				Output:       spent.GetTotal().GetOutput(),
				CacheRead:    spent.GetTotal().GetCacheRead(),
				CacheWritten: spent.GetTotal().GetCacheWritten(),
			}
		}
		// What the machine has left, from the system's own last sample. Swallowed the same way and for
		// the same reason: a system that cannot say still has a header worth drawing.
		known.Room, known.RoomState = RoomFrom(ctx, client)
		return known, nil
	}
}

// Run opens the full screen console and returns when the operator quits. known is what the caller
// can say without asking anybody: the build, the address, and the current context.
// beside is how the console opens a conversation next to itself when the key for it is pressed. It is
// handed in because picking which conversation, and how to open it, belongs to the command line.
func Run(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, known Info,
	beside func(selected string) ([]string, error), freshen func(selected string) error) error {
	registry, err := NewDefaultRegistry(client)
	if err != nil {
		return err
	}
	model, err := New(registry, Default, InfoFrom(client, known))
	if err != nil {
		return err
	}
	// Show what is already known while the control plane is still being asked, rather than an empty
	// block that fills in a moment later.
	model = model.WithInfo(known).WithClient(client).Beside(beside).Freshen(freshen).
		WithCommandRunner(TheToolItself())
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx))
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("console: %w", err)
	}
	return nil
}

// Plain prints one resource as lines and returns. It is what runs when no terminal is attached, so
// the console stays pipeable instead of drawing escape codes into a file.
func Plain(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, out io.Writer) error {
	registry, err := NewDefaultRegistry(client)
	if err != nil {
		return err
	}
	resource, found := registry.Get(Default)
	if !found {
		return fmt.Errorf("console: no resource named %q", Default)
	}
	rows, err := resource.List(ctx, "")
	if err != nil {
		return fmt.Errorf("console: list %s: %w", resource.Name, err)
	}
	if len(rows) == 0 {
		fmt.Fprintf(out, "no %s\n", resource.Name)
		return nil
	}
	for _, row := range rows {
		fmt.Fprintln(out, strings.Join(row.Cells, "  "))
	}
	return nil
}

// HeaderOnly renders the header block a console would draw, with nothing under it. The panel draws it
// in a pane of its own, across the full width, above the list and the conversation.
//
// It goes through the same code the console's own header does rather than a copy of it, because two
// renderings of one header drift, and the one nobody is looking at drifts first.
func HeaderOnly(registry *Registry, view string, info Info, width, height int) ([]string, error) {
	if registry == nil {
		return nil, fmt.Errorf("console: nil registry")
	}
	resource, found := registry.Get(view)
	if !found {
		return nil, fmt.Errorf("console: no resource named %q", view)
	}
	model := Model{registry: registry, active: resource, info: info, width: width, height: height}
	lines := model.headerLines()
	// Never taller than the pane it is drawn in. A header that overflows scrolls, and what scrolls
	// away is its own first line.
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return lines, nil
}

// HeaderHeight is how many rows the header needs at a size, so the panel can give its pane exactly
// that many and no more.
func HeaderHeight(registry *Registry, view string, info Info, width, height int) int {
	lines, err := HeaderOnly(registry, view, info, width, height)
	if err != nil {
		return 0
	}
	return len(lines)
}

// RunBare is the panel's lower left: the console with no header of its own, because the pane above is
// drawing it across both halves. It says which view it moves to through publish, so that header names
// this view's keys rather than the keys of the view it opened on.
func RunBare(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, known Info,
	beside func(selected string) ([]string, error), freshen func(selected string) error,
	publish func(view string) error) error {
	registry, err := NewDefaultRegistry(client)
	if err != nil {
		return err
	}
	model, err := New(registry, Default, InfoFrom(client, known))
	if err != nil {
		return err
	}
	model = model.WithInfo(known).WithClient(client).Beside(beside).Freshen(freshen).
		WithCommandRunner(TheToolItself()).WithoutHeader().WithViewPublisher(publish)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx))
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("console: %w", err)
	}
	return nil
}
