Feature: A person approves the plan before any work starts

  A person says one sentence. Something turns that sentence into a brief. The system then executes
  the brief faithfully and fast, and nothing ever holds the brief against the sentence, because
  reading the brief costs nearly as much as reading the result: one of them ran to 1,109 words for a
  1,505 word result. So a misreading of one sentence becomes two days of correct work in the wrong
  direction, and it looks like progress the whole way, because every check is green and every job did
  what it was told.

  A request for an article about what had been built became a diary of throughput. A product built
  from a design document took the video identifier as its key, when what the person wanted was to
  paste a link and get the text back. Every check was green in both.

  So a job that states the sentence writes its plan first, and a person approves it. The plan is at
  most seven steps of one line each, because a plan as long as the work costs the reading and buys
  nothing. An answer of no does not end the job: it replaces the plan, and the session writes the
  next one from what the person said, so nobody has to write a plan by hand.

  The approval is worth nothing on its own, so the work is then held to it. The plan is numbered, the
  session records each step it finishes with its number, and a step nothing accounts for stops the
  job.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A job that states the sentence is asked for its plan and for no work
    Given a job that says a person "pastes a link and gets the text back"
    When the controller ticks
    Then the session is asked for a plan and told to do no work

  Scenario: The plan lands on the row and the job stops for a person
    Given a job that says a person "pastes a link and gets the text back"
    And the session will answer with a plan of 2 steps
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is asking, and the row carries the plan it wrote
    And the question names the sentence and the plan
    And the plan is not approved yet
    And the system was asked to run 1 task

  # The whole point of an answer of no. It costs one task, and the same answer after everything is
  # built costs the job.
  Scenario: Told no, the session writes the plan again from what the person said
    Given a job whose plan is waiting to be approved
    When the operator answers the job with "a reader pastes a link, so do not make them find an identifier first"
    And the controller ticks again
    Then the session is sent the plan it wrote and what the person said
    And the plan is not approved yet

  Scenario: Told yes, the work starts and carries the plan
    Given a job whose plan is waiting to be approved
    When the operator answers the job with "yes"
    And the controller ticks again
    Then the plan is approved
    And the session is sent the brief and the plan it is held to

  # Approval is worth nothing if the work can walk away from the thing that was approved.
  Scenario: A plan approved and then not followed stops the job
    Given a job whose plan was approved
    And the session records step "1: read the design" and nothing else
    When the task the controller sent lands
    And the controller ticks again
    Then the job is stopped, and the reason names the step nothing accounted for
    And the answer the session gave is still on the row

  # A check that always fires is the same as no check.
  Scenario: A plan approved and followed finishes, and says nothing
    Given a job whose plan was approved
    And the session records every step of the plan
    When the task the controller sent lands
    And the controller ticks again
    Then the job is done, and it carries no reason

  # A job that states no sentence is an errand. There is nothing to write a plan from and nothing to
  # hold it against, so nothing stops.
  Scenario: An errand is never asked to plan
    Given a job titled "read the electricity bill" that claims the answer carries "due"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then that job carries no plan and asked nothing
