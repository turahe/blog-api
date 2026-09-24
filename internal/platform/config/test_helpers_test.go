package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

// setJWTKeys points Load() at a throwaway RSA keypair written to a temp dir, so
// tests do not depend on the untracked configs/dev keys from `make dev-keys`.
func setJWTKeys(t *testing.T) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}

	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	dir := t.TempDir()
	t.Setenv("APP_JWT_PRIVATE_KEY_PATH", writePEM(t, dir, "jwt-rsa-private.pem", "PRIVATE KEY", privateDER))
	t.Setenv("APP_JWT_PUBLIC_KEY_PATH", writePEM(t, dir, "jwt-rsa-public.pem", "PUBLIC KEY", publicDER))
}

func writePEM(t *testing.T, dir, name, blockType string, der []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	return path
}
