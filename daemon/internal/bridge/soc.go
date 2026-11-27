package bridge

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"froniusmqtt/internal/fronius"
)

// socDevice ist der Teil von fronius.BatteryController, den socLimiter
// braucht (Interface fuer Tests).
type socDevice interface {
	ReadBatteryConfig() (fronius.SocSettings, []byte, error)
	SetSocMin(percent int) error
}

// socLimiter: minimaler Ladezustand lesen/setzen (battery/state/soc_*,
// battery/cmd/soc_min). Reine Protokollbruecke - WELCHER Wert sinnvoll ist,
// entscheidet Loxone. Das Plugin schuetzt nur den Wechselrichter-Flash:
// geschrieben wird nur bei Abweichung vom Geraetewert und hoechstens alle
// writeInterval; ein in der Sperrzeit eintreffender Wert wird danach
// nachgeholt (nur der jeweils letzte).
//
// Alle Geraetezugriffe laufen in run() (eine Goroutine); MQTT-Kommandos
// kommen ueber cmds herein. mu schuetzt nur den Cache fuer republish().
type socLimiter struct {
	dev           socDevice
	min, max      int
	writeInterval time.Duration
	readInterval  time.Duration
	publish       func(topic, value string) // topic ohne Prefix
	now           func() time.Time

	cmds chan int

	mu        sync.Mutex
	state     fronius.SocSettings
	haveState bool
	lastWrite time.Time // erfolgreicher oder fehlgeschlagener Schreibversuch
	lastOK    int64     // Unix-Zeit des letzten erfolgreichen Schreibens (0 = keins)

	// nur in run():
	pending     *int
	announce    bool // pending neu -> Drossel-Hinweis einmal loggen
	loggedRaw   bool
	readFailing bool
}

func newSocLimiter(dev socDevice, min, max int, writeInterval, readInterval time.Duration, publish func(topic, value string)) *socLimiter {
	return &socLimiter{
		dev: dev, min: min, max: max,
		writeInterval: writeInterval, readInterval: readInterval,
		publish: publish, now: time.Now,
		cmds: make(chan int, 8),
	}
}

// parseCommand prueft eine battery/cmd/soc_min-Payload: Ganzzahl (auch
// "28.0" - Loxone formatiert Analogwerte teils mit Nachkommastellen)
// innerhalb [min,max].
func (s *socLimiter) parseCommand(payload string) (int, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(payload), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%q ist keine Zahl", payload)
	}
	if math.Abs(f-math.Round(f)) > 0.01 {
		return 0, fmt.Errorf("%q ist keine ganze Zahl", payload)
	}
	v := int(math.Round(f))
	if v < s.min || v > s.max {
		return 0, fmt.Errorf("%d%% liegt ausserhalb der erlaubten Grenzen %d..%d%% (Web-UI, Batterie-Control)", v, s.min, s.max)
	}
	return v, nil
}

// submit wird aus dem MQTT-Handler aufgerufen.
func (s *socLimiter) submit(payload string) {
	v, err := s.parseCommand(payload)
	if err != nil {
		log.Printf("bridge: battery/cmd/soc_min verworfen: %v", err)
		return
	}
	select {
	case s.cmds <- v:
	default:
		log.Printf("bridge: battery/cmd/soc_min=%d verworfen - Warteschlange voll", v)
	}
}

func (s *socLimiter) run(ctx context.Context) {
	s.refresh()
	ticker := time.NewTicker(s.readInterval)
	defer ticker.Stop()
	var retry <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case v := <-s.cmds:
			s.pending, s.announce = &v, true
		case <-ticker.C:
			s.refresh()
		case <-retry:
		}
		retry = nil
		if wait := s.tryWrite(); wait > 0 {
			retry = time.After(wait)
		}
	}
}

