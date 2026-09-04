// Command hook reads the message a session was sent, asks a small model to restate it, and hands
// the session both. Claude Code never lets a hook replace what was typed, so it only ever adds.
//
// It fails open everywhere. A missing model, a timeout, an empty answer, a broken config file: each
// one ends with the message still getting through.
package main

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Pass is what the model returns when a message needs no analysis.
const Pass = "pass"

type Config struct {
	Model            string
	Timeout          time.Duration
	MaxAnalysisChars int
	MaxDocChars      int
	SkillDirs        []string
	Docs             []string
	RulesFile        string
	Skip             []string
	// LastRunFile is overwritten with one line per run, which is how you tell a hook that fired and
	// stayed quiet from one that never fired. Empty turns it off, as does an empty RulesFile.
	LastRunFile string
}

// Default is what a hook with no config file runs under: the operator's own machine. Inside a
// sandbox the file beside the hook names the sandbox's paths instead.
func Default(home string) Config {
	return Config{
		Model:            "haiku",
		Timeout:          12 * time.Second,
		MaxAnalysisChars: 1400,
		MaxDocChars:      4000,
		SkillDirs: []string{
			"~/.claude/skills",
			"~/claude/global/skills",
			"~/claude/orgs/*/skills",
		},
		RulesFile:   "~/claude/global/RULES.md",
		LastRunFile: "~/.claude/prompt-analyser.last",
	}.expand(home)
}

// LoadConfig merges a config file over the defaults. Anything missing, of the wrong type or out of
// range keeps its default, so a half written file leaves a working hook rather than a broken one.
func LoadConfig(body []byte, home string) Config {
	config := Default(home)
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		return config
	}

	read(raw, "model", &config.Model, func(v string) bool { return strings.TrimSpace(v) != "" })
	read(raw, "skillDirs", &config.SkillDirs, nil)
	read(raw, "docs", &config.Docs, nil)
	read(raw, "skip", &config.Skip, nil)
	read(raw, "rulesFile", &config.RulesFile, nil)
	read(raw, "lastRunFile", &config.LastRunFile, nil)
	read(raw, "maxAnalysisChars", &config.MaxAnalysisChars, func(v int) bool { return v > 0 })
	read(raw, "maxDocChars", &config.MaxDocChars, func(v int) bool { return v > 0 })

	milliseconds := 0.0
	read(raw, "timeoutMs", &milliseconds, func(v float64) bool { return v > 0 && !math.IsInf(v, 0) })
	if milliseconds > 0 {
		config.Timeout = time.Duration(milliseconds) * time.Millisecond
	}
	return config.expand(home)
}

func read[T any](raw map[string]json.RawMessage, key string, into *T, usable func(T) bool) {
	body, named := raw[key]
	if !named {
		return
	}
	var value T
	if json.Unmarshal(body, &value) != nil {
		return
	}
	if usable == nil || usable(value) {
		*into = value
	}
}

func (c Config) expand(home string) Config {
	c.Model = strings.TrimSpace(c.Model)
	for i, path := range c.SkillDirs {
		c.SkillDirs[i] = ExpandHome(path, home)
	}
	for i, path := range c.Docs {
		c.Docs[i] = ExpandHome(path, home)
	}
	c.RulesFile = ExpandHome(c.RulesFile, home)
	c.LastRunFile = ExpandHome(c.LastRunFile, home)
	return c
}

// ExpandHome resolves a leading ~. Empty stays empty, because empty means off rather than home.
func ExpandHome(path, home string) string {
	switch {
	case path == "" || !strings.HasPrefix(path, "~"):
		return path
	case path == "~":
		return home
	case strings.HasPrefix(path, "~/"):
		return home + "/" + path[2:]
	default:
		return path
	}
}

// Skipped says the hook stays out of the way. Every message is analysed unless the config names a
// pattern for it, so narrowing this is a config edit rather than a code edit.
func (c Config) Skipped(prompt string) bool {
	if strings.TrimSpace(prompt) == "" {
		return true
	}
	for _, pattern := range c.Skip {
		// An unparseable pattern is passed over, not allowed to break every message.
		if matcher, err := regexp.Compile(pattern); err == nil && matcher.MatchString(prompt) {
			return true
		}
	}
	return false
}

