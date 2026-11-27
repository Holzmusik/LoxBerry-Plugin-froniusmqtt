package bridge

import (
	"time"

	"froniusmqtt/internal/fronius"
)

// fullResyncInterval: so oft werden ALLE Topics publiziert, auch
// unveraenderte - Sicherheitsnetz, falls ein Broker ohne Persistenz neu
// startet oder ein Abonnent eine Nachricht verpasst hat.
const fullResyncInterval = 5 * time.Minute

// changeFilter laesst nur Topics durch, deren Wert sich seit der letzten
// Veroeffentlichung geaendert hat. Alle fullResyncInterval (und nach reset,
// z.B. bei MQTT-Reconnect) geht einmal alles raus. Nicht threadsafe - wird
// nur aus der Poll-Schleife benutzt.
type changeFilter struct {
	interval time.Duration
	last     map[string]string
	lastFull time.Time // Null-Zeit = naechster Aufruf ist Vollabgleich
}

func newChangeFilter(interval time.Duration) *changeFilter {
	return &changeFilter{interval: interval, last: map[string]string{}}
}

func (f *changeFilter) reset() {
	f.last = map[string]string{}
	f.lastFull = time.Time{}
}

func (f *changeFilter) filter(kv []fronius.KV, now time.Time) []fronius.KV {
	full := f.lastFull.IsZero() || now.Sub(f.lastFull) >= f.interval
	if full {
		f.lastFull = now
	}
	out := kv[:0:0]
	for _, e := range kv {
		if prev, ok := f.last[e.Topic]; full || !ok || prev != e.Value {
			out = append(out, e)
			f.last[e.Topic] = e.Value
		}
	}
	return out
}
