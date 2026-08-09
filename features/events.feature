Feature: Every turn is exported to the event log, when there is one

  The store holds the truth: a turn is written to it in the same breath as the turn itself, and
  history is served from it. The log is an audit export beside that, in order, keyed by session, for
  whatever second consumer eventually wants it: a dashboard, a data pipeline, another machine.

  A turn is exported whether it worked or not, because a turn that failed is exactly the one
  somebody comes looking for.

  Exporting never fails a turn, and a crew with no broker configured loses nothing but the export.
  The turn already ran and the store already holds it; an unreachable broker is not a reason to tell
  the operator their work did not happen.

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

  # What an operator pastes into a conversation can be a credential, and the log keeps what is
  # published, so the payload goes through the same redaction a failure message does before it is
  # written anywhere. Every value the workspace keeps sealed is matched exactly; the subscription
  # token's published shape is caught even when the crew never held the value. A value the crew
  # could not know about is not protected, and the documents say so.
  Scenario: A secret pasted into a conversation does not reach the log
    Given the workspace has the secret "GITHUB_TOKEN" set to "ghp-a-credential-somebody-pasted"
    When the operator dispatches "clone with ghp-a-credential-somebody-pasted please" to the project
    Then 1 turn is on the log for "acme"
    And nothing on the published turn says "ghp-a-credential-somebody-pasted"
    And the published turn names "GITHUB_TOKEN" as redacted

  Scenario: A token shaped value is caught even when the crew never held it
    When the operator dispatches "what is sk-ant-abcdefghijklmnop" to the project
    Then 1 turn is on the log for "acme"
    And nothing on the published turn says "sk-ant-abcdefghijklmnop"

  Scenario: The history reads back redacted too
    Given the workspace has the secret "GITHUB_TOKEN" set to "ghp-a-credential-somebody-pasted"
    When the operator dispatches "clone with ghp-a-credential-somebody-pasted please" to the project
    Then the session's history carries no "ghp-a-credential-somebody-pasted"

  Scenario: A stack with no broker runs turns and says there is no export
    Given the crew has no event log configured
    When the operator dispatches "hello" to the project
    Then the reply is "you said: hello"
    And the crew reports that nothing is connected to the event log
