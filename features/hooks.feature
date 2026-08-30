Feature: A hook is a constraint the crew holds

  A crew gives every session its rules as context, and context is advice. A hook is the other kind of
  statement: what a session may not do, checked when it tries. It is authored as files, imported into
  the crew, and attached to a workspace or to the whole crew, which is the shape a skill already has.

  These scenarios drive the control plane over its real interface. What a hook does inside a sandbox
  is proved separately, against a real container, because a hook the runtime never calls is a hook
  that does nothing.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A hook is imported and the crew holds it
    When the operator imports a hook "git-approval" firing on "PreToolUse" for "Bash"
    Then the crew holds 1 hook
    And the hook "git-approval" fires on "PreToolUse"

  # This is the refusal that matters most. A misspelled event imports, attaches, mounts and is never
  # called, and nothing anywhere says so, which reads exactly like a hook that approves of everything.
  Scenario: A hook firing on an event the runtime never raises is refused
    When the operator imports a hook "guard" firing on "PreToolUseHook" for ""
    Then the control plane refuses it as invalid
    And the refusal names "PreToolUseHook"
    And the crew holds 0 hooks

  # Overwriting would change a constraint under sessions already running under it, which is how a gate
  # quietly stops gating.
  Scenario: Importing a different hook at a version already imported is refused
    Given a hook "git-approval" imported firing on "PreToolUse"
    When the operator imports "git-approval" again at the same version carrying something different
    Then the control plane refuses it as the wrong state
    And the refusal says to raise the version

  Scenario: A workspace runs under the hooks attached to it
    Given a hook "git-approval" imported firing on "PreToolUse"
    When the operator attaches the hook "git-approval" to the workspace
    Then the workspace runs under 1 hook

  # Issue 280's first acceptance criterion.
  Scenario: A hook attached to one workspace does not reach another
    Given a hook "git-approval" imported firing on "PreToolUse"
    And another workspace named "other"
    When the operator attaches the hook "git-approval" to the workspace
    Then the other workspace runs under 0 hooks

  # Issue 280's second acceptance criterion. This is the level most hooks want: a constraint the crew
  # agreed on is not usually a per workspace opinion.
  Scenario: A hook held by the crew reaches a workspace made after it
    Given a hook "git-approval" imported firing on "PreToolUse"
    When the operator attaches the hook "git-approval" to the crew
    And another workspace named "later"
    Then the other workspace runs under 1 hook
    And that hook is reported as the crew's

  # Two separate statements, and the wider one does not undo the narrower one.
  Scenario: Taking a hook off the crew leaves a workspace that attached it for itself
    Given a hook "git-approval" imported firing on "PreToolUse"
    When the operator attaches the hook "git-approval" to the crew
    And the operator attaches the hook "git-approval" to the workspace
    And the operator takes the hook "git-approval" off the crew
    Then the workspace runs under 1 hook

  Scenario: Detaching a hook leaves it imported, so another workspace can still have it
    Given a hook "git-approval" imported firing on "PreToolUse"
    When the operator attaches the hook "git-approval" to the workspace
    And the operator detaches the hook "git-approval" from the workspace
    Then the workspace runs under 0 hooks
    And the crew holds 1 hook

  Scenario: Attaching a hook the crew has not imported is refused
    When the operator attaches the hook "nowhere" to the workspace
    Then the control plane refuses it as not found

  Scenario: A crew with no hooks enforces nothing, and says so rather than saying nothing
    Then the crew holds 0 hooks

  # A hook reaches a session by being mounted and bound, and both halves have to be true. The files
  # without the settings is a directory nothing reads; the settings without the files names a command
  # that is not there. Proved against a real container in internal/sandbox, because a hook the runtime
  # never calls is a hook that does nothing.
  Scenario: A session is built with the hooks its workspace runs under
    Given a hook "git-approval" imported firing on "PreToolUse"
    And the operator attaches the hook "git-approval" to the workspace
    When the operator dispatches "hello" to the project
    Then the session's sandbox carries the hooks directory
    And the hooks directory is mounted read only
    And the settings file binds "git-approval" to "PreToolUse"

  Scenario: The task is told to load the hooks settings
    Given a hook "git-approval" imported firing on "PreToolUse"
    And the operator attaches the hook "git-approval" to the workspace
    When the operator dispatches "hello" to the project
    Then the task loaded the hooks settings

  # The settings the crew renders carry more than hooks: the line the runtime draws under the
  # conversation is in there too, and an operator attached to a session with no hooks needs that line
  # as much as anybody. So the directory and the file travel to every session, holding no hook.
  Scenario: A session under no hooks still carries the crew's settings
    When the operator dispatches "hello" to the project
    Then the session's sandbox carries the hooks directory
    And the settings file binds nothing to any event
    And the task loaded the hooks settings

  # A seeded hook used to mean a hook that cannot refuse, because a hook that refuses wrongly blocks
  # the work. The merge gate refuses, and it is seeded anyway: it refuses one thing, no session in
  # this crew is ever meant to do that thing, and a gate somebody has to remember to attach is off in
  # every crew nobody set up.
  Scenario: A fresh crew is already under the hooks this build ships
    Given a crew seeded with the hooks this build ships
    Then the crew holds the "prompt-analyser" hook
    And the crew holds the "merge-gate" hook
    And the workspace runs under 2 hooks

  # An operator who takes a hook off has said something. Putting it back on the next restart is the
  # crew overruling the person operating it.
  Scenario: A hook taken off the crew stays off across a restart
    Given a crew seeded with the hooks this build ships
    When the operator takes the hook "prompt-analyser" off the crew
    And the control plane restarts
    Then the workspace runs under no "prompt-analyser" hook

  # A fix to a shipped hook has to be able to reach a crew that is already using it. Seeding once
  # reached only a crew with no hooks at all, which is no crew that has ever been used, and the
  # analyser's first fix was stranded there.
  Scenario: A newer version of a shipped hook reaches a crew that already holds an older one
    Given a crew already under version 1 of "prompt-analyser"
    When the control plane restarts
    Then the crew holds "prompt-analyser" at the version this build ships

  # Importing offers it. Being under it is a separate decision, because a hook is pinned so it cannot
  # change under a session already running with it.
  Scenario: An upgrade does not move a crew onto a newer version of a hook by itself
    Given a crew already under version 1 of "prompt-analyser"
    When the control plane restarts
    Then the workspace is still under "prompt-analyser" at version 1
    And attaching it again moves the workspace to the version this build ships
