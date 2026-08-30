Feature: A secret every workspace needs is held once by the system

  A subscription token, a forge token, a credential file: each one was set again for every
  workspace, and a workspace made tomorrow started with none of them. Setting up a system was
  setting up each workspace.

  So a secret can be set on the system instead. That is the level the system's skills, hooks and roles
  are already attached at, and it is reached the same way: say "system" where a workspace goes.

  A workspace wins on a name. The system's level is what every workspace gets, and a workspace that
  says something different about a name means it, which is how one workspace uses a different token
  without every other workspace losing the shared one.

  These scenarios use a sandbox double, so they say what the system hands a sandbox and not that a
  real daemon honours it.

  Background:
    Given a running control plane

  Scenario: A secret the system holds reaches a workspace that set nothing
    Given the system has the secret "GITHUB_TOKEN" set to "ghp-shared"
    And a workspace named "me"
    And a project named "house-bills"
    When the operator dispatches "hello" to the project
    Then the sandbox carries "GITHUB_TOKEN" set to "ghp-shared"

  # The whole point. A system set up once stays set up, and the next workspace costs nothing.
  Scenario: A workspace made after the secret was set still gets it
    Given a workspace named "me"
    And a project named "house-bills"
    And the system has the secret "GITHUB_TOKEN" set to "ghp-shared"
    And a workspace named "acme"
    And a project named "gardening"
    When the operator dispatches "hello" to the project
    Then the sandbox carries "GITHUB_TOKEN" set to "ghp-shared"

  # Without this the system's level would be a floor rather than a default, and the one workspace
  # that needs its own token could not have one.
  Scenario: A workspace that sets the same name keeps its own value
    Given the system has the secret "GITHUB_TOKEN" set to "ghp-shared"
    And a workspace named "me"
    And a project named "house-bills"
    And the workspace has the secret "GITHUB_TOKEN" set to "ghp-mine"
    When the operator dispatches "hello" to the project
    Then the sandbox carries "GITHUB_TOKEN" set to "ghp-mine"

  # A credential that is a file is the case that hurts most to repeat, because it is pasted rather
  # than piped from a tool.
  Scenario: A credential file mounted on the system reaches every workspace
    Given the system mounts the secret "gitconfig" holding "[user] name = operator"
    And a workspace named "me"
    And a project named "house-bills"
    When the operator dispatches "hello" to the project
    Then the sandbox is given the file "/run/secrets/gitconfig" holding "[user] name = operator"
    And the sandbox carries nothing called "gitconfig"

  # Signing and the system's level are two features that only pay off together: a key mounted once
  # signs the job of every workspace, including the ones made tomorrow. Neither test suite covers
  # the join, so each could keep passing while the pair stopped working.
  Scenario: A gpg key the system holds makes every workspace sign
    Given the system mounts the secret "GPG_SIGNING_KEY" holding "an exported secret key"
    And a workspace named "me"
    And a project named "house-bills"
    When the operator dispatches "hello" to the project
    Then the sandbox is given the file "/run/secrets/GPG_SIGNING_KEY" holding "an exported secret key"
    And the sandbox is told to set "gpg.format" to "openpgp"
    And the sandbox is told to set "commit.gpgsign" to "true"

  # The one workspace that signs as somebody else, without the other workspaces losing the shared
  # key. A private key is the secret an operator is most likely to want to say something different
  # about.
  Scenario: A workspace that mounts its own gpg key signs with that one
    Given the system mounts the secret "GPG_SIGNING_KEY" holding "the shared key"
    And a workspace named "me"
    And a project named "house-bills"
    And the workspace mounts the secret "GPG_SIGNING_KEY" holding "the workspace's own key"
    When the operator dispatches "hello" to the project
    Then the sandbox is given the file "/run/secrets/GPG_SIGNING_KEY" holding "the workspace's own key"
    And the sandbox is told to set "commit.gpgsign" to "true"

  # A workspace that attached nothing and has three secrets is a puzzle. The listing answers it.
  Scenario: A listing says which secrets the system holds
    Given the system has the secret "GITHUB_TOKEN" set to "ghp-shared"
    And a workspace named "me"
    And the workspace has the secret "STRIPE_KEY" set to "sk-mine"
    When the operator asks which secrets the workspace has
    Then the listing says the system holds "GITHUB_TOKEN"
    And the listing says the workspace holds "STRIPE_KEY"

  # "system" is the word every address takes for the level above a workspace. A workspace called system
  # would take the secrets and skills meant for all of them, and nothing else would ever read them.
  Scenario: A workspace cannot be called system
    When the operator creates a workspace named "system"
    Then the system refuses it, saying that word means the whole system
