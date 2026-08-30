Feature: A controller makes declared jobs happen

  Declaring a job records intent. This is the half that makes reality match it: a loop reads the jobs
  the crew holds, sends a task for what has not started, reads the task back, and writes what came of
  it onto the row.

  The loop never waits on a model. It sends the task and lets go, and reads the answer off the record
  on a later tick, so a task that takes an hour costs the loop nothing.

  It runs pending jobs with nothing outstanding. A job that names a role is run as that role, and a
  job that requires material its role does not receive never reaches a container. A job under a
  parent runs too, because a flow declares every step under its own run. A job that waits for
  something else is left alone, because nothing honours ordering yet.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A declared job runs, and the caller finds it done
    Given a job titled "read the electricity bill"
    When the caller goes away and the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is done, and its answer is what the model said
    And the job says which session did it
    And the job carries the moment it started and the moment it finished

  Scenario: The controller sends one task for one job, however often it ticks
    Given a job titled "read the electricity bill"
    When the controller ticks 3 times
    Then the crew was asked to run 1 task
    And the job is running

  Scenario: A job that is running is left alone until its task lands
    Given a job titled "read the electricity bill"
    And the model takes longer over a task than anybody will wait
    When the controller ticks
    And the controller ticks again
    Then the job is running
    And one task is recorded against that job

  Scenario: A task that failed leaves the job failed, saying why
    Given the next task will fail
    And a job titled "read the electricity bill"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is failed, and the reason says what the model said

  # The model reporting on its own job is what the claim exists to stop.
  # A machine with no room to make a container is not a job that was wrong. This used to be failed,
  # which lost the work: nothing raised it, and the operator had one word in a listing to notice it
  # by. Pending is the state that exists for exactly this. See issue 465.
  Scenario: A job the crew could not give a sandbox waits for room rather than failing
    Given a sandbox that never starts
    And a job titled "read the electricity bill"
    When the controller ticks
    And the crew gives up on the sandbox
    And the controller ticks again
    Then the job is pending, and the reason says it waits for room
    And the records for that job say the job was given up, and never that it failed

  Scenario: The job the machine had no room for runs when the room comes back
    Given a sandbox that never starts
    And a job titled "read the electricity bill"
    When the controller ticks
    And the crew gives up on the sandbox
    And the controller ticks again
    And the machine has room again
    And the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is done, and its answer is what the model said

  Scenario: A job whose answer does not carry what it claimed is stopped
    Given a job titled "read the electricity bill" that claims the answer carries "paid"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is stopped, and the reason names what was claimed
    And the answer is still on the record

  # The reason this exists. An acceptance run took three hours and produced one readable thing at the
  # end, because nothing said a phase ends in a pull request. A job that names a repository carries
  # that expectation itself, so no brief has to remember it.
  Scenario: A session doing a job in a repository is told the job ends in a pull request
    Given a job titled "sort the listing" in the repository "atlantic-blue/quay-crew"
    When the controller ticks
    Then the session was asked to open a pull request against "atlantic-blue/quay-crew", and not to merge

  Scenario: A job whose answer names its pull request is done, and says where the work is
    Given a job titled "sort the listing" in the repository "atlantic-blue/quay-crew"
    And the model will answer "opened https://github.com/atlantic-blue/quay-crew/pull/454"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is done, and it names the pull request "https://github.com/atlantic-blue/quay-crew/pull/454"
    And the crew was asked to run 1 task

  # The refusal. The branch is in the session, the session is open, and opening the pull request is
  # one command, so the session is asked rather than the job being landed with the work invisible.
  Scenario: A job whose answer names no pull request sends the session back for one
    Given a job titled "sort the listing" in the repository "atlantic-blue/quay-crew"
    And the model will answer "I made the change and the tests pass"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is running
    And the crew was asked to run 2 tasks
    And the session was asked again for the pull request against "atlantic-blue/quay-crew"

  Scenario: The session opens the pull request when asked, and the job is done
    Given a job titled "sort the listing" in the repository "atlantic-blue/quay-crew"
    And the model will answer "I made the change and the tests pass"
    And then the model will answer "opened https://github.com/atlantic-blue/quay-crew/pull/454"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    And the task the controller sent lands
    And the controller ticks again
    Then the job is done, and it names the pull request "https://github.com/atlantic-blue/quay-crew/pull/454"

  # Asked once and no more. A session that cannot push would otherwise be asked forever, and every
  # ask is a task somebody pays for.
  Scenario: A session that still names no pull request stops the job rather than being asked again
    Given a job titled "sort the listing" in the repository "atlantic-blue/quay-crew"
    And the model will answer "there is no token here, so I could not push"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    And the task the controller sent lands
    And the controller ticks again
    Then the job is stopped, and the reason names the repository "atlantic-blue/quay-crew"
    And the crew was asked to run 2 tasks
    And the answer is still on the record

  Scenario: A job a person stopped is never started
    Given a job titled "read the electricity bill"
    When the caller stops the first job saying "the bill is not due yet"
    And the controller ticks
    Then the crew was asked to run 0 tasks
    And the job is stopped, and the reason is "the bill is not due yet"

  # Ordering is the one thing still not honoured, so this is the one thing left alone.
  Scenario: A job that waits for something else is left for a later slice
    Given a job titled "read the electricity bill"
    And a job titled "pay the electricity bill" after the first
    When the controller ticks
    And the controller ticks again
    Then the job titled "pay the electricity bill" is pending
    And the crew was asked to run 1 task

  # The reason the whole substrate was built. The job names a role, and the session that runs it
  # runs as that role, so the credential it holds carries what that role declared it may call.
  Scenario: A job in a role runs, in a session running as that role
    Given the workspace holds the role "backlog-clearer" at version 1 receiving "job"
    And a job titled "clear the backlog" in the role "backlog-clearer"
    When the controller ticks
    Then the crew was asked to run 1 task
    And the session doing that job runs as the "backlog-clearer" role

  # The boundary, at the moment the material would be handed over. The role was attached receiving
  # the crew's context when the job was declared, and narrowed while the job sat pending.
  Scenario: A job that requires material its role stopped receiving never reaches a container
    Given the workspace holds the role "test-writer" at version 1 receiving "job, context"
    And a job titled "write the tests" in the role "test-writer" requiring "context"
    And the workspace holds the role "test-writer" at version 2 receiving "job"
    When the controller ticks
    Then the crew was asked to run 0 tasks
    And the crew built 0 sandboxes
    And the job is stopped, saying the "test-writer" role does not receive "context"
    And the job ran in no session

  # A role the workspace no longer holds is a session that would run as nobody, so the job stops
  # rather than running with the boundary gone.
  Scenario: A job naming a role the workspace has given up stops, and names it
    Given the workspace holds the role "backlog-clearer" at version 1 receiving "job"
    And a job titled "clear the backlog" in the role "backlog-clearer"
    And the operator detaches the "backlog-clearer" role from the workspace
    When the controller ticks
    Then the crew was asked to run 0 tasks
    And the job is stopped, and the reason names the "backlog-clearer" role

  # The listing an operator reads while the work is happening. A job is one long task, so a name
  # written behind a task that has answered arrives when the job is over: four jobs running, four
  # blank name cells, and no way to tell which conversation was burning which tokens. The title is
  # typed at declaration, so the crew already has it.
  Scenario: A job's session carries the title it was declared with, while the job is still running
    Given a job titled "read the electricity bill"
    And the model takes longer over a task than anybody will wait
    When the controller ticks
    Then the session doing that job is listed as "read the electricity bill"
    And nothing has described that conversation

  Scenario: Every movement is on the record
    Given a job titled "read the electricity bill"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the records for that job read "job.declared", "job.claimed", "job.started", "job.answered"

  # The failure the whole design opens with: a controller is disposable and the job is not. The
  # task keeps running when its controller goes, because the sandbox belongs to the crew.
  Scenario: The controller is killed after the task starts, and the answer is still adopted once
    Given a job titled "read the electricity bill"
    And the controller that started it goes away after the task starts
    When the task the controller sent lands
    And another controller ticks
    Then the job is done, and its answer is what the model said
    And the crew was asked to run 1 task
    And the records for that job say the job was taken over once, and started once

  Scenario: A controller that is still alive keeps hold of its job
    Given a job titled "read the electricity bill"
    And the model takes longer over a task than anybody will wait
    When the controller ticks
    And another controller ticks
    Then the job is still held by the controller that started it
    And one task is recorded against that job

  # The point of the port is twelve roles a person can use rather than twelve directories, so one of
  # them runs a real job here, end to end, and the answer comes back on the row.
  Scenario: A job runs as a role this build ships
    Given the operator imports every role this build ships
    And the operator attaches the "test-writer" role to the workspace
    And a job titled "write the tests" in the role "test-writer"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is done, and its answer is what the model said
    And the session doing that job runs as the "test-writer" role
