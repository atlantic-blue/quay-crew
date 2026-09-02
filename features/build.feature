Feature: The failing tests become an implementation, and nothing that builds can change a test

  The tests existed and nothing implemented against them under a boundary. A session that builds and
  runs the suite can reach a green suite two ways, and the shorter one is to change the assertion.
  From inside the session a failing test looks exactly like a wrong test, so this is not dishonesty,
  and nothing stopped it.

  So the build stage holds one rule. A worker may read every test as much as it needs to, and it may
  not change one. Reading is allowed on purpose: a build that cannot read the test cannot tell a
  failing assertion from a broken one, and it guesses instead. The refusal is a hook rather than a
  sentence in the brief, because a boundary a session can talk itself past is not a boundary.

  It fans out. One worker for each vertical a person accepted, all at once, each turning its own
  failing tests green and nothing else. Two workers must never build one vertical, so each holds the
  claim on its own, which is the same refusal a second job taking a first job's work already meets.

  The stage closes on a build that happened. A run that executed nothing reports success just the
  same, a test that was already passing before anything was written holds nothing, and a suite that is
  still red is not built, so all three are refused. Then the job holds for a person: the machine's
  three checks are in the report it just read, and the fourth is somebody looking at the thing and
  agreeing the value arrived.

  Background:
    Given a running control plane
    And the system listens on an address the tool can dial
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A red suite becomes one worker for each vertical, and the job holds for acceptance
    Given a job whose plan a person approved and whose suite is red for 2 verticals
    When the controller ticks
    Then a worker is building each vertical, and the job itself has no session
    And each worker was given its own vertical and told it may not change a test
    When the caller reads that job back through the tool
    Then the reading says one session for each vertical is building, and none can change a test
    When every worker answers with its run
    And the controller ticks again
    Then the row carries what was built for every vertical
    And every file that was written says which vertical it came from
    And the job is waiting for a person to accept what was built
    When the caller reads that job back through the tool
    Then the reading says the job is in the "build" stage
    And the reading says it waits for a person to accept what was built

  # The claim doing the work it already does for anything else. The second declaration is refused by
  # the store, so a second controller ticking the same row buys no second session.
  Scenario: A second worker for one vertical is refused
    Given a job whose plan a person approved and whose suite is red for 2 verticals
    When the controller ticks
    And the controller ticks again
    Then 2 workers are building, one for each vertical

  Scenario: A list of one vertical is a fan out of one
    Given a job whose plan a person approved and whose suite is red for 1 vertical
    When the controller ticks
    Then 1 worker is building, one for each vertical

  # The three shapes of false green. Each of them reads as success everywhere else in this system.
  Scenario: A run that executed nothing is not a build
    Given a job whose plan a person approved and whose suite is red for 1 vertical
    And the builder will answer that its run executed no tests
    When the controller ticks
    And every worker answers with its run
    And the controller ticks again
    Then the job is asking, and the row carries nothing built
    And the question says the run found nothing to execute

  Scenario: A vertical whose tests still fail is not built
    Given a job whose plan a person approved and whose suite is red for 1 vertical
    And the builder will answer that its suite is still red
    When the controller ticks
    And every worker answers with its run
    And the controller ticks again
    Then the job is asking, and the row carries nothing built
    And the question says which test is wrong is a person's to decide

  Scenario: A test that was already green before the build began is not a build
    Given a job whose plan a person approved and whose suite is red for 1 vertical
    And the builder will answer that it changed no file
    When the controller ticks
    And every worker answers with its run
    And the controller ticks again
    Then the job is asking, and the row carries nothing built
    And the question says the test was already passing

  Scenario: A vertical whose worker died stops the job for a person
    Given a job whose plan a person approved and whose suite is red for 2 verticals
    When the controller ticks
    And the builder for vertical 2 dies
    And every worker answers with its run
    And the controller ticks again
    Then the job is asking, and the row carries nothing built
    And the question names vertical 2

  # The boundary itself, run the way a sandbox runs it. These fire the entry point this build ships,
  # fed the payload the model runtime feeds it.

  Scenario: A fresh system is under the test gate without anybody attaching it
    Given a system seeded with the hooks this build ships
    Then the system holds the "test-gate" hook
    And the workspace runs under the "test-gate" hook

  Scenario Outline: A session that is building and about to change a test is refused
    Given a session that the system is building with
    When that session is about to write to "<file>"
    Then the test gate refuses it
    And the refusal names the file and says to answer that the test is wrong

    Examples: the write tools, in each shape the runtime offers
      | file                              |
      | internal/job/build_test.go        |
      | features/build.feature            |
      | features/build_steps_test.go      |
      | web/src/basket.spec.ts            |
      | api/test_basket.py                |
      | internal/store/testdata/rows.json |

  Scenario Outline: A session that is building and about to change a test through the shell is refused
    Given a session that the system is building with
    When that session is about to run the command: <command>
    Then the test gate refuses it
    And the refusal names the file and says to answer that the test is wrong

    Examples: the file, named
      | command                                              |
      | rm internal/job/build_test.go                        |
      | mv internal/job/build_test.go /tmp/aside             |
      | sed -i 's/want 3/want 2/' internal/job/build_test.go |
      | echo "func TestNothing() {}" > features/x_test.go    |
      | git checkout origin/main -- features/build.feature   |
      | sudo rm internal/job/build_test.go                   |

    Examples: the same write, in the spelling a session reaches for next
      | command                                            |
      | sed --in-place 's/a/b/' internal/job/build_test.go |
      | gofmt -w internal/job/build_test.go                |
      | ln -sf /tmp/mine internal/job/build_test.go        |
      | echo internal/job/build_test.go \| xargs rm        |
      | find . -name '*_test.go' -delete                   |
      | python3 -c "open('features/build.feature','w')"    |
      | somewriter --out internal/job/build_test.go        |

    Examples: the directory of tests, taken whole
      | command                       |
      | rm -rf features/              |
      | rm -r internal/store/testdata |
      | mv features /tmp/aside        |

  # A command that names no path is pointed at the working directory, and the tests are under it. A
  # name says nothing about what a directory holds, so this reads the disk rather than the word.
  Scenario Outline: A session that is building and about to take a directory of tests whole is refused
    Given a session that the system is building with
    When that session is about to run the command: <command>
    Then the test gate refuses it
    And the refusal says to name the files it means

    Examples: the verbs that cover everything under them
      | command           |
      | git checkout -- . |
      | git checkout .    |
      | git stash         |
      | git clean -fd     |

  # The other direction, and the one that decides whether this boundary is worth having. Reading the
  # test is the whole difference between this and the discipline it comes from.
  Scenario Outline: A session that is building reads and builds freely
    Given a session that the system is building with
    When that session is about to run the command: <command>
    Then the test gate allows it

    Examples: the work, which a gate that stopped it would be worse than no gate
      | command                                    |
      | go test ./features/                        |
      | make features                              |
      | gofmt -w internal/job/build.go             |
      | rm -rf build/                              |
      | find . -name '*_test.go'                   |
      | git checkout -- internal/job/build.go      |
      | git add internal/job/build.go              |
      | git commit -m "make the failing test pass" |

    Examples: reading the tests, which this stage allows on purpose
      | command                                     |
      | cat internal/job/build_test.go              |
      | sed -n '1,40p' internal/job/build_test.go   |
      | grep -rn TestBuild features/ internal/      |
      | go test -count=1 ./internal/job/            |

  Scenario: A session that is not building may write a test
    Given a session the system is not building with
    When that session is about to write to "internal/job/build_test.go"
    Then the test gate allows it

  # The variable is the boundary, and a session that sets it would decide its own.
  Scenario: A session that sets the boundary variable itself is refused
    Given a session that the system is building with
    When that session is about to run the command: KREWE_BUILDING= rm internal/job/build_test.go
    Then the test gate refuses it
    And the refusal names the variable the system sets

  # It fires on every write every session makes, so a payload it cannot read has to go through.
  Scenario: A payload the gate cannot read lets the write happen
    Given a session that the system is building with
    When that session sends the test gate a payload it cannot read
    Then the test gate allows it
