# linear: how tickets are read and updated here

Linear speaks GraphQL at a single endpoint. The key is in the environment as LINEAR_API_KEY; a
personal key goes bare in the Authorization header, with no Bearer prefix. Never print the key, put
it in a message, or pass it anywhere but the header.

    curl -sS https://api.linear.app/graphql \
      -H "Content-Type: application/json" \
      -H "Authorization: $LINEAR_API_KEY" \
      -d '{"query": "<the query>"}'

## Reading is always fine

The issues assigned to the operator:

    { viewer { assignedIssues(first: 20) { nodes { identifier title state { name } url } } } }

One issue by its identifier, with its description and latest comments:

    { issue(id: "ABC-123") { identifier title description state { name }
        comments(last: 10) { nodes { body createdAt } } } }

An identifier like ABC-123 works wherever an id is asked for.

## Writing happens when the conversation asks for it

Comment on an issue:

    mutation { commentCreate(input: {issueId: "<issue id>", body: "<text>"}) { success } }

Move an issue to another state. States are per team, so read them first rather than guessing:

    { issue(id: "ABC-123") { team { states { nodes { id name } } } } }
    mutation { issueUpdate(id: "<issue id>", input: {stateId: "<state id>"}) { success } }

Never create, close or reassign an issue unless the operator asked for exactly that in this
conversation, and say what you changed afterwards, with the issue's address.

## When it fails

A 401 means the workspace needs the key: say it is set with
`quay secret set <workspace> LINEAR_API_KEY <value>` rather than working around it. GraphQL errors
come back in the response body with the request still returning 200, so read the errors field, not
only the status.
