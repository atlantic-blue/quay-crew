Feature: A session's history can be read back

  A task is written to the store when it starts, so an operator can see what a session was asked
  while it is still working on it, and what it came to is written into that same record when it
  lands. What a system was asked and what it answered survives the container the conversation ran in,
  whether or not any broker is running. The event log is an export, not the road history travels by.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A conversation reads back in the order it happened
    When the operator dispatches "hello" to the project
    And the operator dispatches "and again" to the same session
    Then the session has 2 tasks
    And the first task says "hello" and the second says "and again"

  # A task used to be written only when it ended. So for the minutes or the hours it ran there was
  # nothing to see: the listing called the session idle and its history said it had been asked
  # nothing. Three sessions worked for over half an hour each and every one read that way.
  #
  # The task is held open rather than timed, because what is specified here is what is true *while*
  # one runs, and a scenario that waits a duration for that passes by accident.
  Scenario: A task is in the history while it is still running
    Given the model takes longer over a task than anybody will wait
    And a task dispatched with the caller waiting for it
    And a task is under way
    Then the system's one session is reported as running
    And the system's one session was asked "read the repository" and is still running

  Scenario: A task nobody is waiting for is visible while it runs too
    Given the model takes longer over a task than anybody will wait
    And a task dispatched without waiting for it
    And a task is under way
    Then the system's one session was asked "read the repository" and is still running

  # One task, not two. The landing closes the record the start opened, so the operator reads the
  # prompt once with the answer under it.
  Scenario: What a task came to is written into the task that started
    Given the model takes longer over a task than anybody will wait
    And a task dispatched with the caller waiting for it
    And a task is under way
    When the model finishes the task
    And the operator's dispatch comes back
    Then the system's one session has 1 task
    And the session carries what the model said
    And the system's one session is reported as idle

  Scenario: A session nobody has spoken to has no history
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new session
    Then each session has 1 task

  Scenario: The history of a session that does not exist is refused
    When the operator asks for the history of a session that does not exist
    Then the control plane refuses it as not found
