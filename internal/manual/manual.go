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

you work in one place at a time, and say where with an address: workspace/project/thread.

  quay use me/house-bills
  quay dispatch "when is the electricity bill due"

commands:
  help                                    print this, which -h and --help do too
  version                                 print which build this is
  features                                what this crew can do, and what proves it
  manual                                  what quay is and how to drive it, to pipe into a context
  use [<address>]                         show where you are, or move there
  workspace create <name>                 create a workspace and move into it
  workspace list                          list workspaces
  workspace delete <workspace>            remove it, its projects, threads and secrets. Type its
                                          name to confirm, or pipe the name in to script it
  project create [<workspace>/]<name>     create a project and move into it
  project list [<workspace>]              list projects
  project delete [<workspace>/]<project>  remove it and the threads inside it, confirmed the same way
  flow import <file>                      store an automation graph the crew can run
  flow start [<address>] <graph>          begin a run of it in a project
  flow schedule [<address>] <graph>       let it run on its own, as often as it says
  flow unschedule [<address>] <graph>     stop it running on its own
  flow list [<address>]                   what has run, newest first
  flow show <run>                         where one run got to, what it cost, why it stopped
  flow stop <run> [<reason>]              halt a run in flight, keeping the reason
  flow answer <run> <answer>              tell a run waiting on you what you decided
  dispatch [<address>] <text>             start or continue a thread
  sessions [<address>]                    list threads, which thread and threads also do
  turns <thread>                          what a thread was asked, and what came back
  mode <thread> [<mode>]                  what a thread's turns may do without asking: plan, edits
                                          or dangerous. A dispatched turn has nobody to approve
                                          anything, so this is how it is given room to work
  context [<address>]                     where the files the model reads live
  context set [<address>] < file          write what a level says, from standard input. Say crew
                                          where the address goes and it applies to everything the
                                          crew does, which skill attach takes too
  context edit [<address>]                open a project's context in $EDITOR
  context clear [<address>]               empty what a level says
  attach <thread>                         open a thread's conversation, with its history
  secret set [<workspace>] <key>          set a workspace secret from standard input, so the value
                                          never reaches your shell history: pipe it in, or redirect
                                          a file. A value given as an argument still works
  secret list [<workspace>]               which secrets are set, never what they say
  skill import <directory>                take a skill into the crew from its directory. A fresh
                                          crew already holds the ones this build ships with
  skill list [<workspace>]                what the crew can do, or what one workspace holds
  skill attach [<workspace>] <name>       give a workspace a skill, so its sessions hold it. Say
                                          crew where the workspace goes and every workspace holds
                                          it, including the ones made later
  skill detach [<workspace>] <name>       take a skill away from a workspace, or from the crew

a level of an address is a name or an id, so me/house-bills and me/3db6b81e both work, and a thread
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
  thread      one conversation. A turn runs in a project.
  session     a thread that is running, inside its own sandbox container.
  sandbox     the isolated container a session runs in. A session runs IN a sandbox.

They nest: workspace, then project, then thread. An address is written the way a path is,
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
subscription token, load a folder as a project's context, start a thread. Everything below is what
the tool can actually do. Prefer running ` + "`quay`" + ` to guessing, and if a command refuses, read what it
says: refusals here name what would have worked.
`
