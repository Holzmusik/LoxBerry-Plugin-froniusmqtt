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
	// md5HA1: Fronius-Eigenheit (Community-Berichte, nicht offiziell):
	// bei algorithm=SHA256 wird HA1 trotzdem per MD5 gebildet. Wird beim
	// ersten Request ermittelt (siehe digestRequest) und dann beibehalten.
	md5HA1 bool
	// variant: Beschreibung der zuletzt erfolgreichen Digest-Variante,
	// fuer TestLogin/Log.
	variant string
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
// RFC-7616-Testvektoren nachrechenbar sind. md5HA1=true bildet HA1 per MD5
// unabhaengig vom angekuendigten Algorithmus (siehe BatteryController.md5HA1).
func digestAuthorization(ch map[string]string, user, password, method, uri, cnonce string, md5HA1 bool) (string, error) {
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
	if md5HA1 {
		ha1 = md5hex(user + ":" + realm + ":" + password)
	}
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

// challengeOf liest die Digest-Challenge aus WWW-Authenticate oder - bei
// neuerer Fronius-Firmware (auf dem GEN24 des Nutzers bestaetigt,
// 2026-11-27) - aus X-WWW-Authenticate, damit der Browser keinen eigenen
// Login-Dialog oeffnet.
func challengeOf(h http.Header) string {
	if c := h.Get("WWW-Authenticate"); c != "" {
		return c
	}
	return h.Get("X-WWW-Authenticate")
}

func isSHA256(algorithm string) bool {
	return strings.ToUpper(strings.ReplaceAll(algorithm, "-", "")) == "SHA256"
}

func variantName(algorithm string, md5HA1 bool) string {
	if algorithm == "" {
		algorithm = "MD5"
	}
	if md5HA1 && isSHA256(algorithm) {
		return algorithm + " (HA1 per MD5)"
	}
	return algorithm
}

// send fuehrt einen einzelnen Request aus (auth == "" = ohne Authorization).
func (b *BatteryController) send(method, path string, body []byte, auth string) (int, http.Header, []byte, error) {
	req, err := http.NewRequest(method, b.client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := b.client.http.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, respBody, nil
}

// digestRequest fuehrt method/path erst UNAUTHENTIFIZIERT aus, um vom Geraet
// die Digest-Challenge zu bekommen, und wiederholt den Request danach mit
// passendem Authorization-Header. Bei SHA-256 wird nach einem 401 genau
// EINMAL die Variante "HA1 per MD5" probiert (Fronius-Eigenheit, siehe
// md5HA1); die funktionierende Variante wird gemerkt. Sonst kein Retry
// (falsches Passwort soll sichtbar fehlschlagen, nicht endlos wiederholt
// werden).
func (b *BatteryController) digestRequest(method, path string, body []byte) (int, []byte, error) {
	status, hdr, respBody, err := b.send(method, path, body, "")
	if err != nil {
		return 0, nil, err
	}
	if status != http.StatusUnauthorized {
		// Geraet verlangt (in diesem Aufbau) keine Auth, oder ein anderer
		// Fehler ist aufgetreten - Antwort unveraendert durchreichen.
		return status, respBody, nil
	}
	challenge := challengeOf(hdr)
	if challenge == "" {
		return 0, nil, fmt.Errorf("%s %s: HTTP 401 ohne Digest-Challenge (weder WWW-Authenticate noch X-WWW-Authenticate)", method, path)
	}
	ch := parseDigestChallenge(challenge)

	variants := []bool{b.md5HA1}
	if isSHA256(ch["algorithm"]) {
		variants = append(variants, !b.md5HA1)
	}
	for _, v := range variants {
		auth, err := digestAuthorization(ch, b.user, b.password, method, path, randomHex(8), v)
		if err != nil {
			return 0, nil, fmt.Errorf("%s %s: %w (Challenge: %q)", method, path, err, challenge)
		}
		status, hdr, respBody, err = b.send(method, path, body, auth)
		if err != nil {
			return 0, nil, fmt.Errorf("%s %s (authenticated): %w", method, path, err)
		}
		if status != http.StatusUnauthorized {
			b.md5HA1 = v
			b.variant = variantName(ch["algorithm"], v)
			return status, respBody, nil
		}
		// Frische Challenge (neuer nonce) fuer den naechsten Versuch.
		if c := challengeOf(hdr); c != "" {
			ch = parseDigestChallenge(c)
		}
	}
	return status, respBody, nil
}

// LoginResult ist die Antwort von TestLogin - als JSON fuer api.cgi
// ("froniusmqtt battery-test", siehe main.go).
type LoginResult struct {
	OK       bool   `json:"ok"`
	Path     string `json:"path,omitempty"`
	HTTPCode int    `json:"http_code,omitempty"`
	Variant  string `json:"variant,omitempty"`
	Error    string `json:"error,omitempty"`
}

// TestLogin prueft die Zugangsdaten mit einem rein LESENDEN GET auf den
// timeofuse-Endpunkt - mit exakt derselben Digest-Implementierung, die
// SetHold zum Schreiben benutzt. 2xx oder 405 = Login akzeptiert.
func (b *BatteryController) TestLogin() LoginResult {
	if err := b.ResolvePath(); err != nil {
		return LoginResult{Error: err.Error()}
	}
	r := LoginResult{Path: b.path}
	status, _, err := b.digestRequest(http.MethodGet, b.path, nil)
	if err != nil {
		if isConnError(err) {
			r.Error = "Wechselrichter nicht erreichbar: " + err.Error()
		} else {
			r.Error = err.Error()
		}
		return r
	}
	r.HTTPCode = status
	switch {
	case status >= 200 && status < 300, status == http.StatusMethodNotAllowed:
		r.OK, r.Variant = true, b.variant
	case status == http.StatusUnauthorized:
		r.Error = "Login abgelehnt - Benutzername/Passwort pruefen"
	case status == http.StatusNotFound:
		r.Error = "Endpunkt " + b.path + " existiert auf diesem Geraet nicht (HTTP 404) - anderen Endpunkt-Pfad waehlen"
	default:
		r.Error = fmt.Sprintf("Unerwartete Antwort HTTP %d auf %s", status, b.path)
	}
	return r
}
