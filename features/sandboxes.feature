Feature: A sandbox keeps a session's state outside itself

  A sandbox is a container, and a container's filesystem is thrown away with it. The conversation the
  model keeps is in there, so a session whose state lived only in its container would lose that
  conversation the moment the container was replaced.

  So state is kept per workspace and per project instead, and the sandbox is told which of each it
  belongs to. The conversation store and the workspace's own context belong to the workspace, shared
  by every project in it. The working files and the project's context belong to the project.

  These scenarios use a sandbox double, so they state where state belongs and not that a real daemon
  honours it. The real thing is proved twice against Docker: TestDockerProviderKeepsStateAcrossContainers
  writes a file, destroys the container and reads it back, and continuous integration does the same
  through the composed stack and the quay command line tool.

  Background:
    Given a running control plane
    And a workspace named "me"
    And a project named "house-bills"

  Scenario: A replaced sandbox belongs to the same project, so it finds the same state
    Given a session started by dispatching "remember this"
    When the operator stops the session
    And the operator dispatches "are you still there" to the same session
    Then 2 sandboxes have been created
    And every sandbox was created for the same workspace and project

  Scenario: Two sessions in one project share the project's working directory
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new session
    Then 2 sandboxes have been created
    And every sandbox was created for the same workspace and project

  Scenario: Two projects in one workspace share the conversation store and nothing else
    Given a second project named "gardening"
    When the operator dispatches "hello" to the project
    And the operator dispatches "hello" to the second project
    Then the sandboxes were created for one workspace but different projects

  # A sandbox holds a value for the life of its container and the model can read it, which is the
  # point of giving it one. Setting a secret on a workspace is the operator saying its sessions may
  # use it, so that is the whole of the decision and there is no second list to keep.
  Scenario: A session is given the secrets its workspace holds
    Given a workspace named "acme"
    And a project named "house-bills"
    And the workspace has the secret "GITHUB_TOKEN" set to "ghp-1234"
    And the workspace has the secret "STRIPE_KEY" set to "sk-live-and-wanted"
    When the operator dispatches "hello" to the project
    Then the sandbox carries "GITHUB_TOKEN" set to "ghp-1234"
    And the sandbox carries "STRIPE_KEY" set to "sk-live-and-wanted"

  # One workspace's secrets are its own. This is the isolation the whole design turns on, and it is
  # the only boundary left now that naming is gone.
  Scenario: A session is given nothing from another workspace
    Given a workspace named "acme"
    And a project named "house-bills"
    And a second workspace named "rivals" with a project
    And the second workspace has the secret "STRIPE_KEY" set to "sk-live-nobody-asked"
    When the operator dispatches "hello" to the project
    Then the sandbox carries nothing called "STRIPE_KEY"

  Scenario: A secret nobody set is not carried
    Given a workspace named "acme"
    And a project named "house-bills"
    When the operator dispatches "hello" to the project
    Then the reply is "you said: hello"
    And the sandbox carries nothing called "GITHUB_TOKEN"

  # The system puts the address a session dials and the token it dials with into the sandbox itself.
  # A workspace secret answering to one of those names would be posing as the system rather than being
  # handed out by it, so those names never travel however they were set.
  Scenario: A workspace secret cannot pose as the system's own configuration
    Given a workspace named "acme"
    And a project named "house-bills"
    And the workspace has the secret "QC_TOKEN" set to "not-the-systems-token"
    When the operator dispatches "hello" to the project
    Then the sandbox carries nothing called "QC_TOKEN"

  # The model's token is how a task runs at all, so it is carried without being named.
  Scenario: The model's own token needs no naming
    Given a workspace named "acme"
    And a project named "house-bills"
    And the workspace has the subscription token "tok-xyz"
    When the operator dispatches "hello" to the project
    Then the sandbox carries "CLAUDE_CODE_OAUTH_TOKEN" set to "tok-xyz"

  # `git` has been in the sandbox image the whole time and unusable, because a container has no
  # identity: the tool refuses to commit rather than guessing, which is right and is a wall to walk
  # into halfway through a job.
  Scenario: A session can commit as the operator
    Given a system whose commits are by "A Name" at "a@example.com"
    And a workspace named "acme"
    And a project named "house-bills"
    When the operator dispatches "hello" to the project
    Then the sandbox can commit as "A Name" at "a@example.com"

  # Half of one is worse than none. Git refuses either way, and a half identity looks configured, so
  # the operator goes looking for the problem somewhere else.
  Scenario: Half an identity is carried as none of one
    Given a system whose commits are by "A Name" at no address
    And a workspace named "acme"
    And a project named "house-bills"
    When the operator dispatches "hello" to the project
    Then the sandbox carries no part of an identity

  # The workspace's volume is one directory shared by every session in it, and that is what makes one
  # clone of a repository serve all of them. Sharing a directory means naming what you put in it, so a
  # session is told which session it is: the git skill names a working tree and a branch after this.
  Scenario: A session is told which session it is
    When the operator dispatches "hello" to the project
    Then the sandbox carries its own session identifier

  # The collision this exists to avoid. Two sessions see the same paths, so two working trees named
  # the same are one path as far as the clone is concerned, and the second takes the first away.
  Scenario: Two sessions in one workspace are told different identifiers
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new session
    Then the two sandboxes carry different session identifiers
