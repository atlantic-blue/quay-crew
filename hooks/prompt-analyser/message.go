package main

import "strings"

// Facts are what is true around a message, as against what it says. The model is given these so it
// can name the repository, the branch or the ticket without guessing at one.
type Facts struct {
	Cwd    string
	Repo   string
	Branch string
	Ticket string
	// State is the fenced state block of a ticket's STATE.md, which says where the work stands.
	State string
}

// Document is one file put in front of the model whole, under a label of its own.
type Document struct {
	Label string
	Text  string
}

// Ask is everything the model is given about one message.
type Ask struct {
	Prompt string
	Facts  Facts
	Skills []Skill
	Rules  []string
	Docs   []Document
}

// Pass is the single line the model returns when a message needs no analysis.
const Pass = "pass"

// SystemPrompt is what the model is told it is for.
//
// The last constraint is the one that keeps the hook quiet. Without it every acknowledgement and
// every answer to a question comes back with five lines of restatement attached, and a hook that
// always speaks is a hook nobody reads.
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

// UserMessage is the message and everything around it, in the order the model reads it. Each section
// is left out entirely when there is nothing in it, so an empty checkout does not send the model a
// page of empty headings to reason about.
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
	parts = appendSection(parts, "facts", strings.Join(facts, "\n"), len(facts) > 0)

	lines := make([]string, 0, len(a.Skills))
	for _, skill := range a.Skills {
		lines = append(lines, skill.Name+": "+skill.Description)
	}
	parts = appendSection(parts, "skills", strings.Join(lines, "\n"), len(a.Skills) > 0)
	parts = appendSection(parts, "rules", strings.Join(a.Rules, "\n"), len(a.Rules) > 0)

	for _, doc := range a.Docs {
		parts = appendSection(parts, doc.Label, doc.Text, true)
	}

	return strings.Join(parts, "\n")
}

func appendSection(parts []string, label, body string, include bool) []string {
	if !include {
		return parts
	}
	return append(parts, "", "<"+label+">", body, "</"+label+">")
}
