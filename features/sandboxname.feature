Feature: A sandbox is named after the command, and the name it had before is still read

  A session's sandbox is a container on the daemon, and its name is what everything outside the
  provider reaches it by: the drain that puts a session down, the sweep an upgrade runs, the reader
  that decides how much memory a session holds, and the attach that opens a conversation. The command
  is krewe, so the container is krewe followed by the session.

  The name it carried before the rename is the half that gets skipped. An operator upgrades with
  sessions up, and every container they already have carries the old one. A system that reads only the
  new name does not drain those, does not remove them, and starts a second container beside each one on
  the next exec, while the first keeps running and keeps the machine's memory. So the system writes one
  name and reads both, until the release the changelog names.

  The exact shape is the whole of the safety. The stack the system runs in is a compose project called
  quaycrew, so its own store, broker and dashboards carry the retired prefix too, and a reader looser
  than a session identifier reaps the system it is running in.

  Scenario Outline: A container the daemon holds belongs to a session, whichever name it carries
    Given the daemon holds a container named "<container>"
    Then the system reads it as the sandbox of session "<session>"

    Examples:
      | container                            | session                  |
      | krewe-0123456789abcdef01234567       | 0123456789abcdef01234567 |
      | quaycrew-0123456789abcdef01234567    | 0123456789abcdef01234567 |

  Scenario Outline: A container that is not a sandbox is nobody's to stop
    Given the daemon holds a container named "<container>"
    Then the system reads it as no session's sandbox

    Examples:
      | container                             |
      | quaycrew-postgres-1                   |
      | quaycrew-controlplane-1               |
      | krewe-postgres-1                      |
      | krewe-0123456789abcdef0123456         |
      | krewe-0123456789abcdef012345678       |
      | my-krewe-0123456789abcdef01234567     |

  Scenario: A session's sandbox is created under the name the command spells
    Then a new sandbox for session "0123456789abcdef01234567" is named "krewe-0123456789abcdef01234567"

  Scenario: The system looks for the name it writes before the name it retired
    Then the system looks for session "0123456789abcdef01234567" as "krewe-0123456789abcdef01234567", then "quaycrew-0123456789abcdef01234567"
