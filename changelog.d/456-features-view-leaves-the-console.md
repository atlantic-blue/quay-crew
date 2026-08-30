**The console stopped claiming a scenario passed when nobody had checked.** The features view listed
every scenario in the build under two columns, feature and "proved by". Nothing in the build knew
whether any of those scenarios had passed on it, so the second column named a scenario and left the
reader to take the heading at its word. It also asked the control plane nothing, which meant it said
the same thing whichever crew was on screen, and it printed what `quay features` already prints.

There is one list now and it is the command. The console's command bar runs commands, so typing
`features` there still prints it, into the output panel, from the tool. The short spellings the view
answered to, `f`, `feature` and `capabilities`, have no command behind them: each one says what to
type instead, while the word is being typed and again on enter, rather than reaching the tool and
coming back as `unknown command "f"`.

The other way to answer this was to make "proved by" true, by embedding the result of the run that
proved each scenario. That is a build step and a report format, and the view would still have said
nothing about the crew the operator was looking at.
