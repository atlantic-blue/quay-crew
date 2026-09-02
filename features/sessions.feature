Feature: Sessions run in isolated sandboxes

  A session is a conversation with the model. It runs inside its own sandbox, a container that lives
  across the session's tasks, so whatever the agent sets up in one task is still there in the next.

  These scenarios drive the control plane over its real interface, the same one every channel and the
  dashboard talk to. They are the acceptance criteria for the sessions milestone.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: Dispatching a task starts a session in its own sandbox
    When the operator dispatches "hello" to the project
    Then the reply is "you said: hello"
    And 1 sandbox has been created
    And the sandbox belongs to the session

  Scenario: A second task on the same session reuses the session and its sandbox
    When the operator dispatches "hello" to the project
    And the operator dispatches "and again" to the same session
    Then both tasks ran in the same session
    And 1 sandbox has been created

  Scenario: A second task continues the conversation rather than starting a new one
    When the operator dispatches "hello" to the project
    And the operator dispatches "and again" to the same session
    Then the second task resumed the conversation the first task started

  Scenario: Separate sessions are separate sessions with separate sandboxes
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new session
    Then the tasks ran in different sessions
    And 2 sandboxes have been created

  Scenario: The operator can see the sessions of a workspace
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new session
    Then the workspace has 2 sessions

  # The last column of a listing says how long ago each session moved, and the listing is ordered on
  # that same stamp, so the column reads down the page. Ordered on the created stamp instead, a
  # session made a week ago and used an hour ago sat below one made yesterday and untouched since,
  # and a listing of forty five sessions ran 1d, 1d, 1d, 7d, 7d, 7d, 1d, 7d.
  Scenario: The listing puts the session last worked in at the top
    When the operator dispatches "the older subject" to the project
    And the operator dispatches "a newer subject" to a new session
    And the operator dispatches "carry on" to the session started first
    Then the listing puts the session last worked in at the top

  # An archived session is measured from when it was put away rather than from when it was last
  # touched, so the archived listing is ordered by the same rule and reads the same way down.
  #
  # The session put away first is named at the end, which writes to its row and moves its touched
  # stamp past the other's. The two stamps then say opposite things about this listing, so it can only
  # come back in this order if it was ordered by when each session was put away.
  Scenario: The archived listing puts the session put away last at the top
    When the operator dispatches "the older subject" to the project
    And the operator archives the session
    And the operator dispatches "a newer subject" to a new session
    And the operator archives the session
    And the operator names the session archived first "the older subject"
    Then the archived listing puts the session put away last at the top

  # A task cannot yet be dispatched after a restart: the control plane forgets which container each
  # session was running in, and starting a new one collides with the container still on the host.
  # Reattaching a session to its sandbox is separate job.
  Scenario: A session survives the control plane restarting
    Given a session started by dispatching "remember this"
    When the control plane restarts
    Then the workspace has 1 sessions
    And the session is reported as idle
    And the session still holds the conversation the first task started

  Scenario: Workspaces survive the control plane restarting
    When the control plane restarts
    Then the workspace is listed
    And the workspace can be fetched by its id

  Scenario: Stopping a session tears down its sandbox
    Given a session started by dispatching "hello"
    When the operator stops the session
    Then the session is reported as stopped
    And the session's sandbox has been closed

  # Restarting starts the container straight away rather than waiting for the next task, so the
  # operator can go back into the conversation instead of dispatching a task to make the container
  # exist. It is only safe because the conversation lives on the host now: the new sandbox is a new
  # container over the same conversation store and the same project files.
  Scenario: A stopped session restarts to idle, with a sandbox, and can be attached to
    Given a session started by dispatching "remember this"
    When the operator stops the session
    And the operator restarts the session
    Then the session is reported as idle
    And the session still holds the conversation the first task started
    And a second sandbox has been created for that session
    And the operator asks how to attach to the session
    And the control plane names the session's sandbox

  # Restarting is what the operator reaches for when the container is wrong, and a container that is
  # wrong is usually one that is still running. Refusing until the session was stopped made that two
  # keys, so the live session is stopped here and comes back in a new container.
  Scenario: A live session restarts into a new container rather than being refused
    Given a session started by dispatching "hello"
    When the operator restarts the session
    Then the session is reported as idle
    And the session's sandbox has been closed
    And a second sandbox has been created for that session
    And the session still holds the conversation the first task started

  # An archived session's row says stopped, so a restart that only asked about the status started a
  # container for a session nobody can see.
  Scenario: An archived session cannot be restarted
    Given a session started by dispatching "hello"
    When the operator archives the session
    And the operator restarts the session
    Then the control plane refuses it as the wrong state
    And the workspace has 1 archived sessions

  Scenario: Restarting a session that does not exist is refused
    When the operator restarts a session that does not exist
    Then the control plane refuses it as not found

  # Archiving puts a session away and keeps everything: the row, the conversation handle, the
  # conversation store on the host and the project's files. Nothing is deleted, by anyone, here.
  Scenario: An archived session leaves the default listing and is in the archived one
    Given a session started by dispatching "remember this"
    When the operator archives the session
    Then the workspace has 0 sessions
    And the workspace has 1 archived sessions
    And the session still holds the conversation the first task started

  Scenario: Archiving a running session stops it and closes its sandbox
    Given a session started by dispatching "hello"
    When the operator archives the session
    Then the session is reported as stopped
    And the session's sandbox has been closed

  # Archiving takes the container away while the task is still in it, so the task lands on a session
  # that is already put away. Recording what it came to brought the row back to idle, or marked it
  # failed, and the archived listing then said a session nobody can reach is working.
  #
  # The task is held open here rather than timed, because what is being specified is what happens
  # while one runs, and a scenario that waits a duration for that passes by accident.
  Scenario: A task that lands after its session was archived leaves it stopped
    Given the model takes longer over a task than anybody will wait
    And a task dispatched without waiting for it
    And a task is under way
    When the operator archives the session
    And the model finishes the task
    Then the session is reported as stopped
    And the workspace has 1 archived sessions

  # A handle is matched whether the session is put away or not, so this used to start a container for
  # a session that is not in the listing.
  Scenario: An archived session cannot be dispatched to
    Given a session started by dispatching "hello"
    When the operator archives the session
    And the operator dispatches "carry on" to the same session
    Then the control plane refuses it as the wrong state
    And the session is reported as stopped

  Scenario: A restored session is back in the default listing with its conversation
    Given a session started by dispatching "remember this"
    When the operator archives the session
    And the operator restores the session
    Then the workspace has 1 sessions
    And the workspace has 0 archived sessions
    And the session still holds the conversation the first task started

  Scenario: A session that is not archived cannot be restored
    Given a session started by dispatching "hello"
    When the operator restores the session
    Then the control plane refuses it as the wrong state

  Scenario: Archiving a session twice is refused rather than restamped
    Given a session started by dispatching "hello"
    When the operator archives the session
    And the operator archives the session
    Then the control plane refuses it as the wrong state

  # The mode a task runs in was hardcoded, so no operator could see it or change it. It belongs to the
  # session rather than to a task: a session started to plan something should keep planning instead of
  # being re armed on every dispatch.
  Scenario: A task runs in the mode its session is set to
    Given a session started by dispatching "hello"
    Then the task ran in permission mode "acceptEdits"
    When the session is set to permission mode "bypassPermissions"
    And the operator dispatches "and again" to the same session
    Then the task ran in permission mode "bypassPermissions"

  Scenario: A session keeps its permission mode across a restart of the control plane
    Given a session started by dispatching "hello"
    When the session is set to permission mode "plan"
    And the control plane restarts
    And the operator dispatches "and again" to the same session
    Then the task ran in permission mode "plan"

  Scenario: A mode the model does not understand is refused rather than passed to it
    Given a session started by dispatching "hello"
    When the session is set to permission mode "yolo"
    Then the control plane refuses it as invalid
    And the refusal suggests "bypassPermissions"

  Scenario: A task for a project that does not exist is refused
    When the operator dispatches "hello" to project "ghost"
    Then the control plane refuses it as not found

  Scenario: An empty task is refused
    When the operator dispatches "" to the project
    Then the control plane refuses it as invalid

  # A session's state does not all sit at the same level. The conversation the model keeps, and the
  # workspace's own context, belong to the workspace, so every project in it can resume a session.
  # The working files and the project's context belong to the project. The sandbox is told both, and
  # what it does with them is its own business: a host directory on Docker, a volume elsewhere.
  Scenario: A session's sandbox is created for its project and its workspace
    When the operator dispatches "hello" to the project
    Then the sandbox was created for the session's project and workspace

  # The token is set on the sandbox at creation, not only on each task, so anything the operator
  # starts inside it later is authenticated without the tool carrying a credential around.
  Scenario: The session's sandbox carries the workspace's subscription token
    Given the workspace has the subscription token "tok-xyz"
    When the operator dispatches "hello" to the project
    Then the session's sandbox was created with the subscription token "tok-xyz"

  Scenario: A workspace with no token creates a sandbox with no credential on it
    When the operator dispatches "hello" to the project
    Then the session's sandbox was created with nothing but its own identifier

  Scenario: A task carries the workspace's subscription token into the sandbox
    Given the workspace has the subscription token "tok-xyz"
    When the operator dispatches "hello" to the project
    Then the task ran with the subscription token "tok-xyz"

  Scenario: A workspace with no subscription token still runs a task
    When the operator dispatches "hello" to the project
    Then the reply is "you said: hello"
    And the task ran with nothing but the session's own identifier

  # Shelling in opens the room the conversation happens in. This opens the conversation.
  Scenario: The operator can attach to a session's conversation
    Given a session started by dispatching "remember this"
    When the operator asks how to attach to the session
    Then the control plane names the session's sandbox
    And the command resumes the conversation the task started
    And the command runs in permission mode "acceptEdits"
    And the command runs it inside a terminal the operator can leave
    And the answer carries no credential

  # Watching a task is the reason to attach, and the one moment it did not work was while a task ran,
  # which is every moment that matters. The system passed no name on a session's first task, so the model
  # runtime named that conversation itself and told nobody until the task was over. Attaching meanwhile
  # found nothing on the session, named a second conversation and opened that one: empty, beside the
  # work, and real enough that typing in it left two conversations in one session.
  Scenario: The operator attaches while the first task runs and lands in the conversation doing the work
    Given the model takes longer over a task than anybody will wait
    And a task dispatched without waiting for it
    And a task is under way
    When the operator asks how to attach to the session
    Then the system named the conversation before the task started
    And the command opens the conversation the task is running in
    When the model finishes the task
    Then the session still holds the conversation the first task started

  # Opening a session has to be the same session. One armed to skip permissions that asks anyway the
  # moment it is opened reads as the toggle not working.
  Scenario: Opening a session runs in the mode that session is set to
    Given a session started by dispatching "remember this"
    When the session is set to permission mode "bypassPermissions"
    And the operator asks how to attach to the session
    Then the command runs in permission mode "bypassPermissions"

  # The live sandboxes are a map in the control plane's process, so a restart empties it while the row
  # still says idle. Answering from the row alone handed the operator a container name the daemon had
  # never heard of: "No such container: quaycrew-134c2c6dbf1e907413753cc5".
  Scenario: Attaching to a session the control plane has forgotten starts its sandbox again
    Given a session started by dispatching "remember this"
    When the control plane restarts
    And the operator asks how to attach to the session
    Then the control plane names the session's sandbox
    And the control plane asked for that session's sandbox

  # A handle can outlive what it points at. Every conversation from a sandbox built before state was
  # kept on the host died with that container while the row kept the handle. This was refused, because
  # resuming one printed "No conversation found" and exited, which from the console looks like nothing
  # happening. It cannot be refused any more: a conversation the system has just named has no transcript
  # either, and that is a first open rather than a loss. The sandbox is the only place that can tell
  # them apart, so it resumes what is there and starts what is not, under the name it was given.
  Scenario: A session whose conversation is gone opens under the name the system holds
    Given a session started by dispatching "remember this"
    When the conversation the model kept is lost
    And the operator asks how to attach to the session
    Then the control plane names the session's sandbox
    And the command opens the conversation the system holds

  # Tokens are what a system costs, and the conversations that cost the most never pass through the
  # control plane: an operator talking in the panel is talking to the sandbox. The model's own
  # transcript is the only record, so that is what the system reads.
  Scenario: A session reports what its conversation has cost
    Given a session started by dispatching "hello"
    When the model has written 52 in, 6917 out and 1723404 read from cache
    And the operator lists the sessions
    Then the session reports 52 tokens in and 6917 out
    And the session reports 1723404 read from the cache

  Scenario: A session nobody has spoken in reports no cost at all
    When the operator opens the driver
    And the operator lists the sessions
    Then the driver reports no cost, rather than a cost of nothing

  # A conversation started inside a sandbox picks its own identifier and tells nobody, so every
  # conversation opened from the panel was one the system could not name: no history to read back, no
  # tokens to count, and no way to tell one transcript in a workspace from another. The system names it
  # instead.
  Scenario: The system names a conversation when it opens one
    When the operator opens the driver
    And the operator asks how to attach to the driver
    Then the driver has a conversation the system can name
    And the command opens the conversation the system holds

  Scenario: Opening a conversation twice keeps the name it was given
    When the operator opens the driver
    And the operator asks how to attach to the driver
    And the operator asks how to attach to the driver
    Then the driver has the same conversation both times

  # The control plane kept a handle to every sandbox it had made and trusted it forever. Anything that
  # removed a container behind its back left that handle pointing at nothing, and the operator got a
  # container name for something the daemon had never heard of, over and over:
  # "Error response from daemon: No such container: quaycrew-1edc8349315233e36bf4fd53".
  Scenario: A sandbox removed behind the control plane's back is made again
    Given a session started by dispatching "remember this"
    When the session's sandbox is removed without telling the control plane
    And the operator asks how to attach to the session
    Then the control plane names the session's sandbox
    And a second sandbox has been created for that session

  Scenario: A task after its sandbox was removed behind the control plane's back still runs
    Given a session started by dispatching "remember this"
    When the session's sandbox is removed without telling the control plane
    And the operator dispatches "and again" to the same session
    Then the reply is "you said: and again"
    And a second sandbox has been created for that session

  # The row is the system's own bookkeeping and it used to decide whether a conversation could be
  # opened. `make upgrade` drains first, and a drain puts every live session down, so after an
  # upgrade every session in the system said stopped and every one of them refused to open. The row is
  # brought up to date instead: a session somebody is talking in is not put down.
  Scenario: A stopped session opens, and its row comes back to idle
    Given a session started by dispatching "remember this"
    When the operator stops the session
    And the operator asks how to attach to the session
    Then the control plane names the session's sandbox
    And the command resumes the conversation the task started
    And the session is reported as idle

  # And the operator can carry on. A row still saying stopped would have the next startup reap the
  # container out from under the conversation they are typing into.
  Scenario: A session opened after it was stopped takes the next task
    Given a session started by dispatching "remember this"
    When the operator stops the session
    And the operator asks how to attach to the session
    Then the control plane names the session's sandbox
    And the operator dispatches "and again" to the same session
    And the reply is "you said: and again"
    And both tasks ran in the same session

  # Archiving sets the row to stopped as well, and the stopped answer came first, so an archived
  # session was refused with "is stopped: restart it first". Restarting an archived session is itself
  # refused. Attach named the one action that could not be taken.
  Scenario: An archived session opens, and comes back into the listing
    Given a session started by dispatching "hello"
    When the operator archives the session
    And the operator asks how to attach to the session
    Then the control plane names the session's sandbox
    And the command resumes the conversation the task started
    And the workspace has 0 archived sessions

  # A session exists from the moment a task is dispatched, so a first task that failed leaves one
  # holding no conversation. It sat in the listing with no way to open it at all. The system names a
  # conversation for it, exactly as it does for the driver.
  Scenario: A session whose first task failed opens under a conversation the system names
    When the operator asks how to attach to a session that has never had a task
    Then the control plane names the session's sandbox
    And the command opens the conversation the system holds

  # Every model failure read "run exited: exit status 1": the same sentence for an expired token, a
  # network failure, a missing binary in the image and the model refusing outright. The reason was
  # there the whole time, on standard output, in the stream the reply comes from.
  #
  # These run the real model adapter over a sandbox that fails on purpose, because a double handing
  # back a canned error cannot say anything about an explanation built out of a stream.
  Scenario: A failed task says why, in the model's own words
    Given the workspace has the subscription token "sk-ant-oat01-hVnQ2mXk9pLrT4wYzB7cD1fG5jH8sN0aE3iU6oP"
    And the model refuses the task saying "Failed to authenticate. API Error: 401 Invalid bearer token"
    When the operator dispatches "hello" to the project
    Then the refusal says "401 Invalid bearer token"

  # The task runs with the subscription token in its environment, so every place a failure can quote
  # is a place the token turns up. A tool that prints one because a task failed is a worse defect
  # than the one it is explaining.
  Scenario: A failed task never carries the subscription token
    Given the workspace has the subscription token "sk-ant-oat01-hVnQ2mXk9pLrT4wYzB7cD1fG5jH8sN0aE3iU6oP"
    And the model refuses the task quoting the token back
    When the operator dispatches "hello" to the project
    Then the refusal carries no token
    And the refusal says something was taken out

  Scenario: A task that failed before the model said anything falls back to the error stream
    Given the sandbox fails with nothing on standard output, saying "claude: command not found"
    When the operator dispatches "hello" to the project
    Then the refusal says "claude: command not found"

  # Nothing killed with signal 9 gets to say why: no last line on either stream, and the kernel log
  # is not readable from inside a container. So exit 137 on its own read as a hang, and the operator
  # could not tell a task that ran out of memory from one whose container an upgrade took away. Both
  # are named now, with the command that answers which it was.
  Scenario: A task killed for memory says so rather than showing an exit status
    Given the sandbox is killed for memory part way through the task
    When the operator dispatches "hello" to the project
    Then the refusal says "a kill rather than a failure"
    And the refusal says "krewe room"
    And the refusal says "container went away"

  # The console shows the system and a conversation shows one session, and using both meant losing sight
  # of one. The panel puts them on the screen at once, half the width each, side by side.
  #
  # tmux does the splitting, the same tmux that already keeps an open conversation alive behind
  # ctrl-q. These assert on the commands the panel would run rather than running them, the way the
  # attach scenarios do, because a scenario that took over the terminal could not report anything.
  Scenario: The panel puts the console and a conversation side by side
    Given a session started by dispatching "hello"
    When the operator opens the panel
    Then the panel puts the console in one half and that conversation in the other
    And each half is 50% of the width
    And the console has the keyboard
    And nothing is drawn above either half

  Scenario: The panel opens the conversation you were last in
    Given a session started by dispatching "the older one"
    And a session started by dispatching "the newer one" on a new session
    When the operator opens the panel
    Then the panel opens the newer conversation

  # A pane closes the moment its command exits, and the conversation half of the screen is a pane
  # running one command. So a conversation that could not be opened printed the reason and had it
  # destroyed in the same instant. The operator pressed the key, the screen flickered, and nothing on
  # it ever said why. Attach stays instead: it says what happened and waits there, the way a finished
  # conversation already keeps its terminal alive inside the sandbox.
  #
  # These two drive a real terminal multiplexer, on a socket of their own, because the whole of this
  # is what the multiplexer does with a pane whose command has ended.
  Scenario: A conversation that cannot be opened says why and stays on the screen
    Given a terminal with the console in it
    When krewe attach is put beside the console and cannot reach the system
    Then the reason is on the screen
    And pressing enter gives the operator the console back

  # The measurement the one above is built on, kept so a multiplexer that changed its mind about this
  # would be noticed rather than quietly making the answer pointless.
  Scenario: A conversation that says why and exits takes the reason with it
    Given a terminal with the console in it
    When a conversation that says why and exits is put beside the console
    Then the pane is gone, and the reason with it