// FileSystem is everything the analyser asks of the machine, so the rules for what reaches the model
// can be tested without a directory tree.
type FileSystem interface {
	List(dir string) ([]string, error)
	Read(file string) ([]byte, error)
	Write(file string, body []byte) error
}

type Skill struct {
	Name        string
	Description string
}

// CollectSkills gathers <dir>/<slug>/SKILL.md one level deep. A directory may hold one *, standing
// for one level of subdirectories, so a path through orgs reaches every org without naming them.
// A skill with no description is left out: the description is all the model is given.
func CollectSkills(dirs []string, fs FileSystem) []Skill {
	found := map[string]Skill{}
	for _, dir := range expandStars(dirs, fs) {
		entries, err := fs.List(dir)
		if err != nil {
			continue
		}
		for _, slug := range entries {
			body, err := fs.Read(dir + "/" + slug + "/SKILL.md")
			if err != nil {
				continue
			}
			fields := Frontmatter(string(body))
			name := fields["name"]
			if name == "" {
				name = slug
			}
			if _, already := found[name]; already || fields["description"] == "" {
				continue
			}
			found[name] = Skill{Name: name, Description: fields["description"]}
		}
	}

	skills := make([]Skill, 0, len(found))
	for _, one := range found {
		skills = append(skills, one)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills
}

func expandStars(dirs []string, fs FileSystem) []string {
	expanded := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		star := strings.Index(dir, "*")
		if star == -1 {
			expanded = append(expanded, dir)
			continue
		}
		head := strings.TrimSuffix(dir[:star], "/")
		tail := strings.TrimPrefix(dir[star+1:], "/")
		children, err := fs.List(head)
		if err != nil {
			continue
		}
		for _, child := range children {
			at := head + "/" + child
			if tail != "" {
				at += "/" + tail
			}
			expanded = append(expanded, at)
		}
	}
	return expanded
}

var frontmatterField = regexp.MustCompile(`^([A-Za-z][\w-]*):\s?(.*)$`)

// Frontmatter reads the leading block of a markdown file as flat pairs. An indented line continues
// the key above it, which is how a long description is written.
func Frontmatter(text string) map[string]string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return map[string]string{}
	}
	fields, key := map[string]string{}, ""
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		if match := frontmatterField.FindStringSubmatch(line); match != nil {
			key = match[1]
			fields[key] = strings.TrimSpace(match[2])
			continue
		}
		if key != "" && strings.HasPrefix(line, " ") && strings.TrimSpace(line) != "" {
			fields[key] = strings.TrimSpace(fields[key] + " " + strings.TrimSpace(line))
		}
	}
	return fields
}

var (
	ruleHeadline = regexp.MustCompile(`(?ms)^(\d+)\.\s+\*\*(.+?)\*\*`)
	runsOfSpace  = regexp.MustCompile(`\s+`)
)

// RuleIndex is one line per numbered rule, from its bold headline. Derived every run, so a rule that
// changes wording cannot go stale here.
func RuleIndex(markdown string) []string {
	matches := ruleHeadline.FindAllStringSubmatch(markdown, -1)
	headlines := make([]string, 0, len(matches))
	for _, match := range matches {
		headlines = append(headlines,
			match[1]+". "+strings.TrimSpace(runsOfSpace.ReplaceAllString(match[2], " ")))
	}
	return headlines
}

// Clip cuts to the ceiling and says so. Characters rather than bytes, so a cut never lands inside
// one and hands the model a broken character where a word was.
func Clip(text string, max int) string {
	if max <= 0 || utf8.RuneCountInString(text) <= max {
		return text
	}
	return string([]rune(text)[:max]) + "\n[cut at " + strconv.Itoa(max) + " characters]"
}

// Facts are what is true around a message, as against what it says.
type Facts struct {
	Cwd, Repo, Branch, Ticket string
	// State is the fenced state block of a ticket's STATE.md.
	State string
}

type Document struct{ Label, Text string }

type Ask struct {
	Prompt string
	Facts  Facts
	Skills []Skill
	Rules  []string
	Docs   []Document
}

