package detect

import (
	"log/slog"
	"sync"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/detect/assets"
	"github.com/HuskerMinion/techo5/echod/internal/feature/phone"
	"github.com/HuskerMinion/techo5/echod/internal/feature/talkback"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
	"github.com/HuskerMinion/techo5/echod/internal/lib/wake"
)

// HangupSlot is where the hang-up word listens, next to the stop word's and as far from Home Assistant's.
const HangupSlot = StopSlot + 1

// hangupNone is the hang-up word's select switched off.
const hangupNone = "None"

// hangUp is the hang-up word, or the stop word standing in for it, heard: it hangs up if hangupHeard
// says so. what names which, for the log.
func hangUp(what string) {
	onCall, farEnd := phone.Get().Busy(), phone.Get().FarEndTalking()
	if !hangupHeard(onCall, talkback.Get().Busy(), farEnd) {
		slog.Info(what+" ignored", "on_call", onCall, "far_end_talking", farEnd)
		return
	}
	slog.Info(what + ", hanging up")
	phone.Get().Hangup()
}

// hangupModel describes the device's own hang-up word to the engine, from the manifest it was trained
// with, as stopModel does the stop word.
func hangupModel() (wake.Model, error) {
	path, err := assets.Hangup(layout.StateDir)
	if err != nil {
		return wake.Model{}, err
	}
	m := wake.Model{
		ID:     assets.HangupID,
		Phrase: assets.HangupPhrase,
		Kind:   wake.KindMicroWakeWord,
		Path:   path,
	}
	m.Config.ModelPath = path
	m.Config.SlidingWindowSize = assets.HangupWindowSize
	m.Config.FeaturesStepMs = assets.HangupFeatureStep
	return m, nil
}

// hangupHeard reports whether the hang-up word, heard now, hangs up. Only with a call ringing, being
// placed or up (the word is only loaded then, but a detection can land just after the call ends), not
// over a talk through a camera, and not while the far end was talking: their voice comes out of the
// speaker, and the word in it is not somebody in the room hanging up.
func hangupHeard(onCall, talkBack, farEnd bool) bool {
	return onCall && !talkBack && !farEnd
}

// hangup is the hang-up word's select and threshold, and loading its model while a call needs it.
type hangup struct {
	d         *Detect
	word      *esphome.Select
	threshold *esphome.Number

	// models is what the device has, the same library the wake words come from, and builtin is the
	// device's own "Hang up". Tests stand in for both.
	models  func() []wake.Model
	builtin func() (wake.Model, error)

	// mu keeps one load or clear at a time.
	mu sync.Mutex
}

func newHangup(d *Detect) *hangup {
	h := &hangup{d: d, models: wake.Lib().Ours, builtin: hangupModel}
	h.word = &esphome.Select{
		Base: esphome.Base{
			ObjectID: "hangup_word",
			DeviceID: component.DeviceMicrophone,
			Name:     "Hang-up word",
			Icon:     "mdi:phone-hangup",
			Category: esphome.CategoryConfig,
		},
		Options: h.options(),
	}
	h.word.OnCommand = func(v string) {
		id, ok := h.idFor(v)
		if !ok {
			return
		}
		if err := config.Set().Hangup().ID(id); err != nil {
			slog.Error("saving the hang-up word failed", "err", err)
			return
		}
		h.word.Set(v)
		slog.Info("setting changed", "setting", h.word.ObjectID, "using", v)
		h.follow(phone.Get().Busy())
	}
	h.threshold = &esphome.Number{
		Base: esphome.Base{
			ObjectID: "hangup_word_threshold",
			DeviceID: component.DeviceMicrophone,
			Name:     "Hang-up word threshold",
			Icon:     "mdi:tune-vertical",
			Category: esphome.CategoryConfig,
		},
		Min: 0.5, Max: 0.99, Step: 0.01,
		Mode: esphome.NumberBox,
	}
	h.threshold.OnCommand = func(v float32) {
		if err := config.Set().Hangup().Threshold(float64(v)); err != nil {
			slog.Error("saving the hang-up word threshold failed", "err", err)
			return
		}
		h.threshold.Set(v)
	}
	// Loaded as a call starts and cleared as it ends. Changed's listeners must not block, and a load
	// reads a model off disk.
	phone.Get().Changed.Listen(func(s phone.State) {
		onCall := s.Phase != phone.Idle
		go h.follow(onCall)
	})
	return h
}

