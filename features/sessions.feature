Feature: Sessions run in isolated sandboxes

  A session is a conversation with the model. It runs inside its own sandbox, a container that lives
  across the session's turns, so whatever the agent sets up in one turn is still there in the next.

  These scenarios drive the control plane over its real interface, the same one every channel and the
  dashboard talk to. They are the acceptance criteria for the sessions milestone.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

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

  Scenario: The operator can see the sessions of a workspace
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new thread
    Then the workspace has 2 sessions

  # A turn cannot yet be dispatched after a restart: the control plane forgets which container each
  # session was running in, and starting a new one collides with the container still on the host.
  # Reattaching a session to its sandbox is separate work.
  Scenario: A session survives the control plane restarting
    Given a session started by dispatching "remember this"
    When the control plane restarts
    Then the workspace has 1 sessions
    And the session is reported as idle
    And the session still holds the conversation the first turn started

  Scenario: Workspaces survive the control plane restarting
    When the control plane restarts
    Then the workspace is listed
    And the workspace can be fetched by its id

  Scenario: Stopping a session tears down its sandbox
    Given a session started by dispatching "hello"
    When the operator stops the session
    Then the session is reported as stopped
    And the session's sandbox has been closed

  # Restarting starts the container straight away rather than waiting for the next turn, so the
  # operator can go back into the conversation instead of dispatching a turn to make the container
  # exist. It is only safe because the conversation lives on the host now: the new sandbox is a new
  # container over the same conversation store and the same project files.
  Scenario: A stopped thread restarts to idle, with a sandbox, and can be attached to
    Given a session started by dispatching "remember this"
    When the operator stops the session
    And the operator restarts the session
    Then the session is reported as idle
    And the session still holds the conversation the first turn started
    And a second sandbox has been created for that session
    And the operator asks how to attach to the session
    And the control plane names the session's sandbox

  Scenario: A thread that is not stopped has nothing to restart
    Given a session started by dispatching "hello"
    When the operator restarts the session
    Then the control plane refuses it as the wrong state
    And the session is reported as idle

  Scenario: Restarting a thread that does not exist is refused
    When the operator restarts a session that does not exist
    Then the control plane refuses it as not found

  # Archiving puts a thread away and keeps everything: the row, the conversation handle, the
  # conversation store on the host and the project's files. Nothing is deleted, by anyone, here.
  Scenario: An archived thread leaves the default listing and is in the archived one
    Given a session started by dispatching "remember this"
    When the operator archives the session
    Then the workspace has 0 sessions
    And the workspace has 1 archived sessions
    And the session still holds the conversation the first turn started

  Scenario: Archiving a running thread stops it and closes its sandbox
    Given a session started by dispatching "hello"
    When the operator archives the session
    Then the session is reported as stopped
    And the session's sandbox has been closed

  Scenario: A restored thread is back in the default listing with its conversation
    Given a session started by dispatching "remember this"
    When the operator archives the session
    And the operator restores the session
    Then the workspace has 1 sessions
    And the workspace has 0 archived sessions
    And the session still holds the conversation the first turn started

  Scenario: A thread that is not archived cannot be restored
    Given a session started by dispatching "hello"
    When the operator restores the session
    Then the control plane refuses it as the wrong state

  Scenario: Archiving a thread twice is refused rather than restamped
    Given a session started by dispatching "hello"
    When the operator archives the session
    And the operator archives the session
    Then the control plane refuses it as the wrong state

  # The mode a turn runs in was hardcoded, so no operator could see it or change it. It belongs to the
  # thread rather than to a turn: a thread started to plan something should keep planning instead of
  # being re armed on every dispatch.
  Scenario: A turn runs in the mode its thread is set to
    Given a session started by dispatching "hello"
    Then the turn ran in permission mode "acceptEdits"
    When the thread is set to permission mode "bypassPermissions"
    And the operator dispatches "and again" to the same thread
    Then the turn ran in permission mode "bypassPermissions"

  Scenario: A thread keeps its permission mode across a restart of the control plane
    Given a session started by dispatching "hello"
    When the thread is set to permission mode "plan"
    And the control plane restarts
    And the operator dispatches "and again" to the same thread
    Then the turn ran in permission mode "plan"

  Scenario: A mode the model does not understand is refused rather than passed to it
    Given a session started by dispatching "hello"
    When the thread is set to permission mode "yolo"
    Then the control plane refuses it as invalid
    And the refusal suggests "bypassPermissions"

  Scenario: A turn for a project that does not exist is refused
    When the operator dispatches "hello" to project "ghost"
    Then the control plane refuses it as not found

  Scenario: An empty turn is refused
    When the operator dispatches "" to the project
    Then the control plane refuses it as invalid

  # A session's state does not all sit at the same level. The conversation the model keeps, and the
  # workspace's own context, belong to the workspace, so every project in it can resume a thread.
  # The working files and the project's context belong to the project. The sandbox is told both, and
  # what it does with them is its own business: a host directory on Docker, a volume elsewhere.
  Scenario: A session's sandbox is created for its project and its workspace
    When the operator dispatches "hello" to the project
    Then the sandbox was created for the session's project and workspace

  # The token is set on the sandbox at creation, not only on each turn, so anything the operator
  # starts inside it later is authenticated without the tool carrying a credential around.
  Scenario: The session's sandbox carries the workspace's subscription token
    Given the workspace has the subscription token "tok-xyz"
    When the operator dispatches "hello" to the project
    Then the session's sandbox was created with the subscription token "tok-xyz"

  Scenario: A workspace with no token creates a sandbox with no credential on it
    When the operator dispatches "hello" to the project
    Then the session's sandbox was created with no environment

  Scenario: A turn carries the workspace's subscription token into the sandbox
    Given the workspace has the subscription token "tok-xyz"
    When the operator dispatches "hello" to the project
    Then the turn ran with the subscription token "tok-xyz"

  Scenario: A workspace with no subscription token still runs a turn
    When the operator dispatches "hello" to the project
    Then the reply is "you said: hello"
    And the turn ran with no extra environment

  # Shelling in opens the room the conversation happens in. This opens the conversation.
  Scenario: The operator can attach to a thread's conversation
    Given a session started by dispatching "remember this"
    When the operator asks how to attach to the session
    Then the control plane names the session's sandbox
    And the command resumes the conversation the turn started
    And the command runs in permission mode "acceptEdits"
    And the answer carries no credential

  # Opening a thread has to be the same thread. One armed to skip permissions that asks anyway the
  # moment it is opened reads as the toggle not working.
  Scenario: Opening a thread runs in the mode that thread is set to
    Given a session started by dispatching "remember this"
    When the thread is set to permission mode "bypassPermissions"
    And the operator asks how to attach to the session
    Then the command runs in permission mode "bypassPermissions"

  # The live sandboxes are a map in the control plane's process, so a restart empties it while the row
  # still says idle. Answering from the row alone handed the operator a container name the daemon had
  # never heard of: "No such container: quaycrew-134c2c6dbf1e907413753cc5".
  Scenario: Attaching to a thread the control plane has forgotten starts its sandbox again
    Given a session started by dispatching "remember this"
    When the control plane restarts
    And the operator asks how to attach to the session
    Then the control plane names the session's sandbox
    And the control plane asked for that session's sandbox

  # A handle can outlive what it points at. Every conversation from a sandbox built before state was
  # kept on the host died with that container while the row kept the handle, and resuming one of those
  # prints "No conversation found" and exits, which from the console looks like nothing happening.
  Scenario: A thread whose conversation is gone says so rather than opening nothing
    Given a session started by dispatching "remember this"
    When the conversation the model kept is lost
    And the operator asks how to attach to the session
    Then the control plane refuses it as not yet ready
    And the refusal says the conversation is gone, in the operator's words

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

  Scenario: A turn after its sandbox was removed behind the control plane's back still runs
    Given a session started by dispatching "remember this"
    When the session's sandbox is removed without telling the control plane
    And the operator dispatches "and again" to the same thread
    Then the reply is "you said: and again"
    And a second sandbox has been created for that session

  Scenario: An archived thread cannot be attached to
    Given a session started by dispatching "hello"
    When the operator archives the session
    And the operator asks how to attach to the session
    Then the control plane refuses it as not yet ready

  Scenario: A thread with no conversation yet cannot be attached to
    When the operator asks how to attach to a session that has never had a turn
    Then the control plane refuses it as not yet ready

  Scenario: A stopped thread cannot be attached to
    Given a session started by dispatching "hello"
    When the operator stops the session
    And the operator asks how to attach to the session
    Then the control plane refuses it as not yet ready
