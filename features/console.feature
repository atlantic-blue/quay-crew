Feature: The operator sees the crew from the console

  The console is the full screen view of every resource the crew has. Its job is to show what is
  really there, so these scenarios drive it against the real control plane rather than a double of
  one, and assert on the rows it produces.

  How the console draws those rows, filters them and moves a cursor over them is a table test in
  internal/console, where it belongs. What cannot be said there is this: that the rows are the
  control plane's actual sessions and projects.

  Background:
    Given a running control plane
    And a project named "acme"

  Scenario: The console lists the sessions the control plane has
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new thread
    And the operator opens the console
    Then the console lists 2 sessions

  Scenario: The console lists a project it can drill into
    When the operator opens the console on projects
    Then the console lists 1 project
    And the console can drill from projects into sessions

  Scenario: Drilling into a project shows only that project's sessions
    Given a second project named "other"
    When the operator dispatches "hello" to the project
    And the operator dispatches "hello" to the second project
    And the operator opens the console
    And the operator drills into project "acme"
    Then the console lists 1 session

  Scenario: An empty crew lists nothing rather than failing
    When the operator opens the console
    Then the console lists 0 sessions
