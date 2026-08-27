Feature: A task outlives the caller that started it

  Work is started with a dispatch that lets go of it. The crew runs the task, which takes as long as
  the work takes, so nothing on the operator's machine has to stay alive for it. Holding a task in
  the client made the terminal the weakest part of the crew: a dispatch killed at seventeen minutes
  recorded "failed: model: run exited: signal: killed", said nothing about why, and the work was
  gone.

  A short question is the other case. There the caller waits, because the person typing it is looking
  at the terminal and the answer is the point.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # The task is held open rather than timed, because what is specified here is what happens while one
  # runs, and a scenario that waits a duration for that passes by accident.
  Scenario: A task keeps running after the caller that asked for it has gone
    Given the model takes longer over a task than anybody will wait
    And a task dispatched by a caller that then goes away
    And a task is under way
    When the model finishes the task
    Then the crew's one session is reported as idle
    And the session carries what the model said

  Scenario: A dispatch that lets go answers before the task lands
    Given the model takes longer over a task than anybody will wait
    And a task dispatched without waiting for it
    And a task is under way
    Then the crew's one session is reported as running

  Scenario: A caller that waits is given the answer
    When the operator dispatches "hello" to the project
    Then the reply is "you said: hello"

  # A crew once wrote the session row, waited on something that never answered, and said nothing at
  # all: no task, no container, no line in the log. Every listing kept answering in under a second,
  # so the crew read as well while it started no work for an hour. A dispatch that cannot start now
  # ends, and says which wait it gave up on.
  Scenario: A dispatch that cannot start says what it waited for
    Given a sandbox that never starts
    When the operator dispatches "read the repository" to the project
    Then the crew says it waited for "the sandbox to be created"
    And the session left behind is not sitting idle

  # The audit export is a copy of a record the store already holds, so a broker that never answers
  # costs the export and never the work. It used to cost the whole dispatch.
  Scenario: A task runs while the event log is not answering
    Given an event log that never answers
    When the operator dispatches "hello" to the project
    Then the reply is "you said: hello"