// options are None, the device's own word, the stop word, and the models the device has, by phrase. One
// of the user's with the same phrase as either of the device's own is left out, as it could not be told
// apart in the list.
func (h *hangup) options() []string {
	out := []string{hangupNone, assets.HangupPhrase, assets.StopPhrase}
	for _, m := range h.models() {
		if m.Phrase != assets.HangupPhrase && m.Phrase != assets.StopPhrase {
			out = append(out, m.Phrase)
		}
	}
	return out
}

// idFor is what a select option is saved as, and whether it names anything: config.HangupNone for None,
// empty for the device's own word, config.HangupStop for the stop word, a model's id for one of the
// library's.
func (h *hangup) idFor(phrase string) (string, bool) {
	switch phrase {
	case hangupNone:
		return config.HangupNone, true
	case assets.HangupPhrase:
		return "", true
	case assets.StopPhrase:
		return config.HangupStop, true
	}
	for _, m := range h.models() {
		if m.Phrase == phrase {
			return m.ID, true
		}
	}
	return "", false
}

// phraseFor is the select option for what was saved. A model the device no longer has shows as None,
// which is what the device then does.
func (h *hangup) phraseFor(id string) string {
	switch id {
	case "":
		return assets.HangupPhrase
	case config.HangupNone:
		return hangupNone
	case config.HangupStop:
		return assets.StopPhrase
	}
	if m, ok := wake.Find(h.models(), id); ok {
		return m.Phrase
	}
	return hangupNone
}

func (h *hangup) restore(c config.Hangup) {
	h.word.SetOptions(h.options())
	h.word.Set(h.phraseFor(c.ID))
	h.threshold.Set(float32(c.ThresholdAt()))
	slog.Info("restored", "what", h.word.ObjectID, "using", h.phraseFor(c.ID))
}

// wanted is the model the slot should hold: none off a call, with None chosen, or with the stop word
// chosen (it has a slot of its own, loaded already); the device's own word for the default, or the
// library's model.
func (h *hangup) wanted(onCall bool) (wake.Model, bool) {
	id := config.Get().Wake.Hangup.ID
	switch {
	case !onCall || id == config.HangupNone || id == config.HangupStop:
		return wake.Model{}, false
	case id == "":
		m, err := h.builtin()
		if err != nil {
			slog.Error("the hang-up word is unavailable", "err", err)
			return wake.Model{}, false
		}
		return m, true
	}
	m, ok := wake.Find(h.models(), id)
	if !ok {
		slog.Warn("the hang-up word is not on the device", "id", id)
	}
	return m, ok
}

// follow puts the chosen model in the slot while a call is on, and takes it out otherwise.
func (h *hangup) follow(onCall bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	want, ok := h.wanted(onCall)
	// What the engine has, not what was last put there: a reload of the wake words empties every slot.
	have, loaded := h.d.engine.Loaded(HangupSlot)
	switch {
	case !ok && !loaded:
		return
	case !ok:
		h.d.engine.Clear(HangupSlot)
		slog.Info("hang-up word off")
		return
	case loaded && have.ID == want.ID:
		return
	}
	if err := h.d.engine.Use(HangupSlot, want); err != nil {
		slog.Error("loading the hang-up word failed", "id", want.ID, "err", err)
		h.d.engine.Clear(HangupSlot)
		return
	}
	slog.Info("hang-up word listening", "id", want.ID, "threshold", config.Get().Wake.Hangup.ThresholdAt())
}
