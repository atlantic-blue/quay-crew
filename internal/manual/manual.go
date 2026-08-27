// Package manual is quay describing itself, in a form meant to be read by whatever is running in a
// session rather than by somebody scanning a terminal.
//
// It exists because a session sitting in the panel beside the console knows nothing about the crew it
// is next to, so it is told with this.
//
// Most of it is assembled rather than written: the commands are the same usage text `quay` shows with
// no arguments, and what the crew can do comes from the behaviour specification embedded in the
// binary. Neither can drift from the tool, because a scenario that is wrong fails the build and a
// command that is renamed changes the usage in the same commit. Only the model's own words are prose,
// and those are the part that changes least.
package manual

import (
	"fmt"
	"strings"

	"github.com/atlantic-blue/quay-crew/features"
)

// Commands is the command list, which is also what `quay` prints with no arguments. It lives here so
// the tool and the document a session is told with cannot describe two different tools.
const Commands = `usage: quay [command]

with no command, quay opens the console: a full screen view of every resource the crew has.
press : to switch resource, / to filter, enter to drill in, s to shell into a session, q to quit.

you work in one place at a time, and say where with an address: workspace/project/session.

  quay use me/house-bills
  quay ask "when is the electricity bill due"
  quay dispatch "read the repository and write the migration"

commands:
  help                                    print this, which -h and --help do too
  version                                 print which build this is
  features                                what this crew can do, and what proves it
  manual                                  what quay is and how to drive it, to pipe into a context
  use [<address>]                         show where you are, or move there
  workspace create <name>                 create a workspace and move into it
  workspace list                          list workspaces
  workspace delete <workspace>            remove it, its projects, sessions and secrets. Type its
                                          name to confirm, or pipe the name in to script it
  project create [<workspace>/]<name>     create a project and move into it
  project list [<workspace>]              list projects
  project delete [<workspace>/]<project>  remove it and the sessions inside it, confirmed the same way
  flow import <file>                      store an automation graph the crew can run
  flow start [<address>] <graph>          begin a run of it in a project
  flow schedule [<address>] <graph>       let it run on its own, as often as it says
  flow unschedule [<address>] <graph>     stop it running on its own
  flow list [<address>]                   what has run, newest first
  flow show <run>                         where one run got to, what it cost, why it stopped
  flow stop <run> [<reason>]              halt a run in flight, keeping the reason
  flow answer <run> <answer>              tell a run waiting on you what you decided
  dispatch [<address>] <text>             start or continue a session and let go of the task. It
                                          runs in the crew, so closing the terminal does not take
                                          the work with it. quay tasks reads it back
  ask [<address>] <text>                  the same, and wait here for the answer. For a short
                                          question, where the reply is the point
  sessions [<address>]                    list sessions, which session and sessions also do
  tasks <session>                          what a session was asked to do, and what came back
  answer <session> [--all]                 what a session came back with, and nothing else, so a
                                          caller can pipe it. The most recent answer, or with --all
                                          every one of them, oldest first
  drain [anyway]                          put every live session down, so an upgrade does not take
                                          their containers away underneath them. Refuses while a
                                          task is working, and anyway drains over it
  label <session> [<text>]                 what you call a conversation, so a listing reads as
                                          conversations rather than identifiers. No text reads it,
                                          and "" clears it
  mode <session> [<mode>]                  what a session's tasks may do without asking: plan, edits
                                          or dangerous. A dispatched task has nobody to approve
                                          anything, so this is how it is given room to work
  context [<address>]                     where the files the model reads live
  context set [<address>] < file          write what a level says, from standard input. Say crew
                                          where the address goes and it applies to everything the
                                          crew does, which skill attach takes too
  context edit [<address>]                open a project's context in $EDITOR
  context clear [<address>]               empty what a level says
  attach <session>                         open a session's conversation, with its history
  web [<address>]                          read the crew in a browser on this machine. Read only,
                                           and it serves 127.0.0.1:8080 unless told another port
  room                                     how much memory this sandbox actually has, and what to
                                           do about a gate that does not fit in it. A sandbox with
                                           no limit advertises the whole machine, and the kernel
                                           kills against what is free
  render <url> [<file>] [<size>]           draw a page into a picture and say what it drew, so a
    [light|dark] [<wait>]                  session can look at what it built. The whole page, at
                                           1280x900 in light unless told otherwise
  secret set [<workspace>] <key>          set a workspace secret from standard input, so the value
                                          never reaches your shell history: pipe it in, or redirect
                                          a file. A value given as an argument still works. Say crew
                                          where the workspace goes and every workspace reads it,
                                          including the ones made later
  secret mount [<workspace>] <name>       store a credential that is a file, which reaches a session
    [<path>]                              at /run/secrets/<name> and not through its environment.
                                          Takes crew the same way
  secret list [<workspace>|crew]          which secrets are set, never what they say. It says which
                                          level holds each one
  skill import <directory>                take a skill into the crew from its directory. A fresh
                                          crew already holds the ones this build ships with
  skill list [<workspace>]                what the crew can do, or what one workspace holds
  skill attach [<workspace>] <name>       give a workspace a skill, so its sessions hold it. Say
                                          crew where the workspace goes and every workspace holds
                                          it, including the ones made later
  skill detach [<workspace>] <name>       take a skill away from a workspace, or from the crew
  hook import <directory>                 take a hook into the crew from its directory. A hook is a
                                          constraint a session runs under, checked when it acts
  hook list [<workspace>]                 what the crew enforces, or what one workspace runs under
  hook attach [<workspace>] <name>        put a workspace's sessions under a hook. Say crew where
                                          the workspace goes and every workspace is under it. A
                                          session already running is not: a hook reaches a sandbox
                                          when the sandbox is built
  hook detach [<workspace>] <name>        take a hook away from a workspace, or from the crew
  role import <directory>                 take a role into the crew from its directory. A role is a
                                          named way of working: a brief, the model it runs on, and
                                          the material it may receive
  role list [<workspace>]                 what roles the crew holds, or what one workspace holds
  role attach [<workspace>] <name>        give a workspace a role. Say crew where the workspace
                                          goes and every workspace holds it
  role detach [<workspace>] <name>        take a role away from a workspace, or from the crew

a level of an address is a name or an id, so me/house-bills and me/3db6b81e both work, and a session
may be the shortened id a listing prints. An address typed on the command line applies to that
command only and does not move you.
`

