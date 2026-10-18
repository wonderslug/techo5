// Package assets carries the wake words that are the device's own rather than the user's.
package assets

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

// The stop word, from kahrendt/microWakeWord. Embedded because it is not a choice: nothing selects it,
// nothing installs it, and a device without it would have no way to be interrupted.
//
// The hang-up word, trained for TECHO5 at microwakeword.com. Embedded so that hanging up a call by voice
// works out of the box, with no model to find and install first; another model can still be chosen in
// its place.
//
//go:embed stop.tflite hang_up.tflite
var files embed.FS

// How the model is windowed, from the stop.json of the release it came from.
const (
	StopPhrase      = "Stop"
	StopWindowSize  = 5
	StopFeatureStep = 10
)

// How the hang-up model is windowed, from the hang_up.json it was trained with. Its ID is the device's
// own, so a model of the user's that happens to share the file name is never taken for it.
const (
	HangupID          = "techo5_hang_up"
	HangupPhrase      = "Hang up"
	HangupWindowSize  = 5
	HangupFeatureStep = 10
)

// Stop writes the model into dir if it is not already there, and returns the path.
//
// The interpreter takes a path rather than bytes, so the model has to exist as a file. dir is somewhere
// nothing scans for wake words: found by the picker it would be offered as one, and it is not — it has
// no pipeline and its detection means something else entirely.
func Stop(dir string) (string, error) { return write(dir, "stop.tflite", "stop") }

// Hangup writes the hang-up model into dir as Stop does, and for the same reason somewhere nothing
// scans for wake words.
func Hangup(dir string) (string, error) { return write(dir, "hang_up.tflite", "hang-up") }

// write puts the embedded file name into dir unless it is already there, and returns the path.
func write(dir, name, what string) (string, error) {
	want, err := files.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("assets: reading the embedded %s model: %w", what, err)
	}

	path := filepath.Join(dir, name)
	if same(path, want) {
		return path, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("assets: %w", err)
	}
	if err := os.WriteFile(path, want, 0o644); err != nil {
		return "", fmt.Errorf("assets: writing the %s model: %w", what, err)
	}
	return path, nil
}

// same reports whether the file already holds exactly these bytes, so an unchanged model is not
// rewritten on every start and a changed one is not left stale.
func same(path string, want []byte) bool {
	got, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return sha256.Sum256(got) == sha256.Sum256(want)
}
