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
	"crypto/sha256"
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
// Client, der eine externe Bibliothek braucht). Unterstuetzt: algorithm MD5
// (Default) und SHA-256 (neuere GEN24-Firmware), qop=auth oder ohne qop.
// NICHT unterstuetzt: *-sess-Varianten und qop=auth-int (Body-Hash) - die
// fuehren zu einer klaren Fehlermeldung statt zu einem stillen 401.
// SHA-256 ist gegen die RFC-7616-Testvektoren geprueft (control_test.go),
// aber NICHT gegen ein echtes Geraet mit neuer Firmware.

var challengeParamRe = regexp.MustCompile(`(\w+)=("[^"]*"|[^,]+)`)

func parseDigestChallenge(header string) map[string]string {
	out := map[string]string{}
	for _, m := range challengeParamRe.FindAllStringSubmatch(header, -1) {
		key := m[1]
		val := strings.Trim(strings.TrimSpace(m[2]), `"`)
		out[key] = val
	}
	return out
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// digestHashFor: "SHA-256" und die vereinzelt gesehene Schreibweise
// "SHA256" werden gleich behandelt, Gross-/Kleinschreibung egal.
func digestHashFor(algorithm string) (func(string) string, error) {
	switch strings.ToUpper(strings.ReplaceAll(algorithm, "-", "")) {
	case "", "MD5":
		return md5hex, nil
	case "SHA256":
		return sha256hex, nil
	}
	return nil, fmt.Errorf("Digest-Algorithmus %q wird nicht unterstuetzt", algorithm)
}

// digestQop waehlt aus der Challenge-Liste (z.B. "auth, auth-int") "auth".
// "" = Server verlangt kein qop (RFC-2069-Form).
func digestQop(offered string) (string, error) {
	if strings.TrimSpace(offered) == "" {
		return "", nil
	}
	for _, q := range strings.Split(offered, ",") {
		if strings.TrimSpace(q) == "auth" {
			return "auth", nil
		}
	}
	return "", fmt.Errorf("Digest-qop %q wird nicht unterstuetzt (nur auth)", offered)
}

// digestAuthorization baut den Authorization-Header fuer eine geparste
// Challenge. cnonce wird uebergeben (statt intern erzeugt), damit die
// RFC-7616-Testvektoren nachrechenbar sind.
func digestAuthorization(ch map[string]string, user, password, method, uri, cnonce string) (string, error) {
	realm, nonce, opaque, algorithm := ch["realm"], ch["nonce"], ch["opaque"], ch["algorithm"]
	if realm == "" || nonce == "" {
		return "", fmt.Errorf("Digest-Challenge ohne realm/nonce")
	}
	h, err := digestHashFor(algorithm)
	if err != nil {
		return "", err
	}
	qop, err := digestQop(ch["qop"])
	if err != nil {
		return "", err
	}
	ha1 := h(user + ":" + realm + ":" + password)
	ha2 := h(method + ":" + uri)
	nc := "00000001"

	var authHeader string
	if qop != "" {
		response := h(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
		authHeader = fmt.Sprintf(
			`Digest username="%s", realm="%s", nonce="%s", uri="%s", qop=%s, nc=%s, cnonce="%s", response="%s"`,
			user, realm, nonce, uri, qop, nc, cnonce, response)
	} else {
		response := h(ha1 + ":" + nonce + ":" + ha2)
		authHeader = fmt.Sprintf(
			`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`,
			user, realm, nonce, uri, response)
	}
	if algorithm != "" {
		authHeader += ", algorithm=" + algorithm
	}
	if opaque != "" {
		authHeader += fmt.Sprintf(`, opaque="%s"`, opaque)
	}
	return authHeader, nil
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
	// Neuere Fronius-Firmware schickt die Challenge (nach Community-
	// Berichten, nicht selbst verifiziert) in X-WWW-Authenticate, damit der
	// Browser keinen eigenen Login-Dialog oeffnet - beide pruefen.
	challengeHeader := resp.Header.Get("WWW-Authenticate")
	if challengeHeader == "" {
		challengeHeader = resp.Header.Get("X-WWW-Authenticate")
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		// Geraet verlangt (in diesem Aufbau) keine Auth, oder ein anderer
		// Fehler ist aufgetreten - Antwort unveraendert durchreichen.
		return resp.StatusCode, nil, nil
	}
	if challengeHeader == "" {
		return 0, nil, fmt.Errorf("%s %s: HTTP 401 ohne Digest-Challenge (weder WWW-Authenticate noch X-WWW-Authenticate)", method, path)
	}

	authHeader, err := digestAuthorization(parseDigestChallenge(challengeHeader), b.user, b.password, method, path, randomHex(8))
	if err != nil {
		return 0, nil, fmt.Errorf("%s %s: %w (Challenge: %q)", method, path, err, challengeHeader)
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
