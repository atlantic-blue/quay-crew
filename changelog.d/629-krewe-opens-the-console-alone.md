**`krewe` opens the console, full width, and nothing beside it.** It used to build a tmux window with
the console in one half and a conversation in the other, so a person who typed the name of the tool
got a split terminal and a conversation they had not asked for.

A conversation is still one key away. `p` in the console opens one beside it, from inside tmux, and
`krewe attach <session>` opens one on its own. Both are asked for by name, which is the difference:
opening the tool is not the same thing as starting a conversation.

`krewe panel` still refuses, with new words. It used to say that `krewe` on its own opens the system,
which would have sent a person back to the split screen this took away.

The layout code that built the window is gone from [internal/panel/panel.go](internal/panel/panel.go),
which now holds only the commands that put a pane beside a console already running. The scenarios that
described the two halves are replaced by one that runs the real tool in a real terminal multiplexer
and counts the panes, because a scenario that reads the commands the tool would run is what let the
split ship.

[#627](https://github.com/atlantic-blue/quay-krewe/issues/627) stays open. Its first fault, `krewe` not
rebuilding a panel that lost a half, no longer exists: there is no panel to rebuild. The other four are
about the conversation pane itself, which `p` still opens, so they are still true.