# Leaving the conversation closes the pane it was in, and quitting the console closes the other, so
  # a panel is very often half of one by the time somebody opens the system again. Opening it used to
  # come back to that half and leave it there: the console had no room to open a conversation, and a
  # conversation on its own had no console to go back to.
  Scenario: Opening the system rebuilds a panel that lost a half
    Given a panel with a console and a conversation
    When the conversation is closed the way leaving it closes it
    And the operator opens the system again
    Then the panel has a console and a conversation again

  # And the panel that is whole is left exactly as it is, or every open would take the conversation
  # away and put a fresh one back, which is the fault above with the halves the other way round.
  Scenario: Opening the system again leaves a whole panel alone
    Given a panel with a console and a conversation
    When the operator opens the system again
    Then the conversation is the one that was already there

  Scenario: The panel refuses rather than opening half of one
    When the operator opens the panel
    Then the panel says there is no conversation to put beside the console
    And it says how to start one

  # A session that can reach the control plane can drive the system: make a workspace, start a session,
  # write a context, the same way the operator does. It is a real widening, so it is turned on rather
  # than assumed, and the sandbox is what bounds it.
  Scenario: The driver is told where to reach the system
    Given a system that sessions can reach at "controlplane:50051"
    When the operator opens the driver
    And the driver is sent "hello"
    Then the sandbox carries the address of the system
    And the sandbox carries the driver's own token, not the operator's
    And the sandbox carries no address it was not given

  Scenario: An ordinary session is told nothing, even when the system can be reached
    Given a system that sessions can reach at "controlplane:50051"
    When the operator dispatches "hello" to the project
    Then the sandbox carries no address at all
    And the sandbox carries no system token

  Scenario: The driver is the same session every time it is opened
    When the operator opens the driver
    And the operator opens the driver again
    Then it is the same driver both times
    And the system has one driver

  # The driver acts for the operator rather than doing work of its own, and one that stops to ask
  # before every step describes the task instead of doing it: asked to make a project it explained
  # how you would go about making one. What bounds it is the sandbox, which is the same boundary it
  # would have in any mode.
  Scenario: The driver is made able to act rather than to ask
    When the operator opens the driver
    And the driver is sent "make me a project"
    Then the task ran in permission mode "bypassPermissions"

  # A mode set on the driver is the driver's, the same as any other session: made able to act is not
  # the same as held there.
  Scenario: The driver can be set back to asking
    When the operator opens the driver
    And the driver is set to permission mode "acceptEdits"
    And the driver is sent "make me a project"
    Then the task ran in permission mode "acceptEdits"

  # The driver opens knowing what krewe is, rather than having to be told every time. It is the system
  # describing itself: the command list the tool prints, and the behaviour specification the binary
  # carries, neither of which can drift from what the tool actually does.
  Scenario: The driver opens knowing what krewe is
    When the operator opens the driver
    Then the driver has been told what krewe is
    And what it was told names the words a system is made of

  # Being told is not the same as being able to read it. The manual is written into the store, and the
  # driver only ever sees the file: a driver made before any of this had a memory file with none of
  # the system's marks in it, that file was read back as an edit of what it had never seen, and the
  # manual was gone again before anybody opened the conversation.
  Scenario: A driver that already had notes reads both them and the manual
    Given a driver made before the system described itself
    And its memory file already says "the boiler code is 1985"
    When the operator opens the driver
    Then the driver's memory file says what krewe is
    And the driver's memory file still says "the boiler code is 1985"

  # An operator who edits it has a reason to, and overwriting on every open would make it the one
  # context nobody can change.
  Scenario: Opening the driver again does not overwrite what it has been told
    When the operator opens the driver
    And the operator writes their own instructions into the driver
    And the operator opens the driver again
    Then the driver still carries their own instructions

  # Archiving a session closes its sandbox and says why in the code: a container left running for a
  # session nobody can see is a leak. Deleting never did the same, so a deleted workspace kept every
  # container it was hiding, running, with the workspace's secrets in its environment.
  Scenario: Deleting a workspace closes the sandboxes it was hiding
    Given a session started by dispatching "hello"
    When the operator deletes the workspace
    Then every sandbox the system made is closed

  Scenario: Deleting a project closes the sandboxes it was hiding
    Given a session started by dispatching "hello"
    When the operator deletes the project
    Then every sandbox the system made is closed

  # The system's map of live sandboxes is a process map, so a restart empties it while the containers
  # keep running. Stopping a session then marked the row and left the container: the close has to ask
  # the daemon, not the map.
  Scenario: Stopping a session after a restart still removes its container
    Given a session started by dispatching "hello"
    When the control plane restarts
    And the operator stops the session
    Then every sandbox the system made is closed

  # The leak above already happened on real systems, so starting up reaps what it finds: a container
  # whose row says stopped or archived, or whose row is gone, belongs to nobody.
  Scenario: A container whose session was stopped behind the system's back is reaped at startup
    Given a session started by dispatching "hello"
    And the session's row says stopped while its container still runs
    When the control plane restarts
    Then every sandbox the system made is closed

  # A clone or a skill setup that fails used to leave the container it had just made running and
  # untracked, one per attempt.
  Scenario: A sandbox that cannot be provisioned is not left running
    Given the system has a skill "git" that says "Branch first."
    And the git skill has a file "bin/setup" saying "exit 1"
    And every command run in a sandbox fails
    When the operator dispatches "hello" to the project
    Then the system refuses it saying "could not set itself up"
    And every sandbox the system made is closed