// Text is the whole document.
func Text() string {
	commands := Commands
	var out strings.Builder

	out.WriteString(preamble)
	fmt.Fprintf(&out, "\n## The commands\n\n%s\n", strings.TrimSpace(commands))
	out.WriteString("\n\n## What the crew can do, and what proves it\n\n")
	out.WriteString("Each line under a heading is a behaviour with a test behind it. If something is not\n")
	out.WriteString("here, it is not built yet, whatever anybody says.\n\n")

	for _, feature := range features.All() {
		fmt.Fprintf(&out, "%s\n", feature.Title)
		if feature.Summary != "" {
			fmt.Fprintf(&out, "  %s\n", feature.Summary)
		}
		for _, scenario := range feature.Scenarios {
			fmt.Fprintf(&out, "    %s\n", scenario)
		}
		out.WriteString("\n")
	}
	return out.String()
}

// preamble is the part that cannot be assembled from anywhere: what the words mean, and where
// things are kept. Deliberately short, because everything below it is generated and stays true on its
// own, while every sentence here is one somebody has to remember to change.
const preamble = `# Quay Crew

You are running inside a Quay Crew session. Quay Crew is a self hosted hub for agent sessions: the
operator commands a crew of them from any channel, and each one runs in its own sandbox container.
The ` + "`quay`" + ` command drives it. If it is on your path, you can use it.

## The words, and what they mean

  workspace   who you are, for example "me" or an organisation. Secrets attach here.
  project     a body of work inside a workspace, for example "house bills" or a ticket.
  session      one conversation. A task runs in a project.
  task        one instruction and the work it caused. You ask for something, the crew works
              until it has an answer, and the whole of that is one task. Minutes is normal.
  session     a session that is running, inside its own sandbox container.
  sandbox     the isolated container a session runs in. A session runs IN a sandbox.

They nest: workspace, then project, then session. An address is written the way a path is,
` + "`me/house-bills`" + `, and each level is a name or an identifier. ` + "`quay use me/house-bills`" + ` moves you
there; an address typed on a command applies to that command only.

## Context, which is how you are told things

Context is files, not prompt text. Each level owns a directory, and a session's sandbox mounts them:

  workspace   /home/agent/.claude          its CLAUDE.md, and the conversation store
  project     /home/agent/workspace        its CLAUDE.md, and the project's files

So the way to teach a project something is to write it into that project's context, with
` + "`quay context set <address> < file`" + `, or to drop files into the directory ` + "`quay context`" + ` names. The
model reads CLAUDE.md and the working directory natively. There is no second mechanism.

## What this is for

The operator may ask you to do things to the crew itself: make a workspace, add a project, set a
subscription token, load a folder as a project's context, start a session. Everything below is what
the tool can actually do. Prefer running ` + "`quay`" + ` to guessing, and if a command refuses, read what it
says: refusals here name what would have worked.
`
