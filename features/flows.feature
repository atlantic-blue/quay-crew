Feature: A flow runs a graph across sessions

  Inside a session the model decides what happens next, and it is better at that than any diagram.
  Across sessions the operator wants the opposite: a decision written down where it can be read,
  tested and stopped. A flow is a graph of dispatches and choices, pinned to a version when a run
  starts, moving one node at a time with every movement recorded in the same transaction as the
  position it describes.

  A run holds nothing while it works. Each step is a job: the run writes it down and
  returns, a controller sends its task, and the run carries on when that job ends. So a step owns
  its own session, the session is put away the moment the step ends, and a run waiting on a person
  holds no container at all.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"
    And the system holds this flow graph:
      """
      name: fix-red
      version: 1
      mode: edits
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

  Scenario: A run moves through its graph and puts its sessions away
    When the operator starts the flow "fix-red" in the project
    Then the flow run is done
    And the run's steps were asked "fix the build" and then "push the fix"
    And every session the run started is archived

  # The fault this slice exists for. A run used to dispatch and read the reply in the same statement,
  # so starting one lasted as long as the model did and the run could react to nothing while it
  # waited. It writes the step down and returns instead.
  Scenario: Starting a run writes its step down and returns
    When the operator starts the flow "fix-red" in the project, without driving the system
    Then the flow run is working
    And the system was asked to run 0 tasks
    And the run's step is a job under the run, one level deeper

  Scenario: A run whose step is running holds no call open
    Given the model takes longer over a task than anybody will wait
    When the operator starts the flow "fix-red" in the project, without driving the system
    And the controller ticks
    And a task is under way
    Then the flow run is working
    And the run's step is running while the model has not answered
    When the model finishes the task
    And the system is driven
    Then the flow run is done

  # The trap issue 354 names by name: a run that waits or asks used to hold its container for the
  # whole wait, because it closed its session only at the end. The step's job ended when it
  # answered, so the run asks its question holding nothing.
  Scenario: A run waiting on a person holds no container
    Given the system holds this flow graph:
      """
      name: careful
      version: 1
      mode: edits
      nodes:
        fix:    { type: dispatch, prompt: "fix the build" }
        permit: { type: ask, text: "fixed it locally. push?" }
      edges:
        - [fix, permit]
        - [permit, done]
      """
    When the operator starts the flow "careful" in the project
    Then the flow run is asking "fixed it locally. push?"
    And no session the run started is still live
    And no job of the run is still open

  # A step's answer used to land in the run's state as result.reply, where quay flow show truncated
  # it. It is a field on a job now, so a caller reads it as data.
  Scenario: Every step's answer is a field a caller can read
    When the operator starts the flow "fix-red" in the project
    Then the flow run is done
    And each of the run's steps carries the answer its own task gave
    And the run's own job carries what the run came to

  # The four records issue 349 named and nothing ever wrote. They are written against the
  # job that carries the run, so a reader has one history rather than two.
  Scenario: A run writes the record of its own life
    When the operator starts the flow "fix-red" in the project
    Then the flow run is done
    And the run's own job records "flow.run.started" and then "flow.run.finished"

  Scenario: Every movement of the run is recorded, in order
    When the operator starts the flow "fix-red" in the project
    Then the run's transitions read back as "fix", "push", "done"

  Scenario: A failed task takes the other edge
    Given the next task will fail
    When the operator starts the flow "fix-red" in the project
    Then the flow run is done
    And the run's steps were asked 1 task

  # A wait is a row rather than a timer somebody is holding, which is the whole reason it survives
  # the system being restarted underneath it.
  Scenario: A run waits, and carries on when its time comes
    Given the system holds this flow graph:
      """
      name: patient
      version: 1
      mode: edits
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
    And the run's steps were asked 1 task
    When ten minutes pass and the system looks for waits that are due
    Then the flow run is done
    And the run's steps were asked "start the build" and then "is it done"

  Scenario: A wait that is not yet due is left alone
    Given the system holds this flow graph:
      """
      name: patient
      version: 1
      mode: edits
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
    And the system looks for waits that are due
    Then the flow run is waiting
    And the run's steps were asked 1 task

  # Editing a graph must not change an automation that is halfway through it, which is the whole
  # reason a run pins a version. A wait is where that gets tested, because it is the only moment a
  # run sits still long enough for somebody to edit the file underneath it.
  Scenario: A graph edited while a run waits does not change that run
    Given the system holds this flow graph:
      """
      name: patient
      version: 1
      mode: edits
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
    Given the system holds this flow graph:
      """
      name: patient
      version: 2
      mode: edits
      nodes:
        ask:   { type: dispatch, prompt: "start the build" }
        pause: { type: wait, for: 10m }
        check: { type: dispatch, prompt: "the second version asks something else" }
      edges:
        - [ask, pause]
        - [pause, check]
        - [check, done]
      """
    When ten minutes pass and the system looks for waits that are due
    Then the flow run is done
    And the run's steps were asked "start the build" and then "is it done"

  # The whole difference between an automation and a shell script: a person decides whether it goes
  # further. Delivered through the command line, so it needs no chat channel and no bot token.
  Scenario: A run asks the operator, and carries on with what it is told
    Given the system holds this flow graph:
      """
      name: careful
      version: 1
      mode: edits
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
    And the run's steps were asked 1 task
    When the operator answers the run with "yes"
    Then the flow run is done
    And the run's steps were asked "fix the build" and then "push it"

  Scenario: A run told no does not do the thing it asked about
    Given the system holds this flow graph:
      """
      name: careful
      version: 1
      mode: edits
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
    And the run's steps were asked 1 task

  # A question nobody answered must never answer itself, or an automation asking permission would
  # take silence for a yes.
  Scenario: No timer answers a question
    Given the system holds this flow graph:
      """
      name: careful
      version: 1
      mode: edits
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
    And ten minutes pass and the system looks for waits that are due
    Then the flow run is asking "push?"
    And the run's steps were asked 1 task

  # The system acting on its own, which is the point of the whole thing: without a trigger, an
  # automation is a script somebody still has to remember to run.
  Scenario: A graph runs on its own when its schedule comes due
    Given the system holds this flow graph:
      """
      name: nightly
      version: 1
      mode: edits
      on:
        every: 24h
      nodes:
        sweep: { type: dispatch, prompt: "check the overnight builds" }
      edges:
        - [sweep, done]
      """
    When the operator schedules "nightly" in the project
    Then no run of "nightly" has started
    When a day passes and the system looks for waits that are due
    Then a run of "nightly" has started and finished

  # Scheduling and starting must not be the same act, or an operator cannot arrange an automation
  # for tonight without also running it now.
  Scenario: Scheduling a graph does not start it
    Given the system holds this flow graph:
      """
      name: nightly
      version: 1
      mode: edits
      on:
        every: 24h
      nodes:
        sweep: { type: dispatch, prompt: "check the overnight builds" }
      edges:
        - [sweep, done]
      """
    When the operator schedules "nightly" in the project
    And the system looks for waits that are due
    Then no run of "nightly" has started

  Scenario: A graph that says nothing about when it runs cannot be scheduled
    Given the system holds this flow graph:
      """
      name: manual
      version: 1
      mode: edits
      nodes:
        go: { type: dispatch, prompt: "go" }
      edges:
        - [go, done]
      """
    When the operator schedules "manual" in the project
    Then the control plane refuses it as the wrong state

  Scenario: An unscheduled graph stops running on its own
    Given the system holds this flow graph:
      """
      name: nightly
      version: 1
      mode: edits
      on:
        every: 24h
      nodes:
        sweep: { type: dispatch, prompt: "check the overnight builds" }
      edges:
        - [sweep, done]
      """
    When the operator schedules "nightly" in the project
    And the operator unschedules "nightly" in the project
    And a day passes and the system looks for waits that are due
    Then no run of "nightly" has started

  Scenario: A run is pinned to the version it started with
    Given the system holds this flow graph:
      """
      name: fix-red
      version: 2
      mode: edits
      nodes:
        only: { type: dispatch, prompt: "the second version" }
      edges:
        - [only, done]
      """
    When the operator starts the flow "fix-red" in the project
    Then the flow run is pinned to version 2

  # The first flow ever run against a real system finished at done, reported four transitions and read
  # back as a success. None of the job happened: the repository was not there, every task said so,
  # and the run took the success edge anyway, because `result.failed` says the model did not error
  # and nothing else. A task that could not do the work is not a failed task.
  #
  # So a dispatch node says what will show it worked, and the system checks that rather than reading
  # the model's account of itself.
  Scenario: A run stops when the job a node said it would do did not happen
    Given the system holds this flow graph:
      """
      name: site-check
      version: 1
      mode: edits
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
    And the run's steps were asked 1 task

  # The other direction. A check that stops every run would satisfy the scenario above and be worth
  # nothing, so this one is the same graph against a model that does the work.
  Scenario: A run carries on when the job did happen
    Given the model writes "package.json" while it works
    And the system holds this flow graph:
      """
      name: site-check
      version: 1
      mode: edits
      nodes:
        read: { type: dispatch, prompt: "read package.json and say what runs the tests", expect: { file: package.json } }
        tell: { type: dispatch, prompt: "summarise the project" }
      edges:
        - [read, tell]
        - [tell, done]
      """
    When the operator starts the flow "site-check" in the project
    Then the flow run is done
    And the run's steps were asked 2 tasks

  # The weaker check, for a job that leaves no file behind. It is still the model's own prose, and it
  # is here because a graph that runs a command and reads its answer has nothing else to point at.
  Scenario: A run stops when the reply does not carry what the node said it would
    Given the system holds this flow graph:
      """
      name: test-run
      version: 1
      mode: edits
      nodes:
        run: { type: dispatch, prompt: "run the tests and say how they went", expect: { contains: "all green" } }
      edges:
        - [run, done]
      """
    When the operator starts the flow "test-run" in the project
    Then the flow run is stopped
    And reading the run back says it stopped over "all green"

  # The other half of quay-crew#461. A stopped run recorded the finding under `result.expected` and
  # nothing anywhere else, so `quay flow show` printed one sentence twice and never said what the
  # graph had asked for. `result.failed` read false on the same screen as the line saying the run
  # stopped, which is two fields contradicting each other in front of the reader.
  Scenario: A run that stopped says what the graph wanted apart from what the system found
    Given the system holds this flow graph:
      """
      name: site-check
      version: 1
      mode: edits
      nodes:
        read: { type: dispatch, prompt: "read package.json and say what runs the tests", expect: { file: package.json } }
      edges:
        - [read, done]
      """
    When the operator starts the flow "site-check" in the project
    Then the flow run is stopped
    And reading the run back says it wanted "package.json" and found "is not in the session"

  # A step's session is made when its job starts, so there is nothing to set a mode on beforehand and
  # `quay mode` has nothing to point at. Every automation therefore ran in the mode a session is born
  # in, and a graph whose first step is "clone this" could not take it: cloning needs the network, and
  # a dispatched task has nobody to ask for permission.
  Scenario: A graph says what its runs may do, and the turns run in it
    Given the system holds this flow graph:
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

  # quay-crew#461. Saying nothing used to be allowed, and a run of such a graph took the mode a
  # session is born in, which approves file edits inside the working directory and nothing else. So
  # every command a step ran stopped to ask a person, and a flow is the one thing with no person
  # watching it. One run sat on its first node through 532,978 tokens before anybody saw why.
  #
  # The refusal is at import, where the author is standing, rather than a default wide enough to work
  # unwatched: that default would hand every graph already written more than its author asked for,
  # and it would do it quietly.
  Scenario: A graph that says nothing about its mode is refused at import
    Given the operator imports this flow graph, which is refused:
      """
      name: pr-sweep
      version: 1
      nodes:
        read: { type: dispatch, prompt: "read the open pull requests with gh" }
      edges:
        - [read, done]
      """
    Then the refusal names the line the graph is missing
    And the refusal names the modes there are

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
      mode: edits
      nodes:
        a: { type: dispatch, prompt: "a" }
      edges:
        - [a, nowhere]
      """
    Then the refusal names the node nobody declared

  Scenario: A flow nobody imported cannot start
    When the operator starts the flow "never-imported" in the project
    Then starting it is refused as not found

  # A job cannot wait, so a brief that asks one to wait for the checks, or to merge on the result, is
  # refused where a caller declares it. A step of a flow is a different thing: the graph around it
  # holds the wait, so the node after the wait merges the pull request and means it. Refusing that
  # would refuse the very graph the refusal tells a caller to write.
  Scenario: A step of a flow merges the pull request its wait was for
    Given the system holds this flow graph:
      """
      name: ship
      version: 1
      mode: edits
      nodes:
        push:  { type: dispatch, prompt: "push the branch and open the pull request" }
        hold:  { type: wait, for: 10m }
        read:  { type: dispatch, prompt: "read the checks and answer green or red" }
        green: { type: choice, on: { result.reply: "green" } }
        land:  { type: dispatch, prompt: "merge the pull request, the checks are green" }
      edges:
        - [push, hold]
        - [hold, read]
        - [read, green]
        - [green, land, "true"]
        - [green, done, "false"]
        - [land, done]
      """
    And the model will answer "opened it"
    And then the model will answer "green"
    When the operator starts the flow "ship" in the project
    Then the flow run is waiting
    When ten minutes pass and the system looks for waits that are due
    Then the flow run is done
    And the run's steps were asked 3 tasks
    And one of the run's steps was asked "merge the pull request, the checks are green"
