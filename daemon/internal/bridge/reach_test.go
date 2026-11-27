package bridge

import (
	"errors"
	"testing"
)

func TestReachabilityLogsOnlyTransitions(t *testing.T) {
	var r reachability
	steps := []struct {
		err     error
		changed bool
	}{
		{nil, false},                  // Start ok: kein Log
		{nil, false},                  // weiter ok
		{errors.New("timeout"), true}, // weg
		{errors.New("timeout"), false},
		{nil, true}, // wieder da
		{nil, false},
	}
	for i, s := range steps {
		if _, changed := r.update(s.err); changed != s.changed {
			t.Errorf("Schritt %d: changed=%v, erwartet %v", i, changed, s.changed)
		}
	}
}

func TestReachabilityFirstCycleError(t *testing.T) {
	var r reachability
	if msg, changed := r.update(errors.New("refused")); !changed || msg == "" {
		t.Fatal("Fehler im ersten Zyklus nicht geloggt")
	}
}