// SystemPrompt is what the model is told it is for. The last constraint is what keeps the hook
// quiet: without it every acknowledgement comes back with five lines attached.
func SystemPrompt() string {
	return strings.Join([]string{
		"You restate one message from an engineer into a short analysis for a coding agent.",
		"You never answer the message, never write code, and never do the work in it.",
		"",
		"Return these lines and nothing else, in this order, one line each:",
		"goal: one sentence, what done looks like. Always include this line.",
		"target: the org, repo, ticket or files the work touches. Include it whenever the",
		"  facts give you one.",
		"unclear: each thing the message does not say that changes what gets built. Omit the",
		"  line when the message leaves nothing open.",
		"skills: the skill names from the catalogue that fit, comma separated. Omit the line",
		"  when none fit.",
		"rules: the rule numbers from the index that apply, comma separated, at most four.",
		"  Omit the line when none apply.",
		"first move: the single next action. Always include this line.",
		"",
		"Constraints:",
		"Answer straight away. Do not think it through first.",
		"Use only the facts given. Never invent a repository, a ticket or a file name.",
		"Keep every line under 25 words. Write plain English. No dashes as punctuation.",
		"Prefer the words the engineer used over your own.",
		"If the message is an acknowledgement, an answer to a question, a correction, or is",
		"already precise enough to act on, reply with the single word " + Pass + " and nothing else.",
	}, "\n")
}

// UserMessage is the message and everything around it. An empty section is left out entirely rather
// than sent as a heading with nothing under it.
func (a Ask) UserMessage() string {
	parts := []string{"<message>", a.Prompt, "</message>"}

	var facts []string
	for _, one := range []struct{ label, value string }{
		{"working directory", a.Facts.Cwd},
		{"repository", a.Facts.Repo},
		{"branch", a.Facts.Branch},
		{"ticket", a.Facts.Ticket},
	} {
		if one.value != "" {
			facts = append(facts, one.label+": "+one.value)
		}
	}
	if a.Facts.State != "" {
		facts = append(facts, "ticket state:\n"+a.Facts.State)
	}
	parts = section(parts, "facts", strings.Join(facts, "\n"), len(facts) > 0)

	lines := make([]string, 0, len(a.Skills))
	for _, skill := range a.Skills {
		lines = append(lines, skill.Name+": "+skill.Description)
	}
	parts = section(parts, "skills", strings.Join(lines, "\n"), len(a.Skills) > 0)
	parts = section(parts, "rules", strings.Join(a.Rules, "\n"), len(a.Rules) > 0)
	for _, doc := range a.Docs {
		parts = section(parts, doc.Label, doc.Text, true)
	}
	return strings.Join(parts, "\n")
}

func section(parts []string, label, body string, include bool) []string {
	if !include {
		return parts
	}
	return append(parts, "", "<"+label+">", body, "</"+label+">")
}

// Outcome is how a run ended, for the last run line.
type Outcome string

const (
	Analysed Outcome = "analysed"
	Passed   Outcome = "pass"
	Skipped  Outcome = "skipped"
	NoAnswer Outcome = "no answer"
)

var (
	fencedOpen  = regexp.MustCompile("^\\s*```[a-zA-Z]*\\s*")
	fencedClose = regexp.MustCompile("```\\s*$")
	fieldLine   = regexp.MustCompile(`(?i)^(goal|target|unclear|skills|rules|first move):`)
)

