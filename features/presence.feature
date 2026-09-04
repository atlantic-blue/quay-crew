Feature: A listing says what is inside a session's sandbox

  A session running a conversation read idle. The word only ever meant "no dispatched exec is open",
  and an interactive conversation is not a dispatched exec, so a container answering somebody's
  question looked exactly like an empty one. Eighteen sandboxes were read on 28 August 2026 and six
  held a running model runtime. Every one of the six listed as idle.

  It matters because the listing is what an operator reads before they act. A restart, a drain and a
  reclaim all start from that word, and any of the three takes a running conversation away mid answer.

  So the system asks the sandbox rather than reading a field. The container's own process table says
  whether a model runtime is up, and its own tmux says whether a client is on the conversation, and
  neither needs anything kept fresh. Four states come out of it and the listing names each one:

    idle      nothing is running in there and nobody is in it. The only real idle.
    awake     a model runtime is up with nobody watching it.
    attached  somebody has the conversation open.
    unknown   the system asked the sandbox and was not told. Never idle.

  What this does not do: the drain and the reclaim are unchanged. The reclaim already asks whether
  somebody is attached and leaves those containers alone; the drain reads the row's own status, so it
  still refuses over a dispatched exec and still puts down a session holding a conversation nobody
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

  # The other half. Without this nothing is ever reclaimed and the system holds every container it made.
  Scenario: An empty sandbox is the only real idle
    Given a session started by dispatching "hello"
    When the operator lists the sessions
    Then the listing says the session is "idle"

  # The sad path that matters most: a false idle is what invites somebody to take the container.
  Scenario: A system that cannot reach its sandboxes says so rather than saying idle
    Given a session started by dispatching "hello"
    And the daemon will not say what is inside a sandbox
    When the operator lists the sessions
    Then the listing says the session is "unknown"
    And the listing does not say the session is idle

  # A dispatched exec keeps its own word. Running is what a drain refuses on, and reading the sandbox
  # must not overwrite it.
  Scenario: An exec under way still reads running
    Given the model takes longer over an exec than anybody will wait
    And an exec dispatched without waiting for it
    And an exec is under way
    When the operator lists the sessions
    Then the listing says the session is "running"

  # The cost rule. One question per row that would otherwise read idle, and nothing at all for a row
  # that already says something is happening. A console redraws every three seconds.
  Scenario: A listing asks nothing about a session that is not idle
    Given the model takes longer over an exec than anybody will wait
    And an exec dispatched without waiting for it
    And an exec is under way
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
    Given the system listens on an address the tool can dial
    And a session started by dispatching "hello"
    And that session's sandbox is running a model runtime
    When the caller lists the sessions
    Then standard output carries "awake"
    And standard output does not carry "idle"
    And the command succeeds
