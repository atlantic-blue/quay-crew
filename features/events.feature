Feature: Every turn is written to the event log

  The store holds what a session is now. The log holds what happened to it, in order, so a
  conversation can be read back later, a projection can be rebuilt from scratch, and an operator can
  answer what the crew did on Tuesday.

  A turn is published whether it worked or not, because a turn that failed is exactly the one
  somebody comes looking for. The record is keyed by session, so one session's events stay in the
  order they happened.

  Publishing never fails a turn. The turn already ran by the time the record is written, and a broker
  that is unreachable is not a reason to tell the operator their work did not happen.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A turn that worked is published to the workspace's stream
    When the operator dispatches "hello" to the project
    Then 1 turn is on the log for "acme"
    And the published turn says "hello" was asked and "you said: hello" came back
    And the published turn is keyed by its session

  Scenario: The record says where the session sits, without anyone having to look it up
    When the operator dispatches "hello" to the project
    Then the published turn carries the workspace, the project and the thread

  Scenario: A turn that failed is published too, with what went wrong
    Given the next turn will fail
    When the operator dispatches "hello" to the project
    Then 1 turn is on the log for "acme"
    And the published turn failed and says why

  Scenario: Every turn of a conversation is on the log, in order
    When the operator dispatches "hello" to the project
    And the operator dispatches "and again" to the same thread
    Then 2 turns are on the log for "acme"
    And the turns on the log are in the order they were asked

  Scenario: Two workspaces do not share a stream
    Given a second workspace named "beta" with a project
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to the second workspace's project
    Then 1 turn is on the log for "acme"
    And 1 turn is on the log for "beta"

  Scenario: A stack with no broker runs turns and records nothing
    Given the crew has no event log configured
    When the operator dispatches "hello" to the project
    Then the reply is "you said: hello"
    And the crew reports that nothing is connected to the event log
