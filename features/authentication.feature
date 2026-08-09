Feature: The crew refuses a caller it cannot recognise
  The control plane's port is the whole crew: every secret name, every session,
  the context injected into every sandbox. A caller proves it is the operator's
  tool by presenting the crew's token, and a call that does not carry it is
  refused before it reaches anything.

  Scenario: a caller presenting the crew's token is served
    When a caller presents the crew's token
    Then the caller is served

  Scenario: a caller presenting nothing is refused and told what is missing
    When a caller presents no token
    Then the caller is refused, told a token is missing and where quay reads one from

  Scenario: a caller presenting a token that is not the crew's is refused
    When a caller presents a token that is not the crew's
    Then the caller is refused, told the token is not this crew's

  # The driver drives the crew; it does not widen it. Its token is its own, so the crew can tell
  # its calls apart, and the calls that grant capability are refused to it: a session that can
  # drive the crew must never be able to grant itself anything.
  Scenario: the driver cannot set a secret
    When the driver asks to set a secret
    Then the driver is refused, told the call is the operator's to make

  Scenario: the driver cannot read what secrets exist
    When the driver asks what secrets a workspace holds
    Then the driver is refused, told the call is the operator's to make

  Scenario: the driver cannot import a skill
    When the driver asks to import a skill
    Then the driver is refused, told the call is the operator's to make

  Scenario: the driver cannot attach a skill
    When the driver asks to attach a skill
    Then the driver is refused, told the call is the operator's to make

  Scenario: the driver cannot detach a skill
    When the driver asks to detach a skill
    Then the driver is refused, told the call is the operator's to make

  Scenario: the driver cannot loosen a session's permission mode
    When the driver asks to change a session's permission mode
    Then the driver is refused, told the call is the operator's to make

  Scenario: the driver cannot write the crew's context
    When the driver asks to write the crew's context
    Then the driver is refused, told the call is the operator's to make

  Scenario: the driver can still write a project's context
    Given a workspace named "acme"
    And a project named "house-bills"
    When the driver asks to write the project's context
    Then the caller is served

  Scenario: the driver can still make a workspace
    When the driver asks to make a workspace named "made-by-the-driver"
    Then the caller is served

  Scenario: the operator is refused none of what the driver is
    Given a workspace named "acme"
    When the operator sets a secret with the crew's token
    Then the caller is served
