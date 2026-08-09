Feature: A session's history can be read back

  Every turn is written to the store in the same breath as the turn itself, so what a crew was asked
  and what it answered survives the container the conversation ran in, whether or not any broker is
  running. The event log is an export, not the road history travels by.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A conversation reads back in the order it happened
    When the operator dispatches "hello" to the project
    And the operator dispatches "and again" to the same thread
    Then the session has 2 turns
    And the first turn says "hello" and the second says "and again"

  Scenario: A turn that failed is in the history, saying so
    Given the next turn will fail
    When the operator dispatches "hello" to the project
    Then the one turn on that session is recorded as failed

  Scenario: A session nobody has spoken to has no history
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new thread
    Then each session has 1 turn

  # The whole point of writing history synchronously: a crew with no broker at all keeps every turn.
  # It used to lose them, silently, whenever QC_KAFKA_SEEDS was unset or the broker was down.
  Scenario: A crew with no event log keeps the whole history
    Given the crew has no event log configured
    When the operator dispatches "hello" to the project
    Then the reply is "you said: hello"
    And the session has 1 turn
    And the first turn of the session says "hello" was asked and "you said: hello" came back

  Scenario: The history of a session that does not exist is refused
    When the operator asks for the history of a session that does not exist
    Then the control plane refuses it as not found
