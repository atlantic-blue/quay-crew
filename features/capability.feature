Feature: A session may declare work, within limits

  A session can now declare work of its own. What bounds it is two things that mean different
  things: the role it runs as says what it may do, and the workspace says how much of it. The
  effective capability is the intersection.

  A session holds strictly less than the driver does. Its credential is minted for one piece of
  work, carries only the verbs that work's role declared, and expires with the work. A credential
  read out of a container grants what that one piece of work could do and only until it ends.

  Depth is what stops recursion, and it starts at zero: no session may declare work until an
  operator raises the ceiling, per workspace, deliberately.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A workspace starts with no room for a session to declare anything
    When the operator reads the limits of the workspace
    Then the limits allow no depth at all
    And the limits say the rest is unset

  Scenario: A session whose role does not grant work.create declares nothing
    Given the workspace allows work down to depth 2
    And a piece of work titled "clear the backlog" running as a role that may only read work
    When that session declares a piece of work
    Then the crew refuses it and names the verb it lacks
    And the project holds only the work the operator declared

  Scenario: A session whose role grants work.create declares work under its own
    Given the workspace allows work down to depth 2
    And a piece of work titled "clear the backlog" running as a role that may create work
    When that session declares a piece of work
    Then the new work hangs under the work that declared it, one level deeper

  # A session cannot resolve an address: resolving one means listing workspaces and projects, and a
  # role grants the four work verbs and nothing else. So it names no project, and the crew reads that
  # from the credential, the same place the parent comes from.
  Scenario: A session names no project and its work lands in the one its credential names
    Given the workspace allows work down to depth 2
    And a piece of work titled "clear the backlog" running as a role that may create work
    When that session declares a piece of work naming no project
    Then the new work is in the same project as the work that declared it
    And the new work hangs under the work that declared it, one level deeper

  Scenario: Work deeper than the workspace allows is refused, naming the limit
    Given the workspace allows work down to depth 1
    And a piece of work titled "clear the backlog" running as a role that may create work
    And that session declared a piece of work
    When the work at depth 1 declares another
    Then the crew refuses it and names the limit and the command that raises it

  Scenario: An operator raises the ceiling and the same declaration is allowed
    Given the workspace allows work down to depth 1
    And a piece of work titled "clear the backlog" running as a role that may create work
    And that session declared a piece of work
    And the work at depth 1 declares another
    When the operator allows work down to depth 2
    And the work at depth 1 declares another
    Then the new work hangs under the work that declared it, one level deeper

  Scenario: A session may not raise its own ceiling
    Given the workspace allows work down to depth 2
    And a piece of work titled "clear the backlog" running as a role that may create work
    When that session tries to raise the ceiling
    Then the crew refuses the session that call

  Scenario: A session may not reach the calls that grant capability
    Given the workspace allows work down to depth 2
    And a piece of work titled "clear the backlog" running as a role that may create work
    When that session tries to attach a hook
    Then the crew refuses the session that call
    When that session tries to set a secret
    Then the crew refuses the session that call

  # A session was handed the address and the credential and had no route to the address, so every
  # call died resolving the name and nothing was ever refused. These two say what a session is given
  # and what it is not, which is the half of the fault the crew decides.
  Scenario: A session running a piece of work is told where the crew is, and what it may spend there
    Given a crew that sessions can reach at "controlplane:50051"
    And the workspace allows work down to depth 2
    And a piece of work titled "clear the backlog" running as a role that may create work
    When the crew runs that work
    Then the task carries the address of the crew
    And the task carries the credential minted for that work, not the operator's token

  Scenario: A task running no piece of work is told nothing
    Given a crew that sessions can reach at "controlplane:50051"
    When the operator dispatches "hello" to the project
    Then the task carries no address and no token

  # The load bearing test is the refusal, not the call. A role's may list is the whole boundary, so a
  # verb it does not carry has to come back as a refusal a session can act on.
  Scenario: A session whose role may not stop work is refused, and told where the verb comes from
    Given the workspace allows work down to depth 2
    And a piece of work titled "clear the backlog" running as a role that may create work
    When that session tries to stop the work it is running
    Then the crew refuses it and names the verb it lacks and how an operator grants it
    And the work is still running

  # The credential is the boundary, so what it carries is the whole of what a session holds.
  Scenario: The credential a session runs under is bound to its work and expires
    Given the workspace allows work down to depth 2
    And a piece of work titled "clear the backlog" running as a role that may create work
    Then the credential names that work, carries only the verbs the role declared, and runs out

  Scenario: The driver may no longer touch a hook
    Then the driver is refused importing, listing, attaching and detaching a hook

  Scenario: An operator sets the ceiling and reads it back
    When the operator allows work down to depth 2
    Then the limits allow work down to depth 2
