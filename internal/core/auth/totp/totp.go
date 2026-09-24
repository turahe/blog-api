// Package totp implements RFC 6238 time-based one-time passwords (HMAC-SHA1,
// 30-second steps, 6 digits), the parameters authenticator apps assume.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 default; authenticator apps expect SHA-1
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// Period is the length of one time step.
	Period = 30 * time.Second
	// Digits is the code length.
	Digits = 6
	// secretBytes is the RFC 4226 recommended 160-bit secret.
	secretBytes = 20
)

var encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewSecret returns a random secret.
func NewSecret() ([]byte, error) {
	secret := make([]byte, secretBytes)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("totp secret: %w", err)
	}

	return secret, nil
}

// Encode returns the unpadded base32 form users type into authenticator apps.
func Encode(secret []byte) string {
	return encoding.EncodeToString(secret)
}

// URL returns the otpauth:// URI that authenticator apps read from a QR code.
func URL(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", Encode(secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", strconv.Itoa(Digits))
	q.Set("period", strconv.Itoa(int(Period.Seconds())))

	return "otpauth://totp/" + label + "?" + q.Encode()
}

// Step returns the time step containing t.
func Step(t time.Time) int64 {
	return t.Unix() / int64(Period.Seconds())
}

// Code returns the code for step.
func Code(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // steps are positive

	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	return fmt.Sprintf("%0*d", Digits, value%1_000_000)
}

// Validate reports whether code matches any step within skew steps of t and
// returns that step so callers can refuse to accept it twice.
func Validate(secret []byte, code string, t time.Time, skew int) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != Digits {
		return 0, false
	}

	now := Step(t)
	for delta := -skew; delta <= skew; delta++ {
		step := now + int64(delta)
		if subtle.ConstantTimeCompare([]byte(Code(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}

	return 0, false
}
