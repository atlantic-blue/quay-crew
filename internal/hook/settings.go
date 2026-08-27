package hook

import (
	"encoding/json"
	"fmt"
	"path"
)

// SettingsFile is what the rendered settings are called inside the hooks directory. The model runtime
// is pointed at it explicitly rather than finding it, which is the whole reason it can live here.
const SettingsFile = "settings.json"

// StatusLineCommand is what the runtime runs to draw the line under the conversation. It is the tool
// the image already carries, so this needs nothing installed and nothing configured.
const StatusLineCommand = "quay statusline"

// settings is the runtime's settings document: the hooks a session runs under, and the line the
// runtime keeps under the conversation.
//
// The crew owns this file completely. The alternative was rendering into the conversation directory's
// own settings, which the runtime writes and the operator edits, and that would mean merging on every
// task and losing somebody's edit the first time the merge was wrong.
//
// The status line is here rather than in the image for a harder reason than ownership: the crew
// mounts the workspace's own directory over the conversation directory in every sandbox, and a mount
// hides whatever the image put at that path. Settings shipped in the image are settings no session
// ever reads.
type settings struct {
	Hooks      map[string][]matcherGroup `json:"hooks"`
	StatusLine statusLine                `json:"statusLine"`
}

// statusLine is the runtime's status line configuration. A command, which is the only kind the
// runtime runs.
type statusLine struct {
	Type    string `json:"type"`
	Command string `json:"command"`
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

// Settings renders what the crew tells the model runtime: the hooks a session holds, and the line the
// runtime keeps under the conversation.
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
	document := settings{
		Hooks:      map[string][]matcherGroup{},
		StatusLine: statusLine{Type: "command", Command: StatusLineCommand},
	}
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
