package pagination

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"slices"
	"sync"
)

// Signer produces HMAC-SHA256 signatures over cursor payloads and verifies
// them. Supports two-key rotation: tokens signed with the previous key are
// still accepted so deployments can rotate PAGINATION_HMAC_KEY without
// invalidating in-flight cursors.
type Signer struct {
	// CurrentKey is always used to sign new cursors. Must be non-empty.
	CurrentKey []byte
	// PreviousKey, when non-empty, is also accepted during verification.
	PreviousKey []byte
}

var (
	signerOnce sync.Once
	signerInst *Signer
	errSigner  error
)

// NewSigner constructs a Signer from explicit key bytes. Panics if
// currentKey is empty (callers should validate config before this point).
func NewSigner(currentKey, previousKey []byte) *Signer {
	if len(currentKey) == 0 {
		panic("pagination: NewSigner called with empty currentKey")
	}
	out := &Signer{CurrentKey: slices.Clone(currentKey)}
	if len(previousKey) > 0 {
		out.PreviousKey = slices.Clone(previousKey)
	}
	return out
}

// NewSignerFromEnv loads the HMAC signing keys from environment variables.
// Preference order: PAGINATION_HMAC_KEY > APP_KEY for the current key.
// Previous key taken from PAGINATION_HMAC_KEY_PREVIOUS if set.
//
// Returns an error if neither PAGINATION_HMAC_KEY nor APP_KEY are set — the
// application MUST never run without cursor signing.
func NewSignerFromEnv() (*Signer, error) {
	current := os.Getenv("PAGINATION_HMAC_KEY")
	if current == "" {
		current = os.Getenv("APP_KEY")
	}
	if current == "" {
		return nil, errors.New("pagination: neither PAGINATION_HMAC_KEY nor APP_KEY are set; " +
			"cursors cannot be signed securely")
	}
	previous := os.Getenv("PAGINATION_HMAC_KEY_PREVIOUS")
	return NewSigner([]byte(current), []byte(previous)), nil
}

// GlobalSigner returns a process-wide Signer lazily initialised from env on
// first call. Subsequent calls return the same instance. Failures are cached:
// the first error is returned every time (prevent infinite retry loops).
func GlobalSigner() (*Signer, error) {
	signerOnce.Do(func() {
		signerInst, errSigner = NewSignerFromEnv()
	})
	return signerInst, errSigner
}

// ResetGlobalSigner forgets the cached Signer so the next call to
// GlobalSigner reloads environment variables. Used exclusively by tests.
func ResetGlobalSigner() {
	signerOnce = sync.Once{}
	signerInst = nil
	errSigner = nil
}

// Sign appends a 32-byte HMAC-SHA256 to payload and returns the combined
// slice: payload || '|' || hex(hmac). Callers are expected to own the
// base64url encoding on top.
func (s *Signer) Sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, s.CurrentKey)
	mac.Write(payload)
	sum := mac.Sum(nil)
	out := make([]byte, 0, len(payload)+1+hex.EncodedLen(len(sum)))
	out = append(out, payload...)
	out = append(out, '|')
	encoder := hex.NewEncoder(&byteSliceWriter{&out})
	_, _ = encoder.Write(sum)
	return out
}

// Verify checks that signedBytes contains a valid HMAC under either the
// current key or the previous key (if set). Returns the original payload
// (signedBytes without the "|<mac>" trailer) on success. On failure,
// returns ErrCursorInvalidSignature wrapped with contextual detail.
func (s *Signer) Verify(signedBytes []byte) ([]byte, error) {
	sep := -1
	for i := len(signedBytes) - 1; i >= 0; i-- {
		if signedBytes[i] == '|' {
			sep = i
			break
		}
	}
	if sep < 1 || sep == len(signedBytes)-1 {
		return nil, ErrCursorInvalidSignature
	}
	payload := signedBytes[:sep]
	sumHex := signedBytes[sep+1:]
	want := make([]byte, hex.DecodedLen(len(sumHex)))
	if _, err := hex.Decode(want, sumHex); err != nil {
		return nil, ErrCursorInvalidSignature
	}
	candidates := [][]byte{s.CurrentKey}
	if len(s.PreviousKey) > 0 {
		candidates = append(candidates, s.PreviousKey)
	}
	for _, key := range candidates {
		mac := hmac.New(sha256.New, key)
		mac.Write(payload)
		if hmac.Equal(mac.Sum(nil), want) {
			return payload, nil
		}
	}
	return nil, ErrCursorInvalidSignature
}

// byteSliceWriter is a tiny io.Writer that appends into a provided *[]byte.
type byteSliceWriter struct{ buf *[]byte }

func (w byteSliceWriter) Write(p []byte) (int, error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}
