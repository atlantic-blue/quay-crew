**A declaration keeps a long title, a long sentence and a long label, and says which one is long.**
`krewe job create` refused a title of 201 bytes, and the person lost the words with the job. It
refused the one sentence at the same number, a label key over 63 characters, and a label value over
63 characters. Each number is a guide to what a reader takes in. A guide that refuses is a cap
([#647](https://github.com/atlantic-blue/quay-krewe/issues/647)).

The declaration is made now. The tool prints the job identifier, then one line for each long field.
The line names the field, says how many bytes it is, and says what the guide is. A field inside its
guide gets no line, because a measurement on every job is a measurement nobody reads. The control
plane returns those lines, so the console and the gateway read the same words as the tool.

The four fields come back word for word. `krewe job show` prints the title, the sentence and both
labels at the length they were written.
