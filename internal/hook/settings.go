package hook

import (
	"encoding/json"
	"fmt"
	"path"
)

// SettingsFile is what the rendered settings are called inside the hooks directory. The model runtime
// is pointed at it explicitly rather than finding it, which is the whole reason it can live here.
const SettingsFile = "settings.json"

// settings is the runtime's settings document, carrying nothing but hooks.
//
// The crew owns this file completely. The alternative was rendering into the conversation directory's
// own settings, which the runtime writes and the operator edits, and that would mean merging on every
// task and losing somebody's edit the first time the merge was wrong.
type settings struct {
	Hooks map[string][]matcherGroup `json:"hooks"`
}

// matcherGroup is every hook firing on one event for one matcher. Grouped rather than one entry per
// hook, because that is the shape the runtime's own documentation and every hand written settings
// file use, and a file that does not look like the ones people write is a file people misread.
type matcherGroup struct {
	Matcher string   `json:"matcher,omitempty"`
	Hooks   []action `json:"hooks"`
}

// action is one command the runtime runs.
type action struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	// Timeout is in seconds, and is left out entirely at zero so the runtime applies its own default
	// rather than being told to wait for no time at all.
	Timeout int `json:"timeout,omitempty"`
}

// Settings renders the hooks a session holds into the runtime's settings document.
//
// root is where the hooks are mounted as the sandbox sees them, because the command the runtime runs
// is an absolute path inside the container and nothing here knows that path any other way.
//
// The result is stable for the same input: events come out sorted by name, because Go marshals a map
// that way, and within an event the matchers and the commands keep the order the hooks were written
// in. A settings file that reordered itself between renders would be a diff nobody could review, and
// would rewrite a file on every task for no reason.
func Settings(root string, hooks []Hook) ([]byte, error) {
	if root == "" {
		return nil, fmt.Errorf("hook: rendering settings needs the path the hooks are mounted at")
	}
	document := settings{Hooks: map[string][]matcherGroup{}}
	// Where each matcher's group sits inside its event, so a second hook on the same event and matcher
	// joins the group rather than starting another one.
	at := map[string]int{}

	for _, one := range hooks {
		for _, binding := range one.Events {
			command := path.Join(root, one.Name, binding.Entry)
			key := binding.On + "\x00" + binding.Matcher
			run := action{Type: "command", Command: command, Timeout: binding.TimeoutSeconds}

			if index, grouped := at[key]; grouped {
				document.Hooks[binding.On][index].Hooks =
					append(document.Hooks[binding.On][index].Hooks, run)
				continue
			}
			at[key] = len(document.Hooks[binding.On])
			document.Hooks[binding.On] = append(document.Hooks[binding.On], matcherGroup{
				Matcher: binding.Matcher,
				Hooks:   []action{run},
			})
		}
	}

	rendered, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("hook: render settings: %w", err)
	}
	return append(rendered, '\n'), nil
}
