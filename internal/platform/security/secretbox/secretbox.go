// Package secretbox encrypts small secrets at rest with AES-256-GCM and
// computes keyed hashes, both derived from one application master key.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	// KeySize is the master key length in bytes.
	KeySize = 32
	version = "v1:"
)

// ErrCiphertext reports a value that is malformed or fails authentication.
var ErrCiphertext = errors.New("secretbox: invalid ciphertext")

// Box encrypts, decrypts, and MACs with subkeys of one master key.
type Box struct {
	aead   cipher.AEAD
	macKey []byte
}

// New returns a Box for a 32-byte master key.
func New(masterKey []byte) (*Box, error) {
	if len(masterKey) != KeySize {
		return nil, fmt.Errorf("secretbox: master key must be %d bytes, got %d", KeySize, len(masterKey))
	}

	block, err := aes.NewCipher(derive(masterKey, "secretbox/encrypt/v1"))
	if err != nil {
		return nil, fmt.Errorf("secretbox: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: %w", err)
	}

	return &Box{aead: aead, macKey: derive(masterKey, "secretbox/mac/v1")}, nil
}

// ParseKey decodes a base64 (standard or URL, padded or not) master key.
func ParseKey(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)

	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if key, err := enc.DecodeString(encoded); err == nil {
			if len(key) != KeySize {
				return nil, fmt.Errorf("secretbox: key decodes to %d bytes, want %d", len(key), KeySize)
			}

			return key, nil
		}
	}

	return nil, errors.New("secretbox: key is not valid base64")
}

// Encrypt returns a versioned, base64 ciphertext of plaintext.
func (b *Box) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secretbox nonce: %w", err)
	}

	sealed := b.aead.Seal(nonce, nonce, plaintext, nil)

	return version + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt.
func (b *Box) Decrypt(ciphertext string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(ciphertext, version)
	if !ok {
		return nil, ErrCiphertext
	}

	sealed, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(sealed) < b.aead.NonceSize() {
		return nil, ErrCiphertext
	}

	nonce, body := sealed[:b.aead.NonceSize()], sealed[b.aead.NonceSize():]

	plaintext, err := b.aead.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, ErrCiphertext
	}

	return plaintext, nil
}

// MAC returns the hex HMAC-SHA256 of value. Without the master key a stolen
// table of MACs cannot be brute-forced offline.
func (b *Box) MAC(value string) string {
	mac := hmac.New(sha256.New, b.macKey)
	mac.Write([]byte(value))

	return hex.EncodeToString(mac.Sum(nil))
}

func derive(master []byte, label string) []byte {
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte(label))

	return mac.Sum(nil)
}
