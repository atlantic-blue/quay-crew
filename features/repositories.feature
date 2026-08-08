Feature: A workspace works in repositories, and every session gets a checkout

  A session's working directory started empty, so the crew could describe how git is done here and there
  was nowhere to run it. A workspace names the repositories its sessions work in, and the first turn in a
  session clones each one.

  On the workspace rather than the project, because that is already where a credential lives and where a
  skill attaches, and those are the two things a repository needs. Several rather than one, because a
  workspace routinely spans more than one: a service and its infrastructure, or a frontend and the api
  behind it.

  Each lands in a directory of its own under the working directory, beside the memory file that is
  already there. That the clone is real, and that asking again leaves the session's own work alone, is
  proved against Docker in the sandbox package.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A workspace with no repositories leaves a session's directory alone
    When the operator dispatches "hello" to the project
    Then the session cloned nothing

  Scenario: The first turn clones what the workspace works in
    Given the workspace works in "https://github.com/atlantic-blue/quay-crew.git"
    When the operator dispatches "hello" to the project
    Then the session cloned "https://github.com/atlantic-blue/quay-crew.git"
    And it cloned into a directory of its own under the working directory
    And the clone never carried the credential in its arguments

  Scenario: Every repository the workspace works in is cloned
    Given the workspace works in "https://github.com/atlantic-blue/quay-crew.git"
    And the workspace works in "https://github.com/atlantic-blue/org-cdk.git"
    When the operator dispatches "hello" to the project
    Then the session cloned "https://github.com/atlantic-blue/quay-crew.git"
    And the session cloned "https://github.com/atlantic-blue/org-cdk.git"

  # Every project in the workspace works in the same code, which is the whole reason this moved up.
  Scenario: A second project in the same workspace gets the same repository
    Given the workspace works in "https://github.com/atlantic-blue/quay-crew.git"
    And a second project named "gardening"
    When the operator dispatches "hello" to the second project
    Then the session cloned "https://github.com/atlantic-blue/quay-crew.git"

  Scenario: The clone it asks for only acts when there is no checkout yet
    Given the workspace works in "https://github.com/atlantic-blue/quay-crew.git"
    And a session started by dispatching "hello"
    When the operator dispatches "and again" to the same thread
    Then every clone it asked for was conditional on there being no checkout

  Scenario: A remote that is not a repository address is refused when it is added
    When the operator adds the repository "not-a-remote"
    Then the control plane refuses it as invalid
    And the refusal says "not a repository address"

  Scenario: A remote carrying a credential is refused
    When the operator adds the repository "https://someone:hunter2@github.com/a/b.git"
    Then the control plane refuses it as invalid
    And the refusal says "carries a user in it"

  # Two remotes whose last segment is the same would want one directory, and the second would quietly
  # never be cloned.
  Scenario: A second repository that would land in the same directory is refused
    Given the workspace works in "https://github.com/atlantic-blue/quay-crew.git"
    When the operator adds the repository "https://gitlab.com/someone-else/quay-crew.git"
    Then the control plane refuses it as the wrong state
    And the refusal says "already works in"

  Scenario: A failed clone says what to check
    Given the workspace works in "https://github.com/atlantic-blue/quay-crew.git"
    And the clone will fail saying "fatal: Authentication failed"
    When the operator dispatches "hello" to the project
    Then the control plane refuses it as the wrong state
    And the refusal says "Authentication failed"
    And the refusal says "GH_TOKEN"

  Scenario: Removing a repository stops new sessions cloning it
    Given the workspace works in "https://github.com/atlantic-blue/quay-crew.git"
    When the operator stops working in "quay-crew"
    And the operator dispatches "hello" to the project
    Then the session cloned nothing
