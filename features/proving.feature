Feature: A job that designs something is told to prove its riskiest assumption in the runtime

  A product was built to fetch captions from an AWS Lambda function. The hard part looked like the
  attestation token, and bound to the video id the caption endpoint returned 406,491 bytes. That
  measurement was taken on a laptop. Deployed, the same fetch could not read a title out of the watch
  page at all: the runtime got back the page saying there is no video with that id. The assumption
  held everywhere it was tested and failed in the only place it had to hold, and two days of product
  were already sitting on it.

  The answer is a skill rather than a sentence in one role's brief, so it reaches any job that designs
  something rather than the one that hit this. It is given to a fresh system, so a design job is
  offered it without anybody attaching anything: a skill an operator has to attach to the workspace
  doing the designing arrives after the design.

  It names no secret and no binary, so nothing can leave it out of a session, and it costs what any
  skill costs: one line in the memory file, with the brief on disk until the model opens it.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A job that designs something is offered the skill, and nobody attached it
    Given the system started, seeded from the skills this build ships with
    And the operator imports the "designer" role this build ships
    And the operator attaches the "designer" role to the workspace
    And a job titled "design the captions service" in the role "designer"
    When the controller ticks
    Then the session doing that job holds the "proving" skill

  # A design is written wherever the work is, and most of them are not written by a job in the designer
  # role. So the skill sits at the level that reaches every session, and pays the one line that costs.
  Scenario: An ordinary session is offered it too, for one line
    Given the system started, seeded from the skills this build ships with
    When the operator dispatches "hello" to the project
    Then the memory file names the "proving" skill and where its brief is
    And the memory file does not carry "not yet proved"

  # The two things the design has to carry. They are in the brief the session opens, at the path the
  # index names, in the container: a rule that stops at the store is a rule nothing ever reads.
  Scenario: The brief at that path says what the design has to name
    Given the system started, seeded from the skills this build ships with
    When the operator dispatches "hello" to the project
    Then the "proving" brief the session reads says "Riskiest assumption"
    And the "proving" brief the session reads says "Proved where"
    And the "proving" brief the session reads says "not yet proved"

  # A skill needing a secret is left out of a session whose workspace has not set it, and a skill
  # needing a binary the image lacks refuses the task. This one is prose, so neither can happen to it.
  Scenario: A workspace that has set nothing still holds it
    Given the system started, seeded from the skills this build ships with
    When the operator lists the workspace's skills
    Then the listing says the "proving" skill is held by the system
    And the listing says the "proving" skill was left out of nothing
