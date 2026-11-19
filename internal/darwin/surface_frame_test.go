//go:build darwin && !ios

package darwin

import (
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/internal/pacing"
	"github.com/egoist/mygo/internal/platform"
)

// frameHandler is a window handler that leaves the paced frames it is
// sent to draw later while wait is true.
type frameHandler struct {
	platform.WindowHandler
	wait    bool
	paced   []bool
	onFrame func()
}

func (h *frameHandler) SurfaceEvent(ev platform.SurfaceEvent) bool {
	if ev.Kind != platform.SurfaceFrame {
		return false
	}
	h.paced = append(h.paced, ev.Paced)
	if h.onFrame != nil {
		h.onFrame()
	}
	return ev.Paced && h.wait
}

// TestDeferredFrames checks that the display link keeps running while the
// content leaves a paced frame to draw later, and only then: once FrameNow
// draws it, and once the content draws any other frame instead (as a
// resize's, or a paced one it can draw), which draws what the one left
// would have, as the content then never calls FrameNow for it.
func TestDeferredFrames(t *testing.T) {
	h := &frameHandler{}
	s := &surface{w: &window{h: h}}
	h.wait = true
	s.frameNow(true)
	if !s.deferred {
		t.Fatal("a paced frame left to draw later: not deferred")
	}
	h.wait = false
	s.FrameNow()
	if s.deferred || h.paced[len(h.paced)-1] {
		t.Fatalf("drawn at once: deferred %v, sent paced %v", s.deferred, h.paced[len(h.paced)-1])
	}

	// A frame AppKit displays, not paced, draws the one left.
	h.wait = true
	s.frameNow(true)
	s.frameNow(false)
	if s.deferred {
		t.Fatal("a frame drawn meanwhile left the link running")
	}

	// A paced frame the content draws, as after its renderer was let go.
	s.frameNow(true)
	h.wait = false
	s.frameNow(true)
	if s.deferred {
		t.Fatal("a paced frame drawn left the link running")
	}

	// Refreshes passing while the content waits keep it deferred.
	h.wait = true
	s.frameNow(true)
	s.frameNow(true)
	if !s.deferred {
		t.Fatal("refreshes passing while the content waits: not deferred")
	}
}

// TestDrawSoon checks that a frame asked for after a pause draws from the
// backend's queue, outside AppKit's display of views (#883), once however
// often it is asked for meanwhile, and not at all when another drew
// first, as a resize's does when AppKit displays the view, unless it was
// asked for again after that one.
func TestDrawSoon(t *testing.T) {
	h := &frameHandler{}
	b := &Backend{bySurface: map[id]*surface{}}
	s := &surface{w: &window{b: b, h: h}, view: 1}
	b.bySurface[s.view] = s
	s.drawSoon()
	s.drawSoon()
	if len(h.paced) != 0 {
		t.Fatal("drew at once")
	}
	drainMainQueue()
	if len(h.paced) != 1 || h.paced[0] {
		t.Fatalf("frames drawn from the queue: %v, want one not paced", h.paced)
	}

	s.drawSoon()
	s.frameNow(false)
	drainMainQueue()
	if len(h.paced) != 2 {
		t.Fatalf("drew %d frames, want the one drawn meanwhile alone", len(h.paced)-1)
	}

	// Requests made during the build belong to the new frame.
	h.onFrame = func() {
		h.onFrame = nil
		s.drawSoon()
	}
	s.frameNow(false)
	drainMainQueue()
	if len(h.paced) != 4 {
		t.Fatalf("request during build: drew %d frames, want 4", len(h.paced))
	}

	// One asked for after the frame drawn meanwhile, as once the window
	// whose first frame AppKit drew while the window was out of sight
	// shows (SurfaceShown), still draws: dropped with the first, what
	// moves would stop for good.
	s.drawSoon()
	s.frameNow(false)
	s.drawSoon()
	drainMainQueue()
	if len(h.paced) != 6 || s.soon {
		t.Fatalf("asked for after the frame drawn meanwhile: drew %d frames from the queue, want 1", len(h.paced)-5)
	}

	// A window closed meanwhile draws none.
	s.drawSoon()
	s.w.closed = true
	drainMainQueue()
	if len(h.paced) != 6 || s.soon {
		t.Fatalf("a closed window drew %d frames (soon %v)", len(h.paced)-6, s.soon)
	}
}

