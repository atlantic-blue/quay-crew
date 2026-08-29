Feature: A listing says what is inside a session's sandbox

  A session running a conversation read idle. The word only ever meant "no dispatched task is open",
  and an interactive conversation is not a dispatched task, so a container answering somebody's
  question looked exactly like an empty one. Eighteen sandboxes were read on 28 August 2026 and six
  held a running model runtime. Every one of the six listed as idle.

  It matters because the listing is what an operator reads before they act. A restart, a drain and a
  reclaim all start from that word, and any of the three takes a running conversation away mid answer.

  So the crew asks the sandbox rather than reading a field. The container's own process table says
  whether a model runtime is up, and its own tmux says whether a client is on the conversation, and
  neither needs anything kept fresh. Four states come out of it and the listing names each one:

    idle      nothing is running in there and nobody is in it. The only real idle.
    awake     a model runtime is up with nobody watching it.
    attached  somebody has the conversation open.
    unknown   the crew asked the sandbox and was not told. Never idle.

  What this does not do: the drain and the reclaim are unchanged. The reclaim already asks whether
  somebody is attached and leaves those containers alone; the drain reads the row's own status, so it
  still refuses over a dispatched task and still puts down a session holding a conversation nobody
  dispatched. Making the drain refuse over that is its own change.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # The rule the whole slice ships on.
  Scenario: A conversation answering with nobody watching it does not read idle
    Given a session started by dispatching "hello"
    And that session's sandbox is running a model runtime
    When the operator lists the sessions
    Then the listing says the session is "awake"
    And the listing does not say the session is idle

  Scenario: A session somebody is typing into says so
    Given a session started by dispatching "hello"
    And an operator has that session's conversation open
    When the operator lists the sessions
    Then the listing says the session is "attached"

  # Ending a conversation leaves the terminal alive at a prompt, so there is somebody in a container
  # with no runtime running. Asking only about the runtime would call that container empty.
  Scenario: Somebody sitting in a conversation they closed still holds the container
    Given a session started by dispatching "hello"
    And an operator has that session's conversation open
    And that session's sandbox is running nothing
    When the operator lists the sessions
    Then the listing says the session is "attached"

  # The other half. Without this nothing is ever reclaimed and the crew holds every container it made.
  Scenario: An empty sandbox is the only real idle
    Given a session started by dispatching "hello"
    When the operator lists the sessions
    Then the listing says the session is "idle"

  # The sad path that matters most: a false idle is what invites somebody to take the container.
  Scenario: A crew that cannot reach its sandboxes says so rather than saying idle
    Given a session started by dispatching "hello"
    And the daemon will not say what is inside a sandbox
    When the operator lists the sessions
    Then the listing says the session is "unknown"
    And the listing does not say the session is idle

  # A dispatched task keeps its own word. Running is what a drain refuses on, and reading the sandbox
  # must not overwrite it.
  Scenario: A task under way still reads running
    Given the model takes longer over a task than anybody will wait
    And a task dispatched without waiting for it
    And a task is under way
    When the operator lists the sessions
    Then the listing says the session is "running"

  # A session with no container has nothing to ask, and the crew must not report that as the daemon
  # failing: unknown would send an operator looking for a broken daemon.
  Scenario: A session whose container was taken back reads reclaimed
    Given the workspace reclaims a session after 1 second
    And a session started by dispatching "hello"
    When the reclaim time passes
    And the controller ticks
    And the operator lists the sessions
    Then the listing says the session is "reclaimed"

  # The cost rule. One question per row that would otherwise read idle, and nothing at all for a row
  # that already says something is happening. A console redraws every three seconds.
  Scenario: A listing asks nothing about a session that is not idle
    Given the model takes longer over a task than anybody will wait
    And a task dispatched without waiting for it
    And a task is under way
    When the operator lists the sessions
    Then no sandbox was asked what is inside it

  Scenario: A caller that does not ask what is inside a sandbox pays nothing
    Given a session started by dispatching "hello"
    And that session's sandbox is running a model runtime
    When the operator lists the sessions without asking what is inside them
    Then no sandbox was asked what is inside it
    And the listing says the session is "idle"

  # The command line is the other surface an operator reads, and it has to ask for what is inside a
  # sandbox for itself. A listing that did not ask reads the row's own word, which is the defect.
  Scenario: The command line listing says a session is awake
    Given the crew listens on an address the tool can dial
    And a session started by dispatching "hello"
    And that session's sandbox is running a model runtime
    When the caller lists the sessions
    Then standard output carries "awake"
    And standard output does not carry "idle"
    And the command succeeds
