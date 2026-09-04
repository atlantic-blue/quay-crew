Feature: A session says what happened to it

  The system emitted one event, a finished exec, so nothing could tell that a session was made, that
  work had begun, or that a session had been put away. Nothing could react to a change, and no view
  could say what the system is doing right now.

  Every session emits its lifecycle. A kind names something that happened, in the past tense, at one
  moment, and it is the field a consumer switches on. "idle" and "running" are not kinds: they are
  what the session's row says now, which is the fold of these.

  Emitting never fails the thing it describes, because the thing already happened.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A session that is dispatched to says it was made, started and completed
    When the operator dispatches "hello" to the project
    Then the session's events read "session.created", "session.started", "session.completed"
    And the completed event carries what the model replied

  Scenario: A second exec on the same session says started and completed again, and is not created twice
    When the operator dispatches "hello" to the project
    And the operator dispatches "and again" to the same session
    Then the session's events read "session.created", "session.started", "session.completed", "session.started", "session.completed"

  Scenario: Stopping, archiving and restoring are events
    When the operator dispatches "hello" to the project
    And the operator stops the session
    And the operator archives the session
    And the operator restores the session
    Then the session's events end with "session.stopped", "session.archived", "session.restored"

  # A state and an event are different things, and a consumer handed a state learns nothing about
  # what changed. This is the guard on that decision.
  Scenario: No event is a state
    When the operator dispatches "hello" to the project
    Then no event's kind is "session.idle" or "session.running"

  # An event carries what the model said and what a failure said, and either can hold something the
  # operator pasted, so it goes through the same redactor an exec does.
  Scenario: A secret pasted into an exec does not reach the events
    Given the workspace has the secret "GITHUB_TOKEN" set to "ghp-a-credential-somebody-pasted"
    When the operator dispatches "clone with ghp-a-credential-somebody-pasted please" to the project
    Then nothing in the session's events says "ghp-a-credential-somebody-pasted"
