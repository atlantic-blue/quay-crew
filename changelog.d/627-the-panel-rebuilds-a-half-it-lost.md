**Opening the system rebuilds a panel that lost a half.** `krewe` puts the console on the left and a
conversation on the right. Leaving the conversation closes the pane it was in, and quitting the
console closes the other, so the panel is very often a single pane by the time somebody opens the
system again.

Opening it used to come back to that single pane and leave it there, because the only question asked
was whether the panel's tmux session exists. A console on its own has no room to open a conversation
beside it, and a conversation on its own has no console to go back to, so the one thing a person
does next did not work.

The panes are counted now, and a panel down to one is built again. A panel with both halves is shown
and not touched, so opening the system twice does not take a conversation away and put a fresh one
in its place.
