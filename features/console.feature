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

  # The history is a second key rather than a replacement: enter on a session opens the conversation,
  # which is the thing an operator does most, so it keeps the cheapest key.
  Scenario: The console shows a session's history
    Given a session started by dispatching "hello"
    And the projection has caught up
    When the operator opens the console
    And the operator asks for the selected session's history
    Then the console is showing turns
    And the history lists 1 turn saying "hello"

  Scenario: Asking for a history does not open the conversation
    Given a session started by dispatching "hello"
    When the operator opens the console
    Then enter on a session still opens its conversation rather than its history

  # The wizard makes one thing. It shipped able to make only a whole new crew, because the workspace
  # and the project questions were both required on the way to anything else, so adding a project to a
  # workspace that already existed meant dropping to the command line and knowing what to type.
  #
  # These drive the console's own reducer against the real control plane, so "and nothing else" is a
  # fact about the store rather than about a double.

  Scenario: The wizard makes a workspace on its own
    When the operator answers the wizard with:
      | workspace |
      | other     |
    Then the crew has 2 workspaces
    And the crew has 1 project

  Scenario: The wizard adds a project to a workspace that already exists
    When the operator answers the wizard with:
      | project   |
      | acme      |
      | gardening |
    Then the crew has 2 projects
    And the crew has 1 workspace

  Scenario: The wizard sets the subscription token on a workspace that already exists
    When the operator answers the wizard with:
      | secret           |
      | acme             |
      | sk-ant-oat-typed |
    Then the secrets backend holds "sk-ant-oat-typed" for that workspace
    And the crew has 1 workspace

  Scenario: The wizard writes the context of a project that already exists
    When the operator answers the wizard with:
      | context                  |
      | acme                     |
      | house-bills              |
      | pay the water bill first |
    And the operator asks where context lives
    Then the project's context reads "pay the water bill first"
    And the crew has 1 project

  Scenario: The wizard starts a session in a project that already exists
    When the operator answers the wizard with:
      | session     |
      | acme        |
      | house-bills |
      | hello       |
    Then the crew has 1 session
    And the crew has 1 workspace
    And the crew has 1 project

  # Escape at any point makes nothing at all. The last row here is typed and never accepted, so escape
  # lands on a half answered question rather than on a finished one.
  #
  # That the wizard also forgets the half typed token is asserted in internal/console, against the
  # model the moment it closes. From out here the wizard is no longer drawn, so a console still holding
  # the token would look exactly like one that had dropped it.
  Scenario: Escaping the wizard makes nothing
    When the operator abandons the wizard after typing:
      | secret           |
      | acme             |
      | sk-ant-oat-typed |
    Then the secrets backend holds nothing for that workspace
    And the crew has 1 workspace

  # The way off the old wizard, not only the way onto the new one. It used to open by asking for a new
  # workspace name, so the first thing anybody typed was a name.
  Scenario: A name typed at the first question is refused rather than making a workspace
    When the operator answers the wizard with:
      | acme-two |
    Then the wizard says there is nothing called "acme-two" to make
    And the crew has 1 workspace
