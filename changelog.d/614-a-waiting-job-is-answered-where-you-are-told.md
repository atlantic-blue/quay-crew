**A job that is asking is answered from the console.** The crew rings the terminal bell and draws a
line above the listing when a job stops for a person. Then it asked them to go somewhere else: the
answer could only be typed into `krewe job answer` or into the web briefing, which is the one surface
an operator watching the console does not have open.

`a` answers the job under the cursor. It opens a line, takes the answer in words, and the listing
refreshes underneath, so the row that was asking reads `pending` again without anybody pressing
anything else.

A row that is not asking says so rather than opening the line. An answer nothing is waiting for is
worse than a key that did nothing, because the person writes it first. An empty line is refused for
the same reason: it would start the job again with nothing to go on, and leave somebody believing
they had answered it.
