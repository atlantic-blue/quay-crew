Feature: A secret every workspace needs is held once by the crew

  A subscription token, a forge token, a credential file: each one was set again for every
  workspace, and a workspace made tomorrow started with none of them. Setting up a crew was
  setting up each workspace.

  So a secret can be set on the crew instead. That is the level the crew's skills, hooks and roles
  are already attached at, and it is reached the same way: say "crew" where a workspace goes.

  A workspace wins on a name. The crew's level is what every workspace gets, and a workspace that
  says something different about a name means it, which is how one workspace uses a different token
  without every other workspace losing the shared one.

  These scenarios use a sandbox double, so they say what the crew hands a sandbox and not that a
  real daemon honours it.

  Background:
    Given a running control plane

  Scenario: A secret the crew holds reaches a workspace that set nothing
    Given the crew has the secret "GITHUB_TOKEN" set to "ghp-shared"
    And a workspace named "me"
    And a project named "house-bills"
    When the operator dispatches "hello" to the project
    Then the sandbox carries "GITHUB_TOKEN" set to "ghp-shared"

  # The whole point. A crew set up once stays set up, and the next workspace costs nothing.
  Scenario: A workspace made after the secret was set still gets it
    Given a workspace named "me"
    And a project named "house-bills"
    And the crew has the secret "GITHUB_TOKEN" set to "ghp-shared"
    And a workspace named "acme"
    And a project named "gardening"
    When the operator dispatches "hello" to the project
    Then the sandbox carries "GITHUB_TOKEN" set to "ghp-shared"

  # Without this the crew's level would be a floor rather than a default, and the one workspace
  # that needs its own token could not have one.
  Scenario: A workspace that sets the same name keeps its own value
    Given the crew has the secret "GITHUB_TOKEN" set to "ghp-shared"
    And a workspace named "me"
    And a project named "house-bills"
    And the workspace has the secret "GITHUB_TOKEN" set to "ghp-mine"
    When the operator dispatches "hello" to the project
    Then the sandbox carries "GITHUB_TOKEN" set to "ghp-mine"

  # A credential that is a file is the case that hurts most to repeat, because it is pasted rather
  # than piped from a tool.
  Scenario: A credential file mounted on the crew reaches every workspace
    Given the crew mounts the secret "gitconfig" holding "[user] name = operator"
    And a workspace named "me"
    And a project named "house-bills"
    When the operator dispatches "hello" to the project
    Then the sandbox is given the file "/run/secrets/gitconfig" holding "[user] name = operator"
    And the sandbox carries nothing called "gitconfig"

  # A workspace that attached nothing and has three secrets is a puzzle. The listing answers it.
  Scenario: A listing says which secrets the crew holds
    Given the crew has the secret "GITHUB_TOKEN" set to "ghp-shared"
    And a workspace named "me"
    And the workspace has the secret "STRIPE_KEY" set to "sk-mine"
    When the operator asks which secrets the workspace has
    Then the listing says the crew holds "GITHUB_TOKEN"
    And the listing says the workspace holds "STRIPE_KEY"

  # "crew" is the word every address takes for the level above a workspace. A workspace called crew
  # would take the secrets and skills meant for all of them, and nothing else would ever read them.
  Scenario: A workspace cannot be called crew
    When the operator creates a workspace named "crew"
    Then the crew refuses it, saying that word means the whole crew
