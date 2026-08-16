Feature: A flow runs a graph across sessions

  Inside a session the model decides what happens next, and it is better at that than any diagram.
  Across sessions the operator wants the opposite: a decision written down where it can be read,
  tested and stopped. A flow is a graph of dispatches and choices, pinned to a version when a run
  starts, moving one node at a time with every movement recorded in the same transaction as the
  position it describes.

  A run owns its own session, named after the graph and the run, so the console reads as what the
  run is doing and a task on that session is unambiguously the run's. When a run ends its session is
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

  Scenario: A run moves through its graph and puts its session away
    When the operator starts the flow "fix-red" in the project
    Then the flow run is done
    And the run's session was asked "fix the build" and then "push the fix"
    And the run's session is archived

  Scenario: Every movement of the run is recorded, in order
    When the operator starts the flow "fix-red" in the project
    Then the run's transitions read back as "fix", "push", "done"

  Scenario: A failed task takes the other edge
    Given the next task will fail
    When the operator starts the flow "fix-red" in the project
    Then the flow run is done
    And the run's session was asked 1 task

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
    And the run's session was asked 1 task
    When ten minutes pass and the crew looks for waits that are due
    Then the flow run is done
    And the run's session was asked "start the build" and then "is it done"

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
    And the run's session was asked 1 task

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
    And the run's session was asked "start the build" and then "is it done"

  # The whole difference between an automation and a shell script: a person decides whether it goes
  # further. Delivered through the command line, so it needs no chat channel and no bot token.
  Scenario: A run asks the operator, and carries on with what it is told
    Given the crew holds this flow graph:
      """
      name: careful
      version: 1
      nodes:
        fix:    { type: dispatch, prompt: "fix the build" }
        permit: { type: ask, text: "fixed it locally. push?" }
        yes:    { type: choice, on: { answer: "yes" } }
        push:   { type: dispatch, prompt: "push it" }
      edges:
        - [fix, permit]
        - [permit, yes]
        - [yes, push, "true"]
        - [yes, done, "false"]
        - [push, done]
      """
    When the operator starts the flow "careful" in the project
    Then the flow run is asking "fixed it locally. push?"
    And the run's session was asked 1 task
    When the operator answers the run with "yes"
    Then the flow run is done
    And the run's session was asked "fix the build" and then "push it"

  Scenario: A run told no does not do the thing it asked about
    Given the crew holds this flow graph:
      """
      name: careful
      version: 1
      nodes:
        fix:    { type: dispatch, prompt: "fix the build" }
        permit: { type: ask, text: "push?" }
        yes:    { type: choice, on: { answer: "yes" } }
        push:   { type: dispatch, prompt: "push it" }
      edges:
        - [fix, permit]
        - [permit, yes]
        - [yes, push, "true"]
        - [yes, done, "false"]
        - [push, done]
      """
    When the operator starts the flow "careful" in the project
    And the operator answers the run with "no"
    Then the flow run is done
    And the run's session was asked 1 task

  # A question nobody answered must never answer itself, or an automation asking permission would
  # take silence for a yes.
  Scenario: No timer answers a question
    Given the crew holds this flow graph:
      """
      name: careful
      version: 1
      nodes:
        fix:    { type: dispatch, prompt: "fix the build" }
        permit: { type: ask, text: "push?" }
        push:   { type: dispatch, prompt: "push it" }
      edges:
        - [fix, permit]
        - [permit, push]
        - [push, done]
      """
    When the operator starts the flow "careful" in the project
    And ten minutes pass and the crew looks for waits that are due
    Then the flow run is asking "push?"
    And the run's session was asked 1 task

  # The crew acting on its own, which is the point of the whole thing: without a trigger, an
  # automation is a script somebody still has to remember to run.
  Scenario: A graph runs on its own when its schedule comes due
    Given the crew holds this flow graph:
      """
      name: nightly
      version: 1
      on:
        every: 24h
      nodes:
        sweep: { type: dispatch, prompt: "check the overnight builds" }
      edges:
        - [sweep, done]
      """
    When the operator schedules "nightly" in the project
    Then no run of "nightly" has started
    When a day passes and the crew looks for waits that are due
    Then a run of "nightly" has started and finished

  # Scheduling and starting must not be the same act, or an operator cannot arrange an automation
  # for tonight without also running it now.
  Scenario: Scheduling a graph does not start it
    Given the crew holds this flow graph:
      """
      name: nightly
      version: 1
      on:
        every: 24h
      nodes:
        sweep: { type: dispatch, prompt: "check the overnight builds" }
      edges:
        - [sweep, done]
      """
    When the operator schedules "nightly" in the project
    And the crew looks for waits that are due
    Then no run of "nightly" has started

  Scenario: A graph that says nothing about when it runs cannot be scheduled
    Given the crew holds this flow graph:
      """
      name: manual
      version: 1
      nodes:
        go: { type: dispatch, prompt: "go" }
      edges:
        - [go, done]
      """
    When the operator schedules "manual" in the project
    Then the control plane refuses it as the wrong state

  Scenario: An unscheduled graph stops running on its own
    Given the crew holds this flow graph:
      """
      name: nightly
      version: 1
      on:
        every: 24h
      nodes:
        sweep: { type: dispatch, prompt: "check the overnight builds" }
      edges:
        - [sweep, done]
      """
    When the operator schedules "nightly" in the project
    And the operator unschedules "nightly" in the project
    And a day passes and the crew looks for waits that are due
    Then no run of "nightly" has started

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

  # The first flow ever run against a real crew finished at done, reported four transitions and read
  # back as a success. None of the work happened: the repository was not there, every task said so,
  # and the run took the success edge anyway, because `result.failed` says the model did not error
  # and nothing else. A task that could not do the work is not a failed task.
  #
  # So a dispatch node says what will show it worked, and the crew checks that rather than reading
  # the model's account of itself.
  Scenario: A run stops when the work a node said it would do did not happen
    Given the crew holds this flow graph:
      """
      name: site-check
      version: 1
      nodes:
        read: { type: dispatch, prompt: "read package.json and say what runs the tests", expect: { file: package.json } }
        tell: { type: dispatch, prompt: "summarise the project" }
      edges:
        - [read, tell]
        - [tell, done]
      """
    When the operator starts the flow "site-check" in the project
    Then the flow run is stopped
    And reading the run back says it stopped over "package.json"
    And the run's session was asked 1 task

  # The other direction. A check that stops every run would satisfy the scenario above and be worth
  # nothing, so this one is the same graph against a model that does the work.
  Scenario: A run carries on when the work did happen
    Given the model writes "package.json" while it works
    And the crew holds this flow graph:
      """
      name: site-check
      version: 1
      nodes:
        read: { type: dispatch, prompt: "read package.json and say what runs the tests", expect: { file: package.json } }
        tell: { type: dispatch, prompt: "summarise the project" }
      edges:
        - [read, tell]
        - [tell, done]
      """
    When the operator starts the flow "site-check" in the project
    Then the flow run is done
    And the run's session was asked 2 tasks

  # The weaker check, for work that leaves no file behind. It is still the model's own prose, and it
  # is here because a graph that runs a command and reads its answer has nothing else to point at.
  Scenario: A run stops when the reply does not carry what the node said it would
    Given the crew holds this flow graph:
      """
      name: test-run
      version: 1
      nodes:
        run: { type: dispatch, prompt: "run the tests and say how they went", expect: { contains: "all green" } }
      edges:
        - [run, done]
      """
    When the operator starts the flow "test-run" in the project
    Then the flow run is stopped
    And reading the run back says it stopped over "all green"

  # A run's session is made by its first dispatch, so there is nothing to set a mode on before the run
  # starts and `quay mode` has nothing to point at. Every automation therefore ran in the mode a
  # session is born in, and a graph whose first step is "clone this" could not take it: cloning needs
  # the network, and a dispatched task has nobody to ask for permission.
  Scenario: A graph says what its runs may do, and the turns run in it
    Given the crew holds this flow graph:
      """
      name: clone-first
      version: 1
      mode: dangerous
      nodes:
        clone: { type: dispatch, prompt: "clone the repository into /home/agent/shared" }
      edges:
        - [clone, done]
      """
    When the operator starts the flow "clone-first" in the project
    Then the flow run is done
    And the task ran in permission mode "bypassPermissions"

  Scenario: A graph that says nothing about its mode leaves its runs in the one a session is born in
    When the operator starts the flow "fix-red" in the project
    Then the flow run is done
    And the task ran in permission mode "acceptEdits"

  Scenario: A graph whose mode is not a mode is refused at import
    Given the operator imports this flow graph, which is refused:
      """
      name: nonsense
      version: 1
      mode: whenever
      nodes:
        go: { type: dispatch, prompt: "go" }
      edges:
        - [go, done]
      """
    Then the refusal names the modes there are

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
