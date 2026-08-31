Feature: Work a job finished reaches somebody without a person carrying it

  A session finished the work and wrote the file. The job then stopped, and its last word was an
  instruction to a person: open the container, and push what is inside it. The product of the job sat
  where no command reached it, and the operator became the transport.

  The bytes were never in the container alone. A session's working directory is a mount the system
  made itself, so the system was holding the work the whole time and had no way to name it.

  So the system publishes rather than asking. A job that stops without a pull request has its branch
  pushed, because a push applies nothing and needs nobody's approval. The pull request and the merge
  stay decisions and the system opens neither. Where it cannot push, it says which branch the work is
  on and which directory it is in, and one command reads a file out of the session without attaching
  to it.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # A session that cloned nothing. There is no branch and no repository, and the directory is still
  # named, because whatever it wrote is in there.
  Scenario: A job whose session cloned nothing says where its directory is
    Given a job titled "sort the listing" in the repository "atlantic-blue/krewe"
    And the model will answer "I made the change and the tests pass"
    When the session answers twice without a pull request
    Then the job is stopped, and the reason says the session holds no repository
    And the reason says where the work is on the machine
    And the reason never sends anybody into a container

  # The case that matters most. A branch cut from the base and never committed to is not work, and a
  # reason that names it sends the operator looking for something that is not there.
  Scenario: A job whose session committed nothing says so, and names no branch
    Given a job titled "sort the listing" in the repository "atlantic-blue/krewe"
    And the model will answer "I made the change and the tests pass"
    And the session's git is on the branch "sort-the-listing" with nothing committed
    When the session answers twice without a pull request
    Then the job is stopped, and the reason says the session committed nothing
    And the reason names no branch
    And the reason says where the work is on the machine
    And the reason never sends anybody into a container

  # A push the remote refused. The branch, the path and what git said are the three things an
  # operator acts on, and none of them used to be on the row.
  Scenario: Work the system could not push is named with its branch and its path
    Given a job titled "sort the listing" in the repository "atlantic-blue/krewe"
    And the model will answer "I made the change and the tests pass"
    And the session's git is on the branch "sort-the-listing" with work committed
    And the remote refuses the push saying "remote: Permission to atlantic-blue/krewe.git denied"
    When the session answers twice without a pull request
    Then the job is stopped, and the reason names the branch "sort-the-listing"
    And the reason carries what the remote said
    And the reason says where the work is on the machine
    And the reason never sends anybody into a container

  Scenario: Work the session did not push is pushed by the system, and nothing else is
    Given a job titled "sort the listing" in the repository "atlantic-blue/krewe"
    And the model will answer "I made the change and the tests pass"
    And the session's git is on the branch "sort-the-listing" with work committed
    When the session answers twice without a pull request
    Then the system pushed the branch "sort-the-listing"
    And the job is stopped, and the reason says the system pushed it and one step is left
    And the system opened no pull request and merged nothing

  # What the person does next. The reason names a directory, and this is the command that reads it:
  # no container, no terminal, and it answers for a session whose sandbox has gone.
  Scenario: The operator reads the work out of the stopped session without attaching to it
    Given a job titled "sort the listing" in the repository "atlantic-blue/krewe"
    And the model will answer "I made the change and the tests pass"
    And the session's git is on the branch "sort-the-listing" with work committed
    And the remote refuses the push saying "remote: Permission to atlantic-blue/krewe.git denied"
    And the session wrote "listing.go" saying "sorted by the clock it shows"
    When the session answers twice without a pull request
    And the operator lists what that session made
    Then the listing names "listing.go" and the directory the reason named
    And reading "listing.go" out of that session gives back what the session wrote
