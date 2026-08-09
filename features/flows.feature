Feature: A flow runs a graph across sessions

  Inside a session the model decides what happens next, and it is better at that than any diagram.
  Across sessions the operator wants the opposite: a decision written down where it can be read,
  tested and stopped. A flow is a graph of dispatches and choices, pinned to a version when a run
  starts, moving one node at a time with every movement recorded in the same transaction as the
  position it describes.

  A run owns its own thread, named after the graph and the run, so the console reads as what the
  run is doing and a turn on that thread is unambiguously the run's. When a run ends its thread is
  put away, because a finished run must not leave a container behind.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"
    And the crew holds this flow graph:
      """
      name: fix-red
      version: 1
      nodes:
        fix:   { type: dispatch, prompt: "fix the build" }
        ok:    { type: choice, on: { result.failed: "false" } }
        push:  { type: dispatch, prompt: "push the fix" }
      edges:
        - [fix, ok]
        - [ok, push, "true"]
        - [ok, done, "false"]
        - [push, done]
      """

  Scenario: A run moves through its graph and puts its thread away
    When the operator starts the flow "fix-red" in the project
    Then the flow run is done
    And the run's thread was asked "fix the build" and then "push the fix"
    And the run's thread is archived

  Scenario: Every movement of the run is recorded, in order
    When the operator starts the flow "fix-red" in the project
    Then the run's transitions read back as "fix", "push", "done"

  Scenario: A failed turn takes the other edge
    Given the next turn will fail
    When the operator starts the flow "fix-red" in the project
    Then the flow run is done
    And the run's thread was asked 1 turn

  # A wait is a row rather than a timer somebody is holding, which is the whole reason it survives
  # the crew being restarted underneath it.
  Scenario: A run waits, and carries on when its time comes
    Given the crew holds this flow graph:
      """
      name: patient
      version: 1
      nodes:
        ask:   { type: dispatch, prompt: "start the build" }
        pause: { type: wait, for: 10m }
        check: { type: dispatch, prompt: "is it done" }
      edges:
        - [ask, pause]
        - [pause, check]
        - [check, done]
      """
    When the operator starts the flow "patient" in the project
    Then the flow run is waiting
    And the run's thread was asked 1 turn
    When ten minutes pass and the crew looks for waits that are due
    Then the flow run is done
    And the run's thread was asked "start the build" and then "is it done"

  Scenario: A wait that is not yet due is left alone
    Given the crew holds this flow graph:
      """
      name: patient
      version: 1
      nodes:
        ask:   { type: dispatch, prompt: "start the build" }
        pause: { type: wait, for: 10m }
        check: { type: dispatch, prompt: "is it done" }
      edges:
        - [ask, pause]
        - [pause, check]
        - [check, done]
      """
    When the operator starts the flow "patient" in the project
    And the crew looks for waits that are due
    Then the flow run is waiting
    And the run's thread was asked 1 turn

  # Editing a graph must not change an automation that is halfway through it, which is the whole
  # reason a run pins a version. A wait is where that gets tested, because it is the only moment a
  # run sits still long enough for somebody to edit the file underneath it.
  Scenario: A graph edited while a run waits does not change that run
    Given the crew holds this flow graph:
      """
      name: patient
      version: 1
      nodes:
        ask:   { type: dispatch, prompt: "start the build" }
        pause: { type: wait, for: 10m }
        check: { type: dispatch, prompt: "is it done" }
      edges:
        - [ask, pause]
        - [pause, check]
        - [check, done]
      """
    When the operator starts the flow "patient" in the project
    Then the flow run is waiting
    Given the crew holds this flow graph:
      """
      name: patient
      version: 2
      nodes:
        ask:   { type: dispatch, prompt: "start the build" }
        pause: { type: wait, for: 10m }
        check: { type: dispatch, prompt: "the second version asks something else" }
      edges:
        - [ask, pause]
        - [pause, check]
        - [check, done]
      """
    When ten minutes pass and the crew looks for waits that are due
    Then the flow run is done
    And the run's thread was asked "start the build" and then "is it done"

  Scenario: A run is pinned to the version it started with
    Given the crew holds this flow graph:
      """
      name: fix-red
      version: 2
      nodes:
        only: { type: dispatch, prompt: "the second version" }
      edges:
        - [only, done]
      """
    When the operator starts the flow "fix-red" in the project
    Then the flow run is pinned to version 2

  Scenario: A graph a run could fall off is refused at import
    Given the operator imports this flow graph, which is refused:
      """
      name: broken
      version: 1
      nodes:
        a: { type: dispatch, prompt: "a" }
      edges:
        - [a, nowhere]
      """
    Then the refusal names the node nobody declared

  Scenario: A flow nobody imported cannot start
    When the operator starts the flow "never-imported" in the project
    Then starting it is refused as not found
