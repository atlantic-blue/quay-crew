Feature: A system says whether it can start work

  A system is asked whether it is serving, and it answers by writing rather than by reading. The two
  are different questions. A control plane answered every listing in under a second for an hour
  while it started no work at all, and anything that only read from it agreed that it was well.

  So the check writes where a dispatch writes: a row in the store, and a record on the event log.
  Both of those happen before a sandbox is ever asked for, and both of them are what a system that
  cannot start work is stuck on.

  The answer then has to reach a person. A system told a docker health check it was not serving 1,467
  times in a row while an operator worked through the console all day and saw nothing, so the system
  keeps what each write found, and every row of the console's stats view says it.

  The operator was not in the console for most of it. They were typing commands, so every command
  that talks to the system says on standard error when the system is reporting itself not serving, and
  names the part that is down and why.

  Background:
    Given a running control plane

  Scenario: A system that can write says it is serving
    When the system is asked whether it is serving
    Then the system answers that it is serving

  Scenario: A system whose store takes no write is not serving
    Given a store that never takes a write
    When the system is asked whether it is serving
    Then the system answers that it is not serving

  Scenario: A system whose event log never answers is not serving
    Given an event log that never answers
    When the system is asked whether it is serving
    Then the system answers that it is not serving

  # The console's stats view was open for the whole sixteen hours the event log was dead, and it drew
  # that row in the colour of a working one. Every row it made was ready, because the view was built
  # from configuration and configuration had not changed. See issue 458.
  Scenario: The stats view says which part of the system is down
    Given an event log that never answers
    When the system probes itself
    And the operator opens the console on stats
    Then the stats view says the "Events engine" is "down"
    And the stats view says the "Store engine" is "serving"
    And the operator reads "Events engine" as "down" on the screen

  # Four of the six rows have nothing probing them. Saying so is the point: green on a part nobody
  # read is the same claim the events row made for sixteen hours.
  Scenario: The stats view says nothing about a part nothing probes
    When the system probes itself
    And the operator opens the console on stats
    Then the stats view says the "Model" is "not checked"
    And the stats view says the "Sandbox engine" is "not checked"

  Scenario: A system that has probed nothing claims nothing about itself
    When the operator opens the console on stats
    Then the stats view says the "Store engine" is "not checked"

  # The system knew for sixteen hours. It said so 1,467 times to a container health check nobody
  # watches, while an operator worked through the command line tool all day against a system that
  # answered every read. So the tool asks, and says it where the operator is already looking. See
  # issue 445.
  Scenario: A command against a system that is not serving names the part that is down
    Given an event log that never answers
    And the system listens on an address the tool can dial
    And a workspace named "acme"
    When the system probes itself
    And the caller lists the workspaces
    Then standard error says this system is not serving and names "events"
    And standard error says which write did not land
    And standard output carries the answer and nothing about the system being down
    And the command succeeds

  Scenario: A system that takes every write says nothing on the error stream
    Given the system listens on an address the tool can dial
    When the system probes itself
    And the caller asks the tool for the version
    Then standard error says nothing

  # A system comes up before its first probe lands. Reporting a part nothing has looked at as down is
  # the same claim as drawing it healthy, made in the other direction.
  Scenario: A system that has probed nothing is not reported as down
    Given an event log that never answers
    And the system listens on an address the tool can dial
    When the caller asks the tool for the version
    Then standard error says nothing about the system being down
    And the command succeeds
