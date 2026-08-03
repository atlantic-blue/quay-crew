Feature: The operator sees the crew from the console

  The console is the full screen view of every resource the crew has. Its job is to show what is
  really there, so these scenarios drive it against the real control plane rather than a double of
  one, and assert on the rows it produces.

  How the console draws those rows, filters them and moves a cursor over them is a table test in
  internal/console, where it belongs. What cannot be said there is this: that the rows are the
  control plane's actual threads and workspaces.

  The console says threads where the control plane says sessions. A session is the thread running,
  inside a sandbox, which is a real distinction inside the control plane and means nothing to
  somebody reading a list of conversations.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: The console lists the threads the control plane has
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new thread
    And the operator opens the console
    Then the console lists 2 threads

  Scenario: The console lists a workspace it can drill into
    When the operator opens the console on workspaces
    Then the console lists 1 workspace
    And the console can drill from workspaces into projects

  Scenario: Drilling into a workspace shows only that workspace's projects
    Given a second workspace named "other"
    When the operator opens the console
    And the operator drills into workspace "acme"
    Then the console lists 1 project

  Scenario: Drilling into a project shows only that project's threads
    Given a second project named "gardening"
    When the operator dispatches "hello" to the project
    And the operator dispatches "hello" to the second project
    And the operator opens the console
    And the operator drills into project "house-bills"
    Then the console lists 1 thread

  # The old name is what the muscle memory types, so it keeps working rather than being punished.
  Scenario: Typing sessions still opens the threads view
    When the operator opens the console by typing "sessions"
    Then the console is showing threads

  Scenario: An empty crew lists nothing rather than failing
    When the operator opens the console
    Then the console lists 0 threads

  # An identifier is what actions use, a name is what the operator reads. These say the console shows
  # the second without losing the first.
  Scenario: The console names a thread's workspace rather than showing its identifier
    When the operator dispatches "hello" to the project
    And the operator opens the console
    Then the console shows the thread's workspace as "acme"

  Scenario: The console shortens identifiers so a row can be read
    When the operator dispatches "hello" to the project
    And the operator opens the console
    Then the console shows the thread identifier shortened

  # Enter is the obvious key on a conversation, and on this view it used to do nothing at all, because
  # a thread has nothing to drill into.
  Scenario: Enter on a thread opens its conversation
    Given a session started by dispatching "hello"
    When the operator opens the console
    And the operator presses enter on the selected thread
    Then the console opens that thread's conversation

  Scenario: Enter on a thread with no conversation says why rather than opening something that errors
    Given a thread whose first turn failed
    When the operator opens the console
    And the operator presses enter on the selected thread
    Then the console says the thread has no conversation yet

  Scenario: Acting on a row still uses the whole identifier
    Given a session started by dispatching "hello"
    When the operator opens the console
    And the operator stops the selected thread from the console
    Then the session is reported as stopped
