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
	"encoding/json"
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

// runBatteryTest bedient "froniusmqtt battery-test" fuer den "Login testen"-
// Button der Web-UI (api.cgi): liest {"host","user","password",
// "config_path"} als JSON von STDIN (Passwort nicht in der Prozessliste),
// fuehrt mit der Digest-Implementierung des Diensts einen rein LESENDEN GET
// auf den timeofuse-Endpunkt aus und gibt fronius.LoginResult als JSON aus.
// Ersetzt den frueheren curl --digest-Test, der X-WWW-Authenticate (neuere
// Fronius-Firmware) nicht auswerten kann.
func runBatteryTest() {
	var in struct {
		Host       string `json:"host"`
		User       string `json:"user"`
		Password   string `json:"password"`
		ConfigPath string `json:"config_path"`
	}
	out := json.NewEncoder(os.Stdout)
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		out.Encode(fronius.LoginResult{Error: "Eingabe nicht lesbar: " + err.Error()})
		return
	}
	if in.Host == "" || in.User == "" || in.Password == "" {
		out.Encode(fronius.LoginResult{Error: "Host/Benutzername/Passwort fehlt"})
		return
	}
	client := fronius.NewClient(in.Host, 8*time.Second)
	bc := fronius.NewBatteryController(client, in.User, in.Password, in.ConfigPath)
	out.Encode(bc.TestLogin())
}

// runBatteryConfig bedient "froniusmqtt battery-config" fuer den Button
// "Batterie-Konfiguration lesen" der Web-UI: gleiche STDIN-Eingabe wie
// battery-test, rein lesender GET auf .../config/batteries, gibt die
// erkannten Werte und die Rohantwort als JSON aus.
func runBatteryConfig() {
	var in struct {
		Host       string `json:"host"`
		User       string `json:"user"`
		Password   string `json:"password"`
		ConfigPath string `json:"config_path"`
	}
	type result struct {
		OK      bool   `json:"ok"`
		Path    string `json:"path,omitempty"`
		SocMin  *int   `json:"soc_min,omitempty"`
		SocMax  *int   `json:"soc_max,omitempty"`
		SocMode string `json:"soc_mode,omitempty"`
		Raw     string `json:"raw,omitempty"`
		Error   string `json:"error,omitempty"`
	}
	out := json.NewEncoder(os.Stdout)
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		out.Encode(result{Error: "Eingabe nicht lesbar: " + err.Error()})
		return
	}
	if in.Host == "" || in.User == "" || in.Password == "" {
		out.Encode(result{Error: "Host/Benutzername/Passwort fehlt"})
		return
	}
	client := fronius.NewClient(in.Host, 8*time.Second)
	bc := fronius.NewBatteryController(client, in.User, in.Password, in.ConfigPath)
	st, raw, err := bc.ReadBatteryConfig()
	r := result{Path: bc.BatteriesPath(), SocMin: st.Min, SocMax: st.Max, SocMode: st.Mode, Raw: string(raw)}
	if err != nil {
		r.Error = err.Error()
	} else {
		r.OK = true
	}
	out.Encode(r)
}

func main() {
	log.SetFlags(log.LstdFlags)

	if len(os.Args) >= 3 && (os.Args[1] == "encrypt" || os.Args[1] == "decrypt") {
		runCrypto(os.Args[1], os.Args[2])
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "battery-test" {
		runBatteryTest()
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "battery-config" {
		runBatteryConfig()
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
	poller := fronius.NewPoller(client, cfg.MQTT.EnabledCategories)

	var battery *fronius.BatteryController
	if cfg.Fronius.BatteryControl.Enabled || cfg.Fronius.BatteryControl.SocLimit.Enabled {
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

	sl := cfg.Fronius.BatteryControl.SocLimit
	log.Printf("froniusmqtt gestartet - Host %s, Poll-Intervall %ds, MQTT-Prefix %q, Batterie-Control %v, Min-SoC-Steuerung %v (%d..%d%%, Schreibabstand %d min)",
		cfg.Fronius.Host, cfg.Fronius.PollIntervalSeconds, cfg.MQTT.TopicPrefix, cfg.Fronius.BatteryControl.Enabled,
		sl.Enabled, sl.Min, sl.Max, sl.WriteIntervalMinutes)

	br.RunPollLoop(ctx)
	log.Println("froniusmqtt beendet")
}
