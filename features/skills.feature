Feature: A session is given the skills the crew has

  A session opens knowing nothing about how the operator works. A skill is a capability written down
  as code: a brief the model reads, the binaries it needs, the secrets it names, and its own setup.
  The design is in docs/SKILLS.md.

  These scenarios drive the control plane over its real interface. The sandbox is a double, so they
  say what a session is given rather than that a real daemon mounted it; the mounting itself is proved
  against Docker in the sandbox package.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # A crew with no skills is every crew before this, and the memory file must not grow a heading for
  # something that does not exist.
  Scenario: A session with no skills is told nothing about any
    When the operator dispatches "hello" to the project
    Then the session's memory file mentions no skill

  # The brief is what the model reads on every turn. It is in the file it already reads, marked, the
  # same way every other level of context is.
  Scenario: A skill's brief is in the file the session reads
    Given the crew has a skill "git" that says "Branch first. Stage named files."
    When the operator dispatches "hello" to the project
    Then the session's memory file carries "Branch first. Stage named files."
    And the session's memory file says where the rest of the git skill is

  # The detail costs nothing until the model opens it, which is the whole reason a brief can be short.
  Scenario: The rest of a skill is mounted, and is not in the memory file
    Given the crew has a skill "git" that says "Branch first."
    And the git skill has a file "reference.md" saying "every flag, at length"
    When the operator dispatches "hello" to the project
    Then the sandbox mounts the git skill read only
    And the session's memory file does not carry "every flag, at length"

  # A skill is code the operator wrote and the session is given, not something it edits: a session
  # that can rewrite its own instructions can give itself a capability nobody approved.
  Scenario: A session cannot write to its skills
    Given the crew has a skill "git" that says "Branch first."
    When the operator dispatches "hello" to the project
    Then the sandbox mounts the git skill read only

  # A brief is rendered from the skill's own file every turn. Taken into the crew's context it would
  # be stored, then rendered beside itself, then again, which is exactly what happens to unmarked text
  # in a memory file and is by design. A brief is marked so it cannot be mistaken for that.
  Scenario: A skill's brief is never taken into the session's own context
    Given the crew has a skill "git" that says "Branch first."
    When the operator dispatches "hello" to the project
    And the operator dispatches "and again" to the same thread
    Then the session's own context says nothing about the git skill
    And the session's memory file carries "Branch first." exactly once

  # A capability that silently does not work is worse than one that is absent, because the model
  # improvises around it and the operator reads the improvisation as the answer.
  Scenario: A skill needing a secret the workspace has not set is refused before the turn runs
    Given the crew has a skill "github" needing the secret "GH_TOKEN"
    When the operator dispatches "hello" to the project
    Then the control plane refuses it as the wrong state
    And the refusal names the secret and how to set it
    And no sandbox has been created

  Scenario: A skill needing a binary the image does not carry is refused, and names the image
    Given the crew has a skill "github" needing the binary "gh"
    And the sandbox image does not carry "gh"
    When the operator dispatches "hello" to the project
    Then the control plane refuses it as the wrong state
    And the refusal names the binary and the image to add it to

  # A session gets what its skills name and nothing else, which is the rule the whole design turns on.
  Scenario: A skill's secret reaches the sandbox, and nothing else does
    Given the crew has a skill "github" needing the secret "GH_TOKEN"
    And the workspace has the secret "GH_TOKEN" set to "ghp-1234"
    And the workspace has the secret "STRIPE_KEY" set to "sk-live-nobody-asked"
    When the operator dispatches "hello" to the project
    Then the sandbox carries "GH_TOKEN" set to "ghp-1234"
    And the sandbox carries nothing called "STRIPE_KEY"
