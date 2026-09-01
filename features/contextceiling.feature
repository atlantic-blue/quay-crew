Feature: A session stops taking work before its context window is full

  A session used to run until its context window was full. The system printed the share in a column
  and did nothing with it, so a session at eighty per cent kept taking tasks, and the last task of a
  long job is the one that opens the pull request and writes the answer. The work that mattered most
  was done at the point where the model is worst, and nothing failed: the job finished and looked
  exactly like one that went well.

  So a workspace declares a ceiling, and past it the system gives that session no new task on the job
  it is doing. It asks for one thing instead. The session writes down what is left and what it tried
  that did not work, and the rest of the job goes to a conversation with an empty window, which is
  given the brief, what is already finished, and those words.

  The ceiling ships at 70 per cent. That number comes from a standard, which says quality falls off
  between 50 and 70 per cent of a window and is poor past 70, and from no measurement of this system.
  A workspace sets its own with krewe limits, and 100 turns the gate off.

  Two silences would each undo this. A window nothing has measured must not refuse anything: the size
  of a window is what the model runtime last told a session, and a system nobody has told would
  otherwise stop every job on it. And a session that writes no handoff must not have a fresh one
  started from nothing, because that session would pay for every discovery the last one made and then
  read afterwards exactly like a handover that went well.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # The number is a share of the model's context window. One the system could not act on is refused
  # while the operator is looking, rather than hours later inside a run.
  Scenario: A ceiling that is not a share is refused
    When the operator sets the workspace's context ceiling to 140 per cent
    Then the control plane refuses it, saying a ceiling is a share of the window

  Scenario: A workspace that says nothing takes the system's own ceiling
    When the operator reads the workspace's limits
    Then the ceiling reads 70 per cent, and says it comes from a standard rather than a measurement

  Scenario: A workspace declares its own ceiling, and reads it back
    When the operator sets the workspace's context ceiling to 55 per cent
    And the operator reads the workspace's limits
    Then the ceiling reads 55 per cent

  # The silence that would stop the system. Nothing can work out how big a model's context window is,
  # so until a session has been told, a full conversation and an empty one look the same from outside.
  Scenario: A session whose window nothing measured is asked for the pull request as before
    Given a job titled "sort the listing" in the repository "atlantic-blue/quay-crew" whose session is still working
    And that session has carried 900000 tokens of context
    When the task the controller sent lands
    And the controller ticks again
    Then the session was asked for the pull request rather than for a handoff

  Scenario: A session under the ceiling is asked for the pull request as before
    Given the model runtime told the workspace its context window holds 1000000
    And a job titled "sort the listing" in the repository "atlantic-blue/quay-crew" whose session is still working
    And that session has carried 260000 tokens of context
    When the task the controller sent lands
    And the controller ticks again
    Then the session was asked for the pull request rather than for a handoff

  # The whole of what this buys.
  Scenario: A session past the ceiling is asked to hand over instead of being asked for the pull request
    Given the model runtime told the workspace its context window holds 1000000
    And a job titled "sort the listing" in the repository "atlantic-blue/quay-crew" whose session is still working
    And that session has carried 820000 tokens of context
    When the task the controller sent lands
    And the controller ticks again
    Then the session was asked to hand over, and told to push its branch first
    And the job is running

  # The refusal that decides whether the record is worth having. A test that a second session starts
  # passes whether or not the handoff carries anything.
  Scenario: A handoff that says nothing is refused
    Given the model runtime told the workspace its context window holds 1000000
    And a job titled "sort the listing" in the repository "atlantic-blue/quay-crew" whose session is still working
    And that session has carried 820000 tokens of context
    And the task the controller sent lands
    And the controller ticks again
    When the session running that job hands over nothing
    Then the control plane refuses it, saying a handoff says what is left

  Scenario: A session that writes no handoff stops the job rather than starting a fresh one from nothing
    Given the model runtime told the workspace its context window holds 1000000
    And a job titled "sort the listing" in the repository "atlantic-blue/quay-crew" whose session is still working
    And that session has carried 820000 tokens of context
    And the task the controller sent lands
    And the controller ticks again
    When the task the controller sent lands
    And the controller ticks again
    Then the job is stopped, and the reason says nothing was handed over
    And the system was asked to run 2 tasks

  Scenario: The rest of the job goes to a fresh session carrying what the last one wrote down
    Given the model runtime told the workspace its context window holds 1000000
    And a job titled "sort the listing" in the repository "atlantic-blue/quay-crew" whose session is still working
    And the session running that job records "read the issue"
    And that session has carried 820000 tokens of context
    And the task the controller sent lands
    And the controller ticks again
    When the session running that job hands over "the index is written, the query still reads the old one: branch 539-feat-index" having tried "adding the index inside the renaming migration, which deadlocks"
    And the task the controller sent lands
    And the controller ticks again
    Then the rest of the job went to a session the first one was not in
    And that session was told what is left, what was tried, what is finished, and the brief
    And it is the same job, with the step the first session finished
    And the system was asked to run 3 tasks

  # The console answers the question an operator actually has, which is not what the share is but
  # what the system is about to do about it.
  Scenario: The listing says which sessions are near the ceiling and which are past it
    Given the model runtime told the workspace its context window holds 1000000
    And a job titled "sort the listing" in the repository "atlantic-blue/quay-crew" whose session is still working
    And that session has carried 820000 tokens of context
    When the operator lists the sessions
    Then the row for that session reads "82% over"

  Scenario: A session inside the band below the ceiling is marked near it
    Given the model runtime told the workspace its context window holds 1000000
    And a job titled "sort the listing" in the repository "atlantic-blue/quay-crew" whose session is still working
    And that session has carried 550000 tokens of context
    When the operator lists the sessions
    Then the row for that session reads "55% near"

  Scenario: A session well under the ceiling is marked with nothing
    Given the model runtime told the workspace its context window holds 1000000
    And a job titled "sort the listing" in the repository "atlantic-blue/quay-crew" whose session is still working
    And that session has carried 260000 tokens of context
    When the operator lists the sessions
    Then the row for that session reads "26%"
