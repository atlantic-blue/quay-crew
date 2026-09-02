# browser: how you look at what you built

A green build says the code compiles and the tests agree with themselves. It says nothing about what
the page looks like. A layout defect on every section of a home page passes the build, the linter,
the type check and the whole suite, and the first person to see it is the operator.

So a change with a visual result is not finished until you have drawn the page and looked at the
picture. Not the markup you served, not the test that asked the interface a question: the picture.

## Draw it

    krewe render http://localhost:3000
    krewe render localhost:3000 home.png 390x844 dark 2s

The url comes first. Everything after it is recognised by its shape rather than its position, so any
order is the same command: a file name, a size as `390x844`, `light` or `dark`, and a wait as `2s`.
Say nothing and you get `render.png`, 1280 by 900, light, after half a second.

It draws the whole page rather than the first screen of it, and it says what it drew:

    drew http://localhost:3000 at 390x844, dark, into /home/agent/workspace/home.png (390 by 3120)

A page a session is serving itself is the usual subject, so start the server first and render the
address it prints. `file:///path/page.html` works for a page with no server behind it.

## Record it, where a still frame cannot carry it

Some value is in what happens between two frames: a list that refreshes, a wizard that comes back to
where it started, a key that is swallowed. Capture the screen several times while it runs, then join
the captures:

    tmux capture-pane -t work -e -p > frame-01.txt
    krewe record run.webm frame-*.txt 900x400 2

The file comes first because the captures are a list. Everything after them is read by its shape:
a size, `light` or `dark`, and a number as the frames a second. It says what it recorded:

    recorded 12 captures at 900x400, dark, 6.0 seconds, into /home/agent/workspace/run.webm

It writes webm and nothing else, because the encoder here is the one the headless browser brings
with it. Where a machine has no encoder, `krewe record` says it cannot record: then record on your
own machine and attach the file, or say what you would have shown in steps a person can run.

## Look at it

Read the file. That is the whole point, and it is the step that gets skipped: the command exiting
well proves a file exists, and a file existing proves nothing about the page. Read the picture, and
say what you saw in it.

Draw it again at a phone width, and again in the other colour scheme, whenever the change could
land differently there. One picture at one size says nothing about the other two.

## Label it

A picture is either something you observed or something you generated to illustrate. The two look
identical on the page and are worth completely different amounts, so say which it is in the same
breath, and say what it takes to reproduce it: the command, the address, and anything that had to be
running.

Never present a rendered sample as observed output.

## When it says there is no browser

The browser is in the sandbox image, and a sandbox keeps what it was made with. A session made
before the image carried one cannot install it. Stop the session and dispatch again to get a fresh
sandbox.
