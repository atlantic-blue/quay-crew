Feature: The crew admits work the machine can host, and holds the rest

  On 30 August 2026 nine jobs were dispatched against a workspace whose `max running` was 8. The
  ninth was admitted, because eight is not nine. It waited two minutes and seven seconds for a
  container and was failed. Then the container runtime stopped answering and exited, taking the
  control plane, the database, the event log and eight running jobs with it.

  A count cannot protect a machine, because sandboxes are not the same size. Ten of them on that
  machine held between 4.3 and 764.5 megabytes.

  So the crew does the arithmetic kubernetes does. A sandbox declares what it needs. The crew reads
  what its runtime has, holds back what its own containers are using, and starts a job only where
  what is already placed plus this one still fits. A job that does not fit stays pending, for as long
  as it takes, and says which resource ran out. It is never admitted and then killed.

  The reserve is the one thing that is not kubernetes. A kubelet runs on the node, outside the pods
  it manages. The crew's own control plane, database and event log are containers inside the same
  runtime the work fills, so the crew holds capacity back for itself or it goes down with its own
  workload.

  The runtime is the one thing these scenarios stand in for, because a scenario cannot fill a machine
  on purpose. Its figures are the ones the incident recorded.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A job the machine has no room for waits, rather than starting and being killed
    Given a runtime holding 7000 megabytes of 7653, with 14 processors
    And the crew reads the machine
    And a job titled "read the electricity bill"
    When the controller ticks
    Then the crew was asked to run 0 tasks
    And the job is waiting for room, and says which resource ran out
    And the job ran in no session

  Scenario: The job runs itself once the machine has room, with nobody declaring it again
    Given a runtime holding 7000 megabytes of 7653, with 14 processors
    And the crew reads the machine
    And a job titled "read the electricity bill"
    When the controller ticks
    Then the job is waiting for room, and says which resource ran out
    When the runtime frees memory, holding 1000 megabytes
    And the crew reads the machine
    And the controller ticks
    Then the crew was asked to run 1 task
    And the job is running
    And the job says nothing about waiting for room

  # The burst that broke the machine. Every one of these was admitted before, because a container
  # appears seconds after the job that asked for it and the reading of the machine is ten seconds
  # wide. The crew counts what it has promised as well as what it has built.
  Scenario: A burst of jobs is admitted only as far as the machine goes
    Given a runtime holding 0 megabytes of 7653, with 14 processors
    And the crew reads the machine
    And 9 jobs titled "read the electricity bill"
    When the controller ticks
    Then the crew was asked to run 3 tasks
    And 6 jobs are waiting for room

  # Memory was not the axis that broke first. Eight sandboxes held 913 per cent of a processor on a
  # fourteen processor machine, and what stopped answering was the daemon.
  Scenario: A machine with memory left and no processors left holds the work
    Given a runtime holding 0 megabytes of 23996, with 1 processor
    And the crew reads the machine
    And a job titled "read the electricity bill"
    When the controller ticks
    Then the crew was asked to run 0 tasks
    And the job is waiting because there is not enough "processor"

  # Rule five of the headroom issue, applied to admission: a crew that cannot read its machine has no
  # arithmetic to do. Refusing everything would stop dead every crew whose sessions do not run on a
  # container runtime at all.
  Scenario: A crew that cannot read its machine still runs the work
    Given the crew cannot read the machine
    And the crew reads the machine
    And a job titled "read the electricity bill"
    When the controller ticks
    Then the crew was asked to run 1 task
    And the job is running
