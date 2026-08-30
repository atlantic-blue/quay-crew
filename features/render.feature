Feature: A session can see what it built

  A session used to deliver a visual change on the strength of a passing build, because it had no
  eyes. The build, the linter, the type check and the tests all pass on a layout that is wrong, and
  the operator is the first person to look at it. On juliantellez.com that cost a spacing defect on
  every section of the home page, found by the first screenshot anybody took.

  So the sandbox image carries a browser, and `quay render` draws a url into a picture the session
  then reads. The command talks to nothing, because the page a session wants to see is one it is
  serving inside its own sandbox.

  What it draws is the whole page rather than the first screen of it, at a viewport that is stated
  rather than assumed, after a wait, so a page that draws itself in script is not caught blank. Then
  it reads the picture back and says what it is of, which is the label a screenshot has to carry
  before it can be shown to anybody.

  These scenarios use a browser double, so they say what the system asks a browser for and not that a
  browser honours it. The real thing is proved against the image in
  TestASessionRendersAPageAndReadsItBack, which draws a page in a fresh container and reads the
  picture back.

  Scenario: A session draws the page it is serving
    When the session renders "localhost:3000"
    Then the browser is asked for the whole page
    And the browser is asked for "http://localhost:3000" at 1280 by 900 in light
    And the session is told where the picture is and how big it is

  # A defect that only shows on a phone, or only in the dark, is one a picture at one size in one
  # scheme will never carry.
  Scenario: A session says the viewport and the colour scheme it wants
    When the session renders "localhost:3000 home.png 390x844 dark"
    Then the browser is asked for "http://localhost:3000" at 390 by 844 in dark

  # The failure that makes the whole capability worthless. A session sees a command that worked,
  # reports the page as looked at, and has looked at nothing.
  Scenario: A browser that draws nothing is not reported as a picture
    Given a browser that exits well and writes no file
    When the session renders "localhost:3000"
    Then the session is told the browser wrote nothing

  # A sandbox is made once and keeps what it was made with, so a session started before the image
  # carried a browser has no browser and cannot install one.
  Scenario: A session whose sandbox has no browser is told what to do about it
    Given a sandbox with no browser in it
    When the session renders "localhost:3000"
    Then the session is told to get a fresh sandbox
