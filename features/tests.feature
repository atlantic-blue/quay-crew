Feature: The requirements a person accepted become failing tests before anything is built

  Requirements became code without ever becoming a failing test first. A session that builds and then
  tests writes the test its own code passes, so the suite records the implementation rather than the
  requirement, and it stays green through the change that breaks the product.

  So the accepted list becomes tests, and the tests fail first. The worker that writes a test is a
  different worker from the one that builds, and it never sees an implementation: at this point in the
  job there is nothing to see, because nothing is built until the suite is red.

  It fans out. One worker for each requirement, all at once, and each one writes the tests for its own
  requirement and nothing else. Two workers must never write the same requirement, so each holds the
  claim on its own, which is the same refusal a second job taking a first job's work already meets. The
  system writes that claim rather than a caller, because a mechanism somebody has to remember is a
  mechanism that gets forgotten.

  The stage closes on a suite that is red for the right reasons. A test that passes before anything is
  built asserts nothing, and a run that finds no tests to execute reports success just the same, so
  both are refused. A requirement whose worker died leaves nothing holding that requirement, and the
  job stops for a person rather than closing on the ones that finished.

  Background:
    Given a running control plane
    And the system listens on an address the tool can dial
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: An accepted list becomes one worker for each requirement, and the suite goes red
    Given a job whose list of 2 verticals a person accepted
    When the controller ticks
    Then a worker is writing the tests for each requirement, and the job itself has no session
    And each worker was given its own requirement and told not to implement it
    When every worker answers with its run
    And the controller ticks again
    Then the row carries a failing test for every requirement
    And every failure says which requirement it came from
    When the caller reads that job back through the tool
    Then the reading says the job is in the "build" stage
    And the reading carries a failing test for every requirement

  # The claim doing the work it already does for anything else. The second declaration is refused by
  # the store, so a second controller ticking the same row buys no second session.
  Scenario: A second worker for one requirement is refused
    Given a job whose list of 2 verticals a person accepted
    When the controller ticks
    And the controller ticks again
    Then 2 workers are writing tests, one for each requirement

  Scenario: A list of one requirement is a fan out of one
    Given a job whose list of 1 vertical a person accepted
    When the controller ticks
    Then 1 worker is writing tests, one for each requirement

  # The two shapes of false green this stage exists to refuse. Both read as success everywhere else.
  Scenario: A run that executed nothing is not a pass
    Given a job whose list of 1 vertical a person accepted
    And the worker will answer that its run executed no tests
    When the controller ticks
    And every worker answers with its run
    And the controller ticks again
    Then the job is asking, and the row carries no failing tests
    And the question says the run found nothing to execute

  Scenario: A run where every test passed is not a pass either
    Given a job whose list of 1 vertical a person accepted
    And the worker will answer that every test passed
    When the controller ticks
    And every worker answers with its run
    And the controller ticks again
    Then the job is asking, and the row carries no failing tests
    And the question says nothing failed

  Scenario: A requirement whose worker died stops the job for a person
    Given a job whose list of 2 verticals a person accepted
    When the controller ticks
    And the worker for requirement 2 dies
    And every worker answers with its run
    And the controller ticks again
    Then the job is asking, and the row carries no failing tests
    And the question names requirement 2

  # The plan is the steps that turn a red suite green, so it is asked for after the tests and never
  # before them.
  Scenario: No plan is written until the suite is red
    Given a job whose list of 2 verticals a person accepted
    When the controller ticks
    Then no plan was written
    When every worker answers with its run
    And the controller ticks again
    Then the row carries a failing test for every requirement
    When the controller ticks again
    Then the session is asked for a plan and told to do no work

  # Where the tests go. Each worker writes them in a sandbox of its own, which the stage after this
  # one never sees, so a worker that only reports on them writes tests nobody can open.
  Scenario: The tests of a job in a repository go on a branch of its own
    Given a job in a repository whose list of 2 verticals a person accepted
    When the controller ticks
    Then a worker is writing the tests for each requirement, and the job itself has no session
    And each worker is told to commit its tests to the branch its requirement's tests live on
