// Package bridge ist die reine Protokolluebersetzung Fronius Solar API <->
// MQTT - keine eigene Entscheidungslogik (Lastmanagement/Automatisierung
// bleibt vollstaendig in Loxone, siehe Plan-Datei). Topic-Schema siehe README.
package bridge

import (
	"context"
	"fmt"
	"log"
	"strings"
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
	// battery ist nil, wenn fronius.battery_control.enabled=false - siehe
	// config.BatteryControlConfig. Dann werden weder die battery/cmd/*-
	// Topics abonniert noch battery/state/control_mode veroeffentlicht.
	battery *fronius.BatteryController

	// categoryFilter == nil bedeutet "alles publizieren" - siehe
	// config.MQTTConfig.EnabledCategories.
	categoryFilter map[string]bool
}

func New(cfg *config.Config, poller *fronius.Poller, battery *fronius.BatteryController) *Bridge {
	var filter map[string]bool
	if cfg.MQTT.EnabledCategories != nil {
		filter = make(map[string]bool, len(cfg.MQTT.EnabledCategories))
		for _, cat := range cfg.MQTT.EnabledCategories {
			filter[cat] = true
		}
	}
	return &Bridge{
		cfg:            cfg,
		prefix:         cfg.MQTT.TopicPrefix,
		poller:         poller,
		battery:        battery,
		categoryFilter: filter,
	}
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
		if b.battery != nil {
			b.subscribeBatteryCommands(c)
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
		if err := b.battery.SetHold(hold); err != nil {
			log.Printf("bridge: battery/%s fehlgeschlagen: %v", label, err)
			return
		}
		log.Printf("bridge: battery/%s ausgefuehrt", label)
		c.Publish(b.prefix+"battery/state/control_mode", 0, true, mode)
	}
}

// RunPollLoop blockiert bis ctx endet, fragt im konfigurierten Poll-Intervall
// (siehe config.FroniusConfig.PollIntervalSeconds) den Poller ab und
// veroeffentlicht jedes Ergebnis retained. Erster Poll laeuft sofort (kein
// Warten auf das erste Ticker-Intervall).
func (b *Bridge) RunPollLoop(ctx context.Context) {
	interval := time.Duration(b.cfg.Fronius.PollIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

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
	for _, kv := range b.poller.Poll() {
		// Kategorie = erstes Topic-Segment ("inverter/1/state/pac" ->
		// "inverter") - siehe poller.go fuer die Topic-Struktur.
		if b.categoryFilter != nil {
			category, _, _ := strings.Cut(kv.Topic, "/")
			if !b.categoryFilter[category] {
				continue
			}
		}
		b.client.Publish(b.prefix+kv.Topic, 0, true, kv.Value)
	}
}

func (b *Bridge) Disconnect() {
	if b.client != nil && b.client.IsConnected() {
		b.client.Disconnect(250)
	}
}
