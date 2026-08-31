Feature: One word for the level above every workspace, and the word it replaced refuses

  The level above every workspace is the system. It holds the secrets, the skills, the hooks, the
  roles and the context that every workspace reads without attaching anything, and it is said where
  a workspace goes: krewe secret set system, krewe skill attach system, krewe context set system.

  It was called crew, which was also the word for the whole product, so an address read as a
  sentence about the product rather than as a place. One word for one thing, and the word for the
  place is system.

  The word that went is the other half of the change, and it is the half that gets skipped. It is in
  fingers, in scripts and in notes. Read as an ordinary address it becomes a workspace nobody has,
  and the operator is sent looking for a workspace instead of being told that the word moved. So it
  refuses by name, exits non zero, and says what to type.

  A workspace cannot be called either word. One called system would shadow the level. One called
  crew would be handed everything typed out of habit, quietly, and the acknowledgement would read as
  the level having been set.

  The scenarios below run the command line tool as a caller runs it: its own process, its own
  standard output, its own exit status.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"
    And the system listens on an address the tool can dial

  Scenario Outline: The word says the level, where a workspace goes
    When the caller types "<typed>" through the tool
    Then standard output says "system"
    And the command succeeds

    Examples:
      | typed               |
      | secret list system  |
      | job list system     |
      | sessions system     |

  Scenario Outline: The word that went refuses, names what to type, and fails
    When the caller types "<gone>" through the tool
    Then standard error says the word moved
    And standard output is empty
    And the command fails

    Examples:
      | gone                                  |
      | secret list crew                      |
      | skill attach crew git                 |
      | skill detach crew git                 |
      | hook attach crew merge-gate           |
      | role attach crew implementer          |
      | context show crew                     |
      | job list crew                         |
      | sessions crew                         |
      | use crew                              |

  # The refusal is only worth anything if nothing happened behind it. A command that reads as a
  # refusal and set the secret anyway is worse than one that never existed.
  Scenario: Nothing is set behind the refusal
    When the caller types "secret set crew GITHUB_TOKEN" through the tool, piping in "ghp-x"
    Then standard error says the word moved
    And the command fails
    And the system holds no secret called "GITHUB_TOKEN"

  # "system" is the word every address takes for the level above a workspace. A workspace called
  # system would take the secrets and skills meant for all of them.
  Scenario: A workspace cannot be called system
    When the caller types "workspace create system" through the tool
    Then standard error says "that word means the whole system"
    And the command fails

  Scenario: A workspace cannot be called the word that went either
    When the caller types "workspace create crew" through the tool
    Then standard error says "used to mean the level above every workspace"
    And standard error says the word is now "system"
    And the command fails

  # The manual is what a session reads to find out what this tool takes, so a manual still saying the
  # old word teaches every session to type something that is refused.
  Scenario: The manual says the word, and never the word that went
    When the caller asks for the manual
    Then standard output says "krewe context set system"
    And standard output never says "crew"
    And the command succeeds

  # A name is lowercase letters, digits and hyphens, so neither word can be capitalised and still be
  # the name of anything. Whoever types the word with a capital means the word, and answering them
  # with a workspace they do not have sends them looking for a workspace. One spelling refused and
  # the next one waved through is the same quiet failure as the word being dropped.
  Scenario Outline: The word that went refuses however it is typed
    When the caller types "<gone>" through the tool
    Then standard error says the word moved
    And standard output is empty
    And the command fails

    Examples:
      | gone             |
      | secret list Crew |
      | secret list CREW |
      | job list Crew    |
      | use Crew         |

  # The refusal a capitalised reserved word used to get was the general one about names, whose advice
  # is the typed name lowercased. It told the operator to type "system", which is the one name a
  # workspace may not hold. Advice that cannot be followed reads as the rule not applying here.
  Scenario Outline: A workspace cannot be called either word, however it is typed
    When the caller types "workspace create <typed>" through the tool
    Then standard error says "<because>"
    And standard error never says "use lowercase letters"
    And the command fails

    Examples:
      | typed  | because                                   |
      | System | that word means the whole system          |
      | SYSTEM | that word means the whole system          |
      | Crew   | used to mean the level above every workspace |
      | CREW   | used to mean the level above every workspace |
