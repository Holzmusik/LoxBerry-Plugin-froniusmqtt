package bridge

import (
	"errors"
	"testing"
	"time"

	"froniusmqtt/internal/fronius"
)

type fakeSoc struct {
	min    int
	mode   string
	writes []int
	err    error
}

func (f *fakeSoc) ReadBatteryConfig() (fronius.SocSettings, []byte, error) {
	m := f.min
	return fronius.SocSettings{Min: &m, Mode: f.mode}, []byte("{}"), nil
}

func (f *fakeSoc) SetSocMin(v int) error {
	f.writes = append(f.writes, v)
	if f.err != nil {
		return f.err
	}
	f.min, f.mode = v, "manual"
	return nil
}

func newTestLimiter(dev *fakeSoc) (*socLimiter, map[string]string, *time.Time) {
	pub := map[string]string{}
	clock := time.Unix(1_700_000_000, 0)
	s := newSocLimiter(dev, 5, 80, 10*time.Minute, 10*time.Minute, func(t, v string) { pub[t] = v })
	s.now = func() time.Time { return clock }
	return s, pub, &clock
}

func TestSocParseCommand(t *testing.T) {
	s, _, _ := newTestLimiter(&fakeSoc{})
	for in, want := range map[string]int{"28": 28, " 30.0 ": 30, "5": 5, "80": 80} {
		if v, err := s.parseCommand(in); err != nil || v != want {
			t.Errorf("%q -> %d, %v", in, v, err)
		}
	}
	for _, in := range []string{"4", "81", "28.5", "abc", "", "NaN"} {
		if _, err := s.parseCommand(in); err == nil {
			t.Errorf("%q haette verworfen werden muessen", in)
		}
	}
}

func TestSocWriteDedupeAndThrottle(t *testing.T) {
	dev := &fakeSoc{min: 28, mode: "manual"}
	s, pub, clock := newTestLimiter(dev)
	s.refresh()
	if pub["battery/state/soc_min"] != "28" || pub["battery/state/soc_mode"] != "manual" {
		t.Fatalf("publish: %v", pub)
	}

	// Gleicher Wert wie Geraet: nichts schreiben.
	v := 28
	s.pending = &v
	if w := s.tryWrite(); w != 0 || len(dev.writes) != 0 || s.pending != nil {
		t.Fatalf("Dedupe: wait=%v writes=%v", w, dev.writes)
	}

	// Neuer Wert: sofort schreiben, zurueckgelesen, last_write publiziert.
	v2 := 40
	s.pending = &v2
	if w := s.tryWrite(); w != 0 || len(dev.writes) != 1 || pub["battery/state/soc_min"] != "40" || pub["battery/state/last_write"] == "" {
		t.Fatalf("Schreiben: wait=%v writes=%v pub=%v", w, dev.writes, pub)
	}

	// Innerhalb der Sperrzeit: gedrosselt, nur der LETZTE Wert wird nachgeholt.
	*clock = clock.Add(3 * time.Minute)
	v3, v4 := 50, 60
	s.pending = &v3
	if w := s.tryWrite(); w != 7*time.Minute {
		t.Fatalf("wait=%v", w)
	}
	s.pending = &v4
	s.tryWrite()
	*clock = clock.Add(7 * time.Minute)
	if w := s.tryWrite(); w != 0 || len(dev.writes) != 2 || dev.writes[1] != 60 {
		t.Fatalf("Nachholen: wait=%v writes=%v", w, dev.writes)
	}
}

func TestSocWriteModeAutoForcesWrite(t *testing.T) {
	dev := &fakeSoc{min: 28, mode: "auto"}
	s, _, _ := newTestLimiter(dev)
	s.refresh()
	v := 28
	s.pending = &v
	s.tryWrite()
	if len(dev.writes) != 1 || dev.mode != "manual" {
		t.Fatalf("writes=%v mode=%s", dev.writes, dev.mode)
	}
}

func TestSocWriteDeviceErrorNotRetried(t *testing.T) {
	dev := &fakeSoc{min: 28, mode: "manual", err: errors.New("Wechselrichter meldet validationErrors: x")}
	s, _, _ := newTestLimiter(dev)
	s.refresh()
	v := 40
	s.pending = &v
	if w := s.tryWrite(); w != 0 || s.pending != nil {
		t.Fatalf("fachlicher Fehler soll nicht wiederholt werden: wait=%v", w)
	}
}
