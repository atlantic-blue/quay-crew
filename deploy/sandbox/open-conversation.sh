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

# What the system tells the runtime: the hooks this session runs under, and the line it draws under the
# conversation. The system renders the file and mounts it read only, so its presence is the whole
# question, and a sandbox made before the system rendered one simply has none.
#
# Checked here rather than passed in, so opening a conversation is under the same constraints a
# dispatched task is. A gate that only runs on dispatched tasks is one the operator walks around by
# opening the session, which is the easiest thing in the product to do.
settings=""
if [ -f /home/agent/hooks/settings.json ]; then
    settings="--settings /home/agent/hooks/settings.json"
fi

# Where the model keeps this conversation. The working directory is the same in every sandbox, so the
# transcript's name is the conversation's name, and its presence is how this script tells resuming
# from starting.
transcript="$HOME/.claude/projects/-home-agent-workspace/$conversation.jsonl"

while true; do
    # The system names the conversation and hands the name down, so a conversation opened here is one
    # the system can find afterwards: its history, and what it cost. A name with no transcript behind it
    # is the first open rather than a loss, and starting it under that name is what makes the name
    # true. Resuming a name that is not there would print "No conversation found" and exit.
    if [ -z "$conversation" ]; then
        claude $settings --permission-mode "$mode" || true
    elif [ -f "$transcript" ]; then
        claude $settings --resume "$conversation" --permission-mode "$mode" || true
    else
        claude $settings --session-id "$conversation" --permission-mode "$mode" || true
    fi

    printf '\n  This conversation is closed. Nothing was lost: it is on disk and can be opened again.\n'
    printf '  Press enter to open it, or ctrl-q to leave this running and go back.\n\n'
    # A terminal with nothing reading it is one keystroke from being cleaned up, so it waits here.
    read -r _ || sleep 3600
done
