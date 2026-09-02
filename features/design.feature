Feature: A job says what it would build, and a person accepts the list, before it plans

  A job used to go from a reading somebody agreed with straight to a plan. The plan says what the
  system will do, in the order it will do it, and it never says what a person gets or when. So nobody
  was ever asked which deliverable arrives first, and seven steps that all land together are one
  delivery at the end.

  So the job lists what it would build. Each line says what a person can do when that one lands, and
  the line under it says what that person is shown. A person accepts the list, and nothing is planned
  until they do. An answer that is not the acceptance is the correction: the system writes the list
  again from what the person said, and marks the lines they put there as theirs.

  A database is not a deliverable, and nor is a piece of infrastructure. Those are required work
  towards a deliverable, so a schema, a queue and a role are one vertical with its plumbing inside
  them rather than three. The system refuses that list rather than putting it to a person, and the
  refusal is a rule in the code rather than a sentence in the ask.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: The list lands on the row and the job stops for a person
    Given a job that says a person "pastes a link and gets the text back"
    And a person answered what that job understood
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is asking, and the row carries what it would build
    And every vertical says what a person is shown when it lands
    And the question names the sentence and asks whether this list gets it
    And no plan was written

  # The rule that decides whether a list is a list. Three pieces of required work are one deliverable
  # with its plumbing inside it, and a person is never asked to accept them as three.
  Scenario: A list of plumbing is refused and never reaches a person
    Given a job that says a person "pastes a link and gets the text back"
    And a person answered what that job understood
    And the session will answer with a schema, a queue and a role
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then no list reached a person
    And the session is told it listed one vertical with its plumbing inside it

  # The whole point of an answer that is not the acceptance. It costs one task, and the person who
  # says what is wrong writes no list.
  Scenario: Sent back once, the second list is accepted
    Given a job waiting for a person to accept the list it would build
    When the operator answers the job with "the browser one is not needed, an export is"
    And the controller ticks again
    Then the session is sent the list it wrote and what the person said
    And the list is not accepted yet
    When the task the controller sent lands
    And the controller ticks again
    And the operator answers the job with "yes"
    Then the list is accepted
    And the row marks the vertical the person put there as theirs
    When the controller ticks again
    Then the session is asked for a plan carrying the list a person accepted

  Scenario: A list nobody answers moves nothing
    Given a job waiting for a person to accept the list it would build
    When the controller ticks again
    Then the job is still asking, and the list is not accepted yet
    And no plan was written

  Scenario: A second acceptance is refused and the first one stands
    Given a job waiting for a person to accept the list it would build
    When the operator answers the job with "yes"
    And the operator answers the job again with "yes"
    Then the second answer is refused
    And the list is accepted

  # A list of one is a list. The rule folds plumbing into the vertical it serves, so one deliverable
  # carrying everything under it is the ordinary outcome rather than a mistake.
  Scenario: A list of one vertical is put to a person like any other
    Given a job that says a person "pastes a link and gets the text back"
    And a person answered what that job understood
    And the session will answer with one vertical
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is asking, and the row carries what it would build
    And the row carries 1 vertical

  # A reply carrying no list is prose about building. Putting that in front of a person is the fault
  # this stage exists to catch, so the session is asked once more and the job then stops.
  Scenario: A session that answers with no list is asked once more, then the job stops
    Given a job that says a person "pastes a link and gets the text back"
    And a person answered what that job understood
    And the session will answer "I have read the brief and I know what to build"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the session is asked again for the list and told what was wrong
    When the task the controller sent lands
    And the controller ticks again
    Then the job is stopped because it was asked twice
    And the answer the session gave is still on the row

  Scenario: An errand is never asked what it would build
    Given a job titled "read the electricity bill" that claims the answer carries "due"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then that job was never asked what it would build
