Feature: A role is imported, pinned to a version, and attached at a level

  A role is a named way of working a session is given: a brief the model reads, the model it runs on,
  and the material it is allowed to receive. The design is in docs/ROLES.md.

  The boundary is the point of it. A role that writes tests must not receive the code, or the two
  sessions are one conversation wearing two names. So what a role receives is declared, and a role
  that names material the crew does not hand out is refused at import.

  Nothing here runs as a role yet. This is the catalogue and the two levels it attaches at, which is
  the same shape a skill already has. What a session running as a role receives comes next.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A crew that has imported nothing holds no roles
    When the operator lists the crew's roles
    Then the crew holds no roles

  Scenario: A role is imported and the crew says what it is
    When the operator imports the "test-writer" role
    Then the crew holds the "test-writer" role
    And the listing says the "test-writer" role runs on "opus"
    And the listing says the "test-writer" role receives "context, work"

  # A boundary that means nothing looks exactly like one that holds, and import is the only moment
  # anybody is looking.
  Scenario: A role receiving material the crew does not hand out is refused, and names it
    When the operator imports a role receiving "the whole repository"
    Then the crew refuses the role saying "the whole repository"
    And the crew holds no roles

  # What a role costs is part of what it is, so the crew will not choose it for the operator.
  Scenario: A role naming no model is refused
    When the operator imports a role naming no model
    Then the crew refuses the role saying "names no model"
    And the crew holds no roles

  Scenario: A role that says nothing about what it receives is refused
    When the operator imports a role declaring nothing it receives
    Then the crew refuses the role saying "boundary"
    And the crew holds no roles

  Scenario: A role with no version is refused, because a session is pinned to one
    When the operator imports a role with no version
    Then the crew refuses the role saying "pinned"
    And the crew holds no roles

  # A workspace pins the version it attached, so raising the version in the repository is the way to
  # change a role rather than editing one under a workspace already holding it.
  Scenario: Importing a different role at the same version is refused
    Given the operator imported the "test-writer" role
    When the operator imports a different "test-writer" role at the same version
    Then the crew refuses the role saying "already imported and is a different role"

  Scenario: A workspace with no roles attached says so
    When the operator lists the workspace's roles
    Then the workspace holds no roles

  Scenario: Attaching a role puts it in front of the workspace
    Given the operator imported the "test-writer" role
    When the operator attaches the "test-writer" role to the workspace
    Then the workspace holds the "test-writer" role

  Scenario: A role attached to one workspace does not reach another
    Given a second workspace named "widgets"
    And the operator imported the "test-writer" role
    When the operator attaches the "test-writer" role to the workspace
    Then the second workspace holds no roles

  # The version a workspace pinned does not move when a newer one is imported. That is what lets a
  # role be edited without changing what a session already running as it was told to do.
  Scenario: A newer revision does not move a workspace on its own
    Given the operator imported the "test-writer" role
    And the operator attached the "test-writer" role to the workspace
    When the operator imports version 2 of the "test-writer" role
    Then the workspace still holds version 1 of the "test-writer" role
    When the operator attaches the "test-writer" role to the workspace
    Then the workspace holds version 2 of the "test-writer" role

  Scenario: Detaching a role takes it off the workspace and leaves it imported
    Given the operator imported the "test-writer" role
    And the operator attached the "test-writer" role to the workspace
    When the operator detaches the "test-writer" role from the workspace
    Then the workspace holds no roles
    And the crew holds the "test-writer" role

  # A role given to the crew is held by every workspace, including the ones made after it, which is
  # the difference between setting a crew up once and setting each workspace up again. It takes the
  # word crew where a workspace goes, exactly as quay skill attach does.
  Scenario: A role the crew holds reaches a workspace that attached nothing
    Given the operator imported the "test-writer" role
    When the operator attaches the "test-writer" role to the crew
    Then the workspace holds the "test-writer" role
    And the listing says the "test-writer" role is held by the crew

  Scenario: A workspace created after the crew took a role holds it too
    Given the operator imported the "test-writer" role
    And the operator attached the "test-writer" role to the crew
    When a second workspace named "widgets"
    Then the second workspace holds the "test-writer" role

  # Two separate statements, and the wider one does not undo the narrower one.
  Scenario: Taking a role off the crew leaves a workspace's own attachment alone
    Given the operator imported the "test-writer" role
    And the operator attached the "test-writer" role to the crew
    And the operator attached the "test-writer" role to the workspace
    When the operator detaches the "test-writer" role from the crew
    Then the workspace holds the "test-writer" role

  Scenario: Taking a role off the crew takes it off a workspace that only had it that way
    Given the operator imported the "test-writer" role
    And the operator attached the "test-writer" role to the crew
    And the workspace holds the "test-writer" role
    When the operator detaches the "test-writer" role from the crew
    Then the workspace holds no roles

  Scenario: Attaching a role the crew has not imported is refused
    When the operator attaches the "architect" role to the workspace
    Then the crew refuses the role saying "not found"

  # The roles this build ships, in roles/ at the root of the repository, ported from greenlight's
  # agents. They are read from that directory rather than from a list in the test, so a role added
  # later is held to the same rules without anybody remembering, and a roles/ that lost its contents
  # fails this rather than passing over nothing.
  Scenario: The crew imports every role this build ships
    When the operator imports every role this build ships
    Then the crew holds every role this build ships
    And the listing says the "test-writer" role runs on "sonnet"
    And the listing says the "test-writer" role receives "context, skills, work"

  Scenario: A role this build ships reaches a workspace
    Given the operator imports every role this build ships
    When the operator attaches the "implementer" role to the workspace
    Then the workspace holds the "implementer" role

  # The check that catches an invented material in a ported brief. It is a shipped role with one word
  # changed, so it fails the way a bad port would rather than the way an invented test would.
  Scenario: A shipped role carrying a word the crew does not hand out is refused
    When the operator imports a shipped role receiving "the whole repository"
    Then the crew refuses the role saying "the whole repository"
    And the crew holds no roles
