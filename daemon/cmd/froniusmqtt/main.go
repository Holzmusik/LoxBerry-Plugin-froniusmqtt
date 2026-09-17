// froniusmqtt: reine Protokollbruecke Fronius Solar API v1 (lokal, HTTP/JSON)
// <-> MQTT. Keine eigene Lastmanagement-/Automatisierungslogik - die bleibt
// vollstaendig in Loxone, siehe Plan-Datei/README. Konfigurationsdatei ueber
// FRONIUSMQTT_CONFIG (Default: relativer Pfad "config.json", siehe systemd-
// Unit fuer den echten Pfad) - gleiches Env-Var-statt-Flag-Muster wie bei den
// Schwester-Plugins EaseeMQTT/KNXtoLOX.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"froniusmqtt/internal/bridge"
	"froniusmqtt/internal/config"
	"froniusmqtt/internal/fronius"
)

// runCrypto bedient "froniusmqtt encrypt <config-pfad>" / "froniusmqtt decrypt
// <config-pfad>" - liest den zu (ent-)schluesselnden Wert von STDIN statt als
// Kommandozeilenargument (sonst kurzzeitig in der Prozessliste/ps aux
// sichtbar). api.cgi ruft dies als Subprocess auf - gleiches Muster wie bei
// EaseeMQTT, damit die Kryptografie an einer Stelle (hier, Go-Standard-
// bibliothek) lebt statt in Perl dupliziert zu werden.
func runCrypto(mode, cfgPath string) {
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimRight(input, "\r\n")

	var out string
	var err error
	switch mode {
	case "encrypt":
		out, err = config.Encrypt(cfgPath, input)
	case "decrypt":
		var ok bool
		out, ok = config.Decrypt(cfgPath, input)
		if !ok {
			err = fmt.Errorf("kein gueltiges Chiffrat")
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "froniusmqtt "+mode+": "+err.Error())
		os.Exit(1)
	}
	fmt.Println(out)
}

func main() {
	log.SetFlags(log.LstdFlags)

	if len(os.Args) >= 3 && (os.Args[1] == "encrypt" || os.Args[1] == "decrypt") {
		runCrypto(os.Args[1], os.Args[2])
		return
	}

	cfgPath := os.Getenv("FRONIUSMQTT_CONFIG")
	if cfgPath == "" {
		cfgPath = "config.json"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Konfiguration konnte nicht geladen werden: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := fronius.NewClient(cfg.Fronius.Host, time.Duration(cfg.Fronius.HTTPTimeoutSeconds)*time.Second)
	poller := fronius.NewPoller(client)

	var battery *fronius.BatteryController
	if cfg.Fronius.BatteryControl.Enabled {
		battery = fronius.NewBatteryController(
			client,
			cfg.Fronius.BatteryControl.User,
			cfg.Fronius.BatteryControl.Password,
			cfg.Fronius.BatteryControl.ConfigPath,
		)
	}

	br := bridge.New(cfg, poller, battery)
	if err := br.Connect(); err != nil {
		log.Fatalf("MQTT-Verbindung fehlgeschlagen: %v", err)
	}
	defer br.Disconnect()

	log.Printf("froniusmqtt gestartet - Host %s, Poll-Intervall %ds, MQTT-Prefix %q, Batterie-Control %v",
		cfg.Fronius.Host, cfg.Fronius.PollIntervalSeconds, cfg.MQTT.TopicPrefix, cfg.Fronius.BatteryControl.Enabled)

	br.RunPollLoop(ctx)
	log.Println("froniusmqtt beendet")
}
