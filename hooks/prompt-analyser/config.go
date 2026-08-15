// Package analyser reads the message a session was sent and asks a small model to restate it.
//
// The hook feeds the model the raw message plus the facts around it (the working directory, the
// branch, the ticket, the skills that exist, the rule headlines) and gets back a short restatement.
// It never replaces what was typed: the runtime does not allow that, and it should not, because a
// reading of a message is a guess and the words are not.
//
// Everything in this file and its neighbours is data in, data out, so the shape of the analysis and
// the rules for what gets sent are covered by tests rather than by reading the hook and hoping. The
// parts that touch the machine are in run.go.
package main

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"time"
)

// Config is what the hook reads before it does anything. Extending the hook means editing
// hook.config.json beside it: another directory of skills, another document to put in front of the
// model, a different model or budget.
type Config struct {
	// Model is the alias or full id the analysis runs on.
	Model string
	// Timeout is how long the model gets before the hook gives up and stays silent.
	Timeout time.Duration
	// MaxAnalysisChars is the ceiling on the analysis the model returns.
	MaxAnalysisChars int
	// MaxDocChars is the ceiling on each document put in front of the model.
	MaxDocChars int
	// SkillDirs are searched one level deep for SKILL.md files.
	SkillDirs []string
	// Docs are files included whole, after the character ceiling.
	Docs []string
	// RulesFile is the markdown the rule headlines are read out of. Empty means none.
	RulesFile string
	// Skip are regular expressions. A message matching any of them is not analysed.
	Skip []string
	// LastRunFile is overwritten with one line about the last run, so "did it fire" is a question
	// with an answer. Empty turns it off.
	LastRunFile string
}

// Default is the configuration a hook with no config file runs under. The paths are the operator's
// machine, because that is the case with no file to say otherwise; inside a sandbox the file beside
// the hook names the sandbox's own paths.
func Default(home string) Config {
	config := Config{
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
	}
	return config.expand(home)
}

// LoadConfig merges a config file over the defaults.
//
// Anything missing, of the wrong type, or out of range keeps its default, so a half written config
// file still leaves a working hook rather than a broken one. It never fails: a config file nobody
// can parse is a hook that runs on the defaults, not a message that does not get through.
func LoadConfig(body []byte, home string) Config {
	config := Default(home)

	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		return config
	}

	read(raw, "model", &config.Model, notBlank)
	read(raw, "skillDirs", &config.SkillDirs, nil)
	read(raw, "docs", &config.Docs, nil)
	read(raw, "skip", &config.Skip, nil)
	// Empty is meaningful on both of these: it turns the thing off, so there is nothing to check.
	read(raw, "rulesFile", &config.RulesFile, nil)
	read(raw, "lastRunFile", &config.LastRunFile, nil)
	read(raw, "maxAnalysisChars", &config.MaxAnalysisChars, positive)
	read(raw, "maxDocChars", &config.MaxDocChars, positive)

	// Milliseconds in the file because that is what the config has always said. A duration is what
	// the rest of the program wants, so the conversion happens once, here.
	milliseconds := 0.0
	read(raw, "timeoutMs", &milliseconds, func(value float64) bool {
		return value > 0 && !math.IsInf(value, 0)
	})
	if milliseconds > 0 {
		config.Timeout = time.Duration(milliseconds) * time.Millisecond
	}

	return config.expand(home)
}

// read overwrites one field when the file names it and what it says makes sense. A field of the
// wrong type is passed over rather than allowed to throw away the whole file with it.
func read[T any](raw map[string]json.RawMessage, key string, into *T, usable func(T) bool) {
	body, named := raw[key]
	if !named {
		return
	}
	var value T
	if json.Unmarshal(body, &value) != nil {
		return
	}
	if usable != nil && !usable(value) {
		return
	}
	*into = value
}

func notBlank(value string) bool { return strings.TrimSpace(value) != "" }

func positive(value int) bool { return value > 0 }

// expand resolves a leading ~ in every path against the given home directory.
func (c Config) expand(home string) Config {
	c.Model = strings.TrimSpace(c.Model)
	c.SkillDirs = expandAll(c.SkillDirs, home)
	c.Docs = expandAll(c.Docs, home)
	c.RulesFile = ExpandHome(c.RulesFile, home)
	c.LastRunFile = ExpandHome(c.LastRunFile, home)
	return c
}

func expandAll(paths []string, home string) []string {
	expanded := make([]string, 0, len(paths))
	for _, path := range paths {
		expanded = append(expanded, ExpandHome(path, home))
	}
	return expanded
}

// ExpandHome resolves a leading ~ against the given home directory. An empty path stays empty,
// because empty means the thing is turned off rather than pointing at the home directory itself.
func ExpandHome(path, home string) string {
	switch {
	case path == "":
		return ""
	case path == "~":
		return home
	case strings.HasPrefix(path, "~/"):
		return home + "/" + path[2:]
	default:
		return path
	}
}

// Skipped says the hook stays out of the way.
//
// An empty message is never analysed. Everything else is analysed unless the config names a pattern
// for it, so the default is every message and narrowing it is a config edit rather than a code edit.
func (c Config) Skipped(prompt string) bool {
	if strings.TrimSpace(prompt) == "" {
		return true
	}
	for _, pattern := range c.Skip {
		// An unparseable pattern is passed over rather than allowed to break the hook.
		if matcher, err := regexp.Compile(pattern); err == nil && matcher.MatchString(prompt) {
			return true
		}
	}
	return false
}
