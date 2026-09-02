**Enter on a session opens that session's conversation, and in a panel it opens beside the console.**
The console handed the command line the row under the cursor, and the command line dropped it: the
pane was built from `OpenDriver`, so every open landed on the driver whatever the operator pointed
at. A list of several conversations gave back the same one every time.

Enter now opens into the pane beside the console where there is one, so the listing stays on screen
and the conversation the operator is talking to is the row they chose. A console on its own has no
pane beside it and hands over its own screen, which is what it always did. Opening the system with no
argument still opens the driver, and that is deliberate.

`p` and `P` hand over the same row, and it is honoured now rather than dropped, so showing a
conversation beside the console and starting a fresh one both act on the session under the cursor.
Ending one and opening another was the risk in following the cursor, and both keys read the same
session, so it cannot happen.

A session that cannot be opened is refused by name. The conversation already beside the console stays
where it is, rather than being replaced by somebody else's.

The tests that stayed green through all of this asserted the call rather than the conversation, and
the console's control plane double answered every session with one conversation, so nothing could
tell the right conversation from the wrong one. The double answers per session now, and the console
has a seam for its panes, so a scenario reads what is open beside it.
