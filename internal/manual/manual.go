// Package manual is krewe describing itself, in a form meant to be read by whatever is running in a
// session rather than by somebody scanning a terminal.
//
// It exists because a session sitting in the panel beside the console knows nothing about the system it
// is next to, so it is told with this.
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

	"github.com/atlantic-blue/krewe/features"
)

// Commands is the command list, which is also what `krewe` prints with no arguments. It lives here so
// the tool and the document a session is told with cannot describe two different tools.
const Commands = `usage: krewe [command]

with no command, krewe opens the console: a full screen view of every resource the system has.
press : to switch resource, / to filter, enter to drill in, s to shell into a session, q to quit.

you work in one place at a time, and say where with an address: workspace/project/session.

  krewe use me/house-bills
  krewe task "when is the electricity bill due"
  krewe task --dispatch "read the repository and write the migration"

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
                                          also names a project. A job declared here works in it and
                                          ends in a pull request against it. Public unless you say
                                          otherwise, because a pipeline's minutes are free on a
                                          public repository, and a kind you leave out is the kind
                                          the project already holds
  project repository show [<address>]     read where a project's work lands, recording nothing
  project delete [<workspace>/]<project>  remove it and the sessions inside it, confirmed the same way
  flow import <file>                      store an automation graph the system can run
  flow start [<address>] <graph>          begin a run of it in a project
  flow schedule [<address>] <graph>       let it run on its own, as often as it says
  flow unschedule [<address>] <graph>     stop it running on its own
  flow list [<address>|system]            what has run, newest first. It reads where you are
                                          standing and says so; system reads every project
  flow show <run>                         where one run got to, what it cost, why it stopped,
                                          and how to read the job its steps went out as
  flow stop <run> [<reason>]              halt a run in flight, keeping the reason
  flow answer <run> <answer>              tell a run waiting on you what you decided
  job create [<address>]                  declare a job: what it is, and what has to
    --title "..." --brief "..."           happen. The system keeps it, so the intent outlives the
    [--role <name>] [--mode <mode>]       terminal that asked for it, and a controller runs it as
    [--requires <material>]               the role it names. --requires says what the job cannot be
    [--after <job>] [--label k=v]         done without, one of job, context or skills, and a role
    [--budget-tokens <n>] [--deadline <t>] that does not receive it is never handed the job.
    [--expect-file <path>]                --repository says where the work goes, and a job that
    [--expect-contains "..."]             names one is not done until its answer names a pull
    [--repository <owner>/<name>]         request against it. --product is one sentence in a
    [--product "..."]                     person's words: what somebody does with what gets built
    [--claim <piece of work>]             and what they get back. Every job under this one carries
                                          it, and it is what the design is read against. --claim is
                                          the piece of work this job takes, an issue, a branch or a
                                          name, and a second job claiming it is refused while this
                                          one holds it, so two sessions cannot build the same slice.
                                          A brief that asks the job to wait for the checks, or to
                                          merge on the result, is refused: nothing wakes a job, so
                                          that shape is a flow
  history [<address>|system]              what the system did over a window of time: what ran, what it
    [--since <date>] [--until <date>]     cost, and what failed and why. The read to make instead of
    [--limit <n>]                         being told. It prints the window added up, then one line
                                          for each job, newest first, with the reason under a job
                                          that failed. Dates are written 2026-08-28, and the last
                                          day named is included whole. The window is the last week
                                          unless you say otherwise, and the total always covers the
                                          whole window even when --limit prints fewer rows. It says
                                          how many it left out
  job list [<address>|system]             the jobs there are, newest first. With no address it
    [--phase <phase>] [--outcome <word>]  reads where you are standing and says so, and system reads
    [--label k=v] [--parent <job>]        every project. Narrow it further with --phase, --outcome,
    [--roots]                             --label, --parent or --roots. An outcome is one of proved,
                                          unproved, blocked or decide: the word the session ended on,
                                          which the phase cannot tell you
  job show <job>                          one job whole: what it is, where it got to, the word it
                                          ended on, why it stopped, what came back, and where its
                                          session spent its context
  job stop <job> [<reason>]               halt a job that has not ended, keeping the reason
  job ask "<question>"                    put a question to a person about the job you are running,
                                          when a decision no measurement settles is in your way.
                                          The job stops there and nothing moves it until somebody
                                          answers, so end your task and say you are waiting. The
                                          answer arrives as your next task
  job answer <job> "<answer>"             tell a job waiting on you what you decided. It starts
                                          again with the answer, in the session that asked
  job step "<what you finished>"          record one step of the job you are running, as you finish
                                          it. If the job dies part way, what is on that record is
                                          where it carries on from, and what is not on it is done a
                                          second time
  job resume <job>                        carry on with a job that failed, from the first step it
                                          did not finish. It keeps its session, so its working
                                          directory, its branch and its pull request are where it
                                          left them, and it is asked to fetch its base and say what
                                          moved while it was stopped
  job refuse <job> [<reason>]             the other answer to a job that failed: the work was wrong,
                                          so end it rather than continue it. It stops, and a stopped
                                          job is never continued
  steer [<job>] "<what you said>"         mark one moment you had to say something the system should
                                          have known, asked for, or refused on its own. With no job
                                          it lands on the one in flight where you stand. The count
                                          is the score of that job, so mark it while it happens
  steers [<job>]                          the marks read back. With a job, every steer of that whole
                                          tree in order, with the time and the job each landed on.
                                          With none, every job where you stand against the one
                                          before it, which is how you tell whether this went better
                                          than last time
  target [<address>]                      where a project ships: the account, the region inside it,
    [--account <id>]                      and the role a pipeline assumes to get there. With no
    [--region <name>]                     values it reads what the project declared, and with them
    [--identity <arn>] [--clear]          it declares it. The identity has to belong to the account,
                                          because pasting the role from the other account is
                                          invisible until a pipeline runs. Nothing here deploys
                                          anything: infrastructure ships through the repository's
                                          own pipeline, and this says which account that pipeline is
                                          aimed at. --clear takes it back off
  limits [<workspace>]                    what a workspace lets its sessions declare, and how long
    [--max-depth <n>]                     it keeps a session nobody is using: how deep the tree of
    [--max-running <n>]                   jobs may go, how many run at once, what a tree may spend,
    [--budget-tokens <n>]                 how long a controller holds a job, and how long
    [--lease <duration>]                  a settled session keeps its container before the system
    [--reclaim <duration>]                takes it back and then files it away. Max depth starts at
    [--archive <duration>]                zero, so no session declares a job until you raise it. The
                                          reclaim and archive times start unset, and unset means the
                                          system does nothing. The lease is the system's hold on a job
                                          and not the credential a session runs under: a credential
                                          lasts as long as its job, and this setting does not reach
                                          it. A session may read none of this and set none of it
  task [<address>] <text>                 start or continue a session, and wait here for the
                                          answer. For a short question, where the reply is the
                                          point
  task --dispatch [<address>] <text>      the same, and let go of the task. It runs in the system, so
                                          closing the terminal does not take the work with it
  task list <session>                     what a session was asked to do, and what came back
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
  answer <session> [--all]                 what a session came back with, and nothing else, so a
                                          caller can pipe it. The most recent answer, or with --all
                                          every one of them, oldest first
  stop <session> [<reason>]               halt the task one session is running, keeping the reason.
                                          The session survives: its conversation, its container and
                                          its history all stay, so the next task continues it.
                                          A stop while nothing is running says so and changes
                                          nothing
  drain [anyway]                          put every live session down, so an upgrade does not take
                                          their containers away underneath them. Refuses while a
                                          task is working, and anyway drains over it
  label <session> [<text>]                 what you call a conversation, so a listing reads as
                                          conversations rather than identifiers. No text reads it,
                                          and "" clears it
  mode <session> [<mode>]                  what a session's tasks may do without asking: plan, edits
                                          or dangerous. A task nobody waits for has nobody to approve
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
  web [<address>]                         read the system in a browser on this machine. Read only,
                                           and it serves 127.0.0.1:8080 unless told another port
  room                                     how much memory this sandbox actually has, and what to
                                           do about a gate that does not fit in it. A sandbox with
                                           no limit advertises the whole machine, and the kernel
                                           kills against what is free. Run where there is no such
                                           accounting, on a Mac, it asks the system what its own
                                           machine holds and which session is holding it
  render <url> [<file>] [<size>]           draw a page into a picture and say what it drew, so a
    [light|dark] [<wait>]                  session can look at what it built. The whole page, at
                                           1280x900 in light unless told otherwise
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
  role import <directory>                 take a role into the system from its directory. A role is a
                                          named way of working: a brief, the model it runs on, and
                                          the material it may receive
                                          this build ships sixteen in roles/ at the root of the
                                          repository, and a fresh system is seeded with none of them
  role list [<workspace>]                 what roles the system holds, or what one workspace holds
  role show [<workspace>] <name>          read one role back whole: what it is, what it may do, who
                                          holds it, and the brief in full. The brief is the role, so
                                          this is how you audit what a session was told
  role attach [<workspace>] <name>        give a workspace a role. Say system where the workspace
                                          goes and every workspace holds it
  role detach [<workspace>] <name>        take a role away from a workspace, or from the system

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
const preamble = `# Quay System

You are running inside a Quay System session. Quay System is a self hosted hub for agent sessions: the
operator commands a system of them from any channel, and each one runs in its own sandbox container.
The ` + "`krewe`" + ` command drives it. If it is on your path, you can use it.

## The words, and what they mean

  workspace   who you are, for example "me" or an organisation. Secrets attach here.
  project     a body of work inside a workspace, for example "house bills" or a ticket.
  job         what somebody wants done. A row the system keeps, so the intent outlives the
              terminal that asked for it, and a controller runs it. It is a Kubernetes Job.
  session      one conversation. A task runs in a project.
  task        one instruction and the work it caused. You ask for something, the system works
              until it has an answer, and the whole of that is one task. Minutes is normal.
  session     a session that is running, inside its own sandbox container.
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

## What this is for

The operator may ask you to do things to the system itself: make a workspace, add a project, set a
subscription token, load a folder as a project's context, start a session. Everything below is what
the tool can actually do. Prefer running ` + "`krewe`" + ` to guessing, and if a command refuses, read what it
says: refusals here name what would have worked.
`
