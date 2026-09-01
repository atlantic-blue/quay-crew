package main

import (
	"strings"
	"testing"
)

// The table is the whole gate, so the tests are a table too. Each line is a command a session could
// run, and what this gate says about it.

// TestTheGateRefusesACommandThatEndsAProcess. Refusing comes first, because a gate that always
// passes satisfies every test about passing.
func TestTheGateRefusesACommandThatEndsAProcess(t *testing.T) {
	for _, command := range []string{
		// The three commands in the standard toolset, in every signal form, including the numeric one.
		"kill 4213",
		"kill -9 4213",
		"kill -KILL 4213",
		"kill -SIGTERM 4213",
		"kill -s 9 4213",
		"kill -s SIGKILL 4213",
		"kill -- -4213",
		"pkill node",
		"pkill -9 -f claude",
		"killall docker",
		"killall -9 tmux",
		"/bin/kill -9 4213",
		// The same command, reached another way.
		"sudo kill -9 4213",
		"bash -c \"kill -9 4213\"",
		"cd /tmp && kill -9 4213",
		"ps aux | grep claude ; kill -9 4213",
		"xargs kill -9",
		"env kill -9 4213",

		// The terminal multiplexer, for the server, a session, a window and a pane.
		"tmux kill-server",
		"tmux kill-session -t console",
		"tmux kill-window -t 2",
		"tmux kill-pane -t 1",
		"tmux -L krewe kill-server",
		"tmux kill-ses -t console",

		// The container runtime.
		"docker kill quaycrew-postgres-1",
		"docker stop quaycrew-redpanda-1",
		"docker rm -f quaycrew-controlplane-1",
		"docker rm --force quaycrew-controlplane-1",
		"docker container stop quaycrew-postgres-1",
		"docker container rm -f quaycrew-postgres-1",
		"docker compose down",
		"docker compose -f deploy/docker-compose.yml down -v",
		"docker-compose down",
		"docker compose stop",
		"docker compose kill",
		"docker system prune -af",
		"docker volume prune -f",
		"podman stop quaycrew-postgres-1",

		// The service manager, and the older screen program.
		"systemctl stop docker",
		"systemctl kill --signal=SIGKILL docker",
		"sudo systemctl stop docker.service",
		"screen -X quit",
		"screen -S console -X kill",
	} {
		t.Run(command, func(t *testing.T) {
			refusal, refused := Decide(command, false)
			if !refused {
				t.Fatalf("the gate let it through, and it ends something the operator is using")
			}
			// A refusal that does not say what to do instead is a session trying the next spelling
			// until its budget runs out.
			if refusal.What == "" || refusal.Instead == "" {
				t.Fatalf("the refusal is missing half of itself: %+v", refusal)
			}
		})
	}
}

// TestTheGateAllowsTheWorkASessionDoes. The other direction, and the one that decides whether this
// hook is worth having. A gate that blocks the product is worse than no gate.
func TestTheGateAllowsTheWorkASessionDoes(t *testing.T) {
	for _, command := range []string{
		// This product's own verbs. Ending a job or a flow run ends the work in the record. It
		// signals nothing, and the system closes the sandbox itself.
		"krewe job stop 31a6d96d",
		"krewe flow stop 8f21ac0e",
		"krewe job stop 31a6d96d \"the plan was wrong\"",
		"quay job stop 31a6d96d",
		"krewe drain",
		"krewe session list",

		// The word inside a search, inside a path, and inside a quoted string.
		"grep -rn \"kill\" internal/",
		"grep -rn kill-session docs/HOOKS.md",
		"rg 'docker stop' --files-with-matches",
		"cat hooks/process-gate/killing.md",
		"ls /home/agent/hooks/process-gate",
		"echo \"kill -9 is refused here\"",
		"git commit -m \"fix: the console no longer kills the pane it drew\"",
		"gh pr create --title \"a gate\" --body \"docker stop is refused now\"",

		// Ordinary work with the same programs.
		"docker ps --all",
		"docker compose up -d",
		"docker compose logs -f controlplane",
		"docker inspect quaycrew-postgres-1",
		"docker rm quaycrew-postgres-1",
		"docker image ls",
		"tmux list-sessions",
		"tmux new-session -d -s work",
		"systemctl status docker",
		"screen -ls",
		"ps aux | grep claude",
		"go test -count=1 ./...",
		"make hooks",
	} {
		t.Run(command, func(t *testing.T) {
			if refusal, refused := Decide(command, false); refused {
				t.Fatalf("the gate refused work a session does: %s", refusal)
			}
		})
	}
}

