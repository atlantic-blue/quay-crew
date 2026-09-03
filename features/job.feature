Feature: A job is a record the system keeps

  A caller declares a job and the system keeps it. The intent is a row, so it outlives the
  terminal that asked for it, the session that will run it, and the process that read it. Nothing
  runs the job yet: this is the record, the refusals and the read path.

  Every rule is checked at the moment of the write, while the person who wrote it is looking. A
  refusal that arrives hours later, inside a run, points at nothing.

  What a job cannot be done without is what it requires. The flag was called --hands, and
  the word needed explaining every time somebody read it. --requires also reads correctly in both
  directions: this job requires context, and the architect role receives context. The old flag is in
  fingers, in scripts and in notes, so it refuses and names what to type instead.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: Intent survives the caller
    Given a job titled "read the electricity bill"
    When the caller goes away and the system is asked again
    Then the job is still there, pending, with its brief whole

  Scenario: A job opens pending, under nothing
    Given a job titled "read the electricity bill"
    Then the job is pending
    And the job sits under nothing
    And the job carries the moment it was declared

  Scenario: The system assigns the identifier
    When the caller declares a job carrying an identifier of its own
    Then the system refuses it and says it assigns the identifier

  # The way off the old interface. A caller that used to put a job under another one has nothing to
  # send: the field is gone from the wire, and the tool refuses the flag by name.
  Scenario: A caller has no way to put a job under another job
    Given the system listens on an address the tool can dial
    Then a declaration carries no way to say what a job sits under
    And the tool refuses --parent and says a job cannot be under another job

  Scenario: Job with no title is refused
    When the caller declares a job with no title
    Then the system refuses it and says a title is needed

  Scenario: A title of 201 bytes is refused
    When the caller declares a job with a title of 201 bytes
    Then the system refuses it and says the ceiling is 200

  Scenario: A brief of 16385 bytes is refused
    When the caller declares a job with a brief of 16385 bytes
    Then the system refuses it and says the ceiling is 16384

  Scenario: Job naming a role the workspace does not hold is refused
    When the caller declares a job in the role "backlog-clearer"
    Then the system refuses it and names the role

  Scenario: Job in a role the workspace holds is pinned to the version it holds
    Given the workspace holds the role "backlog-clearer" at version 1
    When the caller declares a job in the role "backlog-clearer"
    Then the job carries the role at version 1

  # A role declares what it receives and a job declares what it cannot be done without.
  # Where the two disagree the job is refused, while the person who wrote it is looking.
  Scenario: Job that requires material its role does not receive is refused
    Given the workspace holds the role "test-writer" at version 1 receiving "job"
    When the caller declares a job in the role "test-writer" requiring "context"
    Then the system refuses it, naming the role, the material and what to change
    And no job was written

  Scenario: Job that requires what its role does receive is kept
    Given the workspace holds the role "backlog-clearer" at version 1 receiving "job, context"
    When the caller declares a job in the role "backlog-clearer" requiring "context"
    Then the job requires "context"

  Scenario: Job that requires something the system does not hand out is refused
    When the caller declares a job requiring "the codebase"
    Then the system refuses it and lists the material it hands out

  # Job that names no role requires its material of nobody, so nothing here applies to it.
  Scenario: Job with no role is held to no boundary
    When the caller declares a job requiring "context"
    Then the job requires "context"

  # A job that names a repository says how it ends: the session pushes and opens a pull request, and
  # the job is not done until the answer names it. It is on the job rather than in a brief, because a
  # brief that forgets to ask for a push produces work nobody can see.
  Scenario: A job names the repository its work goes to
    When the caller declares a job in the repository "atlantic-blue/quay-crew" in the mode "dangerous"
    Then the job works in "atlantic-blue/quay-crew"

  # The address a person has in front of them is the one in their browser, so both spellings are
  # taken and both are kept as one.
  Scenario: The address of a repository is kept as an owner and a name
    When the caller declares a job in the repository "https://github.com/atlantic-blue/quay-crew" in the mode "dangerous"
    Then the job works in "atlantic-blue/quay-crew"

  Scenario: A repository that is not an owner and a name is refused
    When the caller declares a job in the repository "quay-crew"
    Then the system refuses it and says how to write a repository

  # Every way into a repository needs the network: the clone, the push, the pull request. The narrower
  # modes ask a person before they run a network command, and nobody stands beside a dispatched job,
  # so the approval never arrives. The system held both facts at the moment of the write and never
  # compared them, so it admitted the job, spent the session, and said so at the end.
  #
  # The refusal first. A rule that only ever admits is a rule nothing tests.
  Scenario: A job that works in a repository is refused in a mode that cannot reach it
    When the caller declares a job in the repository "atlantic-blue/quay-crew" in the mode "edits"
    Then the system refuses it, naming the mode, the repository and what to type instead
    And no job was written

  Scenario: A job that works in a repository is refused in the mode that only plans
    When the caller declares a job in the repository "atlantic-blue/quay-crew" in the mode "plan"
    Then the system refuses it, naming the mode, the repository and what to type instead
    And no job was written

  Scenario: A job that works in a repository is declared in the mode that reaches it
    When the caller declares a job in the repository "atlantic-blue/quay-crew" in the mode "dangerous"
    Then the job works in "atlantic-blue/quay-crew"

  # The rule is the pair and not the mode. A job that works in no repository asks nothing of the
  # network, so every mode still declares one.
  Scenario: A job that works in no repository is declared in a mode that reaches no network
    When the caller declares a job in the mode "edits"
    Then the job is declared

  Scenario: A job that works in no repository is declared in the mode that only plans
    When the caller declares a job in the mode "plan"
    Then the job is declared

  # The path nobody types a flag for, which is the path this was reported on. The project holds the
  # repository, so every job declared in it carries one without anybody saying so, and the mode is
  # whatever the system was configured with.
  Scenario: A job that takes its repository from its project is refused the same way
    Given the project's work lands in "atlantic-blue/transcript"
    When the caller declares a job
    Then the system refuses it, naming the mode, the repository and what to type instead
    And no job was written

  Scenario: A job that takes its repository from its project is declared in the mode that reaches it
    Given the project's work lands in "atlantic-blue/transcript"
    When the caller declares a job in the mode "dangerous"
    Then the job works in "atlantic-blue/transcript"

  # A job cannot wait. It runs once and answers, and nothing wakes it when the checks land, so a
  # brief that says "merge on green" asks for something the runtime does not have and the session
  # invents an answer. The shape that can do it is a flow, and the refusal says so.
  Scenario: A brief that asks the job to wait for the checks is refused
    When the caller declares a job briefed to "fix the defect, push, watch the checks and merge on green"
    Then the system refuses it and says a job cannot wait, and names the flow

  # The rule reads English, so it is held narrow. A brief merging a branch is ordinary work.
  Scenario: A brief that merges a branch is ordinary work
    When the caller declares a job briefed to "merge origin/main into the branch, then run the gates"
    Then the job is declared

  # The line the system itself puts in front of a session says not to merge. A brief that says it back
  # must not be the thing that gets refused.
  Scenario: A brief that says not to merge is not a brief that merges
    When the caller declares a job briefed to "push the branch, then do not merge the pull request"
    Then the job is declared

  Scenario: Job naming a mode that is not a mode is refused
    When the caller declares a job in the mode "yolo"
    Then the system refuses it and lists the modes

  Scenario: A job whose expected file is absolute is refused
    When the caller declares a job expecting the file "/etc/passwd"
    Then the system refuses it and says the path is read inside the working directory

  Scenario: A job whose expected file climbs out of the working directory is refused
    When the caller declares a job expecting the file "../secrets.txt"
    Then the system refuses it and says the path climbs out

  Scenario: Job waiting on something that does not exist is refused
    When the caller declares a job after "0123456789abcdef01234567"
    Then the system refuses it and names the identifier it cannot find

  Scenario: A job waits for a job that exists
    Given a job titled "read the electricity bill"
    When the caller declares a job after the first job
    Then the job waits for the first job

  Scenario: A budget below zero is refused
    When the caller declares a job with a budget of -1 tokens
    Then the system refuses it and says a budget cannot be below zero

  Scenario: Seventeen labels are refused
    When the caller declares a job carrying 17 labels
    Then the system refuses it and says the ceiling is 16

  Scenario: A label value of 64 characters is refused
    When the caller declares a job carrying a label value of 64 characters
    Then the system refuses it and says the ceiling is 63

  # A workspace with no credential took a whole tree of job and said nothing about it. Every session
  # in it would have died on its first clone, after the budget was spent starting them. The system
  # already knew: the skill listing named the skill, the secret and the command that sets it,
  # unprompted. That is one listing nobody is required to read, so the declaration says it too, while
  # the person who typed it is looking.
  #
  # It says rather than refuses. The system cannot know which skill a brief will reach for, so refusing
  # would stop a job that reads an electricity bill over a forge token it never wanted, and one unset
  # secret would be enough to stop every job in the workspace.
  Scenario: Declaring a job says which skills the session starts without
    Given the system has a skill "github" needing the secret "GH_TOKEN"
    When the caller declares a job titled "fix the defect"
    Then the job is declared
    And the declaration says the session starts without the "github" skill, needing "GH_TOKEN"

  # A note printed every time is a note nobody reads, so a workspace that can supply what its skills
  # need hears nothing.
  Scenario: A workspace that has its credentials is told nothing extra
    Given the system has a skill "github" needing the secret "GH_TOKEN"
    And the workspace has the secret "GH_TOKEN" set to "a token"
    When the caller declares a job titled "fix the defect"
    Then the job is declared
    And the declaration names no skill the session starts without

  # A role that does not receive skills is given none of them by design, so there is no gap to report.
  Scenario: A job whose role receives no skills is told nothing about them
    Given the system has a skill "github" needing the secret "GH_TOKEN"
    And the workspace holds the role "backlog-clearer" at version 1
    When the caller declares a job in the role "backlog-clearer"
    Then the declaration names no skill the session starts without

  Scenario: Job in a workspace that does not exist is refused
    When the caller declares a job in a project that does not exist
    Then the control plane refuses it as not found

  # The tool, in its own process, because what is specified here is the exit status and which stream
  # the sentence went to, and neither exists inside the test process.
  Scenario: The tool declares what a job requires
    Given the system listens on an address the tool can dial
    When the caller declares a job with "--requires context" through the tool
    Then the command succeeds
    And reading that job back says it requires "context"

  # The way off the old flag. A removed flag that is quietly ignored reads as a command that worked,
  # and the operator finds out from the record later that the boundary was never declared.
  Scenario: The flag that went refuses, names what to type, and fails
    Given the system listens on an address the tool can dial
    When the caller declares a job with "--hands context" through the tool
    Then standard error says "--hands is gone"
    And standard error says "--requires"
    And standard output is empty
    And the command fails
    And no job was written

  Scenario: A listing says what a project holds, newest first
    Given a job titled "read the electricity bill"
    And a job titled "pay the electricity bill"
    When the caller lists the job in the project
    Then the listing holds both jobs, newest first

  Scenario: A listing carries no answers
    Given a job titled "read the electricity bill" that answered "the bill is due on the 14th"
    When the caller lists the job in the project
    Then the listing carries the title and not the answer
    And reading that one job carries the answer whole

  Scenario: A listing is narrowed by phase
    Given a job titled "read the electricity bill"
    And a job titled "pay the electricity bill"
    When the caller stops the first job saying "the bill is not due yet"
    And the caller lists the job that is pending
    Then the listing holds only "pay the electricity bill"

  Scenario: A person stops a job and the reason is kept
    Given a job titled "read the electricity bill"
    When the caller stops the first job saying "the bill is not due yet"
    Then the job is stopped, and the reason is "the bill is not due yet"
    And the job carries the moment it finished

  Scenario: Job that already stopped is not stopped again
    Given a job titled "read the electricity bill"
    When the caller stops the first job saying "the bill is not due yet"
    And the caller stops the first job saying "changed my mind"
    Then the system refuses it and says the job already ended
    And the reason on the job is still "the bill is not due yet"

  Scenario: Job nobody has is refused by name
    When the caller asks for a job that does not exist
    Then the control plane refuses it as not found

  # The store is the source of truth in this slice, so the record of what happened is a row beside
  # the row it describes, written in the same transaction. Nothing is published to the log yet.
  Scenario: Declaring job writes the record of the declaration
    Given a job titled "read the electricity bill"
    Then the system holds a "job.declared" record for it, naming the title

  Scenario: Stopping job writes the record of the stop
    Given a job titled "read the electricity bill"
    When the caller stops the first job saying "the bill is not due yet"
    Then the system holds a "job.stopped" record for it, naming the reason
