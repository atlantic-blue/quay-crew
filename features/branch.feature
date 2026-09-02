Feature: One branch carries a requirement from its failing tests to the build that turns them green

  The tests one stage wrote never reached the next one. Each test worker took its own sandbox and its
  own clone, wrote its test files there and answered with three lines, and the sandbox then went away
  with the files in it. The worker that built the same requirement took another fresh clone, and it
  was told to read tests that were not in it. So the boundary that stage works under guarded files
  that were not there, and every check was green the whole time.

  So the work lands on a branch. The worker that writes a requirement's tests cuts the branch the
  system named, pushes it and opens the pull request from it, and that pull request is the one the
  work lands in: it stays open and red, carrying the failing tests for that requirement and nothing
  else. The worker that builds the same requirement fetches the branch, checks it out and turns those
  tests green on it, in the same pull request. The build stage opens none of its own.

  The system names the branch rather than the session, because two workers have to agree on one name
  without either of them being told by the other. Two workers never land on one branch: a branch
  belongs to one requirement, and the claim already refuses a second job taking work a first job
  holds.

  A worker that pushed nothing is a failed worker. Its report can be perfect and the tests it
  describes are gone with the sandbox, so the stage does not close on it.

  Background:
    Given a running control plane
    And the system listens on an address the tool can dial
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: The tests a worker writes reach the worker that builds the same requirement
    Given a job in a repository whose list of 2 verticals a person accepted
    When the controller ticks
    Then each worker was told which branch its requirement's tests go on
    And each worker was told to open its pull request from that branch and leave it red
    When every worker answers with its run
    And the controller ticks again
    And a person approves the plan
    Then the worker building each vertical is on the branch that vertical's tests are on
    And the worker building each vertical is told to check those tests out before it starts
    And each vertical has one pull request, opened by the worker that wrote its tests

  Scenario: A worker whose tests reached no branch stops the job for a person
    Given a job in a repository whose list of 1 vertical a person accepted
    And the worker will answer without opening a pull request
    When the controller ticks
    And every worker answers with its run
    And the controller ticks again
    Then the job is asking, and the row carries no failing tests
    And the question says the tests reached no branch