// refresh liest die Batterie-Konfiguration und publiziert sie.
func (s *socLimiter) refresh() {
	st, raw, err := s.dev.ReadBatteryConfig()
	if err != nil {
		if !s.readFailing {
			log.Printf("bridge: Batterie-Konfiguration lesen fehlgeschlagen: %v", err)
		}
		s.readFailing = true
		return
	}
	if s.readFailing {
		log.Print("bridge: Batterie-Konfiguration wieder lesbar")
	}
	s.readFailing = false
	if !s.loggedRaw {
		s.loggedRaw = true
		r := strings.TrimSpace(string(raw))
		if len(r) > 2000 {
			r = r[:2000] + " ..."
		}
		log.Printf("bridge: Batterie-Konfiguration (einmalig, Rohantwort): %s", r)
		if st.Min == nil {
			log.Printf("bridge: WARNUNG Feld %s nicht in der Antwort gefunden - Min-SoC-Steuerung wird nicht funktionieren", fronius.FieldSocMin)
		}
	}
	s.mu.Lock()
	s.state, s.haveState = st, true
	s.mu.Unlock()
	s.publishState(st)
}

func (s *socLimiter) publishState(st fronius.SocSettings) {
	if st.Min != nil {
		s.publish("battery/state/soc_min", strconv.Itoa(*st.Min))
	}
	if st.Max != nil {
		s.publish("battery/state/soc_max", strconv.Itoa(*st.Max))
	}
	if st.Mode != "" {
		s.publish("battery/state/soc_mode", st.Mode)
	}
}

// republish nach MQTT-Reconnect (Broker-Neustart ohne Persistenz).
func (s *socLimiter) republish() {
	s.mu.Lock()
	st, have, lastOK := s.state, s.haveState, s.lastOK
	s.mu.Unlock()
	if have {
		s.publishState(st)
	}
	if lastOK > 0 {
		s.publish("battery/state/last_write", strconv.FormatInt(lastOK, 10))
	}
}

// tryWrite schreibt einen anstehenden Wert, falls noetig und erlaubt.
// Rueckgabe > 0: so lange warten, dann erneut aufrufen (Drossel).
func (s *socLimiter) tryWrite() time.Duration {
	if s.pending == nil {
		return 0
	}
	v := *s.pending
	s.mu.Lock()
	st, have, last := s.state, s.haveState, s.lastWrite
	s.mu.Unlock()

	if have && st.Min != nil && *st.Min == v && st.Mode == "manual" {
		if s.announce {
			log.Printf("bridge: soc_min=%d entspricht bereits dem Geraetewert - nichts zu schreiben", v)
		}
		s.pending, s.announce = nil, false
		return 0
	}
	if !last.IsZero() {
		if wait := s.writeInterval - s.now().Sub(last); wait > 0 {
			if s.announce {
				log.Printf("bridge: soc_min=%d gedrosselt (Mindestabstand %v) - wird um %s geschrieben",
					v, s.writeInterval, last.Add(s.writeInterval).Format("15:04:05"))
				s.announce = false
			}
			return wait
		}
	}

	s.pending, s.announce = nil, false
	now := s.now()
	s.mu.Lock()
	s.lastWrite = now
	s.mu.Unlock()
	err := s.dev.SetSocMin(v)
	if err != nil {
		log.Printf("bridge: soc_min=%d schreiben fehlgeschlagen: %v", v, err)
		if fronius.IsConnError(err) && s.pending == nil {
			s.pending = &v // Wechselrichter nicht erreichbar: nach Sperrzeit erneut
			return s.writeInterval
		}
		return 0
	}
	s.mu.Lock()
	s.lastOK = now.Unix()
	s.mu.Unlock()
	s.publish("battery/state/last_write", strconv.FormatInt(now.Unix(), 10))
	s.refresh()
	s.mu.Lock()
	got := s.state.Min
	s.mu.Unlock()
	switch {
	case got != nil && *got == v:
		log.Printf("bridge: soc_min=%d geschrieben und zurueckgelesen", v)
	case got != nil:
		log.Printf("bridge: WARNUNG soc_min=%d geschrieben, Geraet meldet aber %d", v, *got)
	default:
		log.Printf("bridge: soc_min=%d geschrieben, Zuruecklesen nicht moeglich", v)
	}
	return 0
}
