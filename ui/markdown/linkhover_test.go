package markdown

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestLinkHover gives the app each link as the view builds it: a card
// anchored on it with PopoverBase shows while the pointer rests on the
// link, stays as the pointer moves onto it, and takes its clicks, and the
// link shows no Preview tip. Links LinkHover declines keep Preview's.
func TestLinkHover(t *testing.T) {
	r := &resolver{known: []string{"octocat"}}
	open, follows := false, 0
	o := &Options{
		Resolve: r.resolve,
		Preview: func(c *ui.Context, dest string) func(tip ui.Element) {
			return func(tip ui.Element) { tip.Children(func() { ui.Text(c, "Tip of "+dest) }) }
		},
		LinkHover: func(c *ui.Context, dest string, link ui.Element) bool {
			if dest != "/users/octocat" {
				return false
			}
			if link.Hovered() {
				open = true
			}
			ui.PopoverBase(c, link, &open, func(panel ui.Element) {
				panel.Padding(8).Children(func() {
					if ui.Button(c, "Follow").Clicked() {
						follows++
					}
				})
			})
			return true
		},
	}
	tt := ui.NewTester(func(c *ui.Context) { View(c, "Hi @octocat, see [docs](https://go.dev).", o) }, 600, 400)
	rest := func() {
		for range 10 {
			tt.Advance(100 * time.Millisecond)
			tt.Frame()
		}
	}
	at, ok := tt.Find("@octocat")
	if !ok {
		t.Fatalf("no @octocat in %q", tt.Texts())
	}
	tt.Move(at.X+at.W/2, at.Y+at.H/2)
	rest()
	if !tt.HasText("Follow") {
		t.Fatalf("no card while the pointer rests on the link: %q", tt.Texts())
	}
	if tt.HasText("Tip of /users/octocat") {
		t.Errorf("a link LinkHover takes shows Preview's tip too: %q", tt.Texts())
	}
	btn, ok := tt.Find("Follow")
	if !ok {
		t.Fatalf("no Follow in %q", tt.Texts())
	}
	// Down from the link onto the card, a step a frame.
	for k := 1; k <= 4; k++ {
		f := float32(k) / 4
		tt.Move(at.X+at.W/2+(btn.X+btn.W/2-at.X-at.W/2)*f, at.Y+at.H/2+(btn.Y+btn.H/2-at.Y-at.H/2)*f)
		tt.Advance(50 * time.Millisecond)
		tt.Frame()
	}
	if err := tt.Click("Follow"); err != nil {
		t.Fatalf("click Follow on the card: %v in %q", err, tt.Texts())
	}
	tt.Frame()
	if follows != 1 {
		t.Errorf("Follow clicked %d times, want 1", follows)
	}
	docs, ok := tt.Find("docs")
	if !ok {
		t.Fatalf("no docs in %q", tt.Texts())
	}
	tt.Move(docs.X+docs.W/2, docs.Y+docs.H/2)
	rest()
	if !tt.HasText("Tip of https://go.dev") {
		t.Errorf("a link LinkHover declines shows no Preview tip: %q", tt.Texts())
	}
}
