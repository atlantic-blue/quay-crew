Feature: The operator drives flows from the command line

  A flow engine nothing can reach delivers nothing. The operator imports a graph, starts a run of it
  in a project, and reads back what the run did, all through the same authenticated interface every
  other caller uses.

  Starting a run answers with the run's identifier rather than waiting for it: a run dispatches
  turns, which take as long as the model takes, and a command line that hangs for ten minutes is a
  command line nobody uses. The run advances behind that answer, and reading it back says where it
  got to.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A graph is imported and a run of it can be read back
    When the operator imports the flow graph "fix-red"
    And the operator starts a run of "fix-red" in the project
    Then the run finishes
    And reading the run back says it ran "fix-red" version 1
    And the run is listed among the project's runs

  Scenario: A graph a run could fall off is refused at import, naming what is wrong
    When the operator imports a flow graph whose edge leads nowhere
    Then the control plane refuses it as invalid
    And the refusal says "nowhere"

  Scenario: The same version imported twice is refused, because a run pins the version it started with
    When the operator imports the flow graph "fix-red"
    And the operator imports the flow graph "fix-red" again
    Then the control plane refuses it as the wrong state
    And the refusal says "already imported"

  Scenario: Starting a flow nobody imported is refused
    When the operator starts a run of "never-imported" in the project
    Then the control plane refuses it as not found

  Scenario: A run reports which node it ended on and what it was told
    When the operator imports the flow graph "fix-red"
    And the operator starts a run of "fix-red" in the project
    Then the run finishes
    And reading the run back says it ended on "done"
    And reading the run back carries what the last turn replied

  # The reason the brakes exist: an automation dispatches turns with nobody watching, so a graph
  # that cycles would spend until somebody noticed the bill.
  Scenario: A cycling graph stops itself at its cap, and says so
    When the operator imports a flow graph that cycles, capped at 4 transitions
    And the operator starts a run of "loop" in the project
    Then the run stops
    And reading the run back says it was stopped for hitting its cap
    And the run's thread was asked no more than 4 turns

  Scenario: A graph whose cap could never be met is refused at import
    When the operator imports a flow graph capped at 0 transitions
    Then the control plane refuses it as invalid

  # The driver is a session that can drive the crew. Writing an automation down is the operator's
  # act, the way importing a skill is; running one is dispatch, which the driver already has.
  Scenario: The driver cannot import a flow graph
    When the driver imports a flow graph
    Then the driver is refused, told the call is the operator's to make

  Scenario: The driver can start a run of a graph the operator imported
    Given the operator imports the flow graph "fix-red"
    When the driver starts a run of "fix-red" in the project
    Then the driver is served
