Feature: The first run is guided

  A crew that opens empty showed an empty console, and nothing suggested what to do next. The wizard
  behind n makes one thing at a time, which is right for a crew in use and wrong for a first run:
  getting from nothing to a working session took four passes and prior knowledge of the order.

  When the console opens with no workspaces it offers a guided setup, chaining the wizard's own
  stages in the order the crew needs them: a workspace, a project in it, the model token, context
  for the project, a skill from what the crew holds, and a first message. An empty answer skips a
  stage, escape leaves the setup keeping whatever was already made, and once a workspace exists the
  setup is never offered.

  Background:
    Given a running control plane

  Scenario: An empty crew opens into the guided setup
    When the operator opens the console on the empty crew
    Then the guided setup is asking for a workspace name

  Scenario: A crew with a workspace is not offered the setup
    Given a workspace named "acme"
    When the operator opens the console on the empty crew
    Then the console is asking nothing

  Scenario: The guided setup builds a working crew in one pass
    Given the operator imported the "notes" skill
    When the operator opens the console on the empty crew
    And the operator answers the guided setup with:
      | acme                   |
      | house-bills            |
      | sk-ant-oat-a-fake-one  |
      | Bills are paid monthly |
      | notes                  |
      | hello                  |
    Then the crew has 1 workspace
    And the crew has 1 project
    And the crew has 1 session
    And the secrets backend holds a token for the workspace named "acme"
    And the context of the project named "house-bills" says "Bills are paid monthly"
    And the workspace named "acme" holds the skill "notes"
    And the console lists the session the wizard started

  Scenario: The skill stage says a secret a skill names has to be set on the workspace
    Given the operator imported the "git" skill
    When the operator opens the console on the empty crew
    And the operator answers the guided setup with:
      | acme                  |
      | house-bills           |
      | sk-ant-oat-a-fake-one |
      | some context          |
    Then the guided setup mentions "set the secrets it names"

  Scenario: An empty answer skips a stage
    When the operator opens the console on the empty crew
    And the operator answers the guided setup with:
      | acme        |
      | house-bills |
    And the operator skips a stage of the guided setup
    And the operator skips a stage of the guided setup
    And the operator answers the guided setup with:
      | hello |
    Then the crew has 1 session
    And the secrets backend holds nothing for the workspace named "acme"

  # A crew with no skills has nothing to offer at the skill stage, so the stage passes itself over
  # rather than asking a question with no possible answer. The scenario above already walks through
  # it silently; this one pins that the question is never even drawn.
  Scenario: A crew holding no skills is not asked about skills
    When the operator opens the console on the empty crew
    And the operator answers the guided setup with:
      | acme          |
      | house-bills   |
      | sk-ant-oat-ok |
      | some context  |
    Then the guided setup is asking for a first message

  Scenario: Skipping the workspace closes the setup and makes nothing
    When the operator opens the console on the empty crew
    And the operator skips a stage of the guided setup
    Then the console is asking nothing
    And the crew has 0 workspaces

  Scenario: Escape leaves the setup keeping what was already made
    When the operator opens the console on the empty crew
    And the operator answers the guided setup with:
      | acme |
    And the operator leaves the guided setup
    Then the console is asking nothing
    And the crew has 1 workspace

  # Context and the first message both belong to a project, so a skipped project takes them with it
  # rather than asking questions whose answers would have nowhere to go. The token belongs to the
  # workspace and is still asked for.
  Scenario: A skipped project drops the stages that need one
    When the operator opens the console on the empty crew
    And the operator answers the guided setup with:
      | acme |
    And the operator skips a stage of the guided setup
    And the operator answers the guided setup with:
      | sk-ant-oat-a-fake-one |
    Then the console is asking nothing
    And the crew has 0 sessions
    And the secrets backend holds a token for the workspace named "acme"

  Scenario: The context stage reads a file when given its path
    Given a file "briefing.md" saying "The bills live in the shared drive"
    When the operator opens the console on the empty crew
    And the operator answers the guided setup with:
      | acme        |
      | house-bills |
    And the operator skips a stage of the guided setup
    And the operator answers the guided setup with the path to "briefing.md"
    Then the context of the project named "house-bills" says "The bills live in the shared drive"
