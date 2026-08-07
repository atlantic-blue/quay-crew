# Changelog

What has actually shipped, newest first. Nothing is released yet, so the dates are the days the work
landed on `main` rather than version numbers, and anything not listed here does not exist.

The behaviour of each of these is written out as scenarios in [`features/`](features/), which you can
read, or run with `make features`.

## 7 August 2026

- **`quay` rebuilds a panel left over from an older build.** Opening it reattached to the tmux session
  already there, whose panes were still running the binary from before the upgrade, so a fix that had
  shipped was not in what you were looking at however many times you ran `make upgrade`. The panel
  records the build that made it and is made again when that differs. A panel from this build is still
  just reattached to, or every open would restart the conversation you were reading.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **A session can drive the crew from inside its sandbox.** The sandbox image carries `quay`, the
  sandbox joins the control plane's network, and the control plane puts its own address into every
  sandbox, so an agent in a session runs `quay workspace create` and it works with nothing to
  configure. That is what makes the conversation beside the console able to do anything rather than
  just talk about it.
  It is off unless it is turned on: a session that can drive the crew can also stop other sessions.
  `deploy/env.example` turns it on for a local stack, and without both the network and the address a
  sandbox reaches nothing of ours. ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **The wordmark is drawn in the panel's header again.** A height check left over from when the
  wordmark was six lines of block letters dropped it from any pane shorter than seven rows, and the
  header pane is one row by design. One line costs no rows, so only width can stop it now.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **The header is the wordmark, which build this is, and how to reach everything else.** It carried the
  crew's description and this view's keys, which at half the window left no room for the wordmark and
  pushed it off the screen. Julian: "the quay logo dissapears because there is too much text, lets
  leave only: the logo + version, and help", then "it occupies too much space". One row now: the
  wordmark is one line rather than six of block letters, and it survives a conversation beside the
  console at every width worth drawing a console in.
- **The help panel carries everything the header dropped**, on top of what it already had: where the
  crew is, where you are standing in it, and what it is running underneath. It scrolls with the arrow
  keys when a short window cannot show all of it, rather than cutting the end off silently, which is
  how a panel missing half its keys looks exactly like a complete one.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **`quay` is the panel. There is no `quay panel`.** Running `quay` opens the crew: the header across
  the whole width, the console under it on the left, and a conversation on the right. `p` shows or
  hides the conversation. Julian: "I dont understand why I need quay panel, is confusing, I need one
  command only, the panel should appear when I press quay and toggled with the key p", and "the header
  should be the whole width".
  The header is the console's own, drawn in a pane of its own so it can reach across both halves, and
  held at exactly its own rows when the terminal is resized. With no conversation to open yet, `quay`
  opens the console on its own rather than refusing.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **`p` shows or hides a conversation beside the console.** It opens the one under the cursor, or the
  one last spoken to when nothing is selected, and pressing it again closes the one it opened. It works
  in `quay`, not only in `quay panel`, so you do not have to decide before opening the console whether
  you wanted a conversation next to it.
- **The console keeps its own header, always.** It was replaced in the panel by a status line tmux
  drew, and what came back had lost the wordmark and the engines and sat on top of the console's own
  rows. Julian: "where is the header? this is a mess", then "I want this header always present". The
  status line is gone: the header is the console's, full width on its own and squeezed to half when a
  conversation is beside it. ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **The panel's header is tmux's own status line, and the panel is two panes again.** It was a third
  pane, which meant a process to draw it, and that process could not see which view the console was on,
  so the console had to publish it. tmux draws a status line itself, across the full width, at a height
  it owns and with no scrollback to scroll into. Julian: "why does header need a process?" It does not.
  Gone with it: the `quay header` command, the alternate screen handling, the view publishing and the
  resize hook that held the pane at a height.
- **The header is the bare essentials and the wordmark.** Which build, which crew, and where you are
  standing, asked again while you work so `quay use` in the other pane moves it.
- **`:stats` is what the crew is running underneath**: the model backend, the sandbox and store
  engines, where secrets and state are kept, whether anything reads the event log. It was six lines of
  the header, which is what made the header too tall to be fixed.
