#!/bin/sh
# Opens a session's conversation and keeps the terminal it runs in alive afterwards.
#
# Two ways out of a conversation, and only one of them was meant to end it. Detaching leaves the model
# running and goes back to the console. Ending the conversation, which is what pressing ctrl-d twice
# does, used to take the whole terminal with it: the tmux session went, and anything the model was in
# the middle of went with it.
#
# So the conversation runs in a loop. When it ends, the terminal stays, says what happened, and waits.
# Enter opens the conversation again, and detaching leaves everything as it is.
set -u

# ^Q and ^S are flow control on a terminal by default, which means the line discipline swallows them
# and no application ever sees one. Turning that off is what lets ctrl-q be a key rather than a
# character the terminal keeps for itself.
stty -ixon 2>/dev/null || true

conversation="$1"
mode="$2"

while true; do
    claude --resume "$conversation" --permission-mode "$mode" || true

    printf '\n  This conversation is closed. Nothing was lost: it is on disk and can be opened again.\n'
    printf '  Press enter to open it, or ctrl-q to leave this running and go back.\n\n'
    # A terminal with nothing reading it is one keystroke from being cleaned up, so it waits here.
    read -r _ || sleep 3600
done
