Feature: A session's history can be read back

  Every turn goes onto the event log. A projection reads that log back into a table, and the control
  plane serves a session's history from it, so what a crew was asked and what it answered survives
  the container the conversation ran in.

  The projection is a consumer, and delivery from a log is at least once, so the same record arrives
  more than once. Writing it twice must leave one turn, or a history would grow every time the crew
  restarted.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A conversation reads back in the order it happened
    When the operator dispatches "hello" to the project
    And the operator dispatches "and again" to the same thread
    And the projection has caught up
    Then the session has 2 turns
    And the first turn says "hello" and the second says "and again"

  Scenario: A turn that failed is in the history, saying so
    Given the next turn will fail
    When the operator dispatches "hello" to the project
    And the projection has caught up
    Then the one turn on that session is recorded as failed

  Scenario: A record delivered twice leaves one turn
    When the operator dispatches "hello" to the project
    And the projection has caught up
    And every record on the log is delivered again
    Then the session has 1 turn

  Scenario: A session nobody has spoken to has no history
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new thread
    And the projection has caught up
    Then each session has 1 turn

  Scenario: The history of a session that does not exist is refused
    When the operator asks for the history of a session that does not exist
    Then the control plane refuses it as not found
