Feature: A job says what a person does with it and what they get back

  A tree of jobs built a design document faithfully and delivered it complete. Every check was
  green. The operator opened it two days later and could not use it. The document said the address reads
  /videos?id=<video id>, so every job downstream took the video identifier as the key, and a reader
  holding a link had to dig that identifier out by hand before the page was any use. Nobody had
  written the sentence a person would say, which is "paste a link and get the text back", so the
  address shape was never measured against anything.

  So a job carries that sentence. It is what somebody does with what gets built and what they get
  back, in that person's words, and it is not the architecture and not the address shape. The job at
  the top states it and every job under it carries the same one, which is what puts it in front of a
  session three levels down without anybody typing it again.

  A design document is evidence for the sentence, never a replacement for it. The session is told
  which of the two wins.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A job carries what a person does and gets, and reading it back says so
    Given the system listens on an address the tool can dial
    When the caller declares a job through the tool saying a person "pastes a link and gets the text back"
    Then the command succeeds
    And reading that job back through the tool says a person "pastes a link and gets the text back"

  # The sentence has to reach the sessions that do the work, and they are not the session that wrote
  # it down. Every job in the tree carries the same one.
  Scenario: A job declared under another carries the same sentence
    Given the workspace allows jobs down to depth 2
    And a job titled "build the transcript page" saying a person "pastes a link and gets the text back"
    When the session running it declares a job
    Then the new job says a person "pastes a link and gets the text back"

  Scenario: A job two levels down still carries it
    Given the workspace allows jobs down to depth 2
    And a job titled "build the transcript page" saying a person "pastes a link and gets the text back"
    And the session running it declares a job
    When the session running that job declares another
    Then the new job says a person "pastes a link and gets the text back"

  # A tree with two products has none. Ignoring the second in silence would be worse: the caller would
  # believe the product had moved.
  Scenario: A job stating a different sentence from the one above it is refused
    Given the workspace allows jobs down to depth 2
    And a job titled "build the transcript page" saying a person "pastes a link and gets the text back"
    When the session running it declares a job saying a person "searches the archive by video id"
    Then the system refuses it, naming the sentence the job above it serves
    And the project holds only the job the operator declared

  # A tree that started without a sentence can still gain one.
  Scenario: Under a job that says nothing, the new job's own sentence stands
    Given the workspace allows jobs down to depth 2
    And a job titled "build the transcript page" saying nothing about what a person gets
    When the session running it declares a job saying a person "pastes a link and gets the text back"
    Then the new job says a person "pastes a link and gets the text back"

  # The point of the whole thing. A session given the brief alone builds what the brief says, and
  # building the brief faithfully is what already went wrong.
  Scenario: The session doing the job is told the sentence, and told that it wins
    Given a job titled "build the transcript page" saying a person "pastes a link and gets the text back"
    When the controller ticks
    Then the session was told a person "pastes a link and gets the text back", and that the sentence wins

  Scenario: A sentence of 201 bytes is refused
    When the caller declares a job saying a sentence of 201 bytes
    Then the system refuses it and says the ceiling is 200

  # The system cannot write the sentence, and a tree of jobs that runs an errand needs none, so this
  # says rather than refuses. It is said where the person who typed the declaration is looking.
  Scenario: Declaring a job at the top with no sentence says one is missing
    Given the system listens on an address the tool can dial
    When the caller declares a job through the tool saying nothing about what a person gets
    Then the command succeeds
    And standard output says the sentence is missing and how to say it

  # The second half of the same failure. A job carrying the sentence puts it in front of every
  # session, and nothing ever measured what was delivered against it. So a run that builds something
  # a person can open stops once, at the first usable path, and asks. An answer of no there costs one
  # step. The same answer once everything is built costs the run.

  Scenario: A flow that stops for a person and says nothing about what they get is refused at import
    When the operator imports this flow graph, which is refused:
      """
      name: transcript
      version: 1
      mode: edits
      nodes:
        page:   { type: dispatch, prompt: "put the thinnest page up", usable: true }
        polish: { type: dispatch, prompt: "finish the page" }
      edges:
        - [page, polish]
        - [polish, done]
      """
    Then the refusal says how to write what a person gets

  Scenario: A run stops at the first thing a person can open, and the question names it and the sentence
    Given the system holds this flow graph:
      """
      name: transcript
      version: 1
      mode: edits
      product: paste a link and get the text back
      nodes:
        page:
          type: dispatch
          prompt: "put the thinnest page up and reply with its address"
          usable: true
        polish:
          type: dispatch
          prompt: "finish the page"
      edges:
        - [page, polish]
        - [polish, done]
      """
    And the model will answer "https://transcripts.example/videos?id=gyN9lV9QgyA"
    When the operator starts the flow "transcript" in the project
    Then the flow run asks about the product, naming "https://transcripts.example/videos?id=gyN9lV9QgyA" and "paste a link and get the text back"
    And the run's steps were asked 1 task

  # The answer the whole thing exists for. It does not end the run: it replaces the sentence, and
  # every step after it is done against the new one.
  Scenario: Told no, the run replaces the sentence and carries on from it
    Given the system holds this flow graph:
      """
      name: transcript
      version: 1
      mode: edits
      product: search the archive by video id
      nodes:
        page:
          type: dispatch
          prompt: "put the thinnest page up and reply with its address"
          usable: true
        polish:
          type: dispatch
          prompt: "finish the page"
      edges:
        - [page, polish]
        - [polish, done]
      """
    And the model will answer "https://transcripts.example/videos?id=gyN9lV9QgyA"
    When the operator starts the flow "transcript" in the project
    And the operator answers the run with "paste a YouTube link and get the text back"
    Then the flow run is done
    And the job carrying the run says a person "paste a YouTube link and get the text back"
    And the step after the question was told a person "paste a YouTube link and get the text back", and that the sentence wins

  Scenario: Told yes, the run carries on against the sentence it had
    Given the system holds this flow graph:
      """
      name: transcript
      version: 1
      mode: edits
      product: paste a link and get the text back
      nodes:
        page:
          type: dispatch
          prompt: "put the thinnest page up and reply with its address"
          usable: true
        polish:
          type: dispatch
          prompt: "finish the page"
      edges:
        - [page, polish]
        - [polish, done]
      """
    And the model will answer "https://transcripts.example/videos?id=gyN9lV9QgyA"
    When the operator starts the flow "transcript" in the project
    And the operator answers the run with "yes"
    Then the flow run is done
    And the job carrying the run says a person "paste a link and get the text back"

  # A question naming an address nobody can open is a question about nothing, and a gate whose
  # question is empty is a gate that passes.
  Scenario: A step that built something and named no address stops the run
    Given the system holds this flow graph:
      """
      name: transcript
      version: 1
      mode: edits
      product: paste a link and get the text back
      nodes:
        page:
          type: dispatch
          prompt: "put the thinnest page up and reply with its address"
          usable: true
        polish:
          type: dispatch
          prompt: "finish the page"
      edges:
        - [page, polish]
        - [polish, done]
      """
    And the model will answer " "
    When the operator starts the flow "transcript" in the project
    Then the flow run stopped because the step named no address
    And the run's steps were asked 1 task
