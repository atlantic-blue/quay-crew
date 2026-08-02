Feature: Sessions run in isolated sandboxes

  A session is a conversation with the model. It runs inside its own sandbox, a container that lives
  across the session's turns, so whatever the agent sets up in one turn is still there in the next.

  These scenarios drive the control plane over its real interface, the same one every channel and the
  dashboard talk to. They are the acceptance criteria for the sessions milestone.

  Background:
    Given a running control plane
    And a project named "acme"

  Scenario: Dispatching a turn starts a session in its own sandbox
    When the operator dispatches "hello" to the project
    Then the reply is "you said: hello"
    And 1 sandbox has been created
    And the sandbox belongs to the session

  Scenario: A second turn on the same thread reuses the session and its sandbox
    When the operator dispatches "hello" to the project
    And the operator dispatches "and again" to the same thread
    Then both turns ran in the same session
    And 1 sandbox has been created

  Scenario: A second turn continues the conversation rather than starting a new one
    When the operator dispatches "hello" to the project
    And the operator dispatches "and again" to the same thread
    Then the second turn resumed the conversation the first turn started

  Scenario: Separate threads are separate sessions with separate sandboxes
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new thread
    Then the turns ran in different sessions
    And 2 sandboxes have been created

  Scenario: The operator can see the sessions of a project
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new thread
    Then the project has 2 sessions

  Scenario: A session survives the control plane restarting
    Given a session started by dispatching "remember this"
    When the control plane restarts
    And the operator dispatches "and again" to the same thread
    Then both turns ran in the same session
    And the second turn resumed the conversation the first turn started

  Scenario: The operator can still see their sessions after a restart
    Given a session started by dispatching "hello"
    When the control plane restarts
    Then the project has 1 sessions
    And the session is reported as idle

  Scenario: Projects survive the control plane restarting
    When the control plane restarts
    Then the project is listed
    And the project can be fetched by its id

  Scenario: Stopping a session tears down its sandbox
    Given a session started by dispatching "hello"
    When the operator stops the session
    Then the session is reported as stopped
    And the session's sandbox has been closed

  Scenario: A turn for a project that does not exist is refused
    When the operator dispatches "hello" to project "ghost"
    Then the control plane refuses it as not found

  Scenario: An empty turn is refused
    When the operator dispatches "" to the project
    Then the control plane refuses it as invalid

  Scenario: A turn carries the project's subscription token into the sandbox
    Given the project has the subscription token "tok-xyz"
    When the operator dispatches "hello" to the project
    Then the turn ran with the subscription token "tok-xyz"

  Scenario: A project with no subscription token still runs a turn
    When the operator dispatches "hello" to the project
    Then the reply is "you said: hello"
    And the turn ran with no extra environment
