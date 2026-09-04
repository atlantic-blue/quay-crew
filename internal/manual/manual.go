// Package manual is krewe describing itself, in a form meant to be read by whatever is running in a
// session rather than by somebody scanning a terminal.
//
// It exists because a session sitting in a conversation knows nothing about the system it is part of,
// so it is told with this.
//
// Most of it is assembled rather than written: the commands are the same usage text `krewe` shows with
// no arguments, and what the system can do comes from the behaviour specification embedded in the
// binary. Neither can drift from the tool, because a scenario that is wrong fails the build and a
// command that is renamed changes the usage in the same commit. Only the model's own words are prose,
// and those are the part that changes least.
package manual

import (
	"fmt"
	"strings"

	"github.com/atlantic-blue/quay-krewe/features"
)

// Commands is the command list, which is also what `krewe` prints with no arguments. It lives here so
// the tool and the document a session is told with cannot describe two different tools.
const Commands = `usage: krewe [command]

with no command, krewe opens the console: a full screen view of every resource the system has.
press : to switch resource, / to filter, enter to drill in, s to shell into a session, q to quit.

you work in one place at a time, and say where with an address: workspace/project/session.

  krewe use me/house-bills
  krewe exec "when is the electricity bill due"
  krewe exec --dispatch "read the repository and write the migration"

commands:
  help                                    print this, which -h and --help do too
  version                                 which build the tool, the system and the sandbox image are,
                                          and where two of them differ
  features                                what this system can do, and what proves it
  manual                                  what krewe is and how to drive it, to pipe into a context
  use [<address>]                         show where you are, or move there
  workspace create <name>                 create a workspace and move into it
  workspace list                          list workspaces
  workspace delete <workspace>            remove it, its projects, sessions and secrets. Type its
                                          name to confirm, or pipe the name in to script it
  project create [<workspace>/]<name>     create a project and move into it
  project list [<workspace>]              list projects
  project repository [<address>]          say where this project's work lands, and what kind of
    [<owner>/<name>] [public|private]     repository that is. On its own it reads it back. One
                                          address is a write, and it is refused where that address
                                          also names a project. A session started here works in it and
                                          ends in a pull request against it. Public unless you say
                                          otherwise, because a pipeline's minutes are free on a
                                          public repository, and a kind you leave out is the kind
                                          the project already holds
  project repository show [<address>]     read where a project's work lands, recording nothing
  project delete [<workspace>/]<project>  remove it and the sessions inside it, confirmed the same way
  target [<address>]                      where a project ships: the account, the region inside it,
    [--account <id>]                      and the role a pipeline assumes to get there. With no
    [--region <name>]                     values it reads what the project declared, and with them
    [--identity <arn>] [--clear]          it declares it. The identity has to belong to the account,
                                          because pasting the role from the other account is
                                          invisible until a pipeline runs. Nothing here deploys
                                          anything: infrastructure ships through the repository's
                                          own pipeline, and this says which account that pipeline is
                                          aimed at. --clear takes it back off
  exec [<address>] <text>                 start or continue a session, and wait here for the
                                          answer. For a short question, where the reply is the
                                          point
  exec --dispatch [<address>] <text>      the same, and let go of the exec. It runs in the system, so
                                          closing the terminal does not take the work with it
  exec list <session>                     what a session was asked to do, and what came back
  sessions [<address>|system]             list sessions, which session and sessions also do. It
                                          reads where you are standing and says so; system reads
                                          every workspace. The
                                          status column says what is inside each sandbox: awake is a
                                          conversation running with nobody watching it, attached is
                                          somebody in it, idle is an empty container, and unknown is
                                          the system asking the sandbox and not being told. The
                                          spent on column says what filled the context: reads is
                                          files, tools is what every other tool returned, turns is
                                          the session's own words, and told is what it was given.
                                          Last moved first, so the session you were last working in
                                          is at the top and the age column reads down the list
  read <session> [<path>]                 what a session made, out of the directory the system keeps
                                          for it. With no path it lists what is there and names the
                                          directory on the machine; with one it prints that file, so
                                          it pipes. It never enters the container, so it answers for
                                          a session whose sandbox has gone
  where [<address>]                       the directory an address is kept in on this machine, so you
                                          can put a file in it by hand. A workspace address answers
                                          with its shared folder, which every session in it reads, and
                                          a session address answers with that session's own working
                                          directory. The path is on the first line and nothing shares
                                          it, so cd "$(krewe where me)" works. Under it is where a
                                          session sees the same directory, which is what to call the
                                          file once it is in there. It starts nothing and reads no
                                          container, so it answers when every sandbox is down
  answer <session> [--all]                 what a session came back with, and nothing else, so a
                                          caller can pipe it. The most recent answer, or with --all
                                          every one of them, oldest first
  stop <session> [<reason>]               halt the exec one session is running, keeping the reason.
                                          The session survives: its conversation, its container and
                                          its history all stay, so the next exec continues it.
                                          A stop while nothing is running says so and changes
                                          nothing
  drain [anyway]                          put every live session down, so an upgrade does not take
                                          their containers away underneath them. Refuses while a
                                          exec is working, and anyway drains over it
  label <session> [<text>]                 what you call a conversation, so a listing reads as
                                          conversations rather than identifiers. No text reads it,
                                          and "" clears it
  mode <session> [<mode>]                  what a session's execs may do without asking: plan, edits
                                          or dangerous. An exec nobody waits for has nobody to approve
                                          anything, so this is how it is given room to work
  context [<address>]                     where the files the model reads live, and how big each
                                          level is. A level past 20,000 characters also says who
                                          reads it and what to move down a level
  context show [<address>]                what a level says, printed as it is stored. This and set
                                          are a pair: krewe context show system > file, edit the file,
                                          then krewe context set system < file, which is how a level is
                                          added to rather than overwritten
  context set [<address>] < file          write what a level says, from standard input. Say system
                                          where the address goes and it applies to everything the
                                          system does, which skill attach takes too
  context edit [<address>]                open a project's context in $EDITOR
  context clear [<address>]               empty what a level says
  attach <session>                         open a session's conversation, with its history
  secret set [<workspace>] <key>          set a workspace secret from standard input, so the value
                                          never reaches your shell history: pipe it in, or redirect
                                          a file. A value given as an argument still works. Say system
                                          where the workspace goes and every workspace reads it,
                                          including the ones made later
  secret mount [<workspace>] <name>       store a credential that is a file, which reaches a session
    [<path>]                              at /run/secrets/<name> and not through its environment.
                                          Takes system the same way
  secret list [<workspace>|system]        which secrets are set, never what they say. It says which
                                          level holds each one
  skill import <directory>                take a skill into the system from its directory. A fresh
                                          system already holds the ones this build ships with
  skill list [<workspace>]                what the system can do, or what one workspace holds
  skill attach [<workspace>] <name>       give a workspace a skill, so its sessions hold it. Say
                                          system where the workspace goes and every workspace holds
                                          it, including the ones made later
  skill detach [<workspace>] <name>       take a skill away from a workspace, or from the system
  hook import <directory>                 take a hook into the system from its directory. A hook is a
                                          constraint a session runs under, checked when it acts
  hook list [<workspace>]                 what the system enforces, or what one workspace runs under
  hook attach [<workspace>] <name>        put a workspace's sessions under a hook. Say system where
                                          the workspace goes and every workspace is under it. A
                                          session already running is not: a hook reaches a sandbox
                                          when the sandbox is built
  hook detach [<workspace>] <name>        take a hook away from a workspace, or from the system

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
	out.WriteString("\n\n## What the system can do, and what proves it\n\n")
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
const preamble = `# Quay Krewe

