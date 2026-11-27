// Package bridge ist die reine Protokolluebersetzung Fronius Solar API <->
// MQTT - keine eigene Entscheidungslogik (Lastmanagement/Automatisierung
// bleibt vollstaendig in Loxone, siehe Plan-Datei). Topic-Schema siehe README.
package bridge

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"froniusmqtt/internal/config"
	"froniusmqtt/internal/fronius"
)

type Bridge struct {
	cfg    *config.Config
	client mqtt.Client
	prefix string

	poller *fronius.Poller
	// battery ist nil, wenn weder battery_control.enabled noch
	// battery_control.soc_limit.enabled gesetzt ist. hold/release
	// (battery/cmd/hold|release, state/control_mode) nur bei holdEnabled.
	battery     *fronius.BatteryController
	holdEnabled bool
	// soc ist nil, wenn battery_control.soc_limit.enabled=false (siehe soc.go).
	soc *socLimiter

	// categoryFilter == nil bedeutet "alles publizieren" - siehe
	// config.MQTTConfig.EnabledCategories.
	categoryFilter map[string]bool

	// changes: nur geaenderte Werte publizieren (siehe change.go).
	// resync wird vom MQTT-OnConnect-Handler (paho-Goroutine) gesetzt und
	// von der Poll-Schleife verbraucht - nach einem Reconnect geht einmal
	// alles raus, falls der Broker zwischenzeitlich neu gestartet wurde.
	changes *changeFilter
	resync  atomic.Bool

	// reach merkt den letzten Erreichbarkeitszustand des Wechselrichters
	// fuer bridge/fronius_ok (siehe reachability).
	reach reachability
}

func New(cfg *config.Config, poller *fronius.Poller, battery *fronius.BatteryController) *Bridge {
	var filter map[string]bool
	if cfg.MQTT.EnabledCategories != nil {
		filter = make(map[string]bool, len(cfg.MQTT.EnabledCategories))
		for _, cat := range cfg.MQTT.EnabledCategories {
			filter[cat] = true
		}
	}
	b := &Bridge{
		cfg:            cfg,
		prefix:         cfg.MQTT.TopicPrefix,
		poller:         poller,
		battery:        battery,
		holdEnabled:    battery != nil && cfg.Fronius.BatteryControl.Enabled,
		categoryFilter: filter,
		changes:        newChangeFilter(fullResyncInterval),
	}
	if sl := cfg.Fronius.BatteryControl.SocLimit; battery != nil && sl.Enabled {
		b.soc = newSocLimiter(battery, sl.Min, sl.Max,
			time.Duration(sl.WriteIntervalMinutes)*time.Minute,
			time.Duration(sl.ReadIntervalMinutes)*time.Minute,
			func(topic, value string) {
				if b.client != nil {
					b.client.Publish(b.prefix+topic, 0, true, value)
				}
			})
	}
	return b
}

// Connect baut die MQTT-Verbindung auf (mit LWT auf <prefix>bridge/status)
// und abonniert - falls aktiviert - die Batterie-Control-Kommando-Topics.
func (b *Bridge) Connect() error {
	statusTopic := b.prefix + "bridge/status"

	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", b.cfg.MQTT.Host, b.cfg.MQTT.Port))
	opts.SetClientID(b.cfg.MQTT.ClientID)
	if b.cfg.MQTT.Username != "" {
		opts.SetUsername(b.cfg.MQTT.Username)
		opts.SetPassword(b.cfg.MQTT.Password)
	}
	opts.SetWill(statusTopic, "offline", 1, true)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		log.Printf("mqtt: verbunden (%s:%d)", b.cfg.MQTT.Host, b.cfg.MQTT.Port)
		c.Publish(statusTopic, 1, true, "online")
		b.resync.Store(true)
		if b.holdEnabled {
			b.subscribeBatteryCommands(c)
		}
		if b.soc != nil {
			c.Subscribe(b.prefix+"battery/cmd/soc_min", 1, b.socCmdHandler)
			b.soc.republish()
		}
	})
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		log.Printf("mqtt: Verbindung verloren: %v", err)
	})

	b.client = mqtt.NewClient(opts)
	token := b.client.Connect()
	token.Wait()
	return token.Error()
}

