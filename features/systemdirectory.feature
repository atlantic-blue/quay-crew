Feature: A system keeps everything it owns in one directory, and says so when it moves

  The directory holds what nothing can make again: the system token, the driver token, the sealing key
  that unseals every secret, and every conversation. The command a person types is krewe, so the
  directory is `~/.krewe` and the variable that names it is `KREWE_HOME`.

  It was `~/.quay`, under the name the product had before this one. A build that simply read the new
  path would start on a directory with nothing in it. The system would come up on a token nothing else
  holds, with no sealing key, and every conversation would read as lost. So the tool refuses to start
  instead, and prints the one command that moves it.

  It refuses rather than moving anything itself. A tool that quietly relocates a gigabyte of
  transcripts on startup is a tool nobody can undo.

  The command it prints has to be the command that works. `mkdir -p ~/.krewe` followed by
  `mv ~/.quay ~/.krewe` puts the whole directory inside the new one, a level below anything that looks
  for it, and the operator is left with a system that still cannot find its own token. So the refusal
  for a directory that was renamed says one move and no mkdir at all.

  Background:
    Given a running control plane
    And the system listens on an address the tool can dial

  Scenario: A system still in the directory that went is refused, and told what to move
    Given a machine whose system is still in the directory that went
    When the caller types "workspace list" on that machine
    Then standard error says "mv"
    And standard error names the directory that went and the one it moves to
    And standard error never says "mkdir"
    And the command fails

  Scenario: A directory that exists and holds nothing is not a system
    Given a machine whose system is still in the directory that went
    And the new directory exists and holds only the configuration file
    When the caller types "workspace list" on that machine
    Then standard error says "mv"
    And the command fails

  Scenario: A system that has moved starts
    Given a machine whose system is in the directory the tool reads
    When the caller types "workspace list" on that machine
    Then the command succeeds

  Scenario: A machine that never had the old directory starts
    Given a machine with no system on it at all
    When the caller types "workspace list" on that machine
    Then the command succeeds

  # The variable is in shell profiles, in scripts and in service files. A build that stopped reading it
  # would send an operator who exports it to a fresh directory, which is the same loss by another road.
  Scenario: The variable that went still names the directory
    Given a machine whose system is in a directory of its own, named by the variable that went
    When the caller types "workspace list" on that machine
    Then the command succeeds
