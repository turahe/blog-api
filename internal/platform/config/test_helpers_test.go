package config

import (
	"path/filepath"
	"runtime"
	"testing"
)

// setJWTKeys points Load() at the committed local-dev RSA keypair.
func setJWTKeys(t *testing.T) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	t.Setenv("APP_JWT_PRIVATE_KEY_PATH", filepath.Join(root, "configs/dev/jwt-rsa-private.pem"))
	t.Setenv("APP_JWT_PUBLIC_KEY_PATH", filepath.Join(root, "configs/dev/jwt-rsa-public.pem"))
}