// TestTheRuntimeIsNotACommandThisGateSees. The system tears a sandbox down at the end of that
// sandbox's life, and this gate must not be able to stop it.
//
// It cannot, and the reason is the shape rather than an exception in the table. The runtime removes
// a container from the control plane process, which runs no hook. This gate fires on the Bash tool
// inside a session, so a removal typed there is a session reaching outside its own box, at a
// container that is another session's work. That one stays refused.
//
// The product's own way to end a session is the verb, and the verb is what the table lets through.
func TestTheRuntimeIsNotACommandThisGateSees(t *testing.T) {
	// What the control plane runs, if a session typed it: still refused, because a session is not
	// the control plane.
	if _, refused := Decide("docker rm -f krewe-9f2b1c4d5e6a7b8c9d0e1f2a", false); !refused {
		t.Fatal("a session can remove another session's sandbox, which is the operator's container")
	}
	// What a session runs to end its own work.
	if refusal, refused := Decide("krewe job stop 31a6d96d", false); refused {
		t.Fatalf("the gate refuses the product's own way to end a job: %s", refusal)
	}
}

// TestTheGateReadsAWordAfterASeparatorHalfwayDownALine. The refused word is rarely the first thing
// typed. A gate that only read the start of the line is a gate every second command walks through.
func TestTheGateReadsAWordAfterASeparatorHalfwayDownALine(t *testing.T) {
	for _, command := range []string{
		"make hooks && go test ./... ; kill -9 4213",
		"go build ./... || killall claude",
		"echo starting | tee log.txt && docker compose down",
		"for id in 1 2 3; do kill -9 $id; done",
		"( cd deploy && docker compose down )",
		"go test ./... \n tmux kill-server",
	} {
		t.Run(command, func(t *testing.T) {
			if _, refused := Decide(command, false); !refused {
				t.Fatal("the gate read the first word and stopped, so the rest of the line is unguarded")
			}
		})
	}
}

// TestOnlyTheOperatorLiftsTheGate. The lift is an environment variable, and a session that could set
// it would have no gate at all.
func TestOnlyTheOperatorLiftsTheGate(t *testing.T) {
	// Set in the environment the session runs under, by the operator, for one command.
	if refusal, refused := Decide("kill -9 4213", true); refused {
		t.Fatalf("the operator lifted the gate and it refused anyway: %s", refusal)
	}
	// Set on the command line, by the session, which is the session lifting its own gate.
	for _, command := range []string{
		Lift + "=1 kill -9 4213",
		"export " + Lift + "=1",
		"export " + Lift + "=1 && kill -9 4213",
		"env " + Lift + "=1 kill -9 4213",
		"bash -c \"" + Lift + "=1 kill -9 4213\"",
	} {
		t.Run(command, func(t *testing.T) {
			refusal, refused := Decide(command, false)
			if !refused {
				t.Fatal("a session set the lift variable itself, so the gate is advice")
			}
			if !strings.Contains(refusal.String(), Lift) {
				t.Fatalf("the refusal does not name the variable, so nobody knows what happened: %s", refusal)
			}
		})
	}
	// Even with the gate lifted, setting the variable is refused. The operator's yes is a yes to one
	// command, not permission to write the permission.
	if _, refused := Decide(Lift+"=1 kill -9 4213", true); !refused {
		t.Fatal("a lifted gate lets a session set the variable, so the lift outlives the operator's yes")
	}
}

// TestTheRefusalSaysWhatToDoInstead. A refusal that names no way through is a session that tries the
// next spelling of the same command until its budget runs out.
func TestTheRefusalSaysWhatToDoInstead(t *testing.T) {
	refusal, refused := Decide("tmux kill-server", false)
	if !refused {
		t.Fatal("the gate allowed the command that closed every pane on 1 September")
	}
	for _, needed := range []string{"krewe job stop", "ask the operator", "137"} {
		if !strings.Contains(refusal.String(), needed) {
			t.Errorf("the refusal never says %q:\n%s", needed, refusal)
		}
	}
}
