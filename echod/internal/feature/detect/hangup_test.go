package detect

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/detect/assets"
	"github.com/HuskerMinion/techo5/echod/internal/lib/wake"
)

// The hang-up word hangs up only a call, never over a talk through a camera, and not while the far end
// was talking.
func TestHangupHeard(t *testing.T) {
	for _, c := range []struct {
		name                     string
		onCall, talkBack, farEnd bool
		want                     bool
	}{
		{"a call", true, false, false, true},
		{"no call", false, false, false, false},
		{"the far end talking", true, false, true, false},
		{"a talk through a camera", true, true, false, false},
	} {
		if got := hangupHeard(c.onCall, c.talkBack, c.farEnd); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// The chosen model is in its slot only while a call is on: the device's own word by default, None
// leaving it empty, and one of the library's in its place. A slot a reload of the wake words emptied is
// filled again rather than taken as still loaded, and a model the device no longer has loads nothing.
func TestHangupFollowsTheCall(t *testing.T) {
	fakes(t)
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	e := New(HangupSlot+1, nil)
	h := &hangup{
		d:       &Detect{engine: e},
		models:  func() []wake.Model { return []wake.Model{model("computer", wake.KindMicroWakeWord)} },
		builtin: func() (wake.Model, error) { return model(assets.HangupID, wake.KindMicroWakeWord), nil },
	}
	loaded := func() string {
		m, ok := e.Loaded(HangupSlot)
		if !ok {
			return ""
		}
		return m.ID
	}
	choose := func(id string) {
		t.Helper()
		if err := config.Set().Hangup().ID(id); err != nil {
			t.Fatal(err)
		}
	}

	h.follow(false)
	if got := loaded(); got != "" {
		t.Fatalf("no call, loaded %q", got)
	}
	h.follow(true)
	if got := loaded(); got != assets.HangupID {
		t.Fatalf("by default, loaded %q, want the device's own", got)
	}

	choose(config.HangupNone)
	h.follow(true)
	if got := loaded(); got != "" {
		t.Fatalf("None chosen, loaded %q", got)
	}

	// The stop word has its own slot, already loaded: nothing more goes in for it.
	choose(config.HangupStop)
	h.follow(true)
	if got := loaded(); got != "" {
		t.Fatalf("the stop word chosen, loaded %q", got)
	}

	choose("computer")
	h.follow(true)
	if got := loaded(); got != "computer" {
		t.Fatalf("on a call, loaded %q, want computer", got)
	}
	e.Clear(HangupSlot) // as a reload of the wake words does
	h.follow(true)
	if got := loaded(); got != "computer" {
		t.Fatalf("after the slot was emptied, loaded %q, want computer", got)
	}
	h.follow(false)
	if got := loaded(); got != "" {
		t.Fatalf("call over, still loaded %q", got)
	}

	choose("gone")
	h.follow(true)
	if got := loaded(); got != "" {
		t.Fatalf("a model the device does not have, loaded %q", got)
	}
}

// The select offers None and the device's own two words first, saves them as what config reads, and
// shows a model the device no longer has as None. One of the user's called "Hang up" or "Stop" is not
// offered twice.
func TestHangupOptions(t *testing.T) {
	h := &hangup{models: func() []wake.Model {
		return []wake.Model{model("computer", wake.KindMicroWakeWord),
			{ID: "my_hang_up", Phrase: assets.HangupPhrase, Kind: wake.KindMicroWakeWord},
			{ID: "my_stop", Phrase: assets.StopPhrase, Kind: wake.KindMicroWakeWord}}
	}}
	want := []string{hangupNone, assets.HangupPhrase, assets.StopPhrase, "computer"}
	if got := h.options(); !slices.Equal(got, want) {
		t.Errorf("options %v, want %v", got, want)
	}
	for _, c := range []struct{ phrase, id string }{
		{hangupNone, config.HangupNone}, {assets.HangupPhrase, ""}, {assets.StopPhrase, config.HangupStop},
		{"computer", "computer"},
	} {
		if id, ok := h.idFor(c.phrase); !ok || id != c.id {
			t.Errorf("idFor(%q) = %q, %v; want %q", c.phrase, id, ok, c.id)
		}
		if got := h.phraseFor(c.id); got != c.phrase {
			t.Errorf("phraseFor(%q) = %q, want %q", c.id, got, c.phrase)
		}
	}
	if _, ok := h.idFor("nothing like it"); ok {
		t.Error("an unknown option was taken")
	}
	if got := h.phraseFor("gone"); got != hangupNone {
		t.Errorf("a model the device no longer has shows as %q", got)
	}
}

// Its threshold is a wake word's unless one is set.
func TestHangupThreshold(t *testing.T) {
	if got := (config.Hangup{}).ThresholdAt(); got != config.DefaultThreshold {
		t.Errorf("unset: %v, want %v", got, config.DefaultThreshold)
	}
	if got := (config.Hangup{Threshold: 0.93}).ThresholdAt(); got != 0.93 {
		t.Errorf("set: %v", got)
	}
}
