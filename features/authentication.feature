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
