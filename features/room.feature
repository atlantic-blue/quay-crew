Feature: A session knows how much memory it has

  A session could not run a repository's own gates. The linter, the build and the install were each
  killed part way through, and the session reported a partial check. Rule 9 says run the whole of a
  repository's gates and rule 44 says prove the check ran. A session could do neither, because
  nothing in its sandbox would tell it how much room it had. `krewe room` does.

  The cause is a number that is not true. A sandbox with no memory limit of its own reports the whole
  machine in /proc/meminfo, so node sizes its heap from it, Go sizes its collector from it, and jest
  and webpack start one worker for each processor. What is really there is whatever the rest of the
  machine has not taken. Measured on one machine: 7836 megabytes advertised, about 1500 free. The
  session budgets against the first number and the kernel kills it against the second.

  The kill says nothing. It arrives as signal 9, so there is no last line on either stream, and the
  kernel log is not readable from inside a container. The session sees exit 137 and reads it as a
  hang.

  So `krewe room` reads the machine's own accounting and says what is true: what this sandbox
  advertises, what is free, what has already been killed in it, and what to do about a gate that does
  not fit. The advice lives in the tool rather than in each session's memory, so the answer is the
  same every time instead of being invented once per session.

  These scenarios read a machine's accounting the system was given, so they say what a session is told
  and not that a kernel behaves. The figures in them were measured on a real sandbox. What the system
  says when a task is killed for memory sits with the other task failures, in sessions.feature.

  Scenario: A sandbox with no limit is told it is sizing itself against the whole machine
    Given a machine with 8024876 kilobytes of memory and 1539300 free
    And the sandbox has no memory limit
    When the session asks how much memory it has
    Then the session is told the sandbox has no limit of its own
    And the session is told 7836 megabytes are advertised and 1503 are free

  # A limit is the point the kernel takes the process at, so a session that budgeted against the
  # machine's free memory would be killed by its own limit long before it got there.
  Scenario: A limited sandbox is never told it has more than its limit leaves
    Given a machine with 8024876 kilobytes of memory and 1539300 free
    And the sandbox may take 2048 megabytes and holds 1024
    When the session asks how much memory it has
    Then the session is told 1024 megabytes are free

  # A limit is a ceiling and never a reservation. A sandbox allowed four gigabytes on a machine with
  # one left still has one.
  Scenario: A limited sandbox is told what the machine has when that is less
    Given a machine with 8024876 kilobytes of memory and 1048576 free
    And the sandbox may take 4096 megabytes and holds 256
    When the session asks how much memory it has
    Then the session is told 1024 megabytes are free

  # The two counters are the only thing inside a container that tells the two kills apart. A kill by
  # the machine's own out of memory killer raises the kill count and leaves the limit count at zero,
  # because a sandbox with no limit never reaches one. Measured: they went from all zero to one kill
  # and no limit reached.
  Scenario: A session is told which memory ran out
    Given a machine with 8024876 kilobytes of memory and 1539300 free
    And the sandbox has no memory limit
    And an out of memory killer has taken 1 process in this sandbox at no limit of its own
    When the session asks how much memory it has
    Then the session is told the machine ran out rather than the session

  Scenario: A session that ran out of its own limit is told that instead
    Given a machine with 8024876 kilobytes of memory and 4194304 free
    And the sandbox may take 2048 megabytes and holds 2048
    And an out of memory killer has taken 3 processes in this sandbox at its own limit
    When the session asks how much memory it has
    Then the session is told it ran out itself

  # A warning about nothing trains a reader to skip the warnings.
  Scenario: A sandbox nothing was killed in is not told something was
    Given a machine with 8024876 kilobytes of memory and 1539300 free
    And the sandbox has no memory limit
    When the session asks how much memory it has
    Then the session is told nothing about a kill

  Scenario: Every session is told the same thing to do about a gate that does not fit
    Given a machine with 8024876 kilobytes of memory and 1539300 free
    And the sandbox has no memory limit
    When the session asks how much memory it has
    Then the session is told to cap the heap, take one worker, and run the gate in parts
    And the session is told to say what it could not run rather than report a partial check

  # A session is not the only place this command runs, and zero megabytes free would be read as a
  # finding rather than as a missing file.
  Scenario: A machine that keeps no memory accounting is refused with the reason
    Given a machine that keeps no memory accounting
    When the session asks how much memory it has
    Then the session is told this reads a linux sandbox and there is none here
