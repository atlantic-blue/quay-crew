**The console stopped saying a scenario was proved when nobody had checked.** The features view
listed every scenario in the build under two columns, feature and "proved by". Nothing in the build
knew whether any of those scenarios had passed on it. So the second column named a scenario and left
the reader to take the heading at its word. The view also asked the control plane nothing, so it said
the same thing whichever crew was on screen. It printed what `quay features` already prints.

There is one list now, and it is the command. The console's command bar runs commands, so typing
`features` there still prints it. The list arrives in the output panel, from the tool. The short
spellings the view answered to, `f`, `feature` and `capabilities`, have no command behind them. Each
one says what to type instead, while the word is being typed and again on enter. Without that they
would reach the tool and come back as `unknown command "f"`, which reads as the console being broken.

The other way to answer this was to make "proved by" true, by embedding the result of the run that
proved each scenario. That is a build step and a report format. The view would still have said
nothing about the crew the operator was looking at.
