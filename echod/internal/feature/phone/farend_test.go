package phone

import (
	"testing"
	"time"
)

// The far end counts as talking for farEndHold after its last loud frame, and not before one.
func TestFarEndTalking(t *testing.T) {
	defer farEndAt.Store(0)
	now := time.Now()

	farEndAt.Store(0)
	if farEndTalking(now) {
		t.Error("talking before anything was heard")
	}
	farEndAt.Store(now.UnixNano())
	if !farEndTalking(now.Add(farEndHold - time.Millisecond)) {
		t.Error("not talking inside the hold")
	}
	if farEndTalking(now.Add(farEndHold)) {
		t.Error("still talking after the hold")
	}
}
