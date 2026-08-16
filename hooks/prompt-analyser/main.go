package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Guard stops the hook analysing its own model call. The child also runs with no settings sources,
// which is the first thing that stops it; this is the second, because the cost of getting it wrong
// is a hook that calls itself.
const Guard = "CLAUDE_PROMPT_ANALYSER"

// kept are the CLAUDE_ variables the child keeps, against a rule that drops the rest so it does not
// inherit what the running session set for itself.
//
// A credential is not that. On a logged in install it is a file, so dropping every CLAUDE_ variable
// costs nothing. A quay sandbox has no credentials file: the subscription arrives as
// CLAUDE_CODE_OAUTH_TOKEN, and dropping it left the child unable to authenticate. Nothing looked
// wrong, because the hook fails open: it exited 0 in under a second and the only sign anywhere was
// the word "no answer" in a file in /tmp.
var kept = map[string]bool{"CLAUDE_CONFIG_DIR": true, "CLAUDE_CODE_OAUTH_TOKEN": true}

// It always exits 0. An exit code the runtime reads as a refusal would block the message.
func main() {
	if os.Getenv(Guard) == "1" {
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}

	fs := OS{}
	config := Default(home)
	if body, err := fs.Read(configPath()); err == nil {
		config = LoadConfig(body, home)
	}

	Run(Options{
		Config: config,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		FS:     fs,
		Now:    time.Now,
		Facts:  GatherFacts(home, fs),
		Ask:    AskModel(config, home),
	})
}

// configPath finds hook.config.json from the running binary rather than the working directory,
// because the runtime runs the hook from wherever the session happens to be.
func configPath() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(self), "..", "hook.config.json")
}

func trace(label, detail string) {
	if os.Getenv("CLAUDE_PROMPT_ANALYSER_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "[prompt-analyser] %s: %s\n", label, detail)
	}
}

// OS is the real machine, and the only place in the hook that touches a file.
type OS struct{}

func (OS) List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names, nil
}

func (OS) Read(file string) ([]byte, error) { return os.ReadFile(file) }

func (OS) Write(file string, body []byte) error { return os.WriteFile(file, body, 0o644) }

// AskModel runs the analysis on a child Claude with no settings, no tools and no session on disk.
//
// MAX_THINKING_TOKENS=0 is what makes this usable at all. With extended thinking left on, the same
// call spent 3,855 thinking tokens and 42 seconds to produce five short lines. With it off it is
// about 1.5 seconds.
func AskModel(config Config, home string) func(string, string) string {
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

func childEnv(parent []string) []string {
	child := make([]string, 0, len(parent)+2)
	for _, entry := range parent {
		name, _, found := strings.Cut(entry, "=")
		switch {
		case !found:
		case kept[name]:
			child = append(child, entry)
		case strings.HasPrefix(name, "CLAUDE_") || name == "CLAUDECODE":
		default:
			child = append(child, entry)
		}
	}
	return append(child, Guard+"=1", "MAX_THINKING_TOKENS=0")
}

var (
	remote     = regexp.MustCompile(`[:/]([^/:]+)/([^/]+?)(?:\.git)?$`)
	ticket     = regexp.MustCompile(`\b([A-Z]{2,5}-\d+)\b`)
	stateBlock = regexp.MustCompile("(?s)```state\n(.*?)```")
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

func repoOf(cwd string) string {
	match := remote.FindStringSubmatch(git(cwd, "config", "--get", "remote.origin.url"))
	if match == nil {
		return ""
	}
	return match[1] + "/" + match[2]
}

func ticketOf(branch, cwd string) string {
	for _, where := range []string{branch, cwd} {
		if match := ticket.FindStringSubmatch(strings.ToUpper(where)); match != nil {
			return match[1]
		}
	}
	return ""
}

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

// git treats every failure as no answer: this runs wherever the session happens to be, and that is
// often not a repository.
func git(cwd string, args ...string) string {
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()

	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