// FormatAnalysis keeps only the known field lines, and that one rule does all the work: it drops the
// pass word, chatter around the fields, and an answer the model gave instead of an analysis. Empty
// means there is nothing worth adding.
func FormatAnalysis(raw string, max int) string {
	cleaned := strings.TrimSpace(fencedClose.ReplaceAllString(fencedOpen.ReplaceAllString(raw, ""), ""))
	var kept []string
	for _, line := range strings.Split(cleaned, "\n") {
		if line = strings.TrimSpace(line); line != "" && fieldLine.MatchString(line) {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return Clip(strings.Join(kept, "\n"), max)
}

// LastRunLine is the one line the hook leaves behind, and for a long time it said "no answer" and
// stopped there. That is a fact nobody can act on: it reads the same whether the model was slow,
// said nothing, or was never logged in, and it was the only record anywhere of a hook that had never
// once worked in a sandbox. The reason now goes on the end, so the file says what to fix.
//
// The reason is a sentence, never a value: no credential is ever written here.
func LastRunLine(when time.Time, outcome Outcome, elapsed time.Duration, prompt, reason string) string {
	opening := runsOfSpace.ReplaceAllString(strings.TrimSpace(prompt), " ")
	if len(opening) > 60 {
		opening = opening[:60]
	}
	fields := []string{
		when.UTC().Format("2006-01-02T15:04:05.000Z"),
		string(outcome),
		strconv.FormatInt(elapsed.Milliseconds(), 10) + "ms",
		opening,
	}
	// One run is one line, so a reason that arrived with a newline in it is flattened rather than
	// allowed to look like a second run.
	if reason = runsOfSpace.ReplaceAllString(strings.TrimSpace(reason), " "); reason != "" {
		fields = append(fields, reason)
	}
	return strings.Join(fields, "  ") + "\n"
}

// Context labels both halves so the analysis is never mistaken for an instruction.
func Context(prompt, analysis string) string {
	return strings.Join([]string{
		"<prompt-analysis>",
		"A hook generated the analysis below from the message. It is a reading of the message,",
		"not a message. The engineer's own words are the instruction; where the two differ,",
		"follow the words and ask about the difference.",
		"",
		"<as-typed>", prompt, "</as-typed>",
		"",
		"<analysis>", analysis, "</analysis>",
		"</prompt-analysis>",
	}, "\n")
}

// hookOutput is JSON rather than plain text because plain text on this event reaches the session
// only, leaving the person who typed the message unable to see what was made of it.
type hookOutput struct {
	SystemMessage string       `json:"systemMessage"`
	Specific      *specificOut `json:"hookSpecificOutput,omitempty"`
}

type specificOut struct {
	EventName         string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

func Printed(prompt, analysis string) string {
	return encode(hookOutput{
		SystemMessage: "prompt analysis\n" + analysis,
		Specific: &specificOut{
			EventName:         "UserPromptSubmit",
			AdditionalContext: Context(prompt, analysis),
		},
	})
}

// Notice is a line for the terminal and nothing for the session, so a quiet decision never looks the
// same as a hook that did not run.
func Notice(text string) string {
	return encode(hookOutput{SystemMessage: "prompt analysis: " + text})
}

func encode(out hookOutput) string {
	body, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(body)
}

// payload is what the runtime writes to the hook on standard input.
type payload struct {
	Prompt string `json:"prompt"`
	Cwd    string `json:"cwd"`
}

// Options is everything Run needs from outside itself, so the whole hook can be driven by a test
// with no model, checkout or home directory behind it.
type Options struct {
	Config Config
	Stdin  io.Reader
	Stdout io.Writer
	FS     FileSystem
	Now    func() time.Time
	Facts  func(cwd string) Facts
	// Ask returns what the model said, or empty for every kind of failure. There is nothing the hook
	// would do differently, so it never returns an error.
	Ask func(system, user string) (answer, trouble string)
}

// Run is the hook. It returns nothing to check because every failure inside it is already handled as
// silence, and every path through it records how it went first.
func Run(o Options) {
	started := o.Now()

	var read payload
	body, err := io.ReadAll(o.Stdin)
	if err != nil || json.Unmarshal(body, &read) != nil {
		return
	}
	if o.Config.Skipped(read.Prompt) {
		o.finish(started, Skipped, read.Prompt, "", "")
		return
	}

	answer, trouble := o.Ask(SystemPrompt(), o.message(read))
	if trouble == "" && strings.TrimSpace(answer) == "" {
		trouble = "the model answered with nothing"
	}
	if trouble != "" {
		o.finish(started, NoAnswer, read.Prompt, trouble, Notice(trouble))
		return
	}
	analysis := FormatAnalysis(answer, o.Config.MaxAnalysisChars)
	if analysis == "" {
		o.finish(started, Passed, read.Prompt, "nothing to add", Notice("nothing to add"))
		return
	}
	o.finish(started, Analysed, read.Prompt, "", Printed(read.Prompt, analysis))
}

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
		name := filepath.Base(path)
		ask.Docs = append(ask.Docs, Document{
			Label: strings.TrimSuffix(name, filepath.Ext(name)),
			Text:  Clip(string(text), o.Config.MaxDocChars),
		})
	}
	return ask.UserMessage()
}

// finish records how the run went and prints whatever the runtime should read.
//
// The reason travels into the record as well as onto the terminal, because the two are read by
// different people at different times: the terminal line is gone the moment the session scrolls, and
// the file is where somebody looks a day later asking why nothing is ever analysed.
func (o Options) finish(started time.Time, outcome Outcome, prompt, reason, out string) {
	if o.Config.LastRunFile != "" {
		// A hook that cannot write its own log still has a job to do.
		_ = o.FS.Write(o.Config.LastRunFile,
			[]byte(LastRunLine(o.Now(), outcome, o.Now().Sub(started), prompt, reason)))
	}
	if out != "" {
		_, _ = io.WriteString(o.Stdout, out)
	}
}

// Trouble turns a failed model call into a sentence naming what is wrong and what to do about it.
//
// The hook fails open, which is right: a message must always get through. But failing open quietly
// means a hook that has never once worked looks exactly like a hook with nothing to add, and this one
// ran that way in every sandbox from the day it shipped. So a failure says so, and says the next move.
func Trouble(timedOut, err error, said string, config Config) string {
	said = strings.TrimSpace(said)
	switch {
	case timedOut != nil:
		return "the model took longer than " + config.Timeout.String() + ", so there is no analysis. " +
			"Raise timeoutMs in hook.config.json, or use a smaller model than " + config.Model

	case strings.Contains(said, "Not logged in"), strings.Contains(said, "Please run /login"),
		strings.Contains(said, "Invalid bearer token"), strings.Contains(said, "authentication_error"):
		return NotLoggedIn

	case strings.Contains(err.Error(), "executable file not found"), errors.Is(err, exec.ErrNotFound):
		return "there is no claude on the path, so the analysis cannot run. The hook names claude in " +
			"its binaries, so a sandbox image without one should have been refused before this"

	case said != "":
		// Whatever it said, rather than a guess. One line, because this goes to a terminal.
		return "the model call failed: " + Redacted(firstLine(said))

	default:
		return "the model call failed with " + err.Error() + " and said nothing"
	}
}

// NotLoggedIn is the failure a sandbox with no credential hits, written once so it says the whole of
// it and names the next move.
//
// Claude Code removes CLAUDE_CODE_OAUTH_TOKEN from the environment of every process it starts, while
// passing nine other CLAUDE_ variables through, so the token reaches the session and not what the
// session starts. A hook is one of those processes. The system now writes the same value under
// QUAY_MODEL_TOKEN too, which survives, and reaching this sentence means neither name arrived.
const NotLoggedIn = "the model call is not logged in, so this hook cannot analyse anything. " +
	"The system carries the workspace's subscription token into a sandbox under two names, because " +
	"Claude Code strips CLAUDE_CODE_OAUTH_TOKEN from every process a session starts and a hook is " +
	"one of those. Neither name arrived here. Set the token with: krewe secret set <workspace> " +
	"CLAUDE_CODE_OAUTH_TOKEN <token from claude setup-token>, or turn the hook off with: " +
	"krewe hook detach system prompt-analyser"

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	if len(line) > 200 {
		line = line[:200]
	}
	return strings.TrimSpace(line)
}

// tokenShape is a subscription token as the tool that mints it prints one. Long enough that it
// cannot match an ordinary word.
var tokenShape = regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{8,}`)

// Redacted takes any credential out of something the child said before it is repeated.
//
// The reason a run failed goes to the terminal and, since the record started saying why, into the
// last run file as well. That is the child's own words, and the child is a model call: repeating them
// is right, and putting a credential on disk while doing it is not. Nothing is known to print one.
// This is what makes that a fact rather than a hope.
func Redacted(text string) string {
	return tokenShape.ReplaceAllString(text, "[redacted]")
}
