Feature: A crew says whether it can start work

  A crew is asked whether it is serving, and it answers by writing rather than by reading. The two
  are different questions. A control plane answered every listing in under a second for an hour
  while it started no work at all, and anything that only read from it agreed that it was well.

  So the check writes where a dispatch writes: a row in the store, and a record on the event log.
  Both of those happen before a sandbox is ever asked for, and both of them are what a crew that
  cannot start work is stuck on.

  Background:
    Given a running control plane

  Scenario: A crew that can write says it is serving
    When the crew is asked whether it is serving
    Then the crew answers that it is serving

  Scenario: A crew whose store takes no write is not serving
    Given a store that never takes a write
    When the crew is asked whether it is serving
    Then the crew answers that it is not serving

  Scenario: A crew whose event log never answers is not serving
    Given an event log that never answers
    When the crew is asked whether it is serving
    Then the crew answers that it is not serving
