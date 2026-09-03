package console

import (
	"context"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	tea "github.com/charmbracelet/bubbletea"
)

// Default is the resource the console opens on. It is the top of the tree: a first screen of every
// session in the system is a list with no relation between its rows, and one operator command makes
// eleven of them. A workspace opens its projects, a project opens its jobs, and a job opens the work
// running under it. The flat listings stay one word away in the command bar.
const Default = "workspaces"

// Registry builds the console's resources against a control plane client. Adding a view to the
// console means adding a Resource here.
func NewDefaultRegistry(client quaycrewv1.ControlPlaneServiceClient) (*Registry, error) {
	if client == nil {
		return nil, fmt.Errorf("console: nil control plane client")
	}
	registry, err := NewRegistry(Sessions(client), Archived(client), Projects(client),
		Workspaces(client), Contexts(client), Secrets(client), Skills(client),
		Hooks(client), Stats(client))
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
		known.Store, known.State = resp.GetStore(), resp.GetState()
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
		return known, nil
	}
}

// Run opens the full screen console and returns when the operator quits. known is what the caller
// can say without asking anybody: the build, the address, and the current context.
//
// beside is how the console opens a conversation next to itself when the key for it is pressed. It is
// handed in because picking which conversation, and how to open it, belongs to the command line.
//
// remembering is where it keeps the address it is standing at, so the next run opens there. A zero
// store is a console that remembers nothing and opens at the top.
func Run(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, known Info,
	beside func(selected string) ([]string, error), freshen func(selected string) error,
	remembering PlaceStore) error {
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
		WithCommandRunner(TheToolItself()).Remembering(remembering)
	if remembering.Load != nil {
		// A place that cannot be read is no place: the console opens at the top, which is where it
		// opened before it remembered anything.
		if where, err := remembering.Load(); err == nil {
			model = model.Resuming(where)
		}
	}
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
