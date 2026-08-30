Feature: Infrastructure is not ready until the identity that applies it is proved

  A job wrote the infrastructure for a service and opened a pull request. Every check went green in
  eleven seconds. The checks were a format check and a validate, and neither one talks to the cloud
  account. The pull request merged, and the deploy died on the first command that did: the identity
  that runs it held read only access, and could not have created any of the six resources. Granting
  the one action it named would have moved the failure to the next resource, one deploy each.

  Amazon answers that question without creating anything, through iam:SimulatePrincipalPolicy.
  Nothing asked the job to ask it, so it did not.

  So the rule is a skill, and a fresh system gives it to every session rather than waiting to be
  told. A rule that arrives when somebody attaches it is missing in every system nobody set up, and
  that is where this failure happens.

  Background:
    Given a running control plane
    And a workspace named "atlantic-blue"
    And a project named "transcript"

  # Nothing is imported, nothing is attached, and no secret is set. This is a system on its first day.
  Scenario: A fresh system puts the rule in front of every session
    When the system starts, seeded from the skills this build ships with
    Then the workspace holds the "deploy-identity" skill
    And the listing says the "deploy-identity" skill is held by the system

  Scenario: A job that writes infrastructure is told the rule without asking for it
    Given the system started, seeded from the skills this build ships with
    When the operator dispatches "write the terraform for the transcript service" to the project
    Then the memory file names the "deploy-identity" skill and where its brief is

  # The rule is worth exactly what its page says, so the page is held to it: the question to ask, the
  # thing that stops the work, and the two traps that each cost a deploy here.
  Scenario: The brief says which question to ask, and what stops the work
    Given the system started, seeded from the skills this build ships with
    When the operator dispatches "write the terraform for the transcript service" to the project
    Then the "deploy-identity" brief the session can open says "iam:SimulatePrincipalPolicy"
    And the "deploy-identity" brief the session can open says "A denied action stops you reporting the work as ready"
    And the "deploy-identity" brief the session can open says "a green plan is not evidence"
    And the "deploy-identity" brief the session can open says "Ask about every action in one call"

  # The workspace holds no cloud credential, which is the state a workspace whose pipeline
  # authenticates by federated identity is in. A skill that named those secrets would be left out
  # here, and the rule would be missing from exactly the jobs that deploy.
  Scenario: A workspace with no cloud credential still holds the rule
    Given the system started, seeded from the skills this build ships with
    When the operator dispatches "write the terraform for the transcript service" to the project
    And the operator lists the session's skills
    Then the listing does not say the "deploy-identity" skill was left out
