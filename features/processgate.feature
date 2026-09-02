Feature: A process is not ended by a session, and the system is what refuses it

  A session runs with a shell, and nothing stopped it ending a process it did not start. The control
  plane, the database, the message broker and the operator's terminal all run on the same machine as
  the sandboxes, and the state a person waits on lives in them.

  At 13:41 on 1 September 2026 the operator's terminal multiplexer server restarted. Every console
  pane and every conversation pane closed in the same moment, and the build under one of them came
  back as exit code 137. Nothing was lost, and no session was shown to have caused it. The gate
  stands on the harm rather than on the blame: the panes were gone at once, and nothing asked first.

  A signal is finished before the command returns. There is no review step, no revert and no partial
  application, and everything under the target dies with it.

  The gate is a hook, so it is checked inside the sandbox at the moment the command is about to run.
  These scenarios run the entry point this build ships, which is the same file a sandbox mounts, and
  they feed it what the model runtime feeds it.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # A gate somebody has to remember to attach is off in every system nobody set up, and that is the
  # machine this happened on.
  Scenario: A fresh system is under the process gate without anybody attaching it
    Given a system seeded with the hooks this build ships
    Then the system holds the "process-gate" hook
    And the workspace runs under the "process-gate" hook

  Scenario: An operator who takes the gate off can end a process again
    Given a system seeded with the hooks this build ships
    When the operator takes the hook "process-gate" off the system
    Then the workspace runs under no "process-gate" hook

  Scenario Outline: A session about to end a process is refused, and told what to do instead
    When a session is about to run the command: <command>
    Then the process gate refuses it
    And the refusal says to end the work in the record, or to ask the operator

    Examples: the three commands in the standard toolset, in every signal form
      | command                                    |
      | kill 4213                                  |
      | kill -9 4213                               |
      | kill -SIGKILL 4213                         |
      | kill -s TERM 4213                          |
      | pkill -f claude                            |
      | killall docker                             |
      | sudo kill -9 4213                          |
      | make hooks && go test ./... ; kill -9 4213 |

    Examples: the terminal the operator is working in
      | command                    |
      | tmux kill-server           |
      | tmux kill-session -t crew  |
      | tmux kill-window -t 2      |
      | tmux kill-pane -t 1        |
      | screen -X quit             |

    Examples: the containers this system is
      | command                                |
      | docker kill quaycrew-postgres-1        |
      | docker stop quaycrew-redpanda-1        |
      | docker rm -f quaycrew-controlplane-1   |
      | docker compose down                    |
      | docker system prune -af                |
      | systemctl stop docker                  |

  # The lift is the operator's, and a session that could set it would have no gate at all.
  Scenario: A session that sets the lift variable itself is refused
    When a session is about to run the command: KREWE_MAY_END_A_PROCESS=1 kill -9 4213
    Then the process gate refuses it
    And the refusal names the variable the operator sets

  # The other direction, and the one that decides whether this hook is worth having. A gate that
  # blocks the product is worse than no gate.
  Scenario Outline: The work a session does goes through
    When a session is about to run the command: <command>
    Then the process gate allows it

    Examples: this product's own verbs, which end the work in the record and signal nothing
      | command                       |
      | krewe job stop 31a6d96d       |
      | krewe flow stop 8f21ac0e      |
      | krewe drain                   |

    Examples: the word inside a search, inside a path and inside a quoted string
      | command                                                  |
      | grep -rn "kill" internal/                                |
      | cat docs/HOOKS.md                                        |
      | echo "docker stop is refused here"                       |
      | git commit -m "fix: the console no longer kills the pane" |

    Examples: ordinary work with the same programs
      | command                        |
      | docker ps --all                |
      | docker compose up -d           |
      | tmux list-sessions             |
      | systemctl status docker        |
      | go test -count=1 ./...         |

  # It fires on every command every session runs, so a payload it cannot read has to go through. A
  # gate that refuses what it does not understand refuses the work, and a broken hook must not be
  # able to stop a system.
  Scenario: A payload the gate cannot read lets the command run
    When a session sends the process gate a payload it cannot read
    Then the process gate allows it
