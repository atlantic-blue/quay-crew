Feature: A job ends by stating one outcome, so done is a word rather than a reading

  Jobs on one acceptance run reported "done", "complete", "the pull request is open" and "I could not
  finish because the credential expired". All four settled the same way, because the system read the
  prose to decide the job was over. A job that could not do its work and a job that did it read
  identically to anything after them, so the operator opened each one to tell them apart and nothing
  could be counted.

  So a session ends its task with one word on a line of its own, from a set of four the system holds
  and the session cannot add to: proved, unproved, blocked and decide. The prose sits under that line
  as the explanation. The system reads the word off the answer rather than believing a report of it,
  the way it already reads the address of a pull request.

  Three of the four say what became of the work, and the job settles on that word. The fourth says
  the work stopped with a person, so the job stops with them instead of settling: deciding.feature
  holds what happens then.

  A session that states none has not finished the job. It is not asked again, because the line was in
  the task it just answered, and the job stops saying what was missing rather than reading as work
  that went well.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # The session is told, by the system rather than by whoever wrote the brief. A brief that forgets it
  # produces a job nothing after it can read, and every brief forgets eventually.
  Scenario: Every session doing a job is told to state an outcome, and which words it may use
    Given a job titled "read the electricity bill"
    When the controller ticks
    Then the session was told to end its answer with an outcome
    And the session was offered the four words

  # The refusal first. A gate that always passes satisfies every test about passing.
  Scenario: A job whose answer states no outcome does not settle
    Given a job titled "read the electricity bill"
    And the model will answer "I read the bill. It is due on the 14th." and state no outcome
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job did not settle, and says the outcome line was missing
    And what the session said is still on the record

  # The case the fault was about: a word inside a sentence is prose, and prose is what the system was
  # reading before this.
  Scenario: The word inside a sentence is not the outcome
    Given a job titled "read the electricity bill"
    And the model will answer "The tests proved it, so I would call this complete." and state no outcome
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job did not settle, and says the outcome line was missing

  # A word the system does not hand out is not an outcome either. A session that could widen the set
  # would be back to prose with a colon in front of it.
  Scenario: A word the system does not hand out is not an outcome
    Given a job titled "read the electricity bill"
    And the model will answer "It is done." and end on the line "Outcome: complete"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job did not settle, and says the outcome line was missing

  # Asked once, in the task it just answered. Asking again is paying a model to read its own
  # instructions, which is the difference between this and a pull request that was pushed nowhere.
  Scenario: A job that stated no outcome is not asked for one
    Given a job titled "read the electricity bill"
    And the model will answer "I read the bill. It is due on the 14th." and state no outcome
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    And the controller ticks again
    Then the system was asked to run 1 task

  Scenario Outline: A job that states an outcome settles on that word
    Given a job titled "read the electricity bill"
    And the model will answer "It is due on the 14th." and state the outcome "<outcome>"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is done, and it ended on "<outcome>"

    Examples:
      | outcome  |
      | proved   |
      | unproved |
      | blocked  |

  # The fourth word is not a settling. A job whose session says a person has to decide has stopped
  # with that person, so it goes where everything that reads what waits on you looks. The rest of it
  # is in deciding.feature.
  Scenario: The word decide stops the job with a person rather than settling it
    Given a job titled "read the electricity bill"
    And the model will answer "Two stores fit and the cost differs. Which?" and state the outcome "decide"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is waiting on a person, carrying what the session wrote
    And the job did not end

  # The listing this exists for. Two jobs are done and one of them could not do its work, and the
  # phase cannot tell them apart.
  Scenario: A listing narrows by outcome
    Given a job titled "read the electricity bill" that ended on "proved"
    And a job titled "read the water bill" that ended on "blocked"
    When the caller lists the jobs that ended on "blocked"
    Then the listing holds "read the water bill" and nothing else
    And listing by phase holds both of them

  # A filter held to the four words. A word nothing ends on would answer an empty listing, and an
  # empty listing reads exactly like a system holding no such jobs.
  Scenario: A listing asked for a word that is not an outcome is refused
    When the caller lists the jobs that ended on "complete"
    Then the system refuses it and offers the four words

  # A job the model never finished states nothing. Inventing a word for it would be the system
  # reporting an outcome on work nobody did.
  Scenario: A job that failed states no outcome
    Given a job titled "read the electricity bill"
    And the model refuses the task it is given
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is failed, and it ended on no outcome