func (b *Bridge) subscribeBatteryCommands(c mqtt.Client) {
	base := b.prefix + "battery/cmd/"
	c.Subscribe(base+"hold", 1, b.batteryCmdHandler(true))
	c.Subscribe(base+"release", 1, b.batteryCmdHandler(false))
}

func (b *Bridge) batteryCmdHandler(hold bool) mqtt.MessageHandler {
	label, mode := "release", "normal"
	if hold {
		label, mode = "hold", "hold"
	}
	return func(c mqtt.Client, msg mqtt.Message) {
		// Retained-Kommandos verwerfen: ein (versehentlich) retained
		// gesendetes hold/release wuerde sonst bei JEDEM Reconnect des
		// Diensts erneut in den Wechselrichter geschrieben.
		if msg.Retained() {
			log.Printf("bridge: retained battery/cmd/%s ignoriert - Kommandos bitte ohne Retain-Flag senden", label)
			return
		}
		if err := b.battery.SetHold(hold); err != nil {
			log.Printf("bridge: battery/%s fehlgeschlagen: %v", label, err)
			return
		}
		log.Printf("bridge: battery/%s ausgefuehrt", label)
		c.Publish(b.prefix+"battery/state/control_mode", 0, true, mode)
	}
}

func (b *Bridge) socCmdHandler(c mqtt.Client, msg mqtt.Message) {
	// Retained verwerfen - wie bei hold/release (sonst bei jedem Reconnect
	// erneut verarbeitet).
	if msg.Retained() {
		log.Print("bridge: retained battery/cmd/soc_min ignoriert - Kommandos bitte ohne Retain-Flag senden")
		return
	}
	b.soc.submit(string(msg.Payload()))
}

// RunPollLoop blockiert bis ctx endet, fragt im konfigurierten Poll-Intervall
// (siehe config.FroniusConfig.PollIntervalSeconds) den Poller ab und
// veroeffentlicht geaenderte Werte retained (alle fullResyncInterval und
// nach jedem MQTT-Reconnect alle Werte). Erster Poll laeuft sofort (kein
// Warten auf das erste Ticker-Intervall).
func (b *Bridge) RunPollLoop(ctx context.Context) {
	interval := time.Duration(b.cfg.Fronius.PollIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if b.soc != nil {
		go b.soc.run(ctx)
	}

	b.pollOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.pollOnce()
		}
	}
}

func (b *Bridge) pollOnce() {
	if b.client == nil || !b.client.IsConnected() {
		return
	}
	if b.resync.Swap(false) {
		b.changes.reset()
	}
	polled, err := b.poller.Poll()
	now := time.Now()
	if msg, changed := b.reach.update(err); changed {
		log.Print(msg)
	}
	var selected []fronius.KV
	for _, kv := range polled {
		// Kategorie = erstes Topic-Segment ("inverter/1/state/pac" ->
		// "inverter") - siehe poller.go fuer die Topic-Struktur. Der
		// Poller laesst abgewaehlte Kategorien zwar schon weg, PowerFlow
		// liefert aber "site" und "inverter" gemeinsam.
		if b.categoryFilter != nil {
			category, _, _ := strings.Cut(kv.Topic, "/")
			if !b.categoryFilter[category] {
				continue
			}
		}
		selected = append(selected, kv)
	}
	// bridge/* laufen bewusst NICHT durch changes: last_poll aendert sich
	// ohnehin jeden erfolgreichen Zyklus, fronius_ok soll ein Loxone-
	// Watchdog auch ohne Wechsel regelmaessig sehen.
	ok := "1"
	if err != nil {
		ok = "0"
	}
	for _, kv := range b.changes.filter(selected, now) {
		b.client.Publish(b.prefix+kv.Topic, 0, true, kv.Value)
	}
	b.client.Publish(b.prefix+"bridge/fronius_ok", 0, true, ok)
	if err == nil {
		b.client.Publish(b.prefix+"bridge/last_poll", 0, true, strconv.FormatInt(now.Unix(), 10))
	}
}

func (b *Bridge) Disconnect() {
	if b.client != nil && b.client.IsConnected() {
		b.client.Disconnect(250)
	}
}
