Feature: The operator sees the crew from the console

  The console is the full screen view of every resource the crew has. Its job is to show what is
  really there, so these scenarios drive it against the real control plane rather than a double of
  one, and assert on the rows it produces.

  How the console draws those rows, filters them and moves a cursor over them is a table test in
  internal/console, where it belongs. What cannot be said there is this: that the rows are the
  control plane's actual sessions and workspaces.

  The console, the API and the database all say session. It was called threads for a day; that word is
  still accepted in the command bar and appears nowhere else, because one name across the whole system
  beats a console that translates.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

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
    And the operator drills into project "house-bills"
    Then the console lists 1 session

  # This view has been called both things inside a day, so both keep working.
  Scenario: Typing threads still opens the sessions view
    When the operator opens the console by typing "threads"
    Then the console is showing sessions

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

  # Enter is the obvious key on a conversation, and on this view it used to do nothing at all, because
  # a thread has nothing to drill into.
  Scenario: Enter on a session opens its conversation
    Given a session started by dispatching "hello"
    When the operator opens the console
    And the operator presses enter on the selected session
    Then the console opens that session's conversation

  Scenario: Enter on a session with no conversation says why rather than opening something that errors
    Given a session whose first turn failed
    When the operator opens the console
    And the operator presses enter on the selected session
    Then the console says the session has no conversation yet

  # Every destructive key asks first. These drive the console's own reducer against the real control
  # plane, so "nothing was stopped" is a fact about the store rather than about a double.
  Scenario: Backspace asks before it stops a session, and stops nothing until yes
    Given a session started by dispatching "hello"
    When the operator opens the console and presses backspace on the session
    Then the console asks whether to stop that session
    And the session is reported as idle

  Scenario: Answering yes stops the session
    Given a session started by dispatching "hello"
    When the operator opens the console and presses backspace on the session
    And the operator answers "y"
    Then the session is reported as stopped

  Scenario: Anything that is not yes cancels, and stops nothing
    Given a session started by dispatching "hello"
    When the operator opens the console and presses backspace on the session
    And the operator answers "n"
    Then the session is reported as idle

  # Archiving from the console, driven through its own reducer: the thread leaves the view it was put
  # away from and turns up in the archived one, with its conversation intact.
  Scenario: An archived session leaves the sessions view for the archived one
    Given a session started by dispatching "remember this"
    When the operator opens the console and archives the session
    Then the console lists 0 sessions
    And the archived view lists 1 session
    And the archived session still holds its conversation

  Scenario: Acting on a row still uses the whole identifier
    Given a session started by dispatching "hello"
    When the operator opens the console
    And the operator stops the selected session from the console
    Then the session is reported as stopped
