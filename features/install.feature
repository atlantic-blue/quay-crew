Feature: A first run is one command

  A first run used to be four commands, and the order mattered: the configuration file, the sandbox
  image, the stack, and the tool. Miss one and the failure arrived somewhere else. Compose read a
  file that was not there, or a first task was refused for a missing image, which reads as a broken
  system rather than a missing step.

  `make install` is now the whole first run. It writes the configuration file if there is none,
  builds the tool, the hooks and the sandbox image, and brings the stack up. It ends by printing the
  commands it cannot run for you, because it cannot mint your model credential.

  Running it again is safe. It never writes over the configuration file you edited, and it never
  replaces the services under a system that is already working without saying what that costs and
  waiting for you to agree. The four pieces underneath it still work on their own, so rebuilding one
  part does not put you through the other three and a restart.

  These scenarios run the real make against a system directory of their own. Docker is answered by a
  double, so what is proved is what make does and in what order, not what a real daemon does with
  the compose file. The containers job in continuous integration boots the stack for real.

  Scenario: One command leaves a running system and a quay that runs
    Given a machine with no system on it
    When the operator runs "make install"
    Then the system has a configuration file
    And the system has a data directory
    And quay is on the path and says which build it is
    And the stack was brought up once

  Scenario: A first run says what it cannot do
    Given a machine with no system on it
    When the operator runs "make install"
    Then it says it cannot mint the model credential
    And it prints these commands:
      | quay workspace create <name>            |
      | quay project create <name>              |
      | quay secret set CLAUDE_CODE_OAUTH_TOKEN |
      | quay task "say pong"                    |

  Scenario: Running it again keeps the configuration the operator edited
    Given a machine with no system on it
    And the operator ran "make install"
    And the operator edited the configuration file
    When the operator runs "make install"
    Then it succeeds
    And the configuration file still says what the operator wrote
    And the system has a data directory

  # The one thing a second run can take away. Compose replaces the services whose build moved, and a
  # task in flight is executing through the control plane, so it ends with it.
  Scenario: A system that is already working is not replaced without a word
    Given a machine with a system already running
    When the operator runs "make install"
    Then it refuses
    And the stack was never brought up
    And it says the system is already up
    And it offers "make rebuild"
    And it offers "make install YES=1"

  Scenario: Typing the system's name back replaces it
    Given a machine with a system already running
    When the operator runs "make install" and types "quay"
    Then it succeeds
    And the stack was brought up once

  Scenario: Anything else is taken as no
    Given a machine with a system already running
    When the operator runs "make install" and types "no thanks"
    Then it refuses
    And the stack was never brought up

  Scenario: A script goes over it without being asked
    Given a machine with a system already running
    When the operator runs "make install YES=1"
    Then it succeeds
    And the stack was brought up once

  Scenario: Rebuilding one part leaves the running system alone
    Given a machine with a system already running
    When the operator runs "make rebuild"
    Then it succeeds
    And the stack was never brought up
    And quay is on the path and says which build it is

  Scenario: Asking for the configuration alone starts nothing
    Given a machine with no system on it
    When the operator runs "make config"
    Then it succeeds
    And the system has a configuration file
    And the stack was never brought up
