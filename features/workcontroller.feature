Feature: A controller makes declared work happen

  Declaring work records intent. This is the half that makes reality match it: a loop reads the work
  the crew holds, sends a task for what has not started, reads the task back, and writes what came of
  it onto the row.

  The loop never waits on a model. It sends the task and lets go, and reads the answer off the record
  on a later tick, so a task that takes an hour costs the loop nothing.

  It runs root work in this slice: work with no parent and nothing it waits for. Work that names a
  role is run as that role, and work handed material its role does not receive never reaches a
  container.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: Declared work runs, and the caller finds it done
    Given a piece of work titled "read the electricity bill"
    When the caller goes away and the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the work is done, and its answer is what the model said
    And the work says which session did it
    And the work carries the moment it started and the moment it finished

  Scenario: The controller sends one task for one piece of work, however often it ticks
    Given a piece of work titled "read the electricity bill"
    When the controller ticks 3 times
    Then the crew was asked to run 1 task
    And the work is running

  Scenario: Work that is running is left alone until its task lands
    Given a piece of work titled "read the electricity bill"
    And the model takes longer over a task than anybody will wait
    When the controller ticks
    And the controller ticks again
    Then the work is running
    And one task is recorded against that work

  Scenario: A task that failed leaves the work failed, saying why
    Given the next task will fail
    And a piece of work titled "read the electricity bill"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the work is failed, and the reason says what the model said

  # The model reporting on its own work is what the claim exists to stop.
  Scenario: Work whose answer does not carry what it claimed is stopped
    Given a piece of work titled "read the electricity bill" that claims the answer carries "paid"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the work is stopped, and the reason names what was claimed
    And the answer is still on the record

  Scenario: Work a person stopped is never started
    Given a piece of work titled "read the electricity bill"
    When the caller stops the first piece of work saying "the bill is not due yet"
    And the controller ticks
    Then the crew was asked to run 0 tasks
    And the work is stopped, and the reason is "the bill is not due yet"

  # The rest of the loop is later slices. What this one runs is a root and nothing else.
  Scenario: Work that waits for something else is left for a later slice
    Given a piece of work titled "read the electricity bill"
    And a piece of work titled "pay the electricity bill" after the first
    When the controller ticks
    And the controller ticks again
    Then the work titled "pay the electricity bill" is pending
    And the crew was asked to run 1 task

  # The reason the whole substrate was built. The work names a role, and the session that runs it
  # runs as that role, so the credential it holds carries what that role declared it may call.
  Scenario: Work in a role runs, in a session running as that role
    Given the workspace holds the role "backlog-clearer" at version 1 receiving "work"
    And a piece of work titled "clear the backlog" in the role "backlog-clearer"
    When the controller ticks
    Then the crew was asked to run 1 task
    And the session doing that work runs as the "backlog-clearer" role

  # The boundary, at the moment the material would be handed over. The role was attached receiving
  # the crew's context when the work was declared, and narrowed while the work sat pending.
  Scenario: Work handed material its role stopped receiving never reaches a container
    Given the workspace holds the role "test-writer" at version 1 receiving "work, context"
    And a piece of work titled "write the tests" in the role "test-writer" handed "context"
    And the workspace holds the role "test-writer" at version 2 receiving "work"
    When the controller ticks
    Then the crew was asked to run 0 tasks
    And the crew built 0 sandboxes
    And the work is stopped, saying the "test-writer" role does not receive "context"
    And the work ran in no session

  # A role the workspace no longer holds is a session that would run as nobody, so the work stops
  # rather than running with the boundary gone.
  Scenario: Work naming a role the workspace has given up stops, and names it
    Given the workspace holds the role "backlog-clearer" at version 1 receiving "work"
    And a piece of work titled "clear the backlog" in the role "backlog-clearer"
    And the operator detaches the "backlog-clearer" role from the workspace
    When the controller ticks
    Then the crew was asked to run 0 tasks
    And the work is stopped, and the reason names the "backlog-clearer" role

  Scenario: Every movement is on the record
    Given a piece of work titled "read the electricity bill"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the records for that work read "work.declared", "work.claimed", "work.started", "work.answered"

  # The failure the whole design opens with: a controller is disposable and the work is not. The
  # task keeps running when its controller goes, because the sandbox belongs to the crew.
  Scenario: The controller is killed after the task starts, and the answer is still adopted once
    Given a piece of work titled "read the electricity bill"
    And the controller that started it goes away after the task starts
    When the task the controller sent lands
    And another controller ticks
    Then the work is done, and its answer is what the model said
    And the crew was asked to run 1 task
    And the records for that work say the work was taken over once, and started once

  Scenario: A controller that is still alive keeps hold of its work
    Given a piece of work titled "read the electricity bill"
    And the model takes longer over a task than anybody will wait
    When the controller ticks
    And another controller ticks
    Then the work is still held by the controller that started it
    And one task is recorded against that work
