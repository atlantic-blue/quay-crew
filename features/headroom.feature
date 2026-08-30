Feature: The system says how much room the machine has left

  On 27 August 2026 the host ran out of memory. The kernel killed 18 sandboxes, three monitors and a
  build in one event. Nothing in krewe reported it before, during or after, and the console kept
  drawing a healthy system. Every number that mattered had to be read from outside krewe with
  `docker stats`.

  So the system reads the daemon it already talks to, on its own timer, and reports four things: what
  every container holds, the limit that binds, what each sandbox holds, and the pressure on the
  machine underneath. The last two are different questions. The daemon sat at less than half its cap
  the whole time while the machine it ran on was at 94 per cent of its swap.

  Nothing here is estimated. A figure is measured or it reads unknown. An operator stops a session on
  these numbers, so a number the system guessed is a session stopped for nothing.

  The daemon is the one thing these scenarios stand in for, because a scenario cannot make a machine
  run out of memory on purpose. The figures below are the ones the incident recorded.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: The operator reads what the machine holds against the limit that binds
    Given the machine holds 3628 megabytes of a 7837 megabyte limit
    And the system reads the machine
    When the operator asks how much room there is
    Then the system says it holds "3628 MiB" of "7837 MiB"
    And the system says the machine is "room"

  # A Mac with 36 gigabytes and a 7.8 gigabyte cap on its Docker virtual machine is full at 7.8. The
  # limit that binds is the daemon's and never the machine's own memory.
  Scenario: A machine near the daemon's own cap reads full
    Given the machine holds 7200 megabytes of a 7837 megabyte limit
    And the system reads the machine
    When the operator asks how much room there is
    Then the system says the machine is "full"

  # The kill came from the machine underneath while the daemon was at less than half its cap, so the
  # two are reported apart. A system that reported only the daemon would have said there was room.
  Scenario: The machine underneath is reported apart from the daemon
    Given the machine holds 3628 megabytes of a 7837 megabyte limit
    And the machine underneath it is using 16402 megabytes of 17408 megabytes of swap
    And the system reads the machine
    When the operator asks how much room there is
    Then the system says the machine is "room"
    And the system says the swap is "16402 MiB" of "17408 MiB"

  # Rule five of the issue: no number the system cannot measure. Unknown is a valid answer and a guess
  # is not, and a system that could not read the machine must never say there is room.
  Scenario: A system that cannot read the machine says unknown rather than a number
    Given the system cannot read the machine
    And the system reads the machine
    When the operator asks how much room there is
    Then every figure reads unknown
    And the system says the machine is "unknown"
    And the system says why it knows nothing
    And the system still answers everything else

  Scenario: The listing answers which session to stop
    Given the machine holds 3628 megabytes of a 7837 megabyte limit
    And a sandbox holding 2 megabytes for a session that is idle
    And a sandbox holding 1201 megabytes for the session that is working
    And the system reads the machine
    When the operator asks how much room there is
    Then the largest sandbox is listed first
    And each line says what its session is doing and how long since its last task
    And the listing says the largest one is working

  Scenario: The header carries one figure and one word
    Given the machine holds 3628 megabytes of a 7837 megabyte limit
    And the system reads the machine
    When the operator asks how much room there is
    Then the header carries the figure and the word
    And the header says the machine is "room"

  # Full has to be readable without reading the number beside it.
  Scenario: A full machine is readable in the header without reading the number
    Given the machine holds 7200 megabytes of a 7837 megabyte limit
    And the system reads the machine
    When the operator asks how much room there is
    Then the header says the machine is "full"

  # The header redraws every second. Reading the daemon takes as long as the daemon takes, so the
  # header reads the system's last sample and the system reads the machine on a timer of its own.
  Scenario: Asking the system never reads the machine
    Given the machine holds 3628 megabytes of a 7837 megabyte limit
    And the system reads the machine
    When the operator asks how much room there is
    Then the system read the machine once

  # The listing said which session to stop. It never said whether one had to be stopped at all: an
  # operator could read eighteen rows of megabytes, add them up in their head, and still not know how
  # close the machine was. There was no total, no capacity and no headroom anywhere in the view. See
  # issue 457.
  Scenario: The room view says what the machine has left, above the sandboxes
    Given the machine holds 3628 megabytes of a 7837 megabyte limit
    And a sandbox holding 1201 megabytes for the session that is working
    And the system reads the machine
    When the operator opens the room view
    Then the view says "3628 MiB" of "7837 MiB" is held, with "4209 MiB" left
    And the view says what is left in sandboxes
    And that line is above the sandboxes

  # The margin is stated in the unit the operator acts in. A sandbox asks for a measured 1536
  # mebibytes, so a machine with less than that left cannot take another one, whatever fraction of
  # the machine it is.
  Scenario: A machine that will not take another sandbox says so where the operator is reading
    Given the machine holds 7200 megabytes of a 7837 megabyte limit
    And the system reads the machine
    When the operator opens the room view
    Then the view says the machine is "FULL"
    And the view says another sandbox will not fit
