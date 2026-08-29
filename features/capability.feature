Feature: A session may declare jobs, within limits

  A session can now declare jobs of its own. What bounds it is two things that mean different
  things: the role it runs as says what it may do, and the workspace says how much of it. The
  effective capability is the intersection.

  A session holds strictly less than the driver does. Its credential is minted for one
  job, carries only the verbs that job's role declared, and expires with the job. A credential
  read out of a container grants what that one job could do and only until it ends.

  Depth is what stops recursion, and it starts at zero: no session may declare a job until an
  operator raises the ceiling, per workspace, deliberately.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A workspace starts with no room for a session to declare anything
    When the operator reads the limits of the workspace
    Then the limits allow no depth at all
    And the limits say the rest is unset

  Scenario: A session whose role does not grant job.create declares nothing
    Given the workspace allows jobs down to depth 2
    And a job titled "clear the backlog" running as a role that may only read jobs
    When that session declares a job
    Then the crew refuses it and names the verb it lacks
    And the project holds only the job the operator declared

  Scenario: A session whose role grants job.create declares job under its own
    Given the workspace allows jobs down to depth 2
    And a job titled "clear the backlog" running as a role that may create jobs
    When that session declares a job
    Then the new job hangs under the job that declared it, one level deeper

  Scenario: A job deeper than the workspace allows is refused, naming the limit
    Given the workspace allows jobs down to depth 1
    And a job titled "clear the backlog" running as a role that may create jobs
    And that session declared a job
    When the job at depth 1 declares another
    Then the crew refuses it and names the limit and the command that raises it

  Scenario: An operator raises the ceiling and the same declaration is allowed
    Given the workspace allows jobs down to depth 1
    And a job titled "clear the backlog" running as a role that may create jobs
    And that session declared a job
    And the job at depth 1 declares another
    When the operator allows jobs down to depth 2
    And the job at depth 1 declares another
    Then the new job hangs under the job that declared it, one level deeper

  Scenario: A session may not raise its own ceiling
    Given the workspace allows jobs down to depth 2
    And a job titled "clear the backlog" running as a role that may create jobs
    When that session tries to raise the ceiling
    Then the crew refuses the session that call

  Scenario: A session may not reach the calls that grant capability
    Given the workspace allows jobs down to depth 2
    And a job titled "clear the backlog" running as a role that may create jobs
    When that session tries to attach a hook
    Then the crew refuses the session that call
    When that session tries to set a secret
    Then the crew refuses the session that call

  # The credential is the boundary, so what it carries is the whole of what a session holds.
  Scenario: The credential a session runs under is bound to its job and expires
    Given the workspace allows jobs down to depth 2
    And a job titled "clear the backlog" running as a role that may create jobs
    Then the credential names that job, carries only the verbs the role declared, and runs out

  Scenario: The driver may no longer touch a hook
    Then the driver is refused importing, listing, attaching and detaching a hook

  Scenario: An operator sets the ceiling and reads it back
    When the operator allows jobs down to depth 2
    Then the limits allow jobs down to depth 2
