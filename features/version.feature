Feature: The crew says which build it is, and drift is reported

  A crew is three parts and each is built on its own: the tool the operator types, the control plane
  that runs the work, and the image every session runs in. An upgrade stops containers, so an
  operator delays it, and the three drift apart. Nothing said so.

  On 27 August 2026 three defects were investigated as if they were live. All three were fixed
  already. The tool in use was built thirteen minutes before the fix for the first one landed.

  So the crew reports its own build, the tool prints all three, and any command says on standard
  error when the tool and the crew are different builds. The warning goes to standard error and
  never stops the command, because standard output is where a caller reads data.

  Background:
    Given a running control plane
    And the crew was built from "cafe1234"
    And the crew listens on an address the tool can dial

  Scenario: The version command names the tool, the crew and the sandbox image
    When the caller asks the tool for the version
    Then standard output names the build of the tool
    And standard output names the build of the crew
    And standard output names the build of the sandbox image
    And the command succeeds

  Scenario: A tool and a crew from different builds are reported as different
    When the caller asks the tool for the version
    Then standard output says the tool and the crew are different builds
    And standard output names both of those builds

  Scenario: A sandbox image from another build is reported as different
    Given the sandbox image was made from "01dimage"
    When the caller asks the tool for the version
    Then standard output says the sandbox image is a different build
    And standard output names both of those builds

  Scenario: Three parts from one build report no difference at all
    Given the crew and the sandbox image were built from the same build as the tool
    When the caller asks the tool for the version
    Then standard output says nothing about a difference
    And the command succeeds

  # A crew from before this field existed answers with nothing. That is an old crew, not a fault, so
  # the tool says which build began to answer rather than reporting an error.
  Scenario: A crew too old to say which build it is says so
    Given the crew does not say which build it is
    When the caller asks the tool for the version
    Then standard output says the build of the crew is unknown
    And standard output names the build that first reports it
    And standard output says nothing about a difference
    And the command succeeds

  Scenario: Any command reports the difference on standard error
    Given a workspace named "acme"
    When the caller lists the workspaces
    Then standard error says the tool and the crew are different builds
    And standard output carries the answer and nothing about builds
    And the command succeeds

  Scenario: A command against a crew of the same build says nothing
    Given the crew and the sandbox image were built from the same build as the tool
    And a workspace named "acme"
    When the caller lists the workspaces
    Then standard error says nothing
    And the command succeeds

  # A crew that cannot be reached must not hold up a command that does not need it either.
  Scenario: A crew that cannot be reached is not reported as drift
    Given the crew cannot be reached
    When the caller asks the tool for the manual
    Then standard error says nothing about builds
    And the command succeeds
