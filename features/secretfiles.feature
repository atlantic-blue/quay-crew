Feature: A secret can reach a session as a file

  Some credentials are files, not values. A git configuration, a private key, a cloud credentials
  file: a tool opens each one by path, so there is nothing an environment variable can do for them.

  So a secret says how it reaches a sandbox. This is the shape Kubernetes and Docker both settled on:
  the store holds bytes under a name, and whether those bytes become an environment variable or a
  file is a separate choice. A Kubernetes Secret is read through secretKeyRef or mounted through a
  secret volume; a Docker secret is given a target and lands under /run/secrets. Neither writes the
  presentation into the store.

  A mounted secret does not also reach the environment, and that is the second reason to mount one.
  A container's environment is readable through docker inspect for the life of that container, and a
  file in a memory backed directory is not.

  These scenarios use a sandbox double, so they say what the system asks a sandbox to do and not that a
  real daemon honours it. The real thing is proved against Docker in
  TestAMountedSecretIsAFileTheSandboxUserCanReadAndNobodyElseCan.

  Background:
    Given a running control plane
    And a workspace named "me"
    And a project named "house-bills"

  Scenario: A mounted secret is written into the session's sandbox as a file
    Given the workspace mounts the secret "gitconfig" holding "[user] name = operator"
    When the operator dispatches "hello" to the project
    Then the sandbox is given the file "/run/secrets/gitconfig" holding "[user] name = operator"

  # The whole reason to prefer a file for a credential. Putting it in the environment as well would
  # hand back the exposure the file exists to avoid.
  Scenario: A mounted secret does not also reach the environment
    Given the workspace mounts the secret "gitconfig" holding "[user] name = operator"
    When the operator dispatches "hello" to the project
    Then the sandbox carries nothing called "gitconfig"

  # An argument is visible to every process on the host that can list them, and it would reach the
  # task record.
  Scenario: A mounted value is never an argument
    Given the workspace mounts the secret "gitconfig" holding "[user] name = operator"
    When the operator dispatches "hello" to the project
    Then no command run in the sandbox carries "[user] name = operator" in its arguments

  # Every secret a running system already holds is one of these. Getting this wrong would move all of
  # them out of the environment at once, and every session would lose every credential it had.
  Scenario: A secret that says nothing about how it travels still reaches the environment
    Given the workspace has the secret "GITHUB_TOKEN" set to "ghp-1234"
    When the operator dispatches "hello" to the project
    Then the sandbox carries "GITHUB_TOKEN" set to "ghp-1234"
    And the sandbox is given no files

  Scenario: One workspace can hold both kinds at once
    Given the workspace has the secret "GITHUB_TOKEN" set to "ghp-1234"
    And the workspace mounts the secret "gitconfig" holding "[user] name = operator"
    When the operator dispatches "hello" to the project
    Then the sandbox carries "GITHUB_TOKEN" set to "ghp-1234"
    And the sandbox is given the file "/run/secrets/gitconfig" holding "[user] name = operator"

  # A mounted name becomes a file name inside a sandbox, so one that walks out of its own directory
  # is refused when it is set rather than at the moment of writing.
  Scenario: A mounted name that would escape its directory is refused
    When the operator mounts the secret "../../etc/passwd" holding "root"
    Then the system refuses it, saying it cannot be a file name

  # Reading a listing is how the operator finds out where a session should look. Two secrets that
  # arrive in different places and read identically in a listing is the same as not saying.
  Scenario: A listing says how each secret reaches a session
    Given the workspace has the secret "GITHUB_TOKEN" set to "ghp-1234"
    And the workspace mounts the secret "gitconfig" holding "[user] name = operator"
    When the operator asks which secrets the workspace has
    Then the listing says "gitconfig" is mounted
    And the listing says "GITHUB_TOKEN" reaches the environment
    And the listing says nothing that either secret holds
