Feature: An exec outlives the caller that started it

  An exec is started with a dispatch that lets go of it. The system runs the exec, which takes as long as
  the job takes, so nothing on the operator's machine has to stay alive for it. Holding an exec in
  the client made the terminal the weakest part of the system: a dispatch killed at seventeen minutes
  recorded "failed: model: run exited: signal: killed", said nothing about why, and the job was
  gone.

  A short question is the other case. There the caller waits, because the person typing it is looking
  at the terminal and the answer is the point.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # The exec is held open rather than timed, because what is specified here is what happens while one
  # runs, and a scenario that waits a duration for that passes by accident.
  Scenario: An exec keeps running after the caller that asked for it has gone
    Given the model takes longer over an exec than anybody will wait
    And an exec dispatched by a caller that then goes away
    And an exec is under way
    When the model finishes the exec
    Then the system's one session is reported as idle
    And the session carries what the model said

  Scenario: A dispatch that lets go answers before the exec lands
    Given the model takes longer over an exec than anybody will wait
    And an exec dispatched without waiting for it
    And an exec is under way
    Then the system's one session is reported as running

  Scenario: A caller that waits is given the answer
    When the operator dispatches "hello" to the project
    Then the reply is "you said: hello"

  # A system once wrote the session row, waited on something that never answered, and said nothing at
  # all: no exec, no container, no line in the log. Every listing kept answering in under a second,
  # so the system read as well while it started no work for an hour. A dispatch that cannot start now
  # ends, and says which wait it gave up on.
  Scenario: A dispatch that cannot start says what it waited for
    Given a sandbox that never starts
    When the operator dispatches "read the repository" to the project
    Then the system says it waited for "the sandbox to be created"
    And the session left behind is not sitting idle
