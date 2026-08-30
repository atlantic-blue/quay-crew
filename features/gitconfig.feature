Feature: An operator's git configuration reaches a session

  A session commits as the operator, and until now it had no way to know who that was. Identity was
  four environment variables set on the task's own process, so a commit made from an attached
  terminal, or by anything the session started for itself, had none. Git refused those commits.

  So the image reads the operator's own configuration. It ships a git configuration holding one
  include, pointing at where a mounted secret named gitconfig lands, and the operator mounts theirs:

      krewe secret mount me gitconfig ~/.gitconfig

  Identity, aliases and settings then reach every git process in the sandbox, from any shell. A
  workspace that mounts nothing is unchanged, because git ignores an include that is not there.

  Signing is the one thing the system decides rather than the operator, and it has to, because most
  operators who sign have signing on for everything against a key their machine holds and a container
  does not. A workspace that mounts a signing key signs with it, in either format: an ssh key, which
  git reads as a file, or a gpg key, which the sandbox imports into a keyring it makes in memory. A
  workspace that mounts none says so to git, rather than saying nothing and letting the mounted
  configuration fail every commit.

  These scenarios use a sandbox double, so they say what the system asks a sandbox to do and not that
  git honours it. The real thing is proved against the image in
  TestAnOperatorsConfigurationDecidesWhoCommits and TestASessionWithNoKeyCommitsWithoutSigning.

  Background:
    Given a running control plane
    And a workspace named "me"
    And a project named "house-bills"

  Scenario: The operator's configuration lands where the image reads it
    Given the workspace mounts the secret "gitconfig" holding "[user] name = operator"
    When the operator dispatches "hello" to the project
    Then the sandbox is given the file "/run/secrets/gitconfig" holding "[user] name = operator"
    And the image reads its git configuration from that file

  # Without this, a mounted configuration that asks for signing fails every commit in the sandbox,
  # against a key the sandbox was never going to have.
  Scenario: A workspace with no signing key tells git not to sign
    When the operator dispatches "hello" to the project
    Then the sandbox is told to set "commit.gpgsign" to "false"
    And the sandbox is told to set "tag.gpgsign" to "false"

  Scenario: A workspace that mounts a signing key still signs
    Given the workspace mounts the secret "GIT_SSH_SIGNING_KEY" holding "a private key"
    When the operator dispatches "hello" to the project
    Then the sandbox is told to set "commit.gpgsign" to "true"
    And the sandbox is told to set "gpg.format" to "ssh"

  # An operator who already signs with gpg has one key their account knows, and an ssh key in a
  # sandbox signs the same history under a second identity. So a workspace can mount the gpg key
  # instead, and the session signs as the operator does everywhere else.
  Scenario: A workspace that mounts a gpg key signs with it
    Given the workspace mounts the secret "GPG_SIGNING_KEY" holding "an exported secret key"
    When the operator dispatches "hello" to the project
    Then the sandbox imports the mounted gpg key
    And the sandbox is told to set "gpg.format" to "openpgp"
    And the sandbox is told to set "commit.gpgsign" to "true"
    And the sandbox is told to set "tag.gpgsign" to "true"

  # gpg asks for a passphrase through pinentry, and a task nobody is watching waits forever. Batch
  # mode is what makes that a failure with a message instead.
  Scenario: A gpg key with no passphrase mounted still runs unattended
    Given the workspace mounts the secret "GPG_SIGNING_KEY" holding "an exported secret key"
    When the operator dispatches "hello" to the project
    Then the sandbox tells gpg to work in batch
    And the sandbox does not point gpg at a passphrase file

  Scenario: A workspace that mounts the passphrase too points gpg at it
    Given the workspace mounts the secret "GPG_SIGNING_KEY" holding "an exported secret key"
    And the workspace mounts the secret "GPG_SIGNING_KEY_PASSPHRASE" holding "open sesame"
    When the operator dispatches "hello" to the project
    Then the sandbox points gpg at the passphrase file "/run/secrets/GPG_SIGNING_KEY_PASSPHRASE"

  # Mounting a second key is a deliberate act, and the ssh key is the one that was already there, so
  # the newer key is the answer to what the operator changed.
  Scenario: A workspace holding both kinds of key signs with the gpg one
    Given the workspace mounts the secret "GIT_SSH_SIGNING_KEY" holding "a private key"
    And the workspace mounts the secret "GPG_SIGNING_KEY" holding "an exported secret key"
    When the operator dispatches "hello" to the project
    Then the sandbox is told to set "gpg.format" to "openpgp"
