Feature: The operator can find the files the model reads

  Context is files on the operator's machine, mounted into every sandbox: one directory per workspace
  and one per project, with CLAUDE.md in either read by the model as its memory. The mounts have
  existed for a while and nothing said where they were, so the feature worked and nobody could find
  it: both directories were empty and the identifiers in their paths are twenty four characters of
  hexadecimal.

  The control plane answers this rather than either client working it out, because the tool runs on
  the operator's machine and the layout belongs to the crew. The console and the command line are two
  clients of one call, which is the only way the two cannot drift.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: The crew says where a project's context lives
    When the operator asks where context lives
    Then it names a workspace directory and a project directory
    And each one says where it appears inside a sandbox
    And each one names the memory file the model reads

  Scenario: A directory with nothing in it says so
    When the operator asks where context lives
    Then no context has been written yet

  Scenario: Setting a project's context is what the listing then reports
    When the operator sets the project's context to "pay the water bill first"
    And the operator asks where context lives
    Then the project's context has been written
    And the project's context reads "pay the water bill first"

  # The store is where context lives, and the file in a sandbox is a rendering of it. Rendering it is
  # the whole point: the model only reads files.
  Scenario: Setting a project's context writes the file the model reads
    When the operator sets the project's context to "pay the water bill first"
    Then the project's memory file on disk reads "pay the water bill first"

  # An agent that writes something into its own memory has learned something. Overwriting that on the
  # next turn would make the crew's memory strictly worse than a text file, so the file wins and is
  # taken into the store.
  Scenario: What an agent writes into its own memory is kept
    When the operator sets the project's context to "pay the water bill first"
    And something inside the sandbox writes "and the gas bill is quarterly" into the project's memory
    And the operator dispatches "hello" to the project
    And the operator asks where context lives
    Then the project's context reads "and the gas bill is quarterly"

  Scenario: A scope the crew does not have is refused
    When the operator sets context at scope "everything" to "no"
    Then the control plane refuses it as invalid

  Scenario: One workspace directory however many projects it holds
    Given a second project named "gardening"
    When the operator asks where context lives
    Then it names 1 workspace directory and 2 project directories
