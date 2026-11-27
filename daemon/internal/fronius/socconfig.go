// Min-SoC ("Einstellungen Ladezustand" der Fronius-Weboberflaeche) lesen und
// setzen. Wie timeofuse (control.go) ein NICHT offiziell dokumentierter
// Endpunkt der Weboberflaeche: GET/POST /api/config/batteries (aeltere
// Firmware: /config/batteries), gleiche Digest-Anmeldung.
//
// POST-Body und Antwortformat am GEN24 des Nutzers per Browser-DevTools
// mitgeschnitten (2026-11-27): die Weboberflaeche schickt NUR die
// geaenderten Felder - {"BAT_M0_SOC_MIN":28,"BAT_M0_SOC_MODE":"manual"} -,
// die restliche Batterie-Konfiguration bleibt unveraendert. Die Antwort
// (HTTP 200) listet pro Feld, was passiert ist:
//
//	{"errors":[],"permissionFailure":[],"unknownNodes":[],
//	 "validationErrors":[],"writeFailure":[],"writeSuccess":["BAT_M0_SOC_MIN"]}
//
// Unveraenderte Felder (dort: SOC_MODE war schon "manual") tauchen in
// writeSuccess nicht auf. Das Format der GET-Antwort ist NICHT
// mitgeschnitten - die Felder werden deshalb per Name an beliebiger Stelle
// im JSON gesucht (findField), die Rohantwort zeigt die Web-UI an.
package fronius

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const (
	FieldSocMin  = "BAT_M0_SOC_MIN"
	FieldSocMax  = "BAT_M0_SOC_MAX" // Name aus Community-Quellen, per GET pruefen
	FieldSocMode = "BAT_M0_SOC_MODE"
)

// SocSettings sind die aus der GET-Antwort gelesenen Werte. nil bzw. ""
// = Feld in der Antwort nicht gefunden (wird dann nicht publiziert).
type SocSettings struct {
	Min  *int
	Max  *int
	Mode string
}

// BatteriesPath liefert den batteries-Endpunkt passend zum (ggf. per
// ResolvePath ermittelten) timeofuse-Pfad.
func (b *BatteryController) BatteriesPath() string {
	return strings.TrimSuffix(b.path, "/timeofuse") + "/batteries"
}

// ReadBatteryConfig liest die Batterie-Konfiguration (rein lesender GET).
// Liefert die extrahierten Werte und die Rohantwort (fuer die Web-UI,
// "froniusmqtt battery-config"; Pfad danach per BatteriesPath).
func (b *BatteryController) ReadBatteryConfig() (SocSettings, []byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ResolvePath(); err != nil {
		return SocSettings{}, nil, err
	}
	path := b.BatteriesPath()
	status, body, err := b.digestRequest(http.MethodGet, path, nil)
	if err != nil {
		return SocSettings{}, nil, err
	}
	switch {
	case status == http.StatusUnauthorized:
		return SocSettings{}, body, fmt.Errorf("GET %s: Login abgelehnt (HTTP 401) - Benutzername/Passwort pruefen", path)
	case status < 200 || status >= 300:
		return SocSettings{}, body, fmt.Errorf("GET %s: HTTP %d", path, status)
	}
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return SocSettings{}, body, fmt.Errorf("GET %s: keine gueltige JSON-Antwort: %w", path, err)
	}
	return parseSocSettings(doc), body, nil
}

func parseSocSettings(doc any) SocSettings {
	var s SocSettings
	if v, ok := findField(doc, FieldSocMin); ok {
		s.Min = toInt(v)
	}
	if v, ok := findField(doc, FieldSocMax); ok {
		s.Max = toInt(v)
	}
	if v, ok := findField(doc, FieldSocMode); ok {
		if str, isStr := v.(string); isStr {
			s.Mode = str
		}
	}
	return s
}

// findField sucht key in beliebig verschachtelten Objekten/Arrays
// (Tiefensuche, Objekt-Schluessel sortiert = deterministisch).
func findField(v any, key string) (any, bool) {
	switch t := v.(type) {
	case map[string]any:
		if val, ok := t[key]; ok {
			return val, true
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if val, ok := findField(t[k], key); ok {
				return val, true
			}
		}
	case []any:
		for _, e := range t {
			if val, ok := findField(e, key); ok {
				return val, true
			}
		}
	}
	return nil, false
}

func toInt(v any) *int {
	var f float64
	switch t := v.(type) {
	case float64:
		f = t
	case string:
		p, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return nil
		}
		f = p
	default:
		return nil
	}
	i := int(math.Round(f))
	return &i
}

// SetSocMin schreibt den minimalen Ladezustand - exakt der Body, den die
// Fronius-Weboberflaeche schickt (SOC_MIN wirkt nur im Modus "manual").
// Fehler, wenn das Geraet in der Antwort ein Problem meldet; ob der Wert
// wirklich uebernommen wurde, prueft der Aufrufer per ReadBatteryConfig.
func (b *BatteryController) SetSocMin(percent int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ResolvePath(); err != nil {
		return err
	}
	path := b.BatteriesPath()
	body, err := json.Marshal(map[string]any{FieldSocMin: percent, FieldSocMode: "manual"})
	if err != nil {
		return err
	}
	status, resp, err := b.digestRequest(http.MethodPost, path, body)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized {
		return fmt.Errorf("POST %s: Login abgelehnt (HTTP 401) - Benutzername/Passwort pruefen", path)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("POST %s: HTTP %d: %s", path, status, strings.TrimSpace(string(resp)))
	}
	return checkWriteResponse(resp)
}

// checkWriteResponse wertet die Feldlisten der POST-Antwort aus (Format
// siehe Paketkommentar). Unbekanntes Antwortformat = kein Fehler, der
// Aufrufer verifiziert per Zuruecklesen.
func checkWriteResponse(body []byte) error {
	var r map[string]json.RawMessage
	if err := json.Unmarshal(body, &r); err != nil {
		return nil
	}
	var problems []string
	for _, k := range []string{"errors", "permissionFailure", "unknownNodes", "validationErrors", "writeFailure"} {
		var list []json.RawMessage
		if raw, ok := r[k]; !ok || json.Unmarshal(raw, &list) != nil || len(list) == 0 {
			continue
		}
		items := make([]string, len(list))
		for i, e := range list {
			items[i] = string(e)
		}
		problems = append(problems, k+": "+strings.Join(items, ", "))
	}
	if len(problems) > 0 {
		return fmt.Errorf("Wechselrichter meldet %s", strings.Join(problems, "; "))
	}
	return nil
}
