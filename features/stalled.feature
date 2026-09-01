Feature: The system does not sit with work held and nothing running

  On 31 August 2026 twenty five jobs were declared at once. The workspace allowed eight running.
  Fifteen finished. Then nothing ran at all.

  Five jobs sat held, each saying that a sandbox asks for 100 per cent of a processor and that 0 per
  cent of 1200 per cent is unallocated. Twelve sandboxes were idle, every one of them for an hour or
  more, and between them they held the whole processor allocation. The workspace reclaim time was
  thirty minutes and not one container came back. An operator drained thirty three sessions by hand
  to free a resource the reclaim was already meant to free.

  A sandbox reserves the room its job was admitted with, and gives it back when its container goes
  and at no other moment. A session outlives its job, so an idle sandbox holds a whole processor
  while using almost none of one. That is correct scheduler arithmetic and it is what kubernetes
  does. The mismatch is that a pod ends and a session does not.

  So the controller gets a fifth comparison, in the same loop as the other four. The first four make
  reality match what was declared. This one asks whether they are working. Nothing running with
  something held is a state a system can read in one query and it is always wrong, so the system
  takes back the container that has been idle longest and starts again.

  It is the pair and never the pressure. A full machine with jobs running on it is a healthy machine,
  and taking a container back there takes it from a session that is about to get its next task.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # The whole fault and the whole answer, on a machine with room for three sandboxes and five jobs
  # asking for one each. The queue fills, the machine runs out of processors, and the sandboxes the
  # first three jobs left behind hold every one of them.
  #
  # The machine being full is asserted, and so is a job having been turned away, because a recovery
  # tested against a queue that was never stopped proves nothing.
  Scenario: A queue that has stopped starts itself again
    Given a runtime holding 0 megabytes of 23996, with 5 processors
    And the system reads the machine
    And 5 jobs titled "read the electricity bill"
    When the controller ticks
    And the tasks the controller sent land
    Then some jobs are waiting for room
    And every sandbox on the machine is idle
    And the machine has no room left
    When the controller works until nothing moves
    Then the machine turned some of those jobs away
    And a container was taken back to make room
    And every job is done

  # The state the system cannot leave on its own, and the sentence it leaves for a person. An
  # operator is in every container, so the one thing that would give the room back is the one thing
  # the system must not touch.
  Scenario: A queue that cannot start itself says so on every job that is waiting
    Given a runtime holding 0 megabytes of 23996, with 4 processors
    And the system reads the machine
    And an operator is in every container
    And 4 jobs titled "read the electricity bill"
    When the controller works until nothing moves
    Then no container was taken back
    And every sandbox on the machine is idle
    And the machine has no room left
    And every job waiting for room says the system is stopped rather than busy

  # The guard, stated on its own. A busy machine is not a stopped one, and a job waiting behind
  # a job that is working is waiting correctly.
  Scenario: A machine with a job running on it keeps its containers
    Given a runtime holding 0 megabytes of 23996, with 3 processors
    And the system reads the machine
    And a job titled "the long one"
    And the model takes longer over a task than anybody will wait
    When the controller ticks
    And a job titled "the one behind it"
    And the controller ticks again
    Then no container was taken back
    And the job "the one behind it" says nothing about the system being stopped
