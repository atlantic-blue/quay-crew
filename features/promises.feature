Feature: A change carries a scenario, or says why not

  `main` says what this repository holds itself to: the behaviour of this system is written out as
  scenarios in `features/`, and anything not in there does not exist. That was a promise to a reader,
  and nothing asked whether a change kept it.

  One change shipped 200 lines of new behaviour, a rule that refuses a whole class of exec, with no
  scenario. Every check was green. Nothing was wrong with the checks: they were never asked the
  question. The promise held for as long as whoever opened the pull request remembered it, which is
  not a gate.

  So a check now reads the diff. A change that touches behaviour has to carry a scenario under
  `features/`. A change may legitimately have none, and the answer to that is a stated reason rather
  than silence, so a line in the pull request body stands in for it:

      No scenario: the behaviour is unchanged, this moves it between packages

  The reason is a sentence, so one word after the colon is refused. Whether the sentence is a good one
  is the reviewer's to judge; the check only makes it impossible to say nothing at all.

  These scenarios run the real command over real git repositories made for the scenario, so what is
  proved here is what continuous integration runs.

  Scenario: A change that keeps the promise is let through
    Given a change that edits "internal/session/waiting.go"
    And it writes "features/promises.feature"
    When the check reads the change
    Then it lets the change through

  Scenario: Behaviour with no scenario is refused, and told what to write
    Given a change that edits "internal/session/waiting.go"
    When the check reads the change
    Then the check refuses, naming "internal/session/waiting.go"
    And it says the change carries no "scenario"
    And it prints the line that would say why there is none

  Scenario: A stated reason stands in for the scenario
    Given a change that edits "internal/session/waiting.go"
    And the pull request body says "No scenario: the behaviour is unchanged, this moves it between packages"
    When the check reads the change
    Then it lets the change through

  Scenario: One word after the colon is silence with a colon in front
    Given a change that edits "internal/session/waiting.go"
    And the pull request body says "No scenario: none"
    When the check reads the change
    Then the check refuses
    And it says the change carries no "scenario"

  Scenario: A change that touches no behaviour is asked for nothing
    Given a change that edits "docs/ARCHITECTURE.md"
    When the check reads the change
    Then it lets the change through

  Scenario: A change to a test alone is not behaviour
    Given a change that edits "internal/session/waiting_test.go"
    When the check reads the change
    Then it lets the change through

  Scenario: Deleting the last scenario is not carrying one
    Given a change that edits "internal/session/waiting.go"
    And it deletes "features/promises.feature"
    When the check reads the change
    Then the check refuses
    And it says the change carries no "scenario"

  Scenario: A range that holds nothing is refused, because an empty diff keeps every promise
    Given a change that edits "internal/session/waiting.go"
    When the check reads a range that holds nothing
    Then the check refuses
    And it says it read no files at all

  Scenario: A reason shown as an example does not stand in for anything
    Given a change that edits "internal/session/waiting.go"
    And the pull request body shows the reason as a fenced example
    When the check reads the change
    Then the check refuses
    And it says the change carries no "scenario"
