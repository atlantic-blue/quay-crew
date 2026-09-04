Feature: What an exec spent is measured

  Tokens are the bill. The useful question is never what the system cost in total, it is what one job
  cost, so every measurement says which workspace and which project it belongs to and which
  model spent it. An exec that failed spent what it spent and is counted the same way, because the
  exec somebody is investigating is usually the one that went wrong.

  Background:
    Given a running control plane
    And a workspace named "me"
    And a project named "house-bills"

  Scenario: An exec publishes the tokens it spent and what they would cost
    Given the model reports spending 1200 in and 340 out, costing 0.0241
    When the operator dispatches "remember the number" to the project
    Then the system measures 1540 tokens spent on "me" and "house-bills"
    And the system measures 0.0241 of cost
