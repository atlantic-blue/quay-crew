package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Guard is the variable that stops the hook analysing its own model call.
//
// The child runs with no settings sources, which is the first thing that stops it. This is the
// second guard on the same thing, because the cost of getting it wrong is a hook that calls itself.
const Guard = "CLAUDE_PROMPT_ANALYSER"

// kept are the CLAUDE_ variables the child keeps, against a rule that drops the rest.
//
// The rule exists so the child does not inherit what the running session set for itself. A credential
// is not that, and the difference is where it lives. On a machine with a logged in install the
// credential is a file, so stripping every CLAUDE_ variable costs nothing. In a quay sandbox there is
// no credentials file: the workspace's subscription arrives as CLAUDE_CODE_OAUTH_TOKEN, and dropping
// it left the child with nothing to authenticate with.
//
// What that looked like was the hook working: it ran in 946 milliseconds, exited 0 and let the
// message through, because it fails open. The child exited 1 with an empty standard error, and the
// only sign anywhere was the word "no answer" in a file in /tmp.
var kept = map[string]bool{"CLAUDE_CONFIG_DIR": true, "CLAUDE_CODE_OAUTH_TOKEN": true}

// payload is what the runtime writes to the hook on standard input.
type payload struct {
	Prompt string `json:"prompt"`
	Cwd    string `json:"cwd"`
}

// Options is everything Run needs from outside itself, so the whole hook can be driven by a test
// without a model, a checkout or a home directory behind it.
type Options struct {
	Config Config
	Stdin  io.Reader
	Stdout io.Writer
	FS     FileSystem
	// Now is read twice, at the start and at the end, and the difference is what the last run line
	// reports.
	Now func() time.Time
	// Facts is what is true around the message. Separate from Run because it shells out to git.
	Facts func(cwd string) Facts
	// Ask runs the analysis and returns what the model said, or an empty string for every kind of
	// failure. It never returns an error: there is nothing the hook would do differently.
	Ask func(system, user string) string
}

// Run is the hook.
//
// It fails open in every direction. A missing model, a timeout, an empty answer, a broken config
// file: each one ends with a message that still gets through. That is why it returns nothing for the
// caller to check, and why every path through it records how it went before returning.
func Run(o Options) {
	started := o.Now()

	var read payload
	body, err := io.ReadAll(o.Stdin)
	if err != nil || json.Unmarshal(body, &read) != nil {
		return
	}

	if o.Config.Skipped(read.Prompt) {
		o.finish(started, Skipped, read.Prompt, "")
		return
	}

	answer := o.Ask(SystemPrompt(), o.message(read))
	if strings.TrimSpace(answer) == "" {
		o.finish(started, NoAnswer, read.Prompt, Notice("no answer, carrying on"))
		return
	}

	analysis := FormatAnalysis(answer, o.Config.MaxAnalysisChars)
	if analysis == "" {
		o.finish(started, Passed, read.Prompt, Notice("nothing to add"))
		return
	}

	o.finish(started, Analysed, read.Prompt, Printed(read.Prompt, analysis))
}

// message is everything the model is given about this one prompt.
func (o Options) message(read payload) string {
	ask := Ask{
		Prompt: read.Prompt,
		Facts:  o.Facts(read.Cwd),
		Skills: CollectSkills(o.Config.SkillDirs, o.FS),
	}
	if o.Config.RulesFile != "" {
		if rules, err := o.FS.Read(o.Config.RulesFile); err == nil {
			ask.Rules = RuleIndex(string(rules))
		}
	}
	for _, path := range o.Config.Docs {
		text, err := o.FS.Read(path)
		if err != nil || len(text) == 0 {
			continue
		}
		ask.Docs = append(ask.Docs, Document{
			Label: label(path),
			Text:  Clip(string(text), o.Config.MaxDocChars),
		})
	}
	return ask.UserMessage()
}

// label is what a document is called in front of the model: its file name without the extension.
func label(path string) string {
	name := filepath.Base(path)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "document"
	}
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// finish ends the run, recording how it went before printing. Every path through the hook comes
// through here, so the last run file always describes the most recent message.
func (o Options) finish(started time.Time, outcome Outcome, prompt, out string) {
	if o.Config.LastRunFile != "" {
		line := LastRunLine(o.Now(), outcome, o.Now().Sub(started), prompt)
		// A hook that cannot write its own log still has a job to do.
		_ = o.FS.Write(o.Config.LastRunFile, []byte(line))
	}
	if out != "" {
		_, _ = io.WriteString(o.Stdout, out)
	}
}

