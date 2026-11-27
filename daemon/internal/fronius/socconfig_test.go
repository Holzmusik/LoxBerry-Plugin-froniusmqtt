package fronius

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseSocSettingsNested(t *testing.T) {
	var doc any
	json.Unmarshal([]byte(`{"x":{"BAT_M0_SOC_MAX":100,"BAT_M0_SOC_MIN":28.0,"BAT_M0_SOC_MODE":"manual"}}`), &doc)
	s := parseSocSettings(doc)
	if s.Min == nil || *s.Min != 28 || s.Max == nil || *s.Max != 100 || s.Mode != "manual" {
		t.Fatalf("%+v", s)
	}
	json.Unmarshal([]byte(`[{"foo":1}]`), &doc)
	if s := parseSocSettings(doc); s.Min != nil || s.Mode != "" {
		t.Fatalf("leere Antwort: %+v", s)
	}
}

func TestCheckWriteResponse(t *testing.T) {
	ok := `{"errors":[],"permissionFailure":[],"unknownNodes":[],"validationErrors":[],"writeFailure":[],"writeSuccess":["BAT_M0_SOC_MIN"]}`
	if err := checkWriteResponse([]byte(ok)); err != nil {
		t.Fatal(err)
	}
	bad := `{"errors":[],"unknownNodes":["BAT_M0_SOC_MIN"],"validationErrors":[],"writeSuccess":[]}`
	if err := checkWriteResponse([]byte(bad)); err == nil || !strings.Contains(err.Error(), "unknownNodes") {
		t.Fatalf("err = %v", err)
	}
	if err := checkWriteResponse([]byte("kein json")); err != nil {
		t.Fatalf("unbekanntes Format soll kein Fehler sein: %v", err)
	}
}

// Ende-zu-Ende gegen einen Fake-Wechselrichter mit X-WWW-Authenticate:
// exakt der Body der Fronius-Weboberflaeche, Pfad /api/config/batteries.
func TestSetSocMinAndRead(t *testing.T) {
	const challenge = `Digest realm="Webinterface area", nonce="n1", qop="auth", algorithm="SHA256"`
	var gotBody, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/config/timeofuse" && r.Header.Get("Authorization") == "" {
			w.Header().Set("X-WWW-Authenticate", challenge)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Set("X-WWW-Authenticate", challenge)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		got := parseDigestChallenge(auth)
		want, _ := digestAuthorization(parseDigestChallenge(challenge), "technician", "pw", r.Method, r.URL.Path, got["cnonce"], false)
		if parseDigestChallenge(want)["response"] != got["response"] {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		gotPath = r.URL.Path
		if r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			io.WriteString(w, `{"errors":[],"permissionFailure":[],"unknownNodes":[],"validationErrors":[],"writeFailure":[],"writeSuccess":["BAT_M0_SOC_MIN"]}`)
			return
		}
		io.WriteString(w, `{"BAT_M0_SOC_MIN":28,"BAT_M0_SOC_MAX":100,"BAT_M0_SOC_MODE":"manual"}`)
	}))
	defer srv.Close()

	bc := NewBatteryController(NewClient(srv.Listener.Addr().String(), 2*time.Second), "technician", "pw", "auto")
	if err := bc.SetSocMin(30); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/config/batteries" || gotBody != `{"BAT_M0_SOC_MIN":30,"BAT_M0_SOC_MODE":"manual"}` {
		t.Fatalf("Pfad %q Body %q", gotPath, gotBody)
	}
	st, _, err := bc.ReadBatteryConfig()
	if err != nil || st.Min == nil || *st.Min != 28 || st.Mode != "manual" {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestBatteriesPathOldFirmware(t *testing.T) {
	bc := NewBatteryController(NewClient("x", time.Second), "u", "p", "/config")
	if p := bc.BatteriesPath(); p != "/config/batteries" {
		t.Fatal(p)
	}
}
