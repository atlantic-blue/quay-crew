Feature: The operator sees the crew from the console

  The console is the full screen view of every resource the crew has. Its job is to show what is
  really there, so these scenarios drive it against the real control plane rather than a double of
  one, and assert on the rows it produces.

  How the console draws those rows, filters them and moves a cursor over them is a table test in
  internal/console, where it belongs. What cannot be said there is this: that the rows are the
  control plane's actual sessions and workspaces.

  The console, the API and the database all say session. It was called sessions for a day; that word is
  still accepted in the command bar and appears nowhere else, because one name across the whole system
  beats a console that translates.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: The console lists the sessions the control plane has
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new session
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

  # The short forms are what an operator's fingers reach for, so each one lands on the same view.
  Scenario: A short word for the sessions view opens it
    When the operator opens the console by typing "s"
    Then the console is showing sessions

  # The crew dropped these words, so the console must not quietly teach one back. This is the way off
  # them, the way a named refusal is the command line's.
  Scenario: A word the crew dropped opens nothing
    Then typing "threads" in the console opens nothing
    And typing "turns" in the console opens nothing

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
  # a session has nothing to drill into.
  Scenario: Enter on a session opens its conversation
    Given a session started by dispatching "hello"
    When the operator opens the console
    And the operator presses enter on the selected session
    Then the console opens that session's conversation

  Scenario: Enter on a session with no conversation says why rather than opening something that errors
    Given a session whose first task failed
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

  # Archiving from the console, driven through its own reducer: the session leaves the view it was put
  # away from and tasks up in the archived one, with its conversation intact.
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
    When the operator opens the console
    And the operator asks for the selected session's history
    Then the console is showing tasks
    And the history lists 1 task saying "hello"

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
      | dangerous   |
      | hello       |
    Then the crew has 1 session
    And the crew has 1 workspace
    And the crew has 1 project

  # A task takes as long as the work takes, which is minutes, and the console has a screen to draw.
  # The wizard waited for one anyway: it held every key while it waited, gave up at thirty seconds,
  # and left behind a session with a container, a row, and no conversation in it. The operator saw a
  # frozen "making it" and then an error, and read the freeze as the container being slow to start.
  #
  # The task is held open here rather than timed, because what is being specified is what is true
  # while a task runs, and a scenario that waits a duration for that passes by accident.
  Scenario: The wizard comes back before the task it started has finished
    Given the model takes longer over a task than anybody will wait
    When the operator answers the wizard with:
      | session     |
      | acme        |
      | house-bills |
      | dangerous   |
      | hello       |
    Then the console is asking nothing
    And the crew has 1 session
    And a task is under way
    And the crew's one session is reported as running
    When the model finishes the task
    Then the crew's one session is reported as idle
    And the session carries what the model said

  # A task runs inside the crew's own process, so nothing of it survives that process going down. A
  # row still saying running on the way up is a task that died with the last one, and left alone it
  # reads as a conversation that has been thinking since the restart.
  Scenario: A session left mid task by a restart is settled rather than left running
    Given the model takes longer over a task than anybody will wait
    When the operator answers the wizard with:
      | session     |
      | acme        |
      | house-bills |
      | dangerous   |
      | hello       |
    And a task is under way
    And the control plane restarts
    Then the crew's one session is reported as failed

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

  # The wizard made everything it was asked for and then stayed drawn over the list it had already
  # refreshed, so nothing looked like it had happened, and the next enter was taken as an answer to a
  # question nobody was asked.
  #
  # What a key does while the crew is still making it is a table test in internal/console instead: out
  # here the make completes inside the step, so there is no working window to press a key into, and a
  # scenario written for it passed against its own mutation.
  Scenario: The wizard closes when it has made what it was asked for, and the list shows it
    When the operator answers the wizard with:
      | session     |
      | acme        |
      | house-bills |
      | dangerous   |
      | hello       |
    Then the console is asking nothing
    And the console lists the session the wizard started

  # The header is the wordmark, which build this is, and how to reach everything else. It carried the
  # crew's description and this view's keys until there was no room left for the wordmark, which is
  # what the operator noticed: "the quay logo dissapears because there is too much text, lets leave
  # only: the logo + version, and help".
  #
  # These drive the console's own reducer against the real control plane, so what is asserted is what
  # the operator would be looking at.
  Scenario: The header keeps the wordmark, the build and the way to everything else
    When the operator looks at the console
    Then the header shows the wordmark
    And the header says which build this is
    And the header says how to reach everything else
    And the header does not carry what the help panel carries

  # Half the width is what a conversation opened beside the console leaves it, and the wordmark going
  # missing there is the whole reason the rest moved out.
  Scenario: The wordmark survives a conversation beside the console
    When the operator opens the console with a conversation beside it
    Then the header shows the wordmark

  Scenario: The help panel carries everything the header dropped
    When the operator looks at the console and asks for help
    Then the help panel names the crew it is pointed at
    And the help panel names what the crew is running
    And the help panel says what the keys on this view do
    And the help panel never asks a question it has already answered

  # A sandbox is born with its capabilities and never drifts, so the mode is decided when the session
  # starts rather than changed afterwards, which costs a restart. A session born unable to act is a
  # session that apologises: on this crew one was asked to clone a repository and answered that it
  # needed approval from somebody who was not there.
  Scenario: The wizard asks what a session may do, and the session is born in it
    When the operator answers the wizard with:
      | session     |
      | acme        |
      | house-bills |
      | plan        |
      | hello       |
    Then the crew has 1 session
    And that session's mode is "plan"

  # Tab is the other way to answer a question like this one: cycling to a candidate rather than
  # spelling it out. The wizard offers what a step can be answered with everywhere it asks one of a
  # fixed set of things, not only here, but the mode is where it matters most, an operator choosing
  # what a session may do without asking rather than typing "dangerous" correctly.
  Scenario: Tab fills in the mode without typing it
    When the operator answers the wizard with:
      | session     |
      | acme        |
      | house-bills |
    And the operator presses tab 3 times to choose the mode, then sends "hello"
    Then the crew has 1 session
    And that session's mode is "bypassPermissions"

  Scenario: The wizard refuses a mode that is not one of the three
    When the operator answers the wizard with:
      | session     |
      | acme        |
      | house-bills |
      | whatever    |
    Then the console says "not one of them"

  # A listing where every row is one colour has to be read one row at a time, which is what a listing
  # is for avoiding. Every row in every view carries a state, and a state was being drawn over the
  # whole line, so the workspace, the project and the mode all arrived on screen in the same green.
  #
  # The state moved onto the status cell, which is where the sessions tool keeps it. This drives the
  # real console over the real control plane, so what is asserted is the screen the operator has.
  Scenario: A session's row is coloured cell by cell rather than all in its state
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new session
    And the operator looks at the console
    Then a session's row carries more than one colour
    And the row says how the session is doing in its status cell
