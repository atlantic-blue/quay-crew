Feature: What a task spent is measured

  Tokens are the bill. The useful question is never what the crew cost in total, it is what one job
  cost, so every measurement says which workspace and which project it belongs to and which
  model spent it. A task that failed spent what it spent and is counted the same way, because the
  task somebody is investigating is usually the one that went wrong.

  Background:
    Given a running control plane
    And a workspace named "me"
    And a project named "house-bills"

  Scenario: A task publishes the tokens it spent and what they would cost
    Given the model reports spending 1200 in and 340 out, costing 0.0241
    When the operator dispatches "remember the number" to the project
    Then the crew measures 1540 tokens spent on "me" and "house-bills"
    And the crew measures 0.0241 of cost

  Scenario: A task that failed is still counted, and says it failed
    Given the next task will fail
    When the operator dispatches "remember the number" to the project
    Then the crew counts one task, which failed
    # It contributes no tokens, because a task that fails returns nothing to read them from. What it
    # spent before it failed is invisible today, and that is issue 16 rather than this scenario
    # quietly asserting a zero.
    And the crew measures no tokens and no cost

  Scenario: A task whose model said nothing is counted without inventing a cost
    When the operator dispatches "remember the number" to the project
    Then the crew counts one task
    And the crew measures no tokens and no cost
