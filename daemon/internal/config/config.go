// Package config liest die einzige Config-Datei des Daemons (config.json).
// Gleiches Muster wie beim Schwester-Plugin EaseeMQTT: EINE Datei, von der
// Perl-Web-UI (api.cgi) geschrieben, vom Go-Daemon direkt gelesen.
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// BatteryControlConfig steuert den optionalen, experimentellen Zugriff auf
// den undokumentierten Fronius-Endpunkt POST /config/timeofuse (siehe
// internal/fronius/control.go) - standardmaessig deaktiviert.
type BatteryControlConfig struct {
	Enabled  bool   `json:"enabled"`
	User     string `json:"user"`
	Password string `json:"password"`
	// ConfigPath: "auto" (zuerst /api/config/timeofuse versuchen, bei 404
	// Fallback /config/timeofuse - siehe control.go), oder explizit
	// "/config" bzw. "/api/config" wenn Auto-Detect am Zielgeraet nicht
	// zuverlaessig funktioniert.
	ConfigPath string `json:"config_path"`
}

type FroniusConfig struct {
	Host                string               `json:"host"`
	PollIntervalSeconds int                  `json:"poll_interval_seconds"`
	HTTPTimeoutSeconds  int                  `json:"http_timeout_seconds"`
	BatteryControl      BatteryControlConfig `json:"battery_control"`
}

type MQTTConfig struct {
	UseLocalBroker bool   `json:"use_local_broker"` // reine UI-Anzeige, vom Daemon ignoriert
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	TopicPrefix    string `json:"topic_prefix"`
	ClientID       string `json:"client_id"`

	// EnabledCategories ist eine Positivliste der Topic-Kategorien
	// ("inverter","storage","meter","site" - das jeweils erste Segment
	// eines von poller.go erzeugten Topics), die tatsaechlich publiziert
	// werden. Gleiche Nil/leer-Semantik wie bei EaseeMQTTs
	// EnabledObservations: nil (Schluessel fehlt) = keine Einschraenkung,
	// alles publizieren; eine vorhandene (ggf. leere) Liste ist eine echte
	// Positivliste.
	EnabledCategories []string `json:"enabled_categories"`
}

type Config struct {
	Fronius FroniusConfig `json:"fronius"`
	MQTT    MQTTConfig    `json:"mqtt"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config.json konnte nicht gelesen werden (%s): %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("config.json ist kein gueltiges JSON (%s): %w", path, err)
	}

	// Passwoerter liegen in config.json verschluesselt vor (siehe crypto.go).
	// Faellt Decrypt() auf einen Wert zurueck, der kein gueltiges Chiffrat
	// ist, wird er unveraendert als Klartext behandelt (Uebergang von vor
	// Einfuehrung der Verschluesselung bereits bestehenden Dateien).
	if plain, ok := Decrypt(path, c.MQTT.Password); ok {
		c.MQTT.Password = plain
	}
	if plain, ok := Decrypt(path, c.Fronius.BatteryControl.Password); ok {
		c.Fronius.BatteryControl.Password = plain
	}

	if c.Fronius.Host == "" {
		return nil, fmt.Errorf("config.json: fronius.host ist leer - bitte in der Web-UI konfigurieren")
	}
	if c.Fronius.PollIntervalSeconds <= 0 {
		c.Fronius.PollIntervalSeconds = 5
	}
	if c.Fronius.HTTPTimeoutSeconds <= 0 {
		c.Fronius.HTTPTimeoutSeconds = 5
	}
	if c.Fronius.BatteryControl.ConfigPath == "" {
		c.Fronius.BatteryControl.ConfigPath = "auto"
	}
	if c.MQTT.Host == "" {
		return nil, fmt.Errorf("config.json: mqtt.host ist leer - lokalen Broker aktivieren oder externen Broker eintragen")
	}
	if c.MQTT.Port == 0 {
		c.MQTT.Port = 1883
	}
	if c.MQTT.TopicPrefix == "" {
		c.MQTT.TopicPrefix = "fronius/"
	}
	if c.MQTT.ClientID == "" {
		c.MQTT.ClientID = "froniusmqtt"
	}
	return &c, nil
}