// AskModel runs the analysis on a child Claude with no settings, no tools and no session on disk.
//
// MAX_THINKING_TOKENS=0 is what makes this usable at all. With extended thinking left on, the same
// call spent 3,855 thinking tokens and 42 seconds to produce five short lines. With it off the call
// is about 1.5 seconds of interface time.
func AskModel(config Config, home string, trace func(string, string)) func(string, string) string {
	return func(system, user string) string {
		ctx, stop := context.WithTimeout(context.Background(), config.Timeout)
		defer stop()

		started := time.Now()
		command := exec.CommandContext(ctx, "claude",
			"--print",
			"--model", config.Model,
			"--system-prompt", system,
			"--tools", "",
			"--setting-sources", "",
			"--strict-mcp-config",
			"--disable-slash-commands",
			"--no-session-persistence",
			"--output-format", "text",
		)
		command.Stdin = strings.NewReader(user)
		command.Dir = home
		command.Env = childEnv(os.Environ())

		out, err := command.Output()
		trace("model", time.Since(started).Truncate(time.Millisecond).String())
		if err != nil {
			trace("error", err.Error())
			return ""
		}
		trace("answer", strings.TrimSpace(string(out)))
		return string(out)
	}
}

// childEnv is the environment the child runs in: the parent's, minus the variables the running
// session set for itself, plus the guard and the thinking budget.
func childEnv(parent []string) []string {
	child := make([]string, 0, len(parent)+2)
	for _, entry := range parent {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if kept[name] {
			child = append(child, entry)
			continue
		}
		if strings.HasPrefix(name, "CLAUDE_") || name == "CLAUDECODE" {
			continue
		}
		child = append(child, entry)
	}
	return append(child, Guard+"=1", "MAX_THINKING_TOKENS=0")
}

var (
	remote = regexp.MustCompile(`[:/]([^/:]+)/([^/]+?)(?:\.git)?$`)
	ticket = regexp.MustCompile(`\b([A-Z]{2,5}-\d+)\b`)
)

// GatherFacts reads what is true around the message out of the checkout the session sits in.
func GatherFacts(home string, fs FileSystem) func(string) Facts {
	return func(cwd string) Facts {
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		branch := git(cwd, "rev-parse", "--abbrev-ref", "HEAD")
		key := ticketOf(branch, cwd)
		return Facts{
			Cwd:    cwd,
			Repo:   repoOf(cwd),
			Branch: branch,
			Ticket: key,
			State:  ticketState(key, home, fs),
		}
	}
}

// repoOf is the org and repo of the checkout the session sits in, as owner/name.
func repoOf(cwd string) string {
	match := remote.FindStringSubmatch(git(cwd, "config", "--get", "remote.origin.url"))
	if match == nil {
		return ""
	}
	return match[1] + "/" + match[2]
}

// ticketOf is a ticket key such as GUN-1620 or VP-1531, taken from the branch or the path.
func ticketOf(branch, cwd string) string {
	if match := ticket.FindStringSubmatch(strings.ToUpper(branch)); match != nil {
		return match[1]
	}
	if match := ticket.FindStringSubmatch(strings.ToUpper(cwd)); match != nil {
		return match[1]
	}
	return ""
}

var stateBlock = regexp.MustCompile("(?s)```state\n(.*?)```")

// ticketState is the fenced state block of a ticket's STATE.md, which says where the work stands.
func ticketState(key, home string, fs FileSystem) string {
	if key == "" {
		return ""
	}
	root := filepath.Join(home, "claude", "orgs")
	orgs, err := fs.List(root)
	if err != nil {
		return ""
	}
	for _, org := range orgs {
		body, err := fs.Read(filepath.Join(root, org, "tickets", key, "STATE.md"))
		if err != nil {
			continue
		}
		if match := stateBlock.FindStringSubmatch(string(body)); match != nil {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

// git runs one read only git command in the checkout, and treats every failure as no answer: this
// runs in whatever directory the session happens to be in, and that is often not a repository.
func git(cwd string, args ...string) string {
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()

	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
