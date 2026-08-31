Feature: A job that failed is continued rather than repeated

  A job used to have one way back from a failure: declare the same brief again. The second attempt
  read the same issue, cut the same worktree and made the same discoveries, so the work was paid for
  twice and one slice came back as two branches under two names.

  Most of those failures were not about the work. On the acceptance run of 29 August 2026 the
  container runtime went down and took six jobs with it, a credential ran out sixty seconds into
  another, and a session was stopped while its pull request was already open.

  So a session says what it finishes, one line per step, as it finishes it. The lines are rows, so
  they outlive the container and the controller. A job that failed is continued: it keeps its
  session, its working directory, its branch and its pull request, and the next task carries what is
  already finished rather than the brief. The session is asked to fetch the branch its work is based
  on and say what moved while it was stopped, because it may have moved. That answer is read rather
  than believed: an attempt that says nothing about its base is asked once, and then the job stops
  rather than reading as one that went well.

  The other answer is refusing it. A failure that was the work being wrong must not be offered a
  second attempt, so refusing ends the job, and a job that was ended on purpose is never continued.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"
    And a job titled "sort the listing" in the repository "atlantic-blue/quay-crew" whose session is still working

  Scenario: What a session finished outlives the attempt that failed
    Given the session running that job records "read the issue"
    And the session running that job records "cut the worktree from origin/main"
    When the task the controller sent fails
    Then the job is failed, and it still says the two steps it finished

  # A job that failed after opening its pull request names it nowhere else: no answer landed, so the
  # address the answer would have carried was never read. The step carries it instead.
  Scenario: A job that failed keeps the pull request it opened
    Given the session running that job records "opened https://github.com/atlantic-blue/quay-crew/pull/531"
    When the task the controller sent fails
    Then the failed job names the pull request "https://github.com/atlantic-blue/quay-crew/pull/531"

  # The record is the set of what is finished. A session that is continued says again what it said
  # before, and the earlier steps must not be pushed down a list by it.
  Scenario: The same step twice is one step
    Given the session running that job records "read the issue"
    And the session running that job records "read the issue"
    Then the job says it finished one step

  Scenario: A continued job carries on from the first step it did not finish
    Given the session running that job records "read the issue"
    And the session running that job records "cut the worktree from origin/main"
    And the task the controller sent fails
    When the operator continues the job
    And the controller ticks again
    Then the system was asked to run 2 tasks
    And the second task carries the steps that are finished, and not the brief
    And the second task says what it failed with, and asks what moved under its base
    And the second task ran in the session the first attempt was in

  # Continuing twice would put a second task into a conversation that is already working, which is
  # the bill this whole behaviour exists to stop paying.
  Scenario: Continuing a job that is already going again is refused
    Given the session running that job records "read the issue"
    And the task the controller sent fails
    And the operator continues the job
    When the operator continues the job again
    Then the system refuses it, saying the job has not stopped
    And the controller ticks again
    And the system was asked to run 2 tasks

  # The case that protects the operator. Whether a failure was the run or the work is a person's to
  # decide, and nothing else can.
  Scenario: A job that failed because the work was wrong is refused, and then nothing continues it
    Given the session running that job records "read the issue"
    And the task the controller sent fails
    When the operator refuses the job saying "the migration was wrong, this needs declaring again"
    Then the job is stopped, and the reason carries what the operator decided and what it failed with
    And continuing it is refused, and no second task is sent

  # Continuing is the operator's, the way answering a question is. A session that could continue its
  # own job would be deciding that its own failure was not about the work.
  Scenario: The session doing the job cannot continue it
    Given the session running that job records "read the issue"
    And the task the controller sent fails
    When that session tries to continue its own job
    Then the system refuses it, and the job is still failed

  # The base is the part the system cannot see. A resume puts a session back into the working
  # directory it left, and what that work stands on moved while it was stopped, so the attempt that
  # carries on is asked what moved and the answer is read rather than believed.
  Scenario: A continued job that says nothing about its base is asked what moved
    Given the session running that job records "read the issue"
    And the task the controller sent fails
    And the model will answer "I carried on and the tests pass. Opened https://github.com/atlantic-blue/quay-crew/pull/531"
    When the operator continues the job
    And the controller ticks again
    And the task the controller sent lands
    And the controller ticks again
    Then the session was asked what moved under its base
    And the job is running

  # Asked once and no more. Every ask is a task somebody pays for, and an attempt that may have built
  # on a base nobody looked at must not read as one that went well.
  Scenario: A continued job that still says nothing is stopped rather than called done
    Given the session running that job records "opened https://github.com/atlantic-blue/quay-crew/pull/531"
    And the task the controller sent fails
    And the model will answer "I carried on and the tests pass"
    When the operator continues the job
    And the controller ticks again
    And the task the controller sent lands
    And the controller ticks again
    And the task the controller sent lands
    And the controller ticks again
    Then the job is stopped, and the reason says no answer said what moved under its base
    And the system was asked to run 3 tasks
    And the job still names the pull request "https://github.com/atlantic-blue/quay-crew/pull/531"

  Scenario: A continued job that says what moved under its base is done
    Given the session running that job records "read the issue"
    And the task the controller sent fails
    And the model will answer "Base: nothing moved. Opened https://github.com/atlantic-blue/quay-crew/pull/531"
    When the operator continues the job
    And the controller ticks again
    And the task the controller sent lands
    And the controller ticks again
    Then the job is done, and it names the pull request "https://github.com/atlantic-blue/quay-crew/pull/531"
    And the system was asked to run 2 tasks
