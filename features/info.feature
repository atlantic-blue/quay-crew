Feature: The control plane says what it is running

  Two stacks look identical from the outside and behave nothing alike. One runs a real model in a
  container and keeps every conversation on disk; the other echoes a canned reply into a host process
  and forgets every workspace when it restarts. The operator is about to act on one of them, and they
  cannot tell which by looking at a list of sessions.

  So the control plane will say: which model backend a task runs against, what a session is isolated
  in, where workspaces and sessions are kept, and whether a conversation outlives its container. It is
  configuration, not a health check, and it never carries a secret.

  Background:
    Given a running control plane

  Scenario: It reports the backends it was configured with
    When the operator asks what the control plane is running
    Then it reports the model "fake", the sandbox "fake" and the store "memory"

  Scenario: It says a conversation would not outlive its container
    When the operator asks what the control plane is running
    Then it says a session's state is not kept outside its container

  Scenario: It says a conversation would outlive its container
    Given a control plane that keeps session state outside the container
    When the operator asks what the control plane is running
    Then it says a session's state is kept outside its container

  # Configuration is safe to hand out; a token is not. The reply is read by anything that can reach
  # the API, so nothing that arrived from the secrets backend may travel in it.
  Scenario: The answer carries no secret
    Given a workspace named "acme"
    And the workspace has the subscription token "tok-xyz"
    When the operator asks what the control plane is running
    Then the answer carries nothing from the secrets backend
