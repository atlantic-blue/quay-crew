package main

import (
	"fmt"
	"path"
	"strings"
)

// Lift is the environment variable that lets one session end a process. It is read from the
// environment the session's runtime was started in, and nowhere else.
//
// A command line that sets it is refused, whatever else it does. A session that could set the
// variable could lift its own gate, and a gate a session lifts is advice with extra steps. So the
// operator sets it, in the environment they start the session with, and never in an image.
const Lift = "KREWE_MAY_END_A_PROCESS"

// Decide is the whole of the hook. A command line in, a refusal or nothing out.
//
// lifted says whether the operator set Lift in the environment this gate runs in. It is a value
// rather than a read of the environment, so what the gate refuses is a table anybody can read and
// argue with, rather than behaviour you have to start a container to find out.
func Decide(command string, lifted bool) (Refusal, bool) {
	return decide(command, lifted, depth)
}

func decide(command string, lifted bool, left int) (Refusal, bool) {
	if left <= 0 {
		return Refusal{}, false
	}
	for _, words := range Segments(command) {
		// The lift is checked before anything else, and it is checked even when the gate is already
		// lifted. Setting it is the one thing that is refused whatever the rest of the line says.
		if SetsTheLift(words) {
			return Refusal{
				What: fmt.Sprintf("This command sets %s, which is the operator's to set.", Lift),
				Instead: "A session that lifts its own gate has no gate. " +
					"Ask the operator to set it in the environment they start a session with.",
			}, true
		}
		program, argv := Program(words)
		// A shell was handed a command line as one argument, so that argument is the command.
		if inner, isShell := ShellArgument(program, argv); isShell {
			if refusal, refused := decide(inner, lifted, left-1); refused {
				return refusal, true
			}
			continue
		}
		if lifted {
			continue
		}
		if refusal, refused := ends(program, argv); refused {
			return refusal, true
		}
	}
	return Refusal{}, false
}

// A Refusal is what the session is told instead of what it asked for. Both halves are load bearing:
// a refusal that does not name the way through is a session that tries the next spelling of the same
// command until its budget runs out.
type Refusal struct {
	// What names the command that was refused, in the session's own words.
	What string
	// Instead is what to do rather than that.
	Instead string
}

func (r Refusal) String() string {
	return r.What + " " + r.Instead
}

// theOperators is the second half of every refusal here. One sentence, one place, because a session
// that reads two explanations of one rule believes it found an exception.
const theOperators = "This machine also holds the control plane, the database, the message broker and the operator's terminal. " +
	"A signal is finished before the command returns. There is no review step, no revert, and everything under the target dies with it. " +
	"To end this system's own work, run krewe job stop or krewe flow stop, which end the work in the record and signal nothing. " +
	"To end anything else, say what you want ended and why, and ask the operator to end it."

// ends is the table. Each entry is a program and the forms of it that end a running process.
//
// A polite stop is here beside a rude one. `docker stop` sends a signal and waits ten seconds, and
// `kill -9` does not wait at all, and the difference is only how long the work has to notice. Both
// end the work, so both are the operator's.
func ends(program string, argv []string) (Refusal, bool) {
	switch program {
	case "kill", "pkill", "killall":
		// Every form, including the numeric one. The signal decides how the target dies, never
		// whether it dies, so reading the signal would only tell the session which spelling to try
		// next.
		return Refusal{
			What:    fmt.Sprintf("`%s` ends a running process.", spell(program, argv)),
			Instead: theOperators,
		}, true
	case "tmux":
		return multiplexer(argv)
	case "screen":
		return older(argv)
	case "docker", "podman":
		return runtime(program, argv)
	case "docker-compose", "podman-compose":
		return composed(program, argv)
	case "systemctl":
		return manager(argv)
	}
	return Refusal{}, false
}

// multiplexer refuses the teardown verbs of the terminal multiplexer: the server, a session, a
// window and a pane.
//
// Any verb beginning with kill, because tmux takes an unambiguous abbreviation of one of its own
// commands, so `tmux kill-ses` is `tmux kill-session`. A gate that knew the four full spellings is a
// gate the fifth spelling walks through.
//
// The server is the one that matters most. It holds every console pane and every conversation pane
// the operator has open, and they all close in the same moment.
func multiplexer(argv []string) (Refusal, bool) {
	bare := bareWords(argv, valuedTmuxFlags)
	if len(bare) == 0 || !strings.HasPrefix(bare[0], "kill") {
		return Refusal{}, false
	}
	return Refusal{
		What: fmt.Sprintf("`tmux %s` closes the terminal the operator is working in.", bare[0]),
		Instead: "Every pane it holds goes with it, and a build running under one comes back as exit code 137. " +
			theOperators,
	}, true
}

// older refuses the same thing asked of the older screen program, which is its quit form.
func older(argv []string) (Refusal, bool) {
	for _, word := range argv {
		if word != "quit" && word != "kill" {
			continue
		}
		return Refusal{
			What:    fmt.Sprintf("`screen -X %s` closes the terminal the operator is working in.", word),
			Instead: theOperators,
		}, true
	}
	return Refusal{}, false
}

