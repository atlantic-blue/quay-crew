**The console opens where it was last left.** Four levels deep, opening at the top every time makes a
person walk back down to where they were a minute ago. It writes the level down whenever the level
changes, the way the panel already writes down which view it is on, and reads it on the way up. The
top is what it opens on when it has nothing remembered.

It is kept in `console-place` under the system's own directory, beside `panel-view`.

What was remembered can go. A place naming a project somebody removed opens on the deepest level that
is still there, checked one level at a time on the way down, rather than on a listing that promises
rows and has none. A place this build cannot read, which is what a file from an older build looks
like, opens at the top. A console that can neither read nor write one still opens and still works:
losing where somebody was costs a keystroke, and refusing to open costs them the tool.
