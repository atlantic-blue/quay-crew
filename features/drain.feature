Feature: The system is put down before something takes its containers

  Upgrading rebuilds the sandbox image and removes every sandbox container by name, because a session
  running the old image is a session a build behind and its container's name blocks the new one. Done
  from the daemon, that removal takes whatever was working with it: the operator reads "model: run
  exited: exit status 137, and it said nothing about why" against a conversation they were watching,
  and the row still says the session was born holding skills its container no longer has.

  Draining is the same teardown asked for through the system. Each session is stopped first, so the row
  says stopped and the sandbox is closed rather than ripped out, and a task still working refuses the
  whole thing rather than being interrupted quietly.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: Draining puts a live session down and closes its sandbox
    Given a session started by dispatching "hello"
    When the operator drains the system
    Then the session is reported as stopped
    And every sandbox the system made is closed
    And the drain says it put down 1 session

  Scenario: A drain says which sessions it put down, not how many
    Given a session started by dispatching "hello"
    When the operator drains the system
    Then the drain names the session it put down

  Scenario: Draining a system with nothing live puts nothing down
    When the operator drains the system
    Then the drain says it put down 0 sessions

  Scenario: A session already stopped is not put down twice
    Given a session started by dispatching "hello"
    When the operator stops the session
    And the operator drains the system
    Then the drain says it put down 0 sessions

  # The whole reason this exists. A task in flight is somebody's work, and the upgrade that would have
  # taken it says nothing at all today.
  Scenario: A task still working refuses the drain
    Given the model takes longer over a task than anybody will wait
    And a task dispatched without waiting for it
    And a task is under way
    When the operator drains the system
    Then the control plane refuses it as the wrong state
    And the refusal names what is still working

  # A drain that stopped half the system and then refused would be worse than one that did nothing: the
  # operator waits for the task, drains again, and finds every other conversation already down.
  Scenario: A refused drain leaves every session alone
    Given the model takes longer over a task than anybody will wait
    And a task dispatched without waiting for it
    And a task is under way
    When the operator drains the system
    Then no sandbox the system made is closed
    And the session is reported as running

  Scenario: Draining anyway puts down the session that was working
    Given the model takes longer over a task than anybody will wait
    And a task dispatched without waiting for it
    And a task is under way
    When the operator drains the system anyway
    Then the drain says it put down 1 session
    And the drain says the session was working when it went
    And every sandbox the system made is closed
