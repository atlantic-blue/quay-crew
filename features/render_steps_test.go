package features_test

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/krewe/internal/browser"
	"github.com/cucumber/godog"
)

// renderWorld is what the browser was asked to draw and what the session was told, kept beside the
// shared world so the rendering scenarios do not widen what every other scenario carries.
type renderWorld struct {
	drawing browser.Drawing
	asked   bool
	said    strings.Builder
	err     error

	// draw stands in for the browser. It is replaced by a scenario that wants a browser which fails
	// in a particular way.
	draw func(browser.Drawing) error
	// where the pictures go, so a scenario never writes into the checkout.
	dir string
}

type renderKey struct{}

func renderFrom(ctx context.Context) *renderWorld {
	r, _ := ctx.Value(renderKey{}).(*renderWorld)
	return r
}

// Draw records what was asked for and then does whatever this scenario's browser does.
func (r *renderWorld) Draw(drawing browser.Drawing) error {
	r.drawing, r.asked = drawing, true
	return r.draw(drawing)
}

// aPicture is what a browser that works leaves behind: a file that is a picture, of a stated size.
func aPicture(drawing browser.Drawing) error {
	drawn := image.NewRGBA(image.Rect(0, 0, drawing.Width, 1500))
	drawn.Set(1, 1, color.RGBA{R: 255, A: 255})
	handle, err := os.Create(drawing.File)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	return png.Encode(handle, drawn)
}

func initializeRenderSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		dir, err := os.MkdirTemp("", "quaycrew-render")
		if err != nil {
			return ctx, err
		}
		return context.WithValue(ctx, renderKey{}, &renderWorld{draw: aPicture, dir: dir}), nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		if world := renderFrom(ctx); world != nil && world.dir != "" {
			_ = os.RemoveAll(world.dir)
		}
		return ctx, err
	})

	sc.Step(`^a browser that exits well and writes no file$`, func(ctx context.Context) error {
		renderFrom(ctx).draw = func(browser.Drawing) error { return nil }
		return nil
	})

	sc.Step(`^a sandbox with no browser in it$`, func(ctx context.Context) error {
		// The real refusal, from the real thing, against a program this machine does not have.
		absent := browser.Program{Name: "a-browser-this-sandbox-does-not-have"}
		renderFrom(ctx).draw = absent.Draw
		return nil
	})

	sc.Step(`^the session renders "([^"]*)"$`, func(ctx context.Context, typed string) error {
		world := renderFrom(ctx)
		drawing, err := browser.From(strings.Fields(typed))
		if err != nil {
			world.err = err
			return nil
		}
		// The file a session did not name is relative to where it is standing, and where these
		// scenarios stand is the checkout. The scenario's own directory instead.
		drawing.File = filepath.Join(world.dir, filepath.Base(drawing.File))
		world.err = browser.Render(world, drawing, &world.said)
		return nil
	})

	sc.Step(`^the browser is asked for the whole page$`, func(ctx context.Context) error {
		world := renderFrom(ctx)
		if !world.asked {
			return fmt.Errorf("the browser was never asked for anything")
		}
		for _, arg := range (browser.Program{}).Command(world.drawing).Args {
			if arg == "--full-page" {
				return nil
			}
		}
		return fmt.Errorf("the browser was asked for the viewport, so nothing below the fold is in the picture")
	})

	sc.Step(`^the browser is asked for "([^"]*)" at (\d+) by (\d+) in (\w+)$`,
		func(ctx context.Context, url string, width, height int, scheme string) error {
			drawing := renderFrom(ctx).drawing
			if drawing.URL != url {
				return fmt.Errorf("the browser was asked for %q, not %q", drawing.URL, url)
			}
			if drawing.Width != width || drawing.Height != height {
				return fmt.Errorf("the browser was asked for %d by %d", drawing.Width, drawing.Height)
			}
			if drawing.Scheme != scheme {
				return fmt.Errorf("the browser was asked for %q", drawing.Scheme)
			}
			return nil
		})

	sc.Step(`^the session is told where the picture is and how big it is$`, func(ctx context.Context) error {
		world := renderFrom(ctx)
		if world.err != nil {
			return world.err
		}
		said := world.said.String()
		for _, want := range []string{world.drawing.File, "1280 by 1500"} {
			if !strings.Contains(said, want) {
				return fmt.Errorf("the session was told %q, which does not say %q", strings.TrimSpace(said), want)
			}
		}
		return nil
	})

	sc.Step(`^the session is told the browser wrote nothing$`, func(ctx context.Context) error {
		return refusalSays(renderFrom(ctx), "wrote nothing")
	})

	sc.Step(`^the session is told to get a fresh sandbox$`, func(ctx context.Context) error {
		return refusalSays(renderFrom(ctx), "dispatch again")
	})
}

func refusalSays(world *renderWorld, want string) error {
	if world.err == nil {
		return fmt.Errorf("the session was told %q, and nothing failed", strings.TrimSpace(world.said.String()))
	}
	if !strings.Contains(world.err.Error(), want) {
		return fmt.Errorf("the failure does not say %q: %v", want, world.err)
	}
	return nil
}
