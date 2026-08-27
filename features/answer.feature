Feature: An answer comes back out as data

  A caller starts a task, lets go of it, and comes back for the answer later. Asking waits for the
  answer and prints it, and a dispatch lets go, so the answer to a dispatched task had no way out.
  The history listing is written for a person: it shortens a reply at 120 characters and it puts a
  clock and a speaker beside it. A caller that reads that gets a listing where the value belongs.

  So this command prints the reply and nothing else. A refusal goes to standard error, because a
  caller must never receive a sentence where the data belongs.

  The scenarios below run the command line tool as a caller runs it: its own process, its own
  standard output, its own exit status.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"
    And the crew listens on an address the tool can dial

  Scenario: The reply is the whole of standard output
    Given a session that was asked "when is the electricity bill due"
    When the caller asks for the answer of that session
    Then standard output is the reply and one newline
    And standard error says nothing
    And the command succeeds

  # The listing shortens a reply at 120 characters, which is right for a person reading a screen and
  # wrong for a caller that pipes the value into the next command.
  Scenario: A long reply comes back whole
    Given a session that was asked for 400 characters
    When the caller asks for the answer of that session
    Then standard output carries all 400 characters

  Scenario: Either identifier of a session names it
    Given a session that was asked "when is the electricity bill due"
    When the caller asks for the answer by the handle of that session
    Then standard output is the reply and one newline

  Scenario: A session with no landed task is refused, and prints nothing
    Given a session that was opened and never asked anything
    When the caller asks for the answer of that session
    Then standard output is empty
    And standard error says there is no landed task
    And the command fails

  # A task that has not landed has no answer, and the answer of the task before it is not the answer
  # to the question the caller is asking.
  Scenario: A task still running is not an answer
    Given the model takes longer over a task than anybody will wait
    And a task dispatched without waiting for it
    And a task is under way
    When the caller asks for the answer of that session
    Then standard output is empty
    And standard error says the task is still running
    And the command fails

  Scenario: What a task failed with is its answer
    Given a session whose task failed
    When the caller asks for the answer of that session
    Then standard output carries what went wrong
    And the command fails

  Scenario: Every answer, oldest first
    Given a session that was asked "first" and then "second"
    When the caller asks for every answer of that session
    Then standard output is both replies, oldest first
    And the command succeeds

  Scenario: Every answer of a session with no landed task is the same refusal
    Given a session that was opened and never asked anything
    When the caller asks for every answer of that session
    Then standard output is empty
    And standard error says there is no landed task
    And the command fails
