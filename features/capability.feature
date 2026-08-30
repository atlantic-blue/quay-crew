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
    Then the system refuses it and names the verb it lacks
    And the project holds only the job the operator declared

  Scenario: A session whose role grants job.create declares job under its own
    Given the workspace allows jobs down to depth 2
    And a job titled "clear the backlog" running as a role that may create jobs
    When that session declares a job
    Then the new job hangs under the job that declared it, one level deeper

  # A session cannot resolve an address: resolving one means listing workspaces and projects, and a
  # role grants the four job verbs and nothing else. So it names no project, and the system reads that
  # from the credential, the same place the parent comes from.
  Scenario: A session names no project and its job lands in the one its credential names
    Given the workspace allows jobs down to depth 2
    And a job titled "clear the backlog" running as a role that may create jobs
    When that session declares a job naming no project
    Then the new job is in the same project as the job that declared it
    And the new job hangs under the job that declared it, one level deeper

  Scenario: A job deeper than the workspace allows is refused, naming the limit
    Given the workspace allows jobs down to depth 1
    And a job titled "clear the backlog" running as a role that may create jobs
    And that session declared a job
    When the job at depth 1 declares another
    Then the system refuses it and names the limit and the command that raises it

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
    Then the system refuses the session that call

  Scenario: A session may not reach the calls that grant capability
    Given the workspace allows jobs down to depth 2
    And a job titled "clear the backlog" running as a role that may create jobs
    When that session tries to attach a hook
    Then the system refuses the session that call
    When that session tries to set a secret
    Then the system refuses the session that call

  # A session was handed the address and the credential and had no route to the address, so every
  # call died resolving the name and nothing was ever refused. These two say what a session is given
  # and what it is not, which is the half of the fault the system decides.
  Scenario: A session running a job is told where the system is, and what it may spend there
    Given a system that sessions can reach at "controlplane:50051"
    And the workspace allows jobs down to depth 2
    And a job titled "clear the backlog" running as a role that may create jobs
    When the system runs that job
    Then the task carries the address of the system
    And the task carries the credential minted for that job, not the operator's token

  Scenario: A task running no job is told nothing
    Given a system that sessions can reach at "controlplane:50051"
    When the operator dispatches "hello" to the project
    Then the task carries no address and no token

  # The load bearing test is the refusal, not the call. A role's verbs list is the whole boundary, so a
  # verb it does not carry has to come back as a refusal a session can act on.
  Scenario: A session whose role may not stop a job is refused, and told where the verb comes from
    Given the workspace allows jobs down to depth 2
    And a job titled "clear the backlog" running as a role that may create jobs
    When that session tries to stop the job it is running
    Then the system refuses it and names the verb it lacks and how an operator grants it
    And the job is still running

  # The credential is the boundary, so what it carries is the whole of what a session holds.
  Scenario: The credential a session runs under is bound to its job and expires
    Given the workspace allows jobs down to depth 2
    And a job titled "clear the backlog" running as a role that may create jobs
    Then the credential names that job, carries only the verbs the role declared, and runs out

  # A credential is handed to a sandbox once at dispatch and nothing refreshes it, so its life has to
  # cover the job rather than the system's hold on the job. It covered the hold, which is sixty seconds,
  # and a root job that ran for twenty nine minutes declared none of its three children.
  Scenario: A session declares a child long after the first minute of its job
    Given the workspace allows jobs down to depth 2
    And a job titled "clear the backlog" running as a role that may create jobs
    And that job has been running for 29 minutes
    When that session declares a job
    Then the new job hangs under the job that declared it, one level deeper

  # What a session is told matters as much as when. Told the token is not this system's, a session
  # concludes it holds a bad credential and stops, and this one had simply run out.
  Scenario: A session whose credential has run out is told that, and when it ran out
    Given the workspace allows jobs down to depth 2
    And a job titled "clear the backlog" running as a role that may create jobs
    And that job has been running for 30 days
    When that session declares a job
    Then the system refuses it, says the credential ran out, and says when

  Scenario: A session whose job has ended is told the job ended
    Given the workspace allows jobs down to depth 2
    And a job titled "clear the backlog" running as a role that may create jobs
    And the operator stops that job
    When that session declares a job
    Then the system refuses it and names the job that ended and the phase it ended in

  Scenario: The driver may no longer touch a hook
    Then the driver is refused importing, listing, attaching and detaching a hook

  Scenario: An operator sets the ceiling and reads it back
    When the operator allows jobs down to depth 2
    Then the limits allow jobs down to depth 2
