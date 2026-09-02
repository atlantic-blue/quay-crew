Feature: A job says what it understood, and a person answers, before it plans

  A job used to go from one sentence to a plan without ever finding out whether it understood the
  sentence. The plan gate stopped the work and a person approved seven steps, but nobody was ever
  asked what the sentence meant, so the plan was the session marking its own reading. A misreading
  passes that gate whole, because the steps agree with the misreading all the way down.

  So before it writes a plan, the session says what it understands the work to be and what the work
  is not, lists what it does not know, and marks each footing as something it was told or something
  it filled in itself. Those two read the same on a row today and they are not the same thing. It
  says how sure it is in its own words, and nothing is compared against that: the person is the gate,
  a number is not.

  Then it asks what it cannot answer from the repository, the brief and the sentence, and it stops.
  Nothing but an answer moves it.

  The answer is content rather than consent. A plan is approved by one word; this is answered in
  prose, the prose is kept whole, and the plan is then written from it. An answer that leaves a
  question alone leaves that question unknown rather than taken as agreed, and what the session
  assumed stays marked as an assumption all the way through.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A job that states the sentence is asked what it understood, and told to write no plan
    Given a job that says a person "pastes a link and gets the text back"
    When the controller ticks
    Then the session is asked what it understood and told to write no plan

  Scenario: What it understood lands on the row and the job stops for a person
    Given a job that says a person "pastes a link and gets the text back"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is asking, and the row carries what it understood
    And the record marks what it was told apart from what it assumed
    And the question names the sentence and says there is nothing to approve
    And no plan was written
    And the system was asked to run 1 task

  # The whole point of the stage. What a person writes is kept as they wrote it, and the plan is
  # written from those words rather than from the brief a second time.
  Scenario: The answer is kept whole and the plan is written from it
    Given a job waiting for a person to answer what it understood
    When the operator answers the job with "1: on the command line first, the panel can come later"
    And the controller ticks again
    Then the row carries that answer, word for word
    And the session is asked for a plan carrying that answer and what it assumed

  # A person who writes yes has said nothing about the work. Reading that silence as agreement is
  # the failure this stage exists for.
  Scenario: An answer that touches no question leaves it unknown
    Given a job waiting for a person to answer what it understood
    When the operator answers the job with "yes"
    And the controller ticks again
    Then the session is told which question is still unknown
    And no plan is approved

  Scenario: A second answer is refused and the first one stands
    Given a job waiting for a person to answer what it understood
    When the operator answers the job with "1: on the command line"
    And the operator answers the job again with "2: the panel instead"
    Then the second answer is refused
    And the row still carries the first answer

  # A reply the system cannot read is prose about understanding. Putting that in front of a person is
  # the same fault one level up, so the session is asked once more and the job then stops.
  Scenario: A session that answers with nothing readable is asked once more, then the job stops
    Given a job that says a person "pastes a link and gets the text back"
    And the session will answer "I have read the brief and I understand what to do"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the system was asked to run 2 tasks
    And the session is asked again and told what was wrong
    When the task the controller sent lands
    And the controller ticks again
    Then the job is stopped because it was asked twice
    And the answer the session gave is still on the row

  # A job that states no sentence is an errand: there is nothing to read the work against.
  Scenario: An errand is never asked what it understood
    Given a job titled "read the electricity bill" that claims the answer carries "due"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then that job was never asked what it understood

  # A job declared under another is one part of a plan a person already approved. Stopping at each
  # one puts a person back in the loop for every job in the tree.
  Scenario: A job declared under another is never asked what it understood
    Given the workspace allows jobs down to depth 2
    And a job titled "build the transcript page" saying a person "pastes a link and gets the text back"
    When the session running it declares a job
    Then the new job was never asked what it understood
