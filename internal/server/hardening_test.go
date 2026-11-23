package server

import (
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jolovicdev/hayduk/internal/engine"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

func TestSecurityHeaders(t *testing.T) {
	_, ts, _ := testServer(t)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	resp, err := client.Get(ts.URL + "/?token=testtoken")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for header, want := range map[string]string{
		"Content-Security-Policy": "frame-ancestors 'none'",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
	} {
		if got := resp.Header.Get(header); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestOperatorTokenBindsIdentity(t *testing.T) {
	e := engine.New(engine.Config{})
	t.Cleanup(e.Shutdown)
	s := New(e, "sharedtoken", Options{
		Version: "test", Team: true,
		Operators: map[string]string{"alice-secret-token": "alice"},
	})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	c := dialWS(t, s, wsURL, "alice-secret-token")

	hello := readMsg(t, c)
	if hello["type"] != "hello" || hello["identity"] != "alice" {
		t.Fatalf("hello %+v", hello)
	}
	readMsg(t, c) // snapshot for bound operator

	// The name is not the browser's to change on a bound token.
	if err := c.WriteJSON(protocol.ClientMessage{
		Type: protocol.KindCommand, ID: 5, Method: protocol.MethodOperatorJoin,
		Operator: "mallory",
	}); err != nil {
		t.Fatal(err)
	}
	if m := readResponse(t, c); m["ok"] != true {
		t.Fatalf("operator.join response %+v", m)
	}
	waitForOperators(t, e, []string{"alice"})
}

func TestSharedTokenStillAllowsChosenName(t *testing.T) {
	e := engine.New(engine.Config{})
	t.Cleanup(e.Shutdown)
	s := New(e, "sharedtoken", Options{Version: "test", Team: true})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	c := dialWS(t, s, wsURL, "sharedtoken")
	hello := readMsg(t, c)
	if hello["identity"] != nil && hello["identity"] != "" {
		t.Fatalf("shared token exported an identity: %+v", hello)
	}
	if err := c.WriteJSON(protocol.ClientMessage{
		Type: protocol.KindCommand, ID: 6, Method: protocol.MethodOperatorJoin,
		Operator: "dana",
	}); err != nil {
		t.Fatal(err)
	}
	readResponse(t, c)
	waitForOperators(t, e, []string{"dana"})
}

// readResponse skips the event/resource/snapshot traffic that arrives between a
// command and its answer.
func readResponse(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	for i := 0; i < 20; i++ {
		m := readMsg(t, c)
		if m["type"] == protocol.KindResponse {
			return m
		}
	}
	t.Fatal("no command response in the stream")
	return nil
}

func waitForOperators(t *testing.T, e *engine.Engine, want []string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		got := e.State().Operators
		if len(got) == len(want) {
			match := true
			for i := range want {
				if got[i] != want[i] {
					match = false
				}
			}
			if match {
				return
			}
		}
		select {
		case <-deadline:
			t.Fatalf("operators %+v, want %+v", got, want)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestListenTLSServesHTTPSAndSetsSecureCookie(t *testing.T) {
	certFile, keyFile := writeTestCertificate(t)
	e := engine.New(engine.Config{})
	t.Cleanup(e.Shutdown)
	s := New(e, "testtoken", Options{Version: "test"})
	url, err := s.ListenTLS("127.0.0.1:0", certFile, keyFile)
	if err != nil {
		t.Fatalf("ListenTLS: %v", err)
	}
	t.Cleanup(s.Close)
	if !strings.HasPrefix(url, "https://") {
		t.Fatalf("advertised %q, want https", url)
	}

	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(url + "/?token=testtoken")
	if err != nil {
		t.Fatalf("https get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want the token redirect", resp.StatusCode)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatalf("cookie %+v", cookies)
	}
}

func TestListenTLSRejectsBadCertificate(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(cert, []byte("not a cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := engine.New(engine.Config{})
	t.Cleanup(e.Shutdown)
	s := New(e, "tok", Options{Version: "test"})
	if _, err := s.ListenTLS("127.0.0.1:0", cert, key); err == nil {
		s.Close()
		t.Fatal("bad certificate accepted")
	}
	if s.ln != nil {
		t.Fatal("listener bound before the certificate was validated")
	}
}

func writeTestCertificate(t *testing.T) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "hayduk test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func TestGzipCompressesStaticIndex(t *testing.T) {
	s, ts, _ := testServer(t)
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: s.cookieName, Value: "testtoken"})
	req.Header.Set("Accept-Encoding", "gzip")
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	gr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gr.Close()
	body, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	if !strings.Contains(strings.ToLower(string(body)), "<!doctype html") {
		t.Fatalf("unexpected decoded body prefix: %q", string(body[:min(len(body), 80)]))
	}
}

func TestQueryTokenSwitchesBoundOperatorAndStripsURL(t *testing.T) {
	e := engine.New(engine.Config{})
	t.Cleanup(e.Shutdown)
	s := New(e, "sharedtoken", Options{
		Version: "test", Team: true,
		Operators: map[string]string{
			"alice-secret-token": "alice",
			"bob-secret-token-2": "bob",
		},
	})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/?token=bob-secret-token-2", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: s.cookieName, Value: "alice-secret-token"})
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("status=%d location=%q, want 302 -> /", resp.StatusCode, resp.Header.Get("Location"))
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Value != "bob-secret-token-2" {
		t.Fatalf("cookies = %+v, want updated bob token", cookies)
	}
}
