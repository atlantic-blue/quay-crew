**A stopped row says why it stopped, on the row of `krewe job list`.** The listing said "stopped"
and nothing more. A person who wanted to know which job needed them opened one job after another
with `krewe job show`. The reason was on the record and on the wire the whole time.

The listing draws the reason in a column beside the outcome. It draws it for a job that stopped and
for a job that failed. The column arrives with the first row that carries a reason. A listing where
nothing stopped prints the row it always printed.

A reason is free text. It can run to a paragraph. It can hold a line break. The column is 40
characters wide. The row cuts a longer reason to 39 characters. It then draws the one character that
says the text goes on.

The claim column beside it cuts with that same character. A reason with a line break in it reads as
one line. A line break would otherwise make a second row, and that row has no identifier and no
title. The record keeps the whole reason. `krewe job show` prints all of it.

A pending job the machine holds back carries a reason too. That one stays off the row. It is one
fact about the machine, and the listing says it once underneath.
