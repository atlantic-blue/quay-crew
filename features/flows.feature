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
