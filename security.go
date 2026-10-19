package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"sync"
)

// ---------- allowed sites ----------

type originStore struct {
	mu      sync.Mutex
	path    string
	Allowed map[string]bool `json:"allowed"`
	onAdd   func(origin string)
}

var origins = &originStore{Allowed: map[string]bool{}}

func (s *originStore) load() {
	s.path = filepath.Join(dataDir(), "origins.json")
	if b, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(b, s)
	}
	if s.Allowed == nil {
		s.Allowed = map[string]bool{}
	}
}

func (s *originStore) save() {
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(s.path, b, 0o644)
}

func (s *originStore) list() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.Allowed))
	for k := range s.Allowed {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *originStore) get(origin string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Allowed[origin]
}

func (s *originStore) remove(origin string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Allowed, origin)
	s.save()
}

func preconfigured(origin string) bool {
	for _, p := range cfg.AllowedOrigins {
		if ok, _ := path.Match(p, origin); ok {
			return true
		}
	}
	return false
}

// originAllowed decides whether a website may print. With prompt=true an
// unknown site triggers a native "allow this site?" dialog (remembered).
func originAllowed(origin string, prompt bool) bool {
	if origin == "" || origin == "null" {
		return true // non-browser clients (scripts, desktop apps)
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	if preconfigured(origin) {
		return true
	}
	origins.mu.Lock()
	defer origins.mu.Unlock()
	if v, ok := origins.Allowed[origin]; ok {
		return v
	}
	if !prompt {
		return false
	}
	ok := askYesNo(appName, fmt.Sprintf(
		"El sitio web:\n\n%s\n\nquiere imprimir a través de %s.\n\n¿Permitir siempre este sitio?", origin, appName))
	origins.Allowed[origin] = ok
	origins.save()
	if origins.onAdd != nil {
		go origins.onAdd(origin)
	}
	return ok
}

// ---------- signed authorisation (Ed25519) ----------
//
// The agent sends a random nonce when a connection opens. The ERP backend
// signs  nonce + "|" + origin  with its private key and the web page forwards
// the signature. Sites signed by a trusted key never see a prompt.

type trustedKey struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"` // base64, raw 32-byte Ed25519 key
}

var trustedKeys = map[string]ed25519.PublicKey{}

func loadKeys() {
	for _, p := range []string{
		filepath.Join(exeDir(), "trusted_keys.json"),
		filepath.Join(dataDir(), "trusted_keys.json"),
	} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var list []trustedKey
		if json.Unmarshal(b, &list) != nil {
			continue
		}
		for _, k := range list {
			raw, err := base64.StdEncoding.DecodeString(k.PublicKey)
			if err == nil && len(raw) == ed25519.PublicKeySize {
				trustedKeys[k.ID] = ed25519.PublicKey(raw)
			}
		}
	}
}

func newNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func verifyAuth(keyID, nonce, origin, sigB64 string) bool {
	pub, ok := trustedKeys[keyID]
	if !ok {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return false
	}
	return ed25519.Verify(pub, []byte(nonce+"|"+origin), sig)
}

// genKeys creates a key pair. The private key (PEM) stays with the ERP
// backend; the .trusted_keys.json file goes next to ImprentaLabs.exe.
func genKeys(dir string) (string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	id := newNonce()[:8]
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(filepath.Join(dir, "imprenta-labs-"+id+".private.pem"), pemBytes, 0o600); err != nil {
		return "", err
	}
	list := []trustedKey{{ID: id, Name: "ERP", PublicKey: base64.StdEncoding.EncodeToString(pub)}}
	b, _ := json.MarshalIndent(list, "", "  ")
	return id, os.WriteFile(filepath.Join(dir, "imprenta-labs-"+id+".trusted_keys.json"), b, 0o644)
}