func TestDrawableReadyPacing(t *testing.T) {
	uncapped := pacing.Uncapped
	defer func() { pacing.Uncapped = uncapped }()
	for _, tc := range []struct {
		name                     string
		drawn, running, uncapped bool
		wait                     bool
	}{
		{"running", true, true, false, true},
		{"first", false, true, false, false},
		{"fallback", true, false, false, false},
		{"uncapped", true, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pacing.Uncapped = tc.uncapped
			h := &frameHandler{wait: true}
			s := &surface{w: &window{h: h}, drawn: tc.drawn}
			s.frameNow(true)
			h.wait = false
			s.drawableReady(tc.running)
			if tc.wait {
				if len(h.paced) != 1 || !s.due || !s.deferred {
					t.Fatalf("before next tick: frames %v, due %v, deferred %v", h.paced, s.due, s.deferred)
				}
				s.frameNow(true) // the next display-link tick
				if len(h.paced) != 2 || !h.paced[1] || s.due || s.deferred {
					t.Fatalf("next tick: frames %v, due %v, deferred %v", h.paced, s.due, s.deferred)
				}
			} else if len(h.paced) != 2 || h.paced[1] || s.deferred {
				t.Fatalf("immediate resume: frames %v, deferred %v", h.paced, s.deferred)
			}
		})
	}
}

func TestFrameRequestDuringBuild(t *testing.T) {
	h := &frameHandler{}
	s := &surface{w: &window{h: h}, due: true}
	s.frameNow(false)
	if s.due {
		t.Fatal("frame did not coalesce the older due request")
	}
	h.onFrame = func() { s.due = true }
	s.frameNow(false)
	if !s.due {
		t.Fatal("frame consumed a new request made during its build")
	}
}

func TestFrameTimerStartCadence(t *testing.T) {
	load()
	uncapped := pacing.Uncapped
	pacing.Uncapped = false
	defer func() { pacing.Uncapped = uncapped }()
	h := &frameHandler{}
	b := &Backend{bySurface: map[id]*surface{}}
	s := &surface{w: &window{b: b, h: h}, view: 1}
	b.bySurface[s.view] = s
	// Model a build spanning three refreshes without sleeping: the frame
	// starts in the past and ends now. The next request is already due.
	var buildEnd time.Time
	h.onFrame = func() {
		buildEnd = time.Now()
		s.lastFrame = buildEnd.Add(-3 * s.refresh())
	}
	s.frameNow(false)
	if s.lastFrameEnd.Before(buildEnd) || s.lastFrameEnd.Sub(s.lastFrame) < 2*s.refresh() {
		t.Fatal("frame end did not track completion independently of its start")
	}
	s.RequestFrame()
	if !s.soon || s.timing {
		t.Fatalf("slow frame added an end-to-start timer: soon %v, timing %v", s.soon, s.timing)
	}
	drainMainQueue()
}

func TestIdleFrameRequestStartsDisplayLink(t *testing.T) {
	load()
	uncapped := pacing.Uncapped
	pacing.Uncapped = false
	defer func() { pacing.Uncapped = uncapped }()
	const name = "MyGoFrameRequestTestWindow"
	if objc.GetClass(name) == 0 {
		classDef(name, "NSObject", nil, []objc.MethodDef{
			method("isVisible", func(id, objc.SEL) bool { return true }),
			method("occlusionState", func(id, objc.SEL) int { return 2 }),
			method("screen", func(id, objc.SEL) id { return 0 }),
			method("setPaused:", func(id, objc.SEL, bool) {}),
		})
	}
	win := alloc(name)
	defer send(win, "release")
	h := &frameHandler{}
	b := &Backend{bySurface: map[id]*surface{}}
	s := &surface{w: &window{b: b, h: h, win: win}, view: 1, link: win, drawn: true, lastFrameEnd: time.Now().Add(-time.Second)}
	b.bySurface[s.view] = s
	h.onFrame = func() {
		h.onFrame = nil
		s.RequestFrame()
	}
	s.RequestFrame()
	drainMainQueue()
	if len(h.paced) != 1 || h.paced[0] || !s.linkRunning || !s.due || s.soon {
		t.Fatalf("idle input and its next request: frames %v, link %v, due %v, soon %v", h.paced, s.linkRunning, s.due, s.soon)
	}
	s.frameNow(true)
	if len(h.paced) != 2 || !h.paced[1] {
		t.Fatalf("next tick: frames %v, want the requested paced frame", h.paced)
	}
}
