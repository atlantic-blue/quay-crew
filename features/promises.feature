Feature: A change carries a scenario and a changelog entry, or says why not

  `main` says what this repository holds itself to, in two places. `CHANGELOG.md` opens with "anything
  not listed here does not exist", and the line under it says "the behaviour of each of these is
  written out as scenarios in `features/`". Both were promises to a reader, and nothing asked whether
  a change kept them.

  One change shipped 200 lines of new behaviour, a rule that refuses a whole class of job brief, with
  no scenario and no changelog entry. Every check was green. Nothing was wrong with the checks: they
  were never asked the question. The promise held for as long as whoever opened the pull request
  remembered it, which is not a gate.

  So a check now reads the diff. A change that touches behaviour has to carry a file under
  `changelog.d/` and a scenario under `features/`. A change may legitimately have neither, and the
  answer to that is a stated reason rather than silence, so a line in the pull request body stands in
  for either one:

      No changelog entry: this only renames a field, and the name is not in the record
      No scenario: the behaviour is unchanged, this moves it between packages

  The reason is a sentence, so one word after the colon is refused. Whether the sentence is a good one
  is the reviewer's to judge; the check only makes it impossible to say nothing at all.

  These scenarios run the real command over real git repositories made for the scenario, so what is
  proved here is what continuous integration runs.

  Scenario: A change that keeps both promises is let through
    Given a change that edits "internal/job/waiting.go"
    And it writes "changelog.d/486-a-check-reads-the-diff.md"
    And it writes "features/promises.feature"
    When the check reads the change
    Then it lets the change through

  Scenario: Behaviour with no changelog entry is refused, and told what to write
    Given a change that edits "internal/job/waiting.go"
    And it writes "features/promises.feature"
    When the check reads the change
    Then the check refuses, naming "internal/job/waiting.go"
    And it says the change carries no "changelog entry"
    And it prints the line that would say why there is none

  Scenario: Behaviour with no scenario is refused
    Given a change that edits "internal/job/waiting.go"
    And it writes "changelog.d/486-a-check-reads-the-diff.md"
    When the check reads the change
    Then the check refuses
    And it says the change carries no "scenario"

  Scenario: A stated reason stands in for the scenario
    Given a change that edits "internal/job/waiting.go"
    And it writes "changelog.d/486-a-check-reads-the-diff.md"
    And the pull request body says "No scenario: the behaviour is unchanged, this moves it between packages"
    When the check reads the change
    Then it lets the change through

  Scenario: One word after the colon is silence with a colon in front
    Given a change that edits "internal/job/waiting.go"
    And it writes "changelog.d/486-a-check-reads-the-diff.md"
    And the pull request body says "No scenario: none"
    When the check reads the change
    Then the check refuses
    And it says the change carries no "scenario"

  Scenario: A change that touches no behaviour is asked for neither
    Given a change that edits "docs/ARCHITECTURE.md"
    When the check reads the change
    Then it lets the change through

  Scenario: A change to a test alone is not behaviour
    Given a change that edits "internal/job/waiting_test.go"
    When the check reads the change
    Then it lets the change through

  Scenario: Deleting the last scenario is not carrying one
    Given a change that edits "internal/job/waiting.go"
    And it writes "changelog.d/486-a-check-reads-the-diff.md"
    And it deletes "features/promises.feature"
    When the check reads the change
    Then the check refuses
    And it says the change carries no "scenario"

  Scenario: A range that holds nothing is refused, because an empty diff keeps every promise
    Given a change that edits "internal/job/waiting.go"
    When the check reads a range that holds nothing
    Then the check refuses
    And it says it read no files at all

  Scenario: An entry written into the shared file is told where an entry goes now
    Given a change that edits "internal/job/waiting.go"
    And it writes "features/promises.feature"
    And it also edits "CHANGELOG.md"
    When the check reads the change
    Then the check refuses
    And it says the change carries no "changelog entry"
    And it says an entry is its own file now
