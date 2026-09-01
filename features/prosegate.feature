Feature: Prose written for a person is held to the standard

  Every role in this system writes prose for a person: pull request descriptions, changelog
  fragments, issue bodies, commit messages, documentation. The standard for that prose is Simplified
  Technical English, ASD-STE100. Until this, it was a sentence in a brief, which is the position the
  merge rule was in before the merge gate.

  A hook holds part of it. Not all of it, and being honest about which part is the whole design. A
  program can measure the length of a sentence, the length of a paragraph, the tense of a verb and
  the punctuation. It cannot measure the approved vocabulary, which is licensed rather than published
  as a list, and it cannot find an idiom: "fishing in that pond" is six ordinary words. Those two
  stay in the brief, where a person reads them.

  The gate is a hook, so the check happens inside the sandbox at the moment the write is about to
  land. These scenarios run the entry point this build ships, which is the same file a sandbox
  mounts, and they feed it what the model runtime feeds it.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # Attached, not automatic. This one refuses prose, and prose is what a role produces all day, so
  # the workspace that wants it says so.
  Scenario: A fresh system is offered the prose gate and is not put under it
    Given a system seeded with the hooks this build ships
    Then the system holds the "prose-gate" hook
    And the workspace runs under no "prose-gate" hook

  Scenario: A workspace that asks for the prose gate runs under it
    Given a system seeded with the hooks this build ships
    When the operator attaches the hook "prose-gate" to the workspace
    Then the workspace runs under the "prose-gate" hook

  Scenario Outline: Prose the standard refuses is refused, and the writer is told what to do to it
    When a session is about to write <prose> to "docs/EXAMPLE.md"
    Then the prose gate refuses it
    And the refusal quotes the prose and says what to do to it

    Examples: the measurable rules, one line each
      | prose                                                                                                                                      |
      | "The control plane reads the row and answers the question the caller asked, and it does that before the session starts, because nothing else reads that row at all." |
      | "One. Two things happen. Three is the count. Four comes next. Five is here. Six follows it. Seven is too many."                             |
      | "The gate has refused the command."                                                                                                        |
      | "The session had written the file before the check ran."                                                                                    |
      | "The task is running."                                                                                                                     |
      | "The gate reads the command - then it answers."                                                                                             |

  # The other direction, and the one that decides whether this gate is worth attaching. A gate with
  # false positives is a gate somebody turns off, and the real rule goes with it.
  Scenario Outline: Prose the standard allows goes through
    When a session is about to write <prose> to "docs/EXAMPLE.md"
    Then the prose gate allows it

    Examples:
      | prose                                                                     |
      | "The gate reads the command. It refuses a merge. It says what to do."     |
      | "Push the branch. Open a pull request. Ask the operator to merge it."     |
      | "There is nothing to prove. The file is missing."                         |
      | "Run `krewe hook detach system prose-gate` to take the gate off."         |
      | "The branch is named in kebab-case. The manifest is hook.yaml."           |

  # The one that decides whether this gate can be attached at all. A Go file is not prose, and a gate
  # that measured sentence length in source would refuse every file in this repository on its first
  # firing.
  Scenario: A source file is not prose, whatever is written in it
    When a session is about to write "The control plane reads the row and answers the question the caller asked, and it does that before the session starts, because nothing else reads that row at all." to "internal/hook/hook.go"
    Then the prose gate allows it

  # A rule that a document has to meet and a pull request body does not is a rule with a way around
  # it, and every role in this system opens a pull request on every slice.
  Scenario: Prose carried as an argument to a command is held to the same standard
    When a session runs the command: gh pr create --title "508: feat: a gate" --body "The control plane reads the row and answers the question the caller asked, and it does that before the session starts, because nothing else reads that row at all."
    Then the prose gate refuses it
    And the refusal quotes the prose and says what to do to it

  Scenario: The work a role does on every slice goes through
    When a session runs the command: gh pr create --title "508: feat: a gate" --body "What. This adds a gate. Why. The rule was a sentence in a brief."
    Then the prose gate allows it

  # It fires on every write and every command every session makes, so a payload it cannot read has to
  # go through. A gate that refuses what it does not understand refuses the work, and a broken hook
  # must not be able to stop a system.
  Scenario: A payload the gate cannot read lets the write through
    When a session sends the prose gate a payload it cannot read
    Then the prose gate allows it

  # The gate holds four rules and the standard has fifty three. A session that believes this gate is
  # the whole standard believes a write it let through is prose in the standard.
  Scenario: The refusal says which half of the standard a person still holds
    When a session is about to write "The gate has refused the command." to "docs/EXAMPLE.md"
    Then the prose gate refuses it
    And the refusal names the standard and says the vocabulary and the idioms are a person's
