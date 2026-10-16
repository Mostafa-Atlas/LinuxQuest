package webhelp

import (
	"crypto/ed25519"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func writeTestHostKey(t *testing.T) (path, fingerprint string) {
	t.Helper()
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i)
	}
	priv := ed25519.NewKeyFromSeed(seed[:])
	block, err := gossh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(t.TempDir(), "ssh_host_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return path, gossh.FingerprintSHA256(signer.PublicKey())
}

func TestPageShowsFingerprintAndPort(t *testing.T) {
	path, fp := writeTestHostKey(t)
	s := New(path, "2222")
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "example.com:8080"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{fp, "ssh-keygen -t ed25519", "example.com", "2222", "keyadd"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

func TestPageWithoutHostKeyStillRenders(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "missing"), "2222")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "still starting") {
		t.Error("expected first-boot notice")
	}
}
