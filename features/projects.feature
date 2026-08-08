Feature: Projects hold a body of work inside a workspace

  A workspace is who you are. A project is a body of work inside it, and threads happen inside a
  project. So "me" holds "house-bills", and the threads about the energy supplier and the council tax
  sit together under it, apart from the threads about anything else.

  Background:
    Given a running control plane
    And a workspace named "me"

  Scenario: A project belongs to the workspace it was created in
    When the operator creates a project named "house-bills"
    Then the project belongs to the workspace
    And the workspace has 1 project

  Scenario: A project needs a workspace that exists
    When the operator creates a project named "house-bills" in workspace "ghost"
    Then the control plane refuses it as not found

  Scenario: A project needs a name
    When the operator creates a project named ""
    Then the control plane refuses it as invalid

  Scenario: A project name that could not be part of an address is refused
    When the operator creates a project named "House Bills"
    Then the control plane refuses it as invalid
    And the refusal suggests "house-bills"

  # This is the point of the level. Two bodies of work in one workspace stay apart, even when a
  # channel hands both of them the same thread identifier.
  Scenario: Threads in two projects of one workspace are separate sessions
    Given a project named "house-bills"
    And a second project named "gardening"
    When the operator dispatches "hello" to the project
    And the operator dispatches "hello" to the second project
    Then the turns ran in different sessions
    And the workspace has 2 sessions

  Scenario: One workspace's projects are not another's
    Given a project named "house-bills"
    And a second workspace named "someone-else"
    Then the workspace has 1 project

  Scenario: A project can be reached by name
    Given a project named "house-bills"
    When the operator refers to the project as "house-bills"
    Then the project reference resolves to the project

  Scenario: A project reference matching nothing is refused
    Given a project named "house-bills"
    When the operator refers to the project as "ghost"
    Then the project reference is refused as not found

  # A session's working directory starts empty, so a skill about git had nothing to work in: the crew
  # could describe how to commit and there was nowhere to commit. A project names the repository its
  # sessions work in, and the first turn in one clones it.
  Scenario: A project with no remote leaves a session's directory alone
    Given a project named "house-bills"
    When the operator dispatches "hello" to the project
    Then the session cloned nothing

  Scenario: The first turn in a project clones its remote
    Given a project named "house-bills"
    And the project works in "https://github.com/atlantic-blue/quay-crew.git"
    When the operator dispatches "hello" to the project
    Then the session cloned "https://github.com/atlantic-blue/quay-crew.git"
    And it cloned into a directory of its own under the working directory
    And the clone never carried the credential in its arguments

  # A sandbox is adopted across turns, and cloning again would either fail or throw away whatever the
  # first turn did. The crew asks on every turn, the way it asks whether a binary is there, and the
  # command it asks for is the thing that only acts once. That a real second run leaves the checkout
  # alone is proved against Docker in the sandbox package.
  Scenario: The clone it asks for only acts when there is no checkout yet
    Given a project named "house-bills"
    And the project works in "https://github.com/atlantic-blue/quay-crew.git"
    And a session started by dispatching "hello"
    When the operator dispatches "and again" to the same thread
    Then every clone it asked for was conditional on there being no checkout

  # A refusal read by the person who typed it beats the same refusal on somebody's first turn hours
  # later, with nothing pointing back at where it came from.
  Scenario: A remote that is not a repository address is refused when it is set
    Given a project named "house-bills"
    When the operator sets the project's remote to "not-a-remote"
    Then the control plane refuses it as invalid
    And the refusal says "not a repository address"

  # A credential in the address would be a credential in the database and in every listing that prints
  # a project.
  Scenario: A remote carrying a credential is refused
    Given a project named "house-bills"
    When the operator sets the project's remote to "https://someone:hunter2@github.com/a/b.git"
    Then the control plane refuses it as invalid
    And the refusal says "carries a user in it"

  Scenario: A failed clone says what to check
    Given a project named "house-bills"
    And the project works in "https://github.com/atlantic-blue/quay-crew.git"
    And the clone will fail saying "fatal: Authentication failed"
    When the operator dispatches "hello" to the project
    Then the control plane refuses it as the wrong state
    And the refusal says "Authentication failed"
    And the refusal says "GH_TOKEN"

  Scenario: Taking a remote away leaves the checkouts alone
    Given a project named "house-bills"
    And the project works in "https://github.com/atlantic-blue/quay-crew.git"
    And a session started by dispatching "hello"
    When the operator clears the project's remote
    Then the project has no remote
