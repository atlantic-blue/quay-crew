# jira: how tickets are read and moved here

Jira Cloud speaks REST under the instance's own address, which is workspace configuration rather
than part of this skill: read it from the workspace context, and if nothing there names it, ask
rather than guessing. Below it is written as JIRA_BASE_URL, an address like
https://example.atlassian.net.

Authentication is basic, as the pair in the environment. Never print either value or pass them
anywhere but curl's own credential flag:

    curl -sS -u "$JIRA_EMAIL:$JIRA_API_TOKEN" -H "Accept: application/json" \
      "$JIRA_BASE_URL/rest/api/3/issue/ABC-123"

## Reading is always fine

An issue by key gives its summary, description, status and comments. The issues assigned to the
operator:

    curl -sS -u "$JIRA_EMAIL:$JIRA_API_TOKEN" -H "Accept: application/json" \
      --get --data-urlencode "jql=assignee=currentUser() AND resolution=Unresolved" \
      "$JIRA_BASE_URL/rest/api/3/search"

## Writing happens when the conversation asks for it

Comment on an issue (the body is Atlassian document format, one paragraph is enough):

    curl -sS -u "$JIRA_EMAIL:$JIRA_API_TOKEN" -H "Content-Type: application/json" \
      -d '{"body": {"type": "doc", "version": 1, "content": [{"type": "paragraph", "content": [{"type": "text", "text": "<text>"}]}]}}' \
      "$JIRA_BASE_URL/rest/api/3/issue/ABC-123/comment"

Transition an issue. Transitions are per workflow, so read what this issue offers first rather than
guessing an id:

    curl -sS -u "$JIRA_EMAIL:$JIRA_API_TOKEN" -H "Accept: application/json" \
      "$JIRA_BASE_URL/rest/api/3/issue/ABC-123/transitions"
    curl -sS -u "$JIRA_EMAIL:$JIRA_API_TOKEN" -H "Content-Type: application/json" \
      -d '{"transition": {"id": "<transition id>"}}' \
      "$JIRA_BASE_URL/rest/api/3/issue/ABC-123/transitions"

Never create, resolve or reassign an issue unless the operator asked for exactly that in this
conversation, and say what you changed afterwards, with the issue's address.

## When it fails

A 401 means the workspace needs the pair set with `krewe secret set <workspace> JIRA_EMAIL <value>`
and `krewe secret set <workspace> JIRA_API_TOKEN <value>`; say so rather than working around it. A
404 on a key you were given usually means the wrong instance address: check the workspace context
before concluding the issue does not exist.
