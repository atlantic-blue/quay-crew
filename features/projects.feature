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
