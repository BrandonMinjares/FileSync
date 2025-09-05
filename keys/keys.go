package keys

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
)

type KeyPair struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
}

// GenerateOrLoad returns an existing keypair from disk or creates a new one.
// Keys are stored as raw bytes in base64 files (private file is chmod 600).
func GenerateOrLoad(dir string) (*KeyPair, error) {
	if dir == "" {
		dir = ".filesync"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	pubPath := filepath.Join(dir, "peer_pub.b64")
	privPath := filepath.Join(dir, "peer_priv.b64")

	// Try load existing
	pubB64, _ := os.ReadFile(pubPath)
	privB64, _ := os.ReadFile(privPath)
	if len(pubB64) > 0 && len(privB64) > 0 {
		pub, err1 := base64.StdEncoding.DecodeString(string(pubB64))
		priv, err2 := base64.StdEncoding.DecodeString(string(privB64))
		if err1 == nil && err2 == nil && len(pub) == ed25519.PublicKeySize && len(priv) == ed25519.PrivateKeySize {
			return &KeyPair{Public: ed25519.PublicKey(pub), Private: ed25519.PrivateKey(priv)}, nil
		}
	}

	// Generate new
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	// Persist
	if err := os.WriteFile(pubPath, []byte(base64.StdEncoding.EncodeToString(pub)), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(privPath, []byte(base64.StdEncoding.EncodeToString(priv)), 0o600); err != nil {
		return nil, err
	}

	return &KeyPair{Public: pub, Private: priv}, nil
}

// Fingerprint returns a short, shareable ID (you can switch to base32/hex if preferred).
func (k *KeyPair) Fingerprint() (string, error) {
	if k == nil || len(k.Public) == 0 {
		return "", errors.New("no public key")
	}
	// Shorten for UX; for uniqueness you can keep full length:
	// base64(pub) is ~44 chars; here we show first 16 for display.
	fp := base64.StdEncoding.EncodeToString(k.Public)
	if len(fp) > 16 {
		fp = fp[:16]
	}
	return fp, nil
}
