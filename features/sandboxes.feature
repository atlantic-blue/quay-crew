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
    And the operator dispatches "are you still there" to the same thread
    Then 2 sandboxes have been created
    And every sandbox was created for the same workspace and project

  Scenario: Two threads in one project share the project's working directory
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new thread
    Then 2 sandboxes have been created
    And every sandbox was created for the same workspace and project

  Scenario: Two projects in one workspace share the conversation store and nothing else
    Given a second project named "gardening"
    When the operator dispatches "hello" to the project
    And the operator dispatches "hello" to the second project
    Then the sandboxes were created for one workspace but different projects

  # A sandbox holds a value for the life of its container and the model can read it, which is the
  # point of giving it one. So a session is handed the secrets it needs by name, not everything the
  # workspace happens to hold. Before this only the model's own token could reach a sandbox at all,
  # hardcoded, so a workspace could keep a token for anything else and no session could ever use it.
  Scenario: A session is given the secrets the crew named, and no others
    Given a crew that gives its sessions the secret "GITHUB_TOKEN"
    And a workspace named "acme"
    And a project named "house-bills"
    And the workspace has the secret "GITHUB_TOKEN" set to "ghp-1234"
    And the workspace has the secret "STRIPE_KEY" set to "sk-live-nobody-asked"
    When the operator dispatches "hello" to the project
    Then the sandbox carries "GITHUB_TOKEN" set to "ghp-1234"
    And the sandbox carries nothing called "STRIPE_KEY"

  Scenario: A name with nothing set against it is skipped rather than refused
    Given a crew that gives its sessions the secret "GITHUB_TOKEN"
    And a workspace named "acme"
    And a project named "house-bills"
    When the operator dispatches "hello" to the project
    Then the reply is "you said: hello"
    And the sandbox carries nothing called "GITHUB_TOKEN"

  # The model's token is how a turn runs at all, so it is carried without being named.
  Scenario: The model's own token needs no naming
    Given a workspace named "acme"
    And a project named "house-bills"
    And the workspace has the subscription token "tok-xyz"
    When the operator dispatches "hello" to the project
    Then the sandbox carries "CLAUDE_CODE_OAUTH_TOKEN" set to "tok-xyz"

  # `git` has been in the sandbox image the whole time and unusable, because a container has no
  # identity: the tool refuses to commit rather than guessing, which is right and is a wall to walk
  # into halfway through a piece of work.
  Scenario: A session can commit as the operator
    Given a crew whose commits are by "A Name" at "a@example.com"
    And a workspace named "acme"
    And a project named "house-bills"
    When the operator dispatches "hello" to the project
    Then the sandbox can commit as "A Name" at "a@example.com"

  # Half of one is worse than none. Git refuses either way, and a half identity looks configured, so
  # the operator goes looking for the problem somewhere else.
  Scenario: Half an identity is carried as none of one
    Given a crew whose commits are by "A Name" at no address
    And a workspace named "acme"
    And a project named "house-bills"
    When the operator dispatches "hello" to the project
    Then the sandbox carries no part of an identity
