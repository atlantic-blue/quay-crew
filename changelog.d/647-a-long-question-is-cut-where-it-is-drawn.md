**A question a session puts to a person is kept whole, and cut only where it is drawn.** The record
held a question of 4096 bytes. Anything longer was cut, and the end of it was replaced with the words
"cut here: the rest is in the conversation". The last paragraph of a question is usually the decision
itself, so the cut took the choice and left the reasoning for it.

The record now keeps the whole answer, at whatever length the session wrote it. `krewe job show`
prints all of it.

The line an operator reads is still one line. It prints above whatever command the person typed, so a
question that wrapped over four lines would push the output off a short screen. That line now cuts at
80 columns and says which command holds the rest: `796ed880 asks: aurora serverless version two
bills… (krewe job show 796ed880)`. A mark on its own says the text stops. It does not say where the
rest is, and the rest is the reason somebody wrote a question that long.

The reader stops refusing a long question too. `TidyQuestion` in
[internal/job/asking.go](internal/job/asking.go) refused a question over 4096 bytes, so a session
that needed a long decision answered got nothing to the person at all. It keeps the words now.
`QuestionLimit` is the width a surface draws, and the surfaces that draw one line cut there.