You are running inside a Quay Krewe session. Quay Krewe is a self hosted hub for agent sessions: the
operator commands a system of them from any channel, and each one runs in its own sandbox container.
The ` + "`krewe`" + ` command drives it. If it is on your path, you can use it.

## The words, and what they mean

  workspace   who you are, for example "me" or an organisation. Secrets attach here.
  project     a body of work inside a workspace, for example "house bills" or a ticket.
  session     one conversation, running inside its own sandbox container. It belongs to a project.
  exec        one instruction and the work it caused. You ask for something, the system works
              until it has an answer, and the whole of that is one exec. An exec runs in a
              session, and a session is a series of them. Minutes is normal.
  sandbox     the isolated container a session runs in. A session runs IN a sandbox.

They nest: workspace, then project, then session. An address is written the way a path is,
` + "`me/house-bills`" + `, and each level is a name or an identifier. ` + "`krewe use me/house-bills`" + ` moves you
there; an address typed on a command applies to that command only.

## Context, which is how you are told things

Context is files, not prompt text. Each level owns a directory, and a session's sandbox mounts them:

  workspace   /home/agent/.claude          its CLAUDE.md, and the conversation store
  project     /home/agent/workspace        its CLAUDE.md, and the project's files

So the way to teach a project something is to write it into that project's context, with
` + "`krewe context set <address> < file`" + `, or to drop files into the directory ` + "`krewe context`" + ` names. The
model reads CLAUDE.md and the working directory natively. There is no second mechanism.

A level is read back with ` + "`krewe context show <address>`" + `, which prints what it says and
nothing else. Setting overwrites, so adding a paragraph means reading the level out first, appending
to the file, and setting it back.

## What is refused, before you try it

Some rules are checked rather than read. A hook reads the command you are about to run and can
refuse it. You get the reason and the command does not run.

  a merge                    Push the branch and open a pull request. The merge is the operator's.
  ending a process           ` + "`kill`" + `, ` + "`pkill`" + `, ` + "`killall`" + `, the multiplexer's kill verbs, ` + "`docker stop`" + `,
                             ` + "`docker rm -f`" + `, ` + "`docker compose down`" + `, a prune, ` + "`systemctl stop`" + `.
                             This machine also holds the system and the operator's terminal.
                             To end this system's own work, run ` + "`krewe stop <session>`" + `.
                             To end anything else, ask the operator first.
  a pull request over
  infrastructure             Say what the deploy identity may do, in the body.

Writing about a refused command is refused too, because the gate reads the text of the command. Write
that prose to a file with an editor rather than through a shell string.

## What this is for

The operator may ask you to do things to the system itself: make a workspace, add a project, set a
subscription token, load a folder as a project's context, start a session. Everything below is what
the tool can actually do. Prefer running ` + "`krewe`" + ` to guessing, and if a command refuses, read what it
says: refusals here name what would have worked.
`
