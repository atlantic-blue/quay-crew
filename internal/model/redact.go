package model

import (
	"regexp"
	"sort"
	"strings"
)

// A task runs with the subscription token in its environment, so anything a failing task says about
// itself is a place that token can task up: the model's own error text, the tail of what it wrote to
// its error stream, a shell echoing the command it could not run. A tool that prints a subscription
// token into a terminal or a log because a task failed would be a worse defect than the one it was
// explaining, so nothing reaches an operator without going through here first.

// tokenShaped matches a subscription token by its published shape, as a second line of defence for a
// value this process never held: one printed by the model's own tooling out of its configuration, for
// example, which redactValues cannot know about because it was never passed in.
var tokenShaped = regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{8,}`)

// Redact removes every secret it can account for from text. Values passed in the environment map
// are matched exactly, which is precise and cannot mistake something innocent for a secret, and the
// published token shape is matched as well for the ones that were never passed through here. It is
// used on anything that leaves the system or is written down: a failure an operator reads, and every
// task payload before it is persisted.
func Redact(text string, env map[string]string) string {
	if text == "" {
		return ""
	}
	// Longest first, so a value that contains another is replaced whole rather than leaving the tail
	// of one behind.
	keys := make([]string, 0, len(env))
	for key, value := range env {
		if len(value) >= shortestSecret {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return len(env[keys[i]]) > len(env[keys[j]]) })

	for _, key := range keys {
		text = strings.ReplaceAll(text, env[key], "<redacted "+key+">")
	}
	return tokenShaped.ReplaceAllString(text, "<redacted>")
}

// shortestSecret is the length below which an environment value is not treated as a secret. A short
// value is far more likely to be a setting than a credential, and replacing every occurrence of, say,
// "1" would task an explanation into nonsense.
const shortestSecret = 12