// runtime refuses the container runtime's own ending verbs.
//
// The five are the one that signals, the one that stops, the forced removal, the compose teardown
// and the prune. Each one ends a container, and the containers on this machine are the system: the
// control plane, the store, the broker, and every other session's sandbox.
func runtime(program string, argv []string) (Refusal, bool) {
	bare := bareWords(argv, valuedRuntimeFlags)
	if len(bare) == 0 {
		return Refusal{}, false
	}
	// `docker compose down` is the compose teardown under the runtime's own command.
	if bare[0] == "compose" {
		return composed(program+" compose", bare[1:])
	}
	// A prune reads as housekeeping and is not. `docker volume prune` takes the working trees of
	// every session that ended, which is the copy of the work that survived the last crash.
	for _, word := range bare {
		if word != "prune" {
			continue
		}
		return Refusal{
			What: fmt.Sprintf("`%s %s` removes containers, volumes and images this system is using.",
				program, strings.Join(bare, " ")),
			Instead: theOperators,
		}, true
	}
	verb := bare[0]
	// `docker exec <container> kill 1` ends a process inside another container, and the services on
	// this machine are containers. So the rest of the line is read as the command line it is.
	if verb == "exec" && len(bare) > 2 {
		inner, rest := Program(bare[2:])
		if refusal, refused := ends(inner, rest); refused {
			return refusal, true
		}
	}
	// `docker container kill` and `docker container rm` say the same thing one word further along.
	if verb == "container" && len(bare) > 1 {
		verb = bare[1]
	}
	switch verb {
	case "kill", "stop":
		return Refusal{
			What:    fmt.Sprintf("`%s %s` ends a running container.", program, verb),
			Instead: "A stop is refused beside a signal because both end the work. " + theOperators,
		}, true
	case "rm", "remove":
		// A removal that is not forced refuses a running container on its own, so the runtime
		// already holds that line. The forced one is the one that takes a container that is working.
		if !forced(argv) {
			return Refusal{}, false
		}
		return Refusal{
			What:    fmt.Sprintf("`%s rm -f` removes a container that is still running.", program),
			Instead: theOperators,
		}, true
	}
	return Refusal{}, false
}

// composed refuses the compose teardown, whichever of the two spellings asked for it.
//
// Down, stop and kill, because the file names every service in the stack. One word takes the control
// plane, the database, the broker and the whole observability stack together.
func composed(program string, argv []string) (Refusal, bool) {
	bare := bareWords(argv, valuedComposeFlags)
	for _, word := range bare {
		if word != "down" && word != "stop" && word != "kill" {
			continue
		}
		return Refusal{
			What: fmt.Sprintf("`%s %s` ends every service in the compose file.", program, word),
			Instead: "The control plane, the store, the broker and the observability stack go together. " +
				theOperators,
		}, true
	}
	return Refusal{}, false
}

// manager refuses the two service manager equivalents.
func manager(argv []string) (Refusal, bool) {
	bare := bareWords(argv, valuedSystemctlFlags)
	if len(bare) == 0 {
		return Refusal{}, false
	}
	if bare[0] != "stop" && bare[0] != "kill" {
		return Refusal{}, false
	}
	return Refusal{
		What:    fmt.Sprintf("`systemctl %s` ends a service on this machine.", bare[0]),
		Instead: theOperators,
	}, true
}

// SetsTheLift says whether this command sets the lift variable, as an assignment in front of a
// command, as an argument to export, or as an argument to env.
func SetsTheLift(words []string) bool {
	for _, word := range words {
		name, _, found := strings.Cut(word, "=")
		if found && path.Base(name) == Lift {
			return true
		}
	}
	return false
}

// forced says whether a removal was told to take a container that is still running.
func forced(argv []string) bool {
	for _, word := range argv {
		if word == "-f" || word == "--force" {
			return true
		}
		// The short flags travel together, so -fv is a forced removal too. A long flag is never one
		// of these, and a value after a flag is not a flag at all.
		if strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "--") && strings.Contains(word, "f") {
			return true
		}
	}
	return false
}

// spell writes the refused command back the way the session typed it, with its signal, so the
// refusal names what was asked for rather than a category.
func spell(program string, argv []string) string {
	said := append([]string{program}, argv...)
	if len(said) > 4 {
		said = append(said[:4:4], "...")
	}
	return strings.Join(said, " ")
}

// The flags that take a separate value, per program. A flag that takes one takes its value with it,
// or the value reads as the command.
var (
	valuedTmuxFlags      = map[string]bool{"-f": true, "-S": true, "-L": true, "-c": true}
	valuedRuntimeFlags   = map[string]bool{"-H": true, "--host": true, "--context": true, "--config": true, "-l": true, "--log-level": true}
	valuedComposeFlags   = map[string]bool{"-f": true, "--file": true, "-p": true, "--project-name": true, "--project-directory": true, "-t": true, "--timeout": true}
	valuedSystemctlFlags = map[string]bool{"-H": true, "--host": true, "-M": true, "--machine": true, "--signal": true, "-s": true}
)

// bareWords drops the flags, so what is left is the command, its subcommand and its arguments. A
// flag that takes a separate value takes its value with it, or the value reads as a command.
func bareWords(argv []string, valued map[string]bool) []string {
	bare := make([]string, 0, len(argv))
	for at := 0; at < len(argv); at++ {
		word := argv[at]
		if !strings.HasPrefix(word, "-") || word == "-" {
			bare = append(bare, word)
			continue
		}
		name, _, joined := strings.Cut(word, "=")
		if valued[name] && !joined {
			at++
		}
	}
	return bare
}
