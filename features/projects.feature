Feature: Projects hold a body of work inside a workspace

  A workspace is who you are. A project is a body of work inside it, and sessions happen inside a
  project. So "me" holds "house-bills", and the sessions about the energy supplier and the council tax
  sit together under it, apart from the sessions about anything else.

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
  # channel hands both of them the same session identifier.
  Scenario: Sessions in two projects of one workspace are separate sessions
    Given a project named "house-bills"
    And a second project named "gardening"
    When the operator dispatches "hello" to the project
    And the operator dispatches "hello" to the second project
    Then the tasks ran in different sessions
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

  # A project is a body of work, and the repository is where that body of work goes. The crew held no
  # record of it, so a session told to push had a token that worked and nowhere to push to, and the
  # operator was the only index of which repository belonged to which project.
  Scenario: A project says where its work lands
    Given a project named "transcript"
    When the operator says the project's work lands in "atlantic-blue/transcript"
    Then the project works in "atlantic-blue/transcript"

  # The address somebody has in front of them is the one in their browser.
  Scenario: The address of the repository is kept as an owner and a name
    Given a project named "transcript"
    When the operator says the project's work lands in "https://github.com/atlantic-blue/transcript.git"
    Then the project works in "atlantic-blue/transcript"

  Scenario: A repository that is not an owner and a name is refused
    Given a project named "transcript"
    When the operator says the project's work lands in "transcript"
    Then the control plane refuses it as invalid, saying how to write a repository

  # Public unless somebody says otherwise, and the reason is the bill: a pipeline's minutes are free
  # on a public repository and metered on a private one. That rule was in a person's head, and it was
  # said out loud once per project.
  Scenario: A repository nobody called anything is public
    Given a project named "transcript"
    When the operator says the project's work lands in "atlantic-blue/transcript"
    Then the repository is public, and the crew says its pipeline minutes are free

  Scenario: A repository the operator calls private is private
    Given a project named "transcript"
    When the operator says the project's private work lands in "atlantic-blue/transcript"
    Then the repository is private, and the crew says its pipeline minutes are metered

  # A forge has other kinds, and recording "internal" as public would be the crew writing down a cost
  # fact nobody told it.
  Scenario: A kind of repository the crew does not know is refused
    Given a project named "transcript"
    When the operator says the project's work lands in "atlantic-blue/transcript", of kind "internal"
    Then the control plane refuses it as invalid, naming the two kinds

  # The point of the record. Nobody passes the address again, and the session doing the job is told
  # where the work goes.
  Scenario: A job declared in the project works in the project's repository
    Given a project named "transcript"
    And the project's work lands in "atlantic-blue/transcript"
    When the caller declares a job
    Then the job works in "atlantic-blue/transcript"

  Scenario: A job that names its own repository keeps it
    Given a project named "transcript"
    And the project's work lands in "atlantic-blue/transcript"
    When the caller declares a job in the repository "atlantic-blue/quay-crew"
    Then the job works in "atlantic-blue/quay-crew"

  Scenario: A job in a project that works nowhere is asked to push nowhere
    Given a project named "transcript"
    When the caller declares a job
    Then the job works in nothing, and the session doing it is asked for no pull request
