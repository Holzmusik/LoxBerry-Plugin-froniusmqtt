// Batterie-Control ist EXPERIMENTELL und beruht auf einem undokumentierten
// Fronius-Endpunkt (POST /config/timeofuse bzw. /api/config/timeofuse ab
// Firmware 1.36.5-1), nicht auf einem offiziellen Fronius-API-Dokument -
// Quelle: Community-Recherche rund um evcc-io/evcc (Diskussion #11711,
// templates/definition/meter/fronius-solarapi-v1.yaml), siehe Plan-Datei.
// ACHTUNG: schreibt ein Zeitfenster in die Wechselrichter-eigene
// "Energiemanagement -> Batteriemanagement"-Konfiguration und UEBERSCHREIBT
// dort ggf. bereits vom Nutzer eingerichtete Zeitplaene. Kann mit
// Firmware-Updates brechen, da nicht offiziell dokumentiert. Standardmaessig
// deaktiviert (siehe config.BatteryControlConfig.Enabled).
package fronius

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

type BatteryController struct {
	client   *Client
	user     string
	password string
	// path ist entweder "/config/timeofuse" oder "/api/config/timeofuse" -
	// wird von ResolvePath() einmalig ermittelt (siehe dort) und danach fuer
	// alle weiteren Aufrufe wiederverwendet.
	path string
}

func NewBatteryController(client *Client, user, password, configPath string) *BatteryController {
	bc := &BatteryController{client: client, user: user, password: password}
	switch configPath {
	case "/config", "/config/timeofuse":
		bc.path = "/config/timeofuse"
	case "/api/config", "/api/config/timeofuse":
		bc.path = "/api/config/timeofuse"
	default:
		bc.path = "" // "auto" - wird bei Bedarf per ResolvePath() ermittelt
	}
	return bc
}

// ResolvePath ermittelt bei configPath="auto" (siehe NewBatteryController),
// ob das Zielgeraet /api/config/timeofuse (Firmware >=1.36.5-1) oder das
// aeltere /config/timeofuse anbietet - ein einfacher GET ohne Auth reicht
// dafuer: 404 bedeutet "Pfad existiert hier nicht", jede andere Antwort
// (typischerweise 401, da ohne Digest-Auth) bedeutet "Pfad existiert".
func (b *BatteryController) ResolvePath() error {
	if b.path != "" {
		return nil
	}
	resp, err := b.client.http.Get(b.client.baseURL + "/api/config/timeofuse")
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			b.path = "/api/config/timeofuse"
			return nil
		}
	}
	b.path = "/config/timeofuse"
	return nil
}

// SetHold(true) sperrt die Entladung dauerhaft (0-24 Uhr, alle Wochentage),
// SetHold(false) gibt wieder frei (leeres timeofuse-Array). Body-Form
// verifiziert gegen evcc-io/evcc's fronius-solarapi-v1.yaml-Template, siehe
// Plan-Datei.
func (b *BatteryController) SetHold(hold bool) error {
	if err := b.ResolvePath(); err != nil {
		return err
	}
	var body []byte
	var err error
	if hold {
		body, err = json.Marshal(map[string]any{
			"timeofuse": []map[string]any{
				{
					"Active":       true,
					"Power":        0,
					"ScheduleType": "DISCHARGE_MAX",
					"TimeTable":    map[string]string{"Start": "00:00", "End": "23:59"},
					"Weekdays": map[string]bool{
						"Mon": true, "Tue": true, "Wed": true, "Thu": true,
						"Fri": true, "Sat": true, "Sun": true,
					},
				},
			},
		})
	} else {
		body, err = json.Marshal(map[string]any{"timeofuse": []map[string]any{}})
	}
	if err != nil {
		return fmt.Errorf("timeofuse-Body konnte nicht gebaut werden: %w", err)
	}
	status, respBody, err := b.digestRequest(http.MethodPost, b.path, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("POST %s: HTTP %d: %s", b.path, status, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// --- HTTP Digest Auth (RFC 2617/7616), minimale eigene Implementierung ----
// Bewusst ohne zusaetzliche Go-Abhaengigkeit (anders als EaseeMQTTs SignalR-
// Client, der eine externe Bibliothek braucht) - nur MD5/qop=auth wird
// unterstuetzt, das ist alles, was Fronius' Web-UI-Login verlangt.

var challengeParamRe = regexp.MustCompile(`(\w+)=("[^"]*"|[^,]+)`)

func parseDigestChallenge(header string) map[string]string {
	out := map[string]string{}
	for _, m := range challengeParamRe.FindAllStringSubmatch(header, -1) {
		key := m[1]
		val := strings.Trim(m[2], `"`)
		out[key] = val
	}
	return out
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// digestRequest fuehrt method/path erst UNAUTHENTIFIZIERT aus, um vom Geraet
// die Digest-Challenge (WWW-Authenticate-Header) zu bekommen, und wiederholt
// den Request danach EINMAL mit passendem Authorization-Header. Kein Retry
// bei erneutem 401 (falsches Passwort soll sichtbar fehlschlagen, nicht
// endlos wiederholt werden).
func (b *BatteryController) digestRequest(method, path string, body []byte) (int, []byte, error) {
	url := b.client.baseURL + path

	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	challengeHeader := resp.Header.Get("WWW-Authenticate")
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized || challengeHeader == "" {
		// Geraet verlangt (in diesem Aufbau) keine Auth, oder ein anderer
		// Fehler ist aufgetreten - Antwort unveraendert durchreichen.
		return resp.StatusCode, nil, nil
	}

	ch := parseDigestChallenge(challengeHeader)
	realm, nonce, qop, opaque := ch["realm"], ch["nonce"], ch["qop"], ch["opaque"]
	if realm == "" || nonce == "" {
		return 0, nil, fmt.Errorf("%s %s: unerwartete WWW-Authenticate-Antwort: %q", method, path, challengeHeader)
	}

	ha1 := md5hex(b.user + ":" + realm + ":" + b.password)
	ha2 := md5hex(method + ":" + path)
	nc := "00000001"
	cnonce := randomHex(8)

	var response, authHeader string
	if qop == "auth" || qop == "auth-int" {
		response = md5hex(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
		authHeader = fmt.Sprintf(
			`Digest username="%s", realm="%s", nonce="%s", uri="%s", qop=%s, nc=%s, cnonce="%s", response="%s"`,
			b.user, realm, nonce, path, qop, nc, cnonce, response)
	} else {
		response = md5hex(ha1 + ":" + nonce + ":" + ha2)
		authHeader = fmt.Sprintf(
			`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`,
			b.user, realm, nonce, path, response)
	}
	if opaque != "" {
		authHeader += fmt.Sprintf(`, opaque="%s"`, opaque)
	}

	req2, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", authHeader)
	resp2, err := b.client.http.Do(req2)
	if err != nil {
		return 0, nil, fmt.Errorf("%s %s (authenticated): %w", method, path, err)
	}
	defer resp2.Body.Close()
	respBody, _ := io.ReadAll(resp2.Body)
	return resp2.StatusCode, respBody, nil
}
