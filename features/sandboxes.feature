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
