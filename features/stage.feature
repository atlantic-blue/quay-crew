Feature: A job says which of the four stages it is in

  A job says which phase it is in, and a phase says what the system is doing with the row: it is
  pending, it is running, it is asking. It does not say how far through the work the job is. A job
  waiting for an answer about what it understood and a job waiting for an answer about a failed build
  both read "asking", and those two are days apart.

  So a job also says which of the four stages it is in: ideation, design, test and build. The stage
  sits beside the phase and never replaces it. Ideation is what the job understood and assumed, and
  it asks a person before it plans. Design is how the work comes alive as verticals. Test turns the
  requirements into failing tests. Build implements until those tests pass.

  Build is the one that is not written yet. A job that reached it reads as being in build, and the
  reading says build is not built yet, so nobody takes a named stage for a stage that works. It also
  says what that job is doing instead, which is a fact about the job and not about the stage: the
  moment the suite goes red there is no plan at all.

  There is no command that sets a stage. The stage follows from what the job has done, and it is read
  off the row rather than written on it: every boundary is already a fact the row carries, and a
  second copy of a fact could only disagree with it.

  Background:
    Given a running control plane
    And the system listens on an address the tool can dial
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A job nobody has started yet is in the first stage
    Given a job that says a person "pastes a link and gets the text back"
    When the caller reads that job back through the tool
    Then the reading says the job is in the "ideation" stage
    And the reading says what closed the stage before it and what opens the next one
    And the reading does not say the stage is unbuilt

  # The pair is the useful thing to read. The phase says the system is waiting, and the stage says
  # what it is waiting for.
  Scenario: A job waiting for its answer is asking, and still in ideation
    Given a job waiting for a person to answer what it understood
    When the caller reads that job back through the tool
    Then the reading says the job is in the "ideation" stage
    And the reading says the phase is "asking"

  Scenario: The answer closes ideation, and design is where the job stands
    Given a job waiting for a person to answer what it understood
    When the operator answers the job with "1: on the command line first"
    And the caller reads that job back through the tool
    Then the reading says the job is in the "design" stage
    And the reading says the answer closed ideation
    And the reading says accepting the list opens the next stage
    And the reading does not say the stage is unbuilt

  Scenario: The acceptance closes design, and the job writes its failing tests next
    Given a job waiting for a person to accept the list it would build
    When the operator answers the job with "yes"
    And the caller reads that job back through the tool
    Then the reading says the job is in the "test" stage
    And the reading says the acceptance closed design
    And the reading says a failing test for every requirement opens the next stage
    And the reading does not say the stage is unbuilt

  Scenario: A red suite closes test, and build says it is not built
    Given a job whose list of 2 verticals a person accepted
    And its requirements became failing tests
    When the caller reads that job back through the tool
    Then the reading says the job is in the "build" stage
    And the reading says the stage is not built yet
    And the reading says the job writes its plan next
    And the reading does not claim a plan nobody approved

  # A job whose plan a person approved is doing something different, and the same line has to say so
  # rather than saying one thing for the whole stage.
  Scenario: A job working to an approved plan is told it is carrying on under it
    Given a job whose plan was approved
    When the caller reads that job back through the tool
    Then the reading says the job carries on under the plan a person approved

  # The whole reason the listing carries the column: a job stuck at the beginning and a job stuck
  # further on read differently at a glance.
  Scenario: The listing tells a job in ideation from one that moved on
    Given a job waiting for a person to answer what it understood
    When the caller lists the jobs through the tool
    Then the listing carries the stage "ideation"
    When the operator answers the job with "1: on the command line first"
    And the caller lists the jobs through the tool
    Then the listing carries the stage "design"

  # A job that states no sentence is an errand: there is nothing to read the work against, so it
  # never enters the stages at all, and both surfaces say so rather than naming one.
  Scenario: An errand runs no stages, and says why
    Given a job titled "read the electricity bill" that claims the answer carries "due"
    When the caller reads that job back through the tool
    Then the reading says the job is in no stage
    When the caller lists the jobs through the tool
    Then the listing carries no stage for that job
