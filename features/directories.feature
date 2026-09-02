Feature: A name becomes a directory, so a person can put a file in front of a session

  Every level the system keeps on disk is a generated identifier. The names are in the control plane
  and none of them is on the filesystem, so somebody who knows they work in `atlantic-blue` has
  nothing to type. Putting a screenshot in front of a running session meant reading three directories
  named in hex, then inspecting a container that happened to be up to learn that a workspace's volume
  is bound at `/home/agent/shared`.

  That last step is the one with no answer at all. A bind mount can be read off a live container, and
  the question is usually asked once every container is down.

  So an address answers with a directory. A workspace address answers with its shared folder, which
  every session in it reads. A session address answers with that session's own working directory. The
  path is on the first line with nothing beside it, so it can be typed into a shell, and under it is
  where a session sees the same directory, which is what to call the file once it is in there.

  The directory is made if it is not there. A workspace nobody has worked in has no folder yet,
  because the folder is made when a sandbox starts, and a path that cannot be copied into is not an
  answer to somebody holding a file.

  One directory is never named. The top of the data directory holds the system's token, the driver's
  token and the key that unseals every secret, so the word that would name it is refused and says why.

  Background:
    Given a running control plane
    And the system listens on an address the tool can dial

  Scenario: A workspace nobody has worked in yet still has a folder to put a file in
    Given a workspace named "atlantic-blue"
    When the caller asks where "atlantic-blue" is
    Then the directory it names exists on the machine
    And it says a session reads that directory at "/home/agent/shared"

  Scenario: The path is alone on the first line, so it can be typed into a shell
    Given a workspace named "atlantic-blue"
    When the caller asks where "atlantic-blue" is
    Then the first line is a path and nothing else

  Scenario: A file put in that directory by hand is where the session will look for it
    Given a workspace named "atlantic-blue"
    When the caller asks where "atlantic-blue" is
    And a file called "screenshot.png" is put in that directory by hand
    Then a sandbox of that workspace binds that directory at "/home/agent/shared"
    And the file is inside the directory that sandbox binds

  Scenario: An address that does not exist says what there is
    Given a workspace named "atlantic-blue"
    When the caller asks where "nowhere" is
    Then standard error says "atlantic-blue"
    And the command fails

  # The tokens and the sealing key are at the top of the data directory. A command that answers "where
  # do I put a file" must not offer a road to them.
  Scenario: The system's own directory is refused, and says what is in it
    Given a workspace named "atlantic-blue"
    When the caller asks where "system" is
    Then standard error says "credentials"
    And the command fails