- **`:keys` is every key the console answers to**, as a view you can leave open beside what you are
  doing rather than only an overlay behind the question mark. It reads the same list the overlay does.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **`quay manual`: quay describing itself, to be loaded as a session's context.** A session sitting in
  the panel beside the console knew nothing about the crew it was next to. Pipe it where it is needed:
  `quay manual | quay context set me/house-bills`. Most of it is assembled rather than written, from the
  tool's own usage and the behaviour specification embedded in the binary, so neither half can drift
  from what the tool actually does. ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **The panel's header spans the full width, above both halves.** Three regions now: the header across
  the top, then the console and a conversation side by side underneath. A tmux pane is a rectangle, so
  the header is a pane of its own, given exactly the rows it needs. With the whole width it lays its
  key hints out in columns instead of one.
  The console draws no header inside the panel and says which view it has moved to, so the header names
  that view's keys rather than the keys of whichever view the panel opened on.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **The panel opens from inside tmux.** It was being built correctly and then never shown: tmux
  refuses to attach a client that is already inside one, so the two panes sat there running while the
  terminal said `sessions should be nested with care` and nothing appeared. From inside tmux the
  client is switched to the panel instead of attaching a second one.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

- **`quay panel`: the console and a conversation, side by side, half the width each.** The console
  shows the crew and a conversation shows one thread, and using both meant losing sight of one. tmux
  does the splitting, the same tmux that already keeps an open conversation alive behind `ctrl-q`.
  Named a session it opens that one; named nothing it opens the conversation you were last in, and
  refuses rather than opening half a panel when there is none.
  ([#143](https://github.com/atlantic-blue/quay-crew/issues/143))

## 6 August 2026

- **A failed turn says why.** Every model failure read `run turn: model: run exited: exit status 1`,
  which is the same sentence for an expired token, a network failure, a missing model in the sandbox
  image and the model refusing outright. It now carries the reason: the model's own words where it got
  far enough to say anything, and what came back from the sandbox where it did not. A rejected token
  now reads `Failed to authenticate. API Error: 401 OAuth access token is invalid. (status 401)`, and
  a sandbox with no model in it names the binary it could not find.
  ([#51](https://github.com/atlantic-blue/quay-crew/issues/51))
- **Nothing a failed turn says can carry the subscription token.** A turn runs with the token in its
  environment, so every place a failure can quote is a place it turns up. Values passed in this turn's
  environment are matched exactly, and the published token shape is matched as well for one this
  process never held.

## 5 August 2026

- **The wizard makes one thing at a time.** `n` asks what to make first, then only the questions that
  thing needs, and where a question needs a parent it offers what the crew already has instead of a
  blank name. A workspace, a project, the subscription token, a project's context or a session, each on
  its own. It could previously only make a whole new crew from nothing, so there was no way to add a
  project to a workspace you had, or set a token on one. Making a project makes a project and nothing
  else. ([#138](https://github.com/atlantic-blue/quay-crew/issues/138))
- **The wizard closes when it has made what it was asked for.** It made everything correctly and then
  stayed drawn over the list it had already refreshed, so nothing looked like it had happened, and the
  next enter was taken as an answer to the step that was already working, whose prompt is the words
  `making it`. Julian, driving it: "it fails to create anything", then "it says making it: this one is
  needed". A key other than escape now does nothing while the crew is making it, because the wizard is
  asking nothing at that point. ([#140](https://github.com/atlantic-blue/quay-crew/issues/140))
- **A wizard that makes things.** `n` in the console asks for a workspace, a project, the subscription
  token, the project's context and a first message, in the order they depend on each other, and makes
  them in one pass at the end. Escape at any step makes nothing and forgets everything, including a half
  typed token. The token is never echoed. The command line could already create all five; the console
  could create none. ([#139](https://github.com/atlantic-blue/quay-crew/pull/139))
- **The key list stops cutting its longest entries.** Columns are as wide as the widest entry rather
  than an even share of the room, because an even share truncates and a key list missing its last few
  characters is one that lies.
- **Leaving a conversation is `ctrl-q`, and ending one no longer takes the work with it.** A conversation
  runs through a wrapper that keeps its terminal alive: pressing ctrl-d twice used to end the tmux
  session and everything the model was in the middle of, and now it says the conversation is closed and
  waits, with enter to open it again. The status line says how to leave, because it was off and there
  was nothing on screen telling anybody. ctrl-q works because the wrapper turns off flow control first,
  which is the only reason that key can be a key.
  ([#137](https://github.com/atlantic-blue/quay-crew/pull/137))

## 4 August 2026

- **A view of what each workspace has set.** `:secrets` in the console and `quay secret list`, naming
  the workspace and the secret and saying `set, and not shown anywhere` where a value would be. There is
  no call that returns a value and no field for one, so this cannot leak by mistake rather than by
  policy. ([#136](https://github.com/atlantic-blue/quay-crew/pull/136))
- **The console shows a session's history.** `l` on a session opens a `turns` view of what it was
  asked and what came back, read from the projection, so it answers without starting a container and
  keeps answering long after the sandbox is gone. A failed turn shows why it failed where the reply
  would be. Enter still opens the conversation: that is the thing an operator does most on that row,
  so it keeps the cheapest key, and there is a scenario that fails if anything bound to enter starts
  descending instead. ([#134](https://github.com/atlantic-blue/quay-crew/issues/134))
- **The subscription token survives a restart.** Secrets were held in memory, so every `make up` lost
  the token and the next turn failed saying nothing useful. They are kept in Postgres now, sealed with
  a key made once and kept on the host at `~/.quaycrew/data/secrets.key`, so holding the database is
  not enough to read one. The status block says `Secrets: postgres, sealed`, and says it in red when
  they are still in memory. ([#133](https://github.com/atlantic-blue/quay-crew/pull/133))
- **A session's history can be read back.** Turns went onto the event log and nothing read them, so a
  conversation was write only from the outside. A projection now consumes every workspace's turn
  stream, by pattern rather than by a list fixed at startup so a workspace created while the crew is
  running is read too, and materialises it into a `turns` table. `quay turns <session>` prints what a
  session was asked and what came back, in the order it happened, without starting a container and
  long after the sandbox is gone. Delivery from a log is at least once, so each event carries an id
  and writing the same one twice leaves one turn: there is a conformance test for that against both
  stores, and an integration test that runs it against a real broker.
  ([#130](https://github.com/atlantic-blue/quay-crew/issues/130))
- **Every level of context is visible and settable, from both surfaces.** The crew's own level is in
  every listing, `quay context set [<address>|crew]` takes it from a file on standard input, and the
  console edits any level in your editor through a scratch file rather than the rendered one, because a
  level is rendered into every session that reads it and there is no single file to open. That is the
  path for moving what you already have into the crew.
  ([#131](https://github.com/atlantic-blue/quay-crew/pull/131))
- **Every turn is written to the event log.** The broker had run in the stack for weeks holding zero
  topics, because the boundary was built and nothing on either end of it was. The control plane now
  publishes a turn to `<workspace>.turns` whenever one runs, keyed by session so a conversation stays
  in order, carrying the prompt, the reply, the status and where the session sits, so a consumer never
  has to query the store to know what it is reading. A turn that failed is published too, because that
  is the one somebody comes looking for. Publishing never fails a turn: the turn already happened, and
  a broker that is unreachable is logged and dropped. A stack with no `QC_KAFKA_SEEDS` runs turns and
  says out loud that nothing records them. The topic is created on first use, which an integration
  test against a real Redpanda caught: the very first record to a new workspace was being rejected and
  quietly dropped. ([#128](https://github.com/atlantic-blue/quay-crew/issues/128))
- **Four levels of context, and a working directory per session.** Crew, workspace, project and session,
  layered into the two files the model actually reads: the outer two in the conversation store every
  session in a workspace sees, the inner two in that session's own working directory. Sessions no
  longer share a project's working directory, which is what makes the innermost level possible and
  stops two conversations changing files under each other. What something inside a sandbox writes is
  read back into the level it belongs to, and a note appended at the end lands on the session.
  ([#124](https://github.com/atlantic-blue/quay-crew/pull/124))
- **`make up-observability` starts all four services.** Tempo was pointed at `/etc/tempo.yaml`, a file
  neither in its image nor mounted, so it exited on startup and the profile quietly came up one short.
  Loki and Tempo are now configured from `deploy/loki.yaml` and `deploy/tempo.yaml`, kept here rather
  than left to an image default that can move underneath the stack, and a test refuses any service
  that names a config file nobody provides, so the next one cannot fail the same way silently.
  ([#126](https://github.com/atlantic-blue/quay-crew/issues/126))
- **The database doc covers the `contexts` table, and says session throughout.** It was written hours
  before context moved into the store and before the console went back to the word the database uses,
  so it described five tables and called a session a thread. Six tables now, with what `scope` and
  `owner` mean and why that table has no foreign key, plus a query for what the model has been told.
  ([#123](https://github.com/atlantic-blue/quay-crew/issues/123))
- **Observability is documented, including the part that does not work.**
  [`docs/OBSERVABILITY.md`](docs/OBSERVABILITY.md) says which of the three signals is real: structured
  logs are, and no span or metric is created anywhere in the codebase, so the OpenTelemetry wiring
  exports nothing and the collector discards what it receives. It also records that `tempo` in the
  compose profile is pointed at a config file that is neither in the image nor mounted, so it cannot
  start, and that Grafana has no data sources provisioned. What to read when something is wrong, and
  the order the three open issues have to land in.
  ([#121](https://github.com/atlantic-blue/quay-crew/issues/121))
- **The console says sessions again.** It was threads for a day. The database and the API both say
  session, and one name across the whole system beats a console that translates. `threads`, `thread`
  and `t` still open the view, the way `sessions` did while it was called threads.
  ([#120](https://github.com/atlantic-blue/quay-crew/pull/120))
- **Context lives in the database, and the file in a sandbox is a rendering of it.** It was only ever
  files on one machine, which works nowhere else: a pod has no host directory to bind mount and an API
  cannot edit a file on somebody's laptop. Setting it writes the file too, so a running sandbox picks it
  up on its next turn, and **what an agent writes into its own memory is read back into the store**
  rather than overwritten, because an agent that cannot write down what it learned is the problem this
  project exists to solve. ([#119](https://github.com/atlantic-blue/quay-crew/pull/119))
- **The database and the event log are documented.** [`docs/DATABASE.md`](docs/DATABASE.md) covers why
  a thread survives a restart at all, how to shell in with psql, what every table and column means, the
  queries worth knowing, and why reading from the prompt is safe while writing from it is not.
  [`docs/EVENTS.md`](docs/EVENTS.md) covers what the log is for, how to inspect Redpanda with `rpk`,
  how topics are named, and the state it is actually in: the boundary and its client exist, nothing
  publishes to it and nothing consumes it, so a stack today holds zero topics. Documentation only, no
  behaviour changed. ([#113](https://github.com/atlantic-blue/quay-crew/issues/113))
- **The command bar says what it can open.** Pressing `:` asked a question and gave nothing to answer
  it with, so the only way to learn a view's name was to know it already. It now lists them and narrows
  as you type, and `?` has a views section saying what to type for each.
  ([#116](https://github.com/atlantic-blue/quay-crew/pull/116))
- **The detach key works.** It was `ctrl-o` for one release, and on macOS `^O` is the terminal's own
  DISCARD character, so the line discipline swallows it and tmux never sees it: the key did nothing.
  It is `ctrl-space d` now, `ctrl-b d` still works when nothing is nested, and a test refuses every
  control character a terminal reserves rather than the one spelling that broke.
  ([#115](https://github.com/atlantic-blue/quay-crew/pull/115))
- **Editing context works with no `EDITOR` exported.** It refused rather than falling back, which made
  the whole thing dead on any machine that has not set one, which is most machines. `VISUAL`, then
  `EDITOR`, then `vi`, which is what git and crontab do.
  ([#114](https://github.com/atlantic-blue/quay-crew/pull/114))
- **You can edit context from either surface.** `enter` or `e` on a row in the `context` view opens the
  memory file in your own `$EDITOR`, and `quay context edit [<address>]` does the same from the command
  line. The directory is made first, so an editor writing into a project whose sandbox has never run
  does not fail on a path nobody created, and an unset `EDITOR` is said out loud rather than guessed at.
  ([#112](https://github.com/atlantic-blue/quay-crew/pull/112))
- **You can find the files the model reads.** `quay context` on the command line and a `context` view in
  the console, both clients of one control plane call: a row per workspace and per project, where it is
  on your machine, where it appears inside a sandbox, and whether `CLAUDE.md` has been written yet. The
  mounts have existed for a while and nothing said where they were, so the feature worked and nobody
  could find it. ([#111](https://github.com/atlantic-blue/quay-crew/pull/111))
- **You can leave an open thread without ending it.** Opening a conversation handed the terminal to
  `claude --resume` and the only way back to the list was to end it. It now runs inside tmux in the
  thread's own sandbox, so `ctrl-b d` leaves the model running and returns you to the console, and
  opening the thread again lands in the same live conversation. The sandbox image carries tmux, with
  `ctrl-o` as its prefix so it still works when you opened the console from inside your own tmux, and
  `ctrl-b` as a second prefix for when nothing is nested.
  ([#109](https://github.com/atlantic-blue/quay-crew/pull/109))
- **The key list stops silently dropping keys.** It folded into two columns and then cut whatever did
  not fit, so adding a binding pushed the last one off the bottom with nothing to say so. It folds into
  as many columns as it takes.
- **Opening a thread runs in the mode that thread is set to.** The attached session carried no mode at
  all, so a thread armed to skip permissions stopped and asked the moment you opened it, which reads as
  the toggle not working. ([#107](https://github.com/atlantic-blue/quay-crew/pull/107))
- **The daemon is the source of truth about containers, not a map in the control plane.** It remembered
  every sandbox it had made and trusted that memory forever, so anything that removed a container
  behind its back left a handle pointing at nothing and handed the operator a name Docker had never
  heard of: `No such container: quaycrew-1edc8349315233e36bf4fd53`, over and over. Every turn and every
  attach now asks the provider, which adopts the container already carrying that name or makes one.
  ([#106](https://github.com/atlantic-blue/quay-crew/pull/106))
- **A thread's permission mode, shown and toggled.** Every turn ran `acceptEdits`, hardcoded, and no
  operator could see it or change it. The mode now belongs to the thread and survives a restart, the
  listing has a `MODE` column reading `edits`, `plan` or `dangerous`, and `D` in the console flips the
  selected thread between asking and skipping every permission, through the same confirmation as the
  other keys that change what a thread is. A mode the model does not understand is refused rather than
  handed to it, and `bypassPermissions` is refused outright when turns run on the host instead of in a
  container. ([#105](https://github.com/atlantic-blue/quay-crew/pull/105))

## 3 August 2026

- **The refusals are in the operator's words, and name the thread on their screen.** "Its conversation
  is gone, it predates state on the host" is a sentence only somebody who worked on this understands,
  and it named a twenty four character identifier that appears nowhere in the list. Attach now says
  what happened and what to do, about `thread 34e1a6c7` rather than `session 134c2c6dbf1e907413753cc5`.
  ([#103](https://github.com/atlantic-blue/quay-crew/pull/103))
- **A thread whose conversation is gone says so.** A session's handle points into a store the crew does
  not own, so it can outlive what it points at: every conversation from a sandbox built before state
  was kept on the host died with that container while the row kept the handle. Resuming one printed
  `No conversation found` inside the container and exited, which from the console looked like nothing
  happening. Attaching now checks the workspace's store first and says to dispatch a turn instead.
  ([#102](https://github.com/atlantic-blue/quay-crew/pull/102))
- **Opening an idle thread works again.** Attaching answered from the database row alone, so after the
  control plane restarted it handed back a container name the daemon had never heard of:
  `No such container: quaycrew-134c2c6d...`. Attaching now starts the thread's sandbox when there is
  not one, and creating a sandbox adopts the container already carrying that name instead of colliding
  with it. ([#101](https://github.com/atlantic-blue/quay-crew/pull/101))
- **`r` refreshes the view.** It restarted a thread for one afternoon. Refreshing is the key you reach
  for constantly, so it holds the short obvious letter; restart moved to `R`, beside `A` for archive,
  and `g` still refreshes too. ([#99](https://github.com/atlantic-blue/quay-crew/pull/99))
- **A thread can be put away, and brought back.** `A` archives one through the same confirmation, and an
  `archived` view lists what was put away with `u` to restore it. Archiving stops the thread, closes its
  sandbox and hides it from the default listing. Nothing is deleted: the row, the conversation and the
  project files all stay. ([#97](https://github.com/atlantic-blue/quay-crew/pull/97))
- **A stopped thread can be restarted, with its container.** `r` in the console, `RestartSession` on the
  control plane: back to idle with the sandbox already running, so you can attach into the conversation
  instead of dispatching a turn to make the container exist. Only safe because a session's state lives
  on the host. ([#96](https://github.com/atlantic-blue/quay-crew/pull/96))
- **A destructive key asks first, and backspace stops a thread.** `stop thread d754610f?`, drawn where
  the command bar draws. Yes acts, and every other key cancels, because an accidental cancel costs one
  keypress and an accidental yes costs a conversation. `x` still stops, through the same question.
  ([#95](https://github.com/atlantic-blue/quay-crew/pull/95))
- **Enter opens a thread's conversation.** A thread has nothing to drill into, so enter did nothing
  at all on the one view where the obvious key has an obvious meaning. `a` still works, and the
  question mark now lists every key an action answers to.
  ([#93](https://github.com/atlantic-blue/quay-crew/pull/93))
- **The console calls them threads.** The view, its panel title and the breadcrumb say threads, because
  a row in that list is one conversation. The control plane still calls the running thread a session,
  which is a real distinction inside it and means nothing to somebody reading a list of fourteen rows.
  `sessions`, `session`, `sess` and `s` all still open the view.
  ([#90](https://github.com/atlantic-blue/quay-crew/pull/90))
- **A flag is refused rather than swallowed.** `quay dispatch --project default "hello"` used to make
  the flag and its value the first two words of the message, and then complain about the workspace. The
  flags addresses replaced are named, with what to type instead, and any other flag is refused too, so
  the next thing replaced by an address cannot repeat it.
  ([#89](https://github.com/atlantic-blue/quay-crew/pull/89))
- **The status block says what is true, in engines.** `Sandbox engine`, `Store engine` and
  `Events engine`, `Workspace` and `Project` rather than a borrowed `Context`, and the state line names
  where a conversation is kept instead of promising it survives. The events line says
  `none, nothing reads or writes the log yet`, which is the truth: Redpanda is in the compose stack and
  no service is connected to it. ([#87](https://github.com/atlantic-blue/quay-crew/pull/87))
- **`make upgrade` brings the stack back the way you configured it**, and clears the sandboxes from
  before the upgrade. Two bugs: it restarted compose with the defaults, so a stack started with
  `QC_MODEL=claude-code` came back running `echo`, and it left every old sandbox running, which blocks
  those threads from ever starting again because the control plane has forgotten them and their names
  are taken. Configuration now lives in `deploy/.env`, which compose reads on every command.
  ([#86](https://github.com/atlantic-blue/quay-crew/pull/86))
- **The console says when the control plane is older than the tool**, rather than quietly showing four
  fewer lines: `Quay: this control plane is older than the tool, run make upgrade`. Installing the tool
  does not rebuild the stack, so this is the normal state of things after an upgrade, and silence reads
  as the console being broken. ([#81](https://github.com/atlantic-blue/quay-crew/pull/81))
- **`quay features`**, and a `features` view in the console: what this build can do and what proves it,
  read from the behaviour specification embedded in the binary. It asks the control plane nothing,
  because a capability belongs to the build rather than to a running stack, and the question is usually
  asked by somebody who has not started one yet. A hand written list would drift from the scenarios;
  this one cannot. ([#82](https://github.com/atlantic-blue/quay-crew/pull/82))
- **`make upgrade`**: fetch, fast forward, rebuild the tool and the images, restart the stack. One
  command for "bring everything to the latest", because `make install` only ever builds the tool and a
  new tool against an old control plane is the mismatch that costs an afternoon. It refuses on a
  branch, on a dirty checkout, and when it cannot reach the newest build, rather than quietly
  rebuilding the stack from something else.
  ([#80](https://github.com/atlantic-blue/quay-crew/pull/80))
- **The console header reads like k9s**: the status block says which build, which control plane, where
  you are standing, and what a turn would run in; this view's own commands sit beside it as
  `<a> Attach`; `?` lists every key; the panel title is centred; the sorted column is marked `THREAD↑`;
  and the wordmark sits on the right when there is room for it.
  ([#77](https://github.com/atlantic-blue/quay-crew/pull/77))
- **`quay version`**, and every build stamped with the commit it came from, marked dirty when the
  checkout had uncommitted changes. Part of #74; `quay update` and release tags are still open.
  ([#77](https://github.com/atlantic-blue/quay-crew/pull/77))
- **`make install` installs over the copy your shell actually runs**, and says which commit it built,
  so a stale binary earlier on your PATH cannot quietly keep serving you yesterday's tool. Set
  `BINDIR` to put it somewhere specific. ([#73](https://github.com/atlantic-blue/quay-crew/pull/73))
- **The console says which crew you are on and what you can press.** A status block naming the
  address, the model backend, the sandbox, the store and whether a conversation outlives its
  container, with the key hints beside it. The rows sit in a framed panel titled with their scope and
  count, `sessions(house-bills)[3]`, ordered by a column marked with an arrow, and the breadcrumb at
  the bottom reads `me > house-bills > sessions` so it is clear what escape goes back to.
  ([#72](https://github.com/atlantic-blue/quay-crew/pull/72))
- **The control plane says what it is running**: the model backend a turn runs against, what a session
  is isolated in, where workspaces and sessions are kept, and whether a conversation outlives its
  container. Two stacks look identical from a list of sessions and behave nothing alike.
  ([#71](https://github.com/atlantic-blue/quay-crew/pull/71))
- **A session's conversation and files outlive its container.** Every sandbox mounts two directories
  from the host: the workspace's conversation store at `/home/agent/.claude` and the project's working
  directory at `/home/agent/workspace`. Before this, stopping a session ran `docker rm -f` and deleted
  the conversation the database still held a handle to.
  ([#66](https://github.com/atlantic-blue/quay-crew/pull/66))
- **Shared context, through the same directories.** A workspace and a project each own a directory the
  sandbox mounts, so the memory the model already reads from `CLAUDE.md` is a file you edit rather than
  text this tool assembles. Both are read write, because the model writes its own store into one of
  them. ([#66](https://github.com/atlantic-blue/quay-crew/pull/66))
- **The crew is addressed by path, from a current context.** `quay use me/house-bills` and then
  `quay dispatch "..."`, with the place kept in `~/.config/quay/context`. Creating something moves you
  into it. An address typed on a command applies to that command only, and a thread is the third level,
  so standing in one continues that conversation. Replaces `--workspace`, `--project` and `--thread`.
  ([#69](https://github.com/atlantic-blue/quay-crew/pull/69))
- **Names have to be addressable.** A workspace or project name is lowercase letters, digits and
  hyphens, refused otherwise with a suggestion that would work. A name is half of an address, so it has
  to survive being typed without quoting. ([#68](https://github.com/atlantic-blue/quay-crew/pull/68))
- **Attach to a thread's conversation, not just a shell in its sandbox.** `quay attach <session>`, or
  `a` in the console, runs the model's own resume inside that session's container, so you land in the
  conversation with its history. Shelling in shows you the room; this shows you the conversation.
  ([#61](https://github.com/atlantic-blue/quay-crew/pull/61))
- **The sandbox carries the workspace's environment from the moment it is created**, so attaching needs
  no credential from your shell and no tool has to carry a token around.
  ([#62](https://github.com/atlantic-blue/quay-crew/pull/62))
- **Projects, between a workspace and its threads.** A workspace is who you are, a project is a body of
  work, a thread is one conversation. A thread identifier is unique inside its project, which is the
  reason the level exists. ([#59](https://github.com/atlantic-blue/quay-crew/pull/59))
- **`project` renamed to `workspace`** throughout, because the level that already existed was the
  tenancy one, and the word for a body of work inside it was needed.
  ([#58](https://github.com/atlantic-blue/quay-crew/pull/58))
- **Names and short identifiers everywhere a listing prints.** `5d013d07  me/house-bills  thread
  d754610f  idle` rather than three lines of hexadecimal.
  ([#53](https://github.com/atlantic-blue/quay-crew/pull/53))
- **A project can be addressed by its name**, not only by the identifier printed once at creation. An
  id still wins, and an ambiguous name is refused with the candidates rather than guessed.
  ([#50](https://github.com/atlantic-blue/quay-crew/pull/50))
- **Attaching says why it cannot proceed** when a session has never had a turn, or has been stopped,
  instead of opening something that immediately errors.
  ([#63](https://github.com/atlantic-blue/quay-crew/pull/63))
- **The sandbox image ships past the model's first run.** Onboarding and the trust prompt are already
  answered, so attaching lands in the conversation rather than a theme picker, which reads exactly like
  a broken token. ([#64](https://github.com/atlantic-blue/quay-crew/pull/64))

## 2 August 2026

- **The quay console.** `quay` with no arguments opens a full screen view of the crew, in the shape of
  k9s: `:` to switch resource, `/` to filter, enter to drill in, `s` to shell into a session, `x` to
  stop it. Adding a view is declaring a resource, which is the deliverable rather than the two views it
  ships with. ([#48](https://github.com/atlantic-blue/quay-crew/pull/48))
- **Workspaces and sessions survive a restart**, in Postgres, behind one store interface with an in
  memory implementation held to the same conformance suite. A failed turn never erases the conversation
  handle, because that handle is the only pointer to a conversation the model keeps on its own disk.
  ([#44](https://github.com/atlantic-blue/quay-crew/pull/44))
- **Behaviour specifications.** [`features/`](features/) states what the product does, driven against
  the real control plane interface over an in process connection. The suite fails if it finds no
  feature files, because a run that proves nothing otherwise reports success.
  ([#41](https://github.com/atlantic-blue/quay-crew/pull/41))
- **Automation graphs designed** as a pure reducer over the event log, in
  [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md). Legibility is what is wanted at that layer, not
  power: knowing what an arbitrary reducer does means running it.
  ([#43](https://github.com/atlantic-blue/quay-crew/pull/43))

## 11 July 2026

- **Real Claude turns, on your subscription, inside the sandbox.** The image carries the Claude Code
  command line tool and no credentials; the token is a workspace secret, injected into the session's
  sandbox. No API cost. ([#38](https://github.com/atlantic-blue/quay-crew/pull/38))
- **A turn runs in a Docker sandbox**, one long lived container per session, proved by continuous
  integration dispatching a real turn against the composed stack. Asserting the services were merely
  running had let a stack ship that could not execute a single turn.
  ([#37](https://github.com/atlantic-blue/quay-crew/pull/37))

## 8 to 10 July 2026

- **The event log**, consumed by group, committing only after a batch is handled, so delivery is at
  least once and handlers have to be idempotent.
  ([#31](https://github.com/atlantic-blue/quay-crew/pull/31))
- **The `quay` command line tool**, a synchronous client of the control plane.
  ([#30](https://github.com/atlantic-blue/quay-crew/pull/30))
- **The control plane**: the gRPC service, the model adapter, and the sandbox provider, each behind an
  interface so nothing downstream depends on an implementation.
  ([#29](https://github.com/atlantic-blue/quay-crew/pull/29))
- **The channel message contract**, so a chat app and the command line tool say the same thing.
  ([#28](https://github.com/atlantic-blue/quay-crew/pull/28))
- **The scaffold**: one Go monorepo, a service per component, the compose stack, and continuous
  integration. ([#27](https://github.com/atlantic-blue/quay-crew/pull/27))

## Not shipped, despite appearances

Two things the documentation has claimed and the code does not do yet, listed here so nobody plans
around them:

- **Nothing creates a span.** The OpenTelemetry SDK is wired and no tracer is ever used, so the
  collector has received no telemetry at all.
  ([#3](https://github.com/atlantic-blue/quay-crew/issues/3))
- **The telemetry stack is not connected.** Grafana, Loki, Tempo and Prometheus are in the compose file
  with nothing joining them up. ([#12](https://github.com/atlantic-blue/quay-crew/issues/12))
