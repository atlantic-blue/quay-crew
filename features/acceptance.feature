Feature: A job is not done until a person has looked at a picture of it running

  The build stage closes on three checks: the run executed something, nothing fails, and a file was
  written to make it pass. All three are the machine reading its own work. A session that finishes
  the work writes the answer that says the work is right, and it says so in good faith, because from
  inside the session there is nothing to compare against.

  So there is a fourth check and a person makes it. It is visual, and that is the whole of it: a
  screenshot or a recording of the built thing actually running. Not a description of it working, not
  a passing test named after it, and not a sample generated to illustrate what it would look like. A
  picture and a paragraph both read as evidence on the page and are worth completely different
  amounts.

  Every picture carries a label saying where it came from and what it takes to reproduce it, and the
  label is part of the record rather than part of the prose. A reader who cannot reproduce a picture
  concludes the code does not do what was claimed, and they are right to.

  Then the job holds, and only the person's word lands it. An answer that is not the acceptance says
  what was missing, and the verticals go back to the build stage to be built again from what they
  said. A job whose verticals are built and that tries to settle on its own answer is stopped,
  whatever its own checks said.

  This is not a fifth stage. The four are ideation, design, test and build, and this closes the last
  one.

  Background:
    Given a running control plane
    And the system listens on an address the tool can dial
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A built job holds with a picture of every vertical, and the picture carries its label
    Given a job whose plan a person approved and whose suite is red for 2 verticals
    When the controller ticks
    And every worker answers with its run
    And the controller ticks again
    Then the row carries a picture of every vertical running
    And every picture says where it came from and how to get it again
    And the question names each picture and where to open it

  # The gate. Nothing but a person moves this job, and a system that moved it on by itself would be
  # a system that accepted the work on their behalf.
  Scenario: An acceptance that never comes leaves the job exactly where it is
    Given a job whose plan a person approved and whose suite is red for 1 vertical
    When the controller ticks
    And every worker answers with its run
    And the controller ticks again
    And the controller ticks 3 more times with nobody answering
    Then the job is waiting for a person to accept what was built
    And the job is not accepted

  # Their word is permission rather than an ending. The job still owes the pull request its work is
  # read in, so what the word opens is the road to that, and nothing reaches done without it.
  Scenario: The person's word is what lets the job end
    Given a job whose plan a person approved and whose suite is red for 1 vertical
    When the controller ticks
    And every worker answers with its run
    And the controller ticks again
    And the operator answers the job with "yes"
    And the controller ticks again
    Then the record says a person accepted it
    And the row still carries the picture they looked at
    When the controller ticks again
    Then the session finishing the job is told they said the value arrived, and to build nothing more

  # The other answer. What they said is what the next build is held to, so it stays on the row.
  Scenario: An answer that is not the acceptance sends the verticals back to be built
    Given a job whose plan a person approved and whose suite is red for 1 vertical
    When the controller ticks
    And every worker answers with its run
    And the controller ticks again
    And the operator answers the job with "the picture shows an empty page, the link is not read"
    And the controller ticks again
    Then the job is pending, and the row carries nothing built
    And the row still carries what the person said
    When the controller ticks again
    Then a second worker is building that vertical

  # The three shapes a picture is missing in. Each of them reads as a finished build everywhere else,
  # and none of them is something a person can look at.
  Scenario: A vertical with no picture is not built
    Given a job whose plan a person approved and whose suite is red for 1 vertical
    And the builder will answer with no picture of what it built
    When the controller ticks
    And every worker answers with its run
    And the controller ticks again
    Then the job is asking, and the row carries nothing built
    And the question says nothing shows the vertical working

  Scenario: A picture with no label is not a picture anybody can use
    Given a job whose plan a person approved and whose suite is red for 1 vertical
    And the builder will answer with a picture that carries no label
    When the controller ticks
    And every worker answers with its run
    And the controller ticks again
    Then the job is asking, and the row carries nothing built
    And the question says the picture carries no label

  Scenario: A sample generated to illustrate is refused by name
    Given a job whose plan a person approved and whose suite is red for 1 vertical
    And the builder will answer with a picture it generated to illustrate
    When the controller ticks
    And every worker answers with its run
    And the controller ticks again
    Then the job is asking, and the row carries nothing built
    And the question says a sample is not a capture

  # The last one, and the one that names this stage. Whatever a person says short of the word, the
  # job does not end: it goes back to be built, and every tick after that leaves it in front of them.
  #
  # There is a second guard behind this, on the ordinary road a job settles by, and it is a unit test
  # rather than a scenario. Nothing a person can drive reaches it: a job whose verticals are built is
  # taken by this stage before it can get a session, and a stopped job is never continued. It is
  # TestAJobCannotCallItselfDoneWithNobodyHavingLookedAtAPicture in internal/job.
  Scenario: Nothing short of the word ends a job whose verticals are built
    Given a job whose plan a person approved and whose suite is red for 1 vertical
    When the controller ticks
    And every worker answers with its run
    And the controller ticks again
    And the operator answers the job with "it all looks finished to me, carry on"
    And the controller ticks 6 times
    Then the job did not end, and nobody accepted it
