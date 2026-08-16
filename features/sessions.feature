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

  # A task cannot yet be dispatched after a restart: the control plane forgets which container each
  # session was running in, and starting a new one collides with the container still on the host.
  # Reattaching a session to its sandbox is separate work.
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

  Scenario: A session that is not stopped has nothing to restart
    Given a session started by dispatching "hello"
    When the operator restarts the session
    Then the control plane refuses it as the wrong state
    And the session is reported as idle

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
    Then the session's sandbox was created with no environment

  Scenario: A task carries the workspace's subscription token into the sandbox
    Given the workspace has the subscription token "tok-xyz"
    When the operator dispatches "hello" to the project
    Then the task ran with the subscription token "tok-xyz"

  Scenario: A workspace with no subscription token still runs a task
    When the operator dispatches "hello" to the project
    Then the reply is "you said: hello"
    And the task ran with no extra environment

  # Shelling in opens the room the conversation happens in. This opens the conversation.
  Scenario: The operator can attach to a session's conversation
    Given a session started by dispatching "remember this"
    When the operator asks how to attach to the session
    Then the control plane names the session's sandbox
    And the command resumes the conversation the task started
    And the command runs in permission mode "acceptEdits"
    And the command runs it inside a terminal the operator can leave
    And the answer carries no credential

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
  # happening. It cannot be refused any more: a conversation the crew has just named has no transcript
  # either, and that is a first open rather than a loss. The sandbox is the only place that can tell
  # them apart, so it resumes what is there and starts what is not, under the name it was given.
  Scenario: A session whose conversation is gone opens under the name the crew holds
    Given a session started by dispatching "remember this"
    When the conversation the model kept is lost
    And the operator asks how to attach to the session
    Then the control plane names the session's sandbox
    And the command opens the conversation the crew holds

  # Tokens are what a crew costs, and the conversations that cost the most never pass through the
  # control plane: an operator talking in the panel is talking to the sandbox. The model's own
  # transcript is the only record, so that is what the crew reads.
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
  # conversation opened from the panel was one the crew could not name: no history to read back, no
  # tokens to count, and no way to tell one transcript in a workspace from another. The crew names it
  # instead.
  Scenario: The crew names a conversation when it opens one
    When the operator opens the driver
    And the operator asks how to attach to the driver
    Then the driver has a conversation the crew can name
    And the command opens the conversation the crew holds

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

  Scenario: An archived session cannot be attached to
    Given a session started by dispatching "hello"
    When the operator archives the session
    And the operator asks how to attach to the session
    Then the control plane refuses it as not yet ready

  Scenario: A session with no conversation yet cannot be attached to
    When the operator asks how to attach to a session that has never had a task
    Then the control plane refuses it as not yet ready

  Scenario: A stopped session cannot be attached to
    Given a session started by dispatching "hello"
    When the operator stops the session
    And the operator asks how to attach to the session
    Then the control plane refuses it as not yet ready

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
  # is a place the token tasks up. A tool that prints one because a task failed is a worse defect
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

  # The console shows the crew and a conversation shows one session, and using both meant losing sight
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
    And the header spans the whole width above both halves

  Scenario: The panel opens the conversation you were last in
    Given a session started by dispatching "the older one"
    And a session started by dispatching "the newer one" on a new session
    When the operator opens the panel
    Then the panel opens the newer conversation

  Scenario: The panel refuses rather than opening half of one
    When the operator opens the panel
    Then the panel says there is no conversation to put beside the console
    And it says how to start one

  # A session that can reach the control plane can drive the crew: make a workspace, start a session,
  # write a context, the same way the operator does. It is a real widening, so it is turned on rather
  # than assumed, and the sandbox is what bounds it.
  Scenario: The driver is told where to reach the crew
    Given a crew that sessions can reach at "controlplane:50051"
    When the operator opens the driver
    And the driver is sent "hello"
    Then the sandbox carries the address of the crew
    And the sandbox carries the driver's own token, not the operator's
    And the sandbox carries no address it was not given

  Scenario: An ordinary session is told nothing, even when the crew can be reached
    Given a crew that sessions can reach at "controlplane:50051"
    When the operator dispatches "hello" to the project
    Then the sandbox carries no address at all
    And the sandbox carries no crew token

  Scenario: The driver is the same session every time it is opened
    When the operator opens the driver
    And the operator opens the driver again
    Then it is the same driver both times
    And the crew has one driver

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

  # The driver opens knowing what quay is, rather than having to be told every time. It is the crew
  # describing itself: the command list the tool prints, and the behaviour specification the binary
  # carries, neither of which can drift from what the tool actually does.
  Scenario: The driver opens knowing what quay is
    When the operator opens the driver
    Then the driver has been told what quay is
    And what it was told names the words a crew is made of

  # Being told is not the same as being able to read it. The manual is written into the store, and the
  # driver only ever sees the file: a driver made before any of this had a memory file with none of
  # the crew's marks in it, that file was read back as an edit of what it had never seen, and the
  # manual was gone again before anybody opened the conversation.
  Scenario: A driver that already had notes reads both them and the manual
    Given a driver made before the crew described itself
    And its memory file already says "the boiler code is 1985"
    When the operator opens the driver
    Then the driver's memory file says what quay is
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
    Then every sandbox the crew made is closed

  Scenario: Deleting a project closes the sandboxes it was hiding
    Given a session started by dispatching "hello"
    When the operator deletes the project
    Then every sandbox the crew made is closed

  # The crew's map of live sandboxes is a process map, so a restart empties it while the containers
  # keep running. Stopping a session then marked the row and left the container: the close has to ask
  # the daemon, not the map.
  Scenario: Stopping a session after a restart still removes its container
    Given a session started by dispatching "hello"
    When the control plane restarts
    And the operator stops the session
    Then every sandbox the crew made is closed

  # The leak above already happened on real crews, so starting up reaps what it finds: a container
  # whose row says stopped or archived, or whose row is gone, belongs to nobody.
  Scenario: A container whose session was stopped behind the crew's back is reaped at startup
    Given a session started by dispatching "hello"
    And the session's row says stopped while its container still runs
    When the control plane restarts
    Then every sandbox the crew made is closed

  # A clone or a skill setup that fails used to leave the container it had just made running and
  # untracked, one per attempt.
  Scenario: A sandbox that cannot be provisioned is not left running
    Given the crew has a skill "git" that says "Branch first."
    And the git skill has a file "bin/setup" saying "exit 1"
    And every command run in a sandbox fails
    When the operator dispatches "hello" to the project
    Then the crew refuses it saying "could not set itself up"
    And every sandbox the crew made is closed
