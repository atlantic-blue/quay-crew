Feature: The operator sees the system from the console

  The console is the full screen view of every resource the system has. Its job is to show what is
  really there, so these scenarios drive it against the real control plane rather than a double of
  one, and assert on the rows it produces.

  How the console draws those rows, filters them and moves a cursor over them is a table test in
  internal/console, where it belongs. What cannot be said there is this: that the rows are the
  control plane's actual sessions and workspaces.

  The console, the API and the database all say session. It was called threads for a while, and that
  word now opens nothing, because one name across the whole system beats a console that translates.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: The flat listing of every session is still one word away
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new session
    And the operator opens the console on sessions
    Then the console lists 2 sessions

  # Choosing between workspaces without opening any of them. The counts are what the top level is for:
  # a name on its own says nothing about whether anything is happening under it.
  Scenario: A workspace row says how much work is under it
    Given a job titled "read the electricity bill"
    When the operator is at the console
    Then the console screen says "PROJECTS"
    And the console screen says "RUNNING"
    And the console screen says "ASKING"

  # A project says where its work lands. A job declared in a project that names no repository has
  # nowhere to open a pull request, so this is the fact that decides whether the work can finish.
  Scenario: A project row says the repository its work lands in
    Given the project's work lands in "atlantic-blue/quay-krewe"
    When the operator is at the console
    And the operator presses "enter" in the console
    Then the console screen says "atlantic-blue/quay-krewe"

  # A job nobody answers is a job that never finishes. It is marked in the row and counted above the
  # columns, because a listing longer than the screen hides every mark below the fold.
  Scenario: A job waiting for a person is marked and counted
    Given a job titled "choose where the transcripts are stored" whose session is still working
    And the session running that job asked its question
    When the operator is at the console on the "jobs" view
    Then the console screen says "1 job is waiting for a person"
    And the console screen says "asking"

  Scenario: The console lists a workspace it can drill into
    When the operator opens the console on workspaces
    Then the console lists 1 workspace
    And the console can drill from workspaces into projects

  Scenario: Drilling into a workspace shows only that workspace's projects
    Given a second workspace named "other"
    When the operator opens the console
    And the operator drills into workspace "acme"
    Then the console lists 1 project

  # The whole tree, driven one key at a time against the real control plane. The console opens on the
  # workspaces, and each enter goes one level down: projects, then jobs, then the work running under
  # the job.
  Scenario: The console opens at the top and each key goes one level down
    Given a job titled "read the electricity bill"
    When the operator is at the console
    Then the console is on the "workspaces" view
    When the operator presses "enter" in the console
    Then the console is on the "projects" view
    When the operator presses "enter" in the console
    Then the console is on the "jobs" view
    And the console is showing 1 row

  # And back up. Escape from every level, including the one the console opens on, which has nowhere
  # to go and must not take the console with it.
  Scenario: Escape comes back up one level at a time
    Given a job titled "read the electricity bill"
    When the operator is at the console
    And the operator presses "enter" in the console
    And the operator presses "enter" in the console
    Then the console is on the "jobs" view
    When the operator presses "esc" in the console
    Then the console is on the "projects" view
    When the operator presses "esc" in the console
    Then the console is on the "workspaces" view
    When the operator presses "esc" in the console
    Then the console is on the "workspaces" view

  # A console that does not say where it is makes a person guess. The address is the one they could
  # type back: the workspace, then the project, then the job.
  Scenario: The console says where it is, as an address a person could type
    Given a job titled "read the electricity bill"
    When the operator is at the console
    And the operator presses "enter" in the console
    Then the console screen says "projects(acme)"
    When the operator presses "enter" in the console
    Then the console screen says "jobs(acme/house-bills)"

  # The command bar and the filter bar draw over the footer, and those are the two moments somebody is
  # typing at the console and most needs to know what they are typing at.
  Scenario: The position stays on screen while the command bar is open
    When the operator is at the console
    And the operator presses "enter" in the console
    And the operator presses ":" in the console
    Then the console screen says "projects(acme)"

  # Enter on a project used to open its sessions. It opens the jobs now, so the sessions of one
  # project keep a key of their own rather than becoming a trip through the command bar.
  Scenario: A project still reaches its own sessions in one key
    Given a session started by dispatching "hello"
    When the operator is at the console
    And the operator presses "enter" in the console
    Then the console is on the "projects" view
    When the operator presses "s" in the console
    Then the console is on the "sessions" view

  # The short forms are what an operator's fingers reach for, so each one lands on the same view.
  Scenario: A short word for the sessions view opens it
    When the operator opens the console by typing "s"
    Then the console is showing sessions

  # The system dropped these words, so the console must not quietly teach one back. This is the way off
  # them, the way a named refusal is the command line's.
  Scenario: A word the system dropped opens nothing
    Then typing "threads" in the console opens nothing
    And typing "turns" in the console opens nothing

  Scenario: An empty system lists nothing rather than failing
    When the operator opens the console on sessions
    Then the console lists 0 sessions

  # An identifier is what actions use, a name is what the operator reads. These say the console shows
  # the second without losing the first.
  Scenario: The console names a session's workspace rather than showing its identifier
    When the operator dispatches "hello" to the project
    And the operator opens the console on sessions
    Then the console shows the session's workspace as "acme"

  Scenario: The console shortens identifiers so a row can be read
    When the operator dispatches "hello" to the project
    And the operator opens the console on sessions
    Then the console shows the session identifier shortened

  # Enter is the obvious key on a conversation, and on this view it used to do nothing at all, because
  # a session has nothing to drill into.
  Scenario: Enter on a session opens its conversation
    Given a session started by dispatching "hello"
    When the operator opens the console on sessions
    And the operator presses enter on the selected session
    Then the console opens that session's conversation

  # A first task that failed leaves a session holding no conversation. Enter said so and stopped,
  # which left a row in the listing nobody could open. The system names a conversation for it instead.
  Scenario: Enter on a session whose first task failed opens a conversation the system names
    Given a session whose first task failed
    When the operator opens the console on sessions
    And the operator presses enter on the selected session
    Then the console opens a conversation the system can name

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
  # away from and turns up in the archived one, with its conversation intact.
  Scenario: An archived session leaves the sessions view for the archived one
    Given a session started by dispatching "remember this"
    When the operator opens the console and archives the session
    Then the console lists 0 sessions
    And the archived view lists 1 session
    And the archived session still holds its conversation

  # A flow run puts its own thread away when it ends, so the history of an automation is always read
  # from the archived view. Reaching it must not need the thread restored first: nothing about
  # reading what happened needs a container.
  Scenario: An archived session's history is reachable without restoring it
    Given a session started by dispatching "read the package file"
    When the operator opens the console and archives the session
    And the operator asks for the archived session's history
    Then the console is showing tasks
    And the history lists 1 task saying "read the package file"

  Scenario: Acting on a row still uses the whole identifier
    Given a session started by dispatching "hello"
    When the operator opens the console on sessions
    And the operator stops the selected session from the console
    Then the session is reported as stopped

  # The history is a second key rather than a replacement: enter on a session opens the conversation,
  # which is the thing an operator does most, so it keeps the cheapest key.
  Scenario: The console shows a session's history
    Given a session started by dispatching "hello"
    When the operator opens the console on sessions
    And the operator asks for the selected session's history
    Then the console is showing tasks
    And the history lists 1 task saying "hello"

  Scenario: Asking for a history does not open the conversation
    Given a session started by dispatching "hello"
    When the operator opens the console on sessions
    Then enter on a session still opens its conversation rather than its history

  # The wizard makes one thing. It shipped able to make only a whole new system, because the workspace
  # and the project questions were both required on the way to anything else, so adding a project to a
  # workspace that already existed meant dropping to the command line and knowing what to type.
  #
  # These drive the console's own reducer against the real control plane, so "and nothing else" is a
  # fact about the store rather than about a double.

  Scenario: The wizard makes a workspace on its own
    When the operator answers the wizard with:
      | workspace |
      | other     |
    Then the system has 2 workspaces
    And the system has 1 project

  Scenario: The wizard adds a project to a workspace that already exists
    When the operator answers the wizard with:
      | project   |
      | acme      |
      | gardening |
    Then the system has 2 projects
    And the system has 1 workspace

  Scenario: The wizard sets the subscription token on a workspace that already exists
    When the operator answers the wizard with:
      | secret           |
      | acme             |
      | sk-ant-oat-typed |
    Then the secrets backend holds "sk-ant-oat-typed" for that workspace
    And the system has 1 workspace

  Scenario: The wizard writes the context of a project that already exists
    When the operator answers the wizard with:
      | context                  |
      | acme                     |
      | house-bills              |
      | pay the water bill first |
    And the operator asks where context lives
    Then the project's context reads "pay the water bill first"
    And the system has 1 project

  Scenario: The wizard starts a session in a project that already exists
    When the operator answers the wizard with:
      | session     |
      | acme        |
      | house-bills |
      | dangerous   |
      | hello       |
    Then the system has 1 session
    And the system has 1 workspace
    And the system has 1 project

  # A task takes as long as the job takes, which is minutes, and the console has a screen to draw.
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
    And the system has 1 session
    And a task is under way
    And the system's one session is reported as running
    When the model finishes the task
    Then the system's one session is reported as idle
    And the session carries what the model said

  # A task runs inside the system's own process, so nothing of it survives that process going down. A
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
    Then the system's one session is reported as failed

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
    And the system has 1 workspace

  # The way off the old wizard, not only the way onto the new one. It used to open by asking for a new
  # workspace name, so the first thing anybody typed was a name.
  Scenario: A name typed at the first question is refused rather than making a workspace
    When the operator answers the wizard with:
      | acme-two |
    Then the wizard says there is nothing called "acme-two" to make
    And the system has 1 workspace

  # The wizard made everything it was asked for and then stayed drawn over the list it had already
  # refreshed, so nothing looked like it had happened, and the next enter was taken as an answer to a
  # question nobody was asked.
  #
  # What a key does while the system is still making it is a table test in internal/console instead: out
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
    And the console lists what the wizard made

  # The header is the wordmark, which build this is, and how to reach everything else. It carried the
  # system's description and this view's keys until there was no room left for the wordmark, which is
  # what the operator noticed: "the krewe logo dissapears because there is too much text, lets leave
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

  # The mark spells the command a person types, which is krewe. It spelled the product's first word,
  # beside a command that spelled the other half of the name.
  #
  # Five letters cost seven columns more than four: 43 rather than 36, both measured against this
  # tree. What that costs is the narrowest console it is drawn in, and that depends on what else is on
  # the header line: 77 columns beside the build alone, and 80 beside a status block that also names
  # the address and the workspace. Under it the header keeps the build and the way to help, and the
  # mark gives way rather than being drawn over the top of them. A conversation beside the console
  # leaves 84, which is the case above and is the one that matters.
  Scenario Outline: The wordmark is drawn where there is room, and gives way where there is not
    When the operator looks at the console <columns> columns wide
    Then the wordmark <is drawn or not>
    And the header says which build this is

    Examples:
      | columns | is drawn or not |
      | 120     | is on screen    |
      | 84      | is on screen    |
      | 77      | is on screen    |
      | 76      | is not drawn    |
      | 60      | is not drawn    |

  Scenario: The help panel carries everything the header dropped
    When the operator looks at the console and asks for help
    Then the help panel names the system it is pointed at
    And the help panel names what the system is running
    And the help panel says what the keys on this view do
    And the help panel never asks a question it has already answered

  # A sandbox is born with its capabilities and never drifts, so the mode is decided when the session
  # starts rather than changed afterwards, which costs a restart. A session born unable to act is a
  # session that apologises: on this system one was asked to clone a repository and answered that it
  # needed approval from somebody who was not there.
  Scenario: The wizard asks what a session may do, and the session is born in it
    When the operator answers the wizard with:
      | session     |
      | acme        |
      | house-bills |
      | plan        |
      | hello       |
    Then the system has 1 session
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
    Then the system has 1 session
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
    And the operator looks at the sessions listing
    Then a session's row carries more than one colour
    And the row says how the session is doing in its status cell

  # The console was built when a session was the unit of work. A job is what an operator declares now,
  # and five of them were running on this repository the day the console could show none: the full
  # screen interface an operator leaves open was drawing the layer underneath the work rather than the
  # work. See issue 455.
  #
  # These drive the console against the real control plane, so the rows are the system's actual jobs.

  Scenario: The console lists the jobs the system holds
    Given a job titled "read the electricity bill"
    And a job titled "read the water bill" after the first
    When the operator opens the console on jobs
    Then the console lists 2 jobs

  Scenario: A short word for the jobs view opens it
    When the operator opens the console by typing "j"
    Then the console is showing jobs

  # A pending job has no session, which is the normal state rather than a fault, so the cell says
  # which it is. An empty cell reads as something missing.
  Scenario: A job that has not reached a session says so rather than leaving the cell empty
    Given a job titled "read the electricity bill"
    When the operator opens the console on jobs
    Then the job's row says it has no session yet

  Scenario: A job a controller started names the session doing it
    Given a job titled "read the electricity bill"
    When the controller ticks
    And the task the controller sent lands
    And the operator opens the console on jobs
    Then the job's row names the session doing it

  # Enter goes to what the job did rather than to the row it runs in: a job's session is one row, and
  # a listing of one row says nothing the line above it did not. The tasks are the whole account of
  # what was asked and what came back.
  Scenario: Enter on a job opens what it did
    Given a job titled "read the electricity bill"
    When the controller ticks
    And the task the controller sent lands
    And the operator opens the console on jobs
    And the operator presses enter on the selected job
    Then the console shows what the job's session was asked

  # A job with nothing behind it yet is the case enter has to refuse, and the refusal names the phase,
  # so it says why rather than only that it will not.
  Scenario: Enter on a job with no session yet says why
    Given a job titled "read the electricity bill"
    When the operator opens the console on jobs
    And the operator presses enter on the selected job
    Then the console says "pending"
    And the console is still showing the job

  # Stopping is destructive and there is no way back from the wrong row, so it asks first, the way
  # every destructive key in this console already does.
  Scenario: Backspace asks before it stops a job, and stops nothing until yes
    Given a job titled "read the electricity bill"
    When the operator opens the console on jobs and presses backspace on the job
    Then the console asks whether to stop that job
    And the job is pending

  Scenario: Answering yes stops the job
    Given a job titled "read the electricity bill"
    When the operator opens the console on jobs and presses backspace on the job
    And the operator answers "y"
    Then the job is stopped, and the reason says a person did it
