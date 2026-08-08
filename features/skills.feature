Feature: A workspace can be given a skill, and its sessions hold it

  A session opens knowing nothing about how the operator works. Ask it to open a pull request and it
  finds git in the image, no identity, no credential and no gh, and it improvises.

  A skill is the missing piece: a capability written down as a directory in a git repository, imported
  into the crew, attached to the workspaces that want it. The files travel rather than the path,
  because the control plane runs in a container and a directory on somebody's laptop means nothing to
  it, and because the crew has to be whole on a pod where there is no host directory to go back to.

  What reaches a session is one line per skill and not a page: the line says the skill exists and when
  to reach for it, and the brief beside it is opened only when that kind of work comes up. The cost of
  getting this wrong is measured rather than imagined. This crew's four levels of context reached
  51,727 bytes at the workspace, paid by every session before a word was typed.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A workspace with no skills says so
    When the operator lists the workspace's skills
    Then the workspace holds no skills

  Scenario: A skill is imported and the crew says what it can do
    When the operator imports the "github" skill
    Then the crew holds the "github" skill
    And the listing says the skill needs "gh"
    And the listing names the secret "GH_TOKEN" and what it is for

  Scenario: A malformed skill is refused, and says what is wrong
    When the operator imports a skill whose manifest has no version
    Then the crew refuses it saying "version of 1 or more"
    And the crew holds no skills

  # A brief is loaded whenever the model does that kind of work, so it is a page. The reference goes in
  # other files in the skill's directory, which cost nothing until something opens one.
  Scenario: A brief longer than a page is refused
    When the operator imports a skill whose brief is longer than a page
    Then the crew refuses it saying "read only when they are needed"

  Scenario: Attaching a skill puts it in front of the workspace's sessions
    Given the operator imported the "github" skill
    When the operator attaches the "github" skill to the workspace
    And the operator dispatches "hello" to the project
    Then the workspace's memory file names the "github" skill
    And the workspace's memory file says when to use it
    And the session can read the skill's brief at the path the memory file gives

  # The index is what every conversation pays for. The brief is not in it, which is the whole design.
  Scenario: The brief itself is not in the memory file
    Given the operator imported the "github" skill
    And the operator attached the "github" skill to the workspace
    When the operator dispatches "hello" to the project
    Then the workspace's memory file does not carry the body of the brief

  Scenario: A skill reaches a session that is already running
    Given the operator imported the "github" skill
    And a session started by dispatching "hello"
    When the operator attaches the "github" skill to the workspace
    Then the workspace's memory file names the "github" skill

  # A brief left behind is a capability the model can still read about and no longer has, which is
  # worse than never having had it, because it will try.
  Scenario: Detaching a skill takes it off the sessions that held it
    Given the operator imported the "github" skill
    And the operator attached the "github" skill to the workspace
    And a session started by dispatching "hello"
    When the operator detaches the "github" skill from the workspace
    Then the workspace's memory file does not name the "github" skill
    And the "github" skill's directory is gone from the session

  # Detaching the only skill takes the whole directory, which is a different path through the code from
  # taking one away and leaving the rest. Both matter: a stale brief is a capability the model reads
  # about and no longer has.
  Scenario: Detaching one of two skills leaves the other
    Given the operator imported the "github" skill
    And the operator imported the "git" skill
    And the operator attached the "github" skill to the workspace
    And the operator attached the "git" skill to the workspace
    And a session started by dispatching "hello"
    When the operator detaches the "github" skill from the workspace
    Then the workspace's memory file does not name the "github" skill
    And the workspace's memory file names the "git" skill
    And the "github" skill's directory is gone from the session
    And the "git" skill's directory is still there

  Scenario: A skill attached to one workspace does not reach another
    Given a second workspace named "other"
    And the operator imported the "github" skill
    When the operator attaches the "github" skill to the workspace
    Then the second workspace holds no skills

  # A session holding a skill gets the secrets that skill names, and a session that does not hold it
  # never sees them. A sandbox holds a value for the life of its container and the model can read it,
  # which is the point of giving it one and the reason not to give it every one.
  Scenario: A skill's secret reaches a session that holds it
    Given the operator imported the "github" skill
    And the workspace has the secret "GH_TOKEN" set to "a-real-token"
    When the operator attaches the "github" skill to the workspace
    And the operator dispatches "hello" to the project
    Then the sandbox carries "GH_TOKEN" set to "a-real-token"

  Scenario: A secret a skill does not name does not reach a session
    Given the workspace has the secret "GH_TOKEN" set to "a-real-token"
    When the operator dispatches "hello" to the project
    Then the sandbox carries nothing called "GH_TOKEN"

  # A workspace pins the version it holds. Importing a newer revision must not change what a session
  # already using the older one can do.
  Scenario: A newer revision does not move a workspace on its own
    Given the operator imported the "github" skill
    And the operator attached the "github" skill to the workspace
    When the operator imports version 2 of the "github" skill
    Then the workspace still holds version 1 of the "github" skill
    When the operator attaches the "github" skill to the workspace
    Then the workspace holds version 2 of the "github" skill

  Scenario: Importing a different skill at the same version is refused
    Given the operator imported the "github" skill
    When the operator imports a different "github" skill at the same version
    Then the crew refuses it saying "already imported and is a different skill"

  # Context is read back when something inside a sandbox edits it, because an agent writing into its own
  # memory has learned something. A skill is the opposite: it is a capability somebody granted, so an
  # edit from inside is not an edit of it. Nothing self applies.
  Scenario: A brief edited from inside the sandbox does not survive
    Given the operator imported the "github" skill
    And the operator attached the "github" skill to the workspace
    And a session started by dispatching "hello"
    When something inside the sandbox rewrites the "github" brief
    And the operator dispatches "and again" to the same thread
    Then the "github" brief reads what the crew holds

  # The index is rendered from what the workspace holds, so it is not something the operator wrote. Read
  # back as though it were, it would land in the workspace's own context and then be rendered again
  # underneath itself on every turn.
  Scenario: The skills index does not leak into the workspace's context
    Given the operator imported the "github" skill
    And the operator attached the "github" skill to the workspace
    And the operator sets context at scope "workspace" to "no acronyms"
    When the operator dispatches "hello" to the project
    And the operator dispatches "and again" to the same thread
    Then the workspace's context still reads "no acronyms"
