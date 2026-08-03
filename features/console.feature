Feature: The operator sees the crew from the console

  The console is the full screen view of every resource the crew has. Its job is to show what is
  really there, so these scenarios drive it against the real control plane rather than a double of
  one, and assert on the rows it produces.

  How the console draws those rows, filters them and moves a cursor over them is a table test in
  internal/console, where it belongs. What cannot be said there is this: that the rows are the
  control plane's actual sessions and workspaces.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house bills"

  Scenario: The console lists the sessions the control plane has
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new thread
    And the operator opens the console
    Then the console lists 2 sessions

  Scenario: The console lists a workspace it can drill into
    When the operator opens the console on workspaces
    Then the console lists 1 workspace
    And the console can drill from workspaces into projects

  Scenario: Drilling into a workspace shows only that workspace's projects
    Given a second workspace named "other"
    When the operator opens the console
    And the operator drills into workspace "acme"
    Then the console lists 1 project

  Scenario: Drilling into a project shows only that project's sessions
    Given a second project named "gardening"
    When the operator dispatches "hello" to the project
    And the operator dispatches "hello" to the second project
    And the operator opens the console
    And the operator drills into project "house bills"
    Then the console lists 1 session

  Scenario: An empty crew lists nothing rather than failing
    When the operator opens the console
    Then the console lists 0 sessions

  # An identifier is what actions use, a name is what the operator reads. These say the console shows
  # the second without losing the first.
  Scenario: The console names a session's workspace rather than showing its identifier
    When the operator dispatches "hello" to the project
    And the operator opens the console
    Then the console shows the session's workspace as "acme"

  Scenario: The console shortens identifiers so a row can be read
    When the operator dispatches "hello" to the project
    And the operator opens the console
    Then the console shows the session identifier shortened

  Scenario: Acting on a row still uses the whole identifier
    Given a session started by dispatching "hello"
    When the operator opens the console
    And the operator stops the selected session from the console
    Then the session is reported as stopped
