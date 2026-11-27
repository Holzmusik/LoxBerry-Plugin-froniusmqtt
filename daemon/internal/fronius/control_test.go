package fronius

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// RFC 7616, Abschnitt 3.9.1 - offizielle Testvektoren fuer MD5 und SHA-256.
const rfcChallenge = `Digest realm="http-auth@example.org", qop="auth, auth-int", algorithm=%s, nonce="7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v", opaque="FQhe/qaU925kfnzjCev0ciny7QMkPqMAFRtzCUYo5tdS"`

func TestDigestRFC7616Vectors(t *testing.T) {
	cases := map[string]string{
		"MD5":     "8ca523f5e9506fed4657c9700eebdbec",
		"SHA-256": "753927fa0e85d155564e2e272a28d1802ca10daf4496794697cf8db5856cb6c1",
	}
	for alg, want := range cases {
		ch := parseDigestChallenge(strings.Replace(rfcChallenge, "%s", alg, 1))
		hdr, err := digestAuthorization(ch, "Mufasa", "Circle of Life", "GET", "/dir/index.html",
			"f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ", false)
		if err != nil {
			t.Fatalf("%s: %v", alg, err)
		}
		if !strings.Contains(hdr, `response="`+want+`"`) {
			t.Errorf("%s: falsche response in %s", alg, hdr)
		}
		if !strings.Contains(hdr, "algorithm="+alg) || !strings.Contains(hdr, "qop=auth,") {
			t.Errorf("%s: algorithm/qop nicht korrekt zurueckgegeben: %s", alg, hdr)
		}
	}
}

func TestDigestAlgorithmVariants(t *testing.T) {
	for _, alg := range []string{"", "MD5", "md5", "SHA-256", "SHA256", "sha-256"} {
		if _, err := digestHashFor(alg); err != nil {
			t.Errorf("%q: %v", alg, err)
		}
	}
	for _, alg := range []string{"MD5-sess", "SHA-256-sess", "SHA-512-256"} {
		if _, err := digestHashFor(alg); err == nil {
			t.Errorf("%q faelschlich akzeptiert", alg)
		}
	}
	if _, err := digestQop("auth-int"); err == nil {
		t.Error("qop=auth-int faelschlich akzeptiert")
	}
}

// Ende-zu-Ende: Challenge kommt (wie bei neuerer Fronius-Firmware berichtet)
// nur in X-WWW-Authenticate, mit SHA-256.
func TestDigestRequestXWWWAuthenticateSHA256(t *testing.T) {
	const challenge = `Digest realm="Webinterface area", nonce="abc123", qop="auth", algorithm="SHA256"`
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Set("X-WWW-Authenticate", challenge)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		gotAuth = auth
		// Server-seitig nachrechnen, mit der cnonce aus dem Request.
		got := parseDigestChallenge(auth)
		want, err := digestAuthorization(parseDigestChallenge(challenge), "customer", "geheim", r.Method, r.URL.Path, got["cnonce"], false)
		if err != nil || parseDigestChallenge(want)["response"] != got["response"] {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient(srv.Listener.Addr().String(), 2*time.Second)
	bc := NewBatteryController(c, "customer", "geheim", "/api/config")
	status, body, err := bc.digestRequest(http.MethodPost, "/api/config/timeofuse", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("HTTP %d, Authorization war: %s", status, gotAuth)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body = %q", body)
	}
	if !strings.Contains(gotAuth, "algorithm=SHA256") || !strings.Contains(gotAuth, `uri="/api/config/timeofuse"`) {
		t.Fatalf("Authorization unvollstaendig: %s", gotAuth)
	}
}

func TestDigestRequest401WithoutChallenge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := NewClient(srv.Listener.Addr().String(), 2*time.Second)
	bc := NewBatteryController(c, "customer", "x", "/api/config")
	if _, _, err := bc.digestRequest(http.MethodPost, "/api/config/timeofuse", nil); err == nil ||
		!strings.Contains(err.Error(), "ohne Digest-Challenge") {
		t.Fatalf("erwartet klaren Fehler, bekam %v", err)
	}
}

// Server, der bei SHA256 HA1 per MD5 erwartet (Fronius-Eigenheit): erster
// Versuch (Standard) scheitert, zweiter (md5HA1) klappt und wird gemerkt.
func TestDigestRequestFallsBackToMD5HA1(t *testing.T) {
	const challenge = `Digest realm="Webinterface area", nonce="n1", qop="auth", algorithm="SHA256"`
	authed := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "" {
			authed++
			got := parseDigestChallenge(auth)
			want, _ := digestAuthorization(parseDigestChallenge(challenge), "technician", "pw", r.Method, r.URL.Path, got["cnonce"], true)
			if parseDigestChallenge(want)["response"] == got["response"] {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		w.Header().Set("X-WWW-Authenticate", challenge)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	bc := NewBatteryController(NewClient(srv.Listener.Addr().String(), 2*time.Second), "technician", "pw", "/api/config")
	res := bc.TestLogin()
	if !res.OK || res.HTTPCode != 200 {
		t.Fatalf("TestLogin = %+v", res)
	}
	if res.Variant != "SHA256 (HA1 per MD5)" || authed != 2 {
		t.Fatalf("Variante %q nach %d Versuchen", res.Variant, authed)
	}
	// Zweiter Request: Variante gemerkt, nur noch EIN authentifizierter Versuch.
	authed = 0
	if res := bc.TestLogin(); !res.OK || authed != 1 {
		t.Fatalf("gemerkte Variante nicht genutzt: %+v, %d Versuche", res, authed)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	const challenge = `Digest realm="r", nonce="n", qop="auth", algorithm="SHA256"`
	authed := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			authed++
		}
		w.Header().Set("X-WWW-Authenticate", challenge)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	bc := NewBatteryController(NewClient(srv.Listener.Addr().String(), 2*time.Second), "technician", "falsch", "/api/config")
	res := bc.TestLogin()
	if res.OK || res.HTTPCode != 401 || !strings.Contains(res.Error, "abgelehnt") {
		t.Fatalf("TestLogin = %+v", res)
	}
	if authed != 2 {
		t.Fatalf("%d authentifizierte Versuche, erwartet genau 2 (Standard + MD5-HA1)", authed)
	}
}

func TestLogin404(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	bc := NewBatteryController(NewClient(srv.Listener.Addr().String(), 2*time.Second), "u", "p", "/api/config")
	if res := bc.TestLogin(); res.OK || res.HTTPCode != 404 {
		t.Fatalf("TestLogin = %+v", res)
	}
}
