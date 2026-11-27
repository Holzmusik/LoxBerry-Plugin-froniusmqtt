package bridge

import (
	"testing"
	"time"

	"froniusmqtt/internal/fronius"
)

func kvs(pairs ...string) []fronius.KV {
	out := make([]fronius.KV, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, fronius.KV{Topic: pairs[i], Value: pairs[i+1]})
	}
	return out
}

func topics(kv []fronius.KV) []string {
	out := make([]string, len(kv))
	for i, e := range kv {
		out[i] = e.Topic
	}
	return out
}

func TestChangeFilterFirstCallSendsAll(t *testing.T) {
	f := newChangeFilter(5 * time.Minute)
	got := f.filter(kvs("a", "1", "b", "2"), time.Unix(1000, 0))
	if len(got) != 2 {
		t.Fatalf("erster Aufruf: %v, erwartet beide Topics", topics(got))
	}
}

func TestChangeFilterOnlyChanged(t *testing.T) {
	f := newChangeFilter(5 * time.Minute)
	t0 := time.Unix(1000, 0)
	f.filter(kvs("a", "1", "b", "2"), t0)

	got := f.filter(kvs("a", "1", "b", "3"), t0.Add(5*time.Second))
	if len(got) != 1 || got[0].Topic != "b" || got[0].Value != "3" {
		t.Fatalf("got %v, erwartet nur b=3", got)
	}
	if got := f.filter(kvs("a", "1", "b", "3"), t0.Add(10*time.Second)); len(got) != 0 {
		t.Fatalf("unveraendert, trotzdem gesendet: %v", topics(got))
	}
}

func TestChangeFilterNewTopicIsSent(t *testing.T) {
	f := newChangeFilter(5 * time.Minute)
	t0 := time.Unix(1000, 0)
	f.filter(kvs("a", "1"), t0)
	got := f.filter(kvs("a", "1", "c", "9"), t0.Add(time.Second))
	if len(got) != 1 || got[0].Topic != "c" {
		t.Fatalf("got %v, erwartet nur das neue Topic c", topics(got))
	}
}

func TestChangeFilterFullResync(t *testing.T) {
	f := newChangeFilter(5 * time.Minute)
	t0 := time.Unix(1000, 0)
	f.filter(kvs("a", "1", "b", "2"), t0)
	if got := f.filter(kvs("a", "1", "b", "2"), t0.Add(4*time.Minute)); len(got) != 0 {
		t.Fatalf("vor Ablauf gesendet: %v", topics(got))
	}
	if got := f.filter(kvs("a", "1", "b", "2"), t0.Add(5*time.Minute)); len(got) != 2 {
		t.Fatalf("Vollabgleich nach 5 min: %v, erwartet beide", topics(got))
	}
	// Danach wieder nur Aenderungen.
	if got := f.filter(kvs("a", "1", "b", "2"), t0.Add(5*time.Minute+5*time.Second)); len(got) != 0 {
		t.Fatalf("nach Vollabgleich gesendet: %v", topics(got))
	}
}

func TestChangeFilterReset(t *testing.T) {
	f := newChangeFilter(5 * time.Minute)
	t0 := time.Unix(1000, 0)
	f.filter(kvs("a", "1", "b", "2"), t0)
	f.reset()
	if got := f.filter(kvs("a", "1", "b", "2"), t0.Add(time.Second)); len(got) != 2 {
		t.Fatalf("nach reset: %v, erwartet beide", topics(got))
	}
}
