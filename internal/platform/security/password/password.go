// Package password hashes and verifies passwords with Argon2id.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024 // KiB (64 MiB)
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// Hasher implements authports.PasswordHasher with Argon2id.
type Hasher struct{}

// New returns a Hasher using Argon2id (t=3, m=64 MiB, p=2).
func New() *Hasher {
	return &Hasher{}
}

// Hash returns the PHC-encoded Argon2id hash of the password.
func (h *Hasher) Hash(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	sum := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonTime,
		argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum),
	), nil
}

// Compare reports whether password matches a PHC-encoded Argon2id hash.
func (h *Hasher) Compare(encoded, password string) bool {
	salt, sum, params, err := decode(encoded)
	if err != nil {
		return false
	}

	got := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, argonKeyLen)

	return subtle.ConstantTimeCompare(got, sum) == 1
}

type argonParams struct {
	time, memory uint32
	threads      uint8
}

func decode(encoded string) (salt, sum []byte, params argonParams, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return nil, nil, params, errors.New("invalid argon2id hash")
	}

	if params, err = parseParams(parts[2], parts[3]); err != nil {
		return nil, nil, params, err
	}

	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return nil, nil, params, errors.New("invalid argon2 salt")
	}

	sum, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(sum) != int(argonKeyLen) {
		return nil, nil, params, errors.New("invalid argon2 hash")
	}

	return salt, sum, params, nil
}

// parseParams reads the "v=" and "m=,t=,p=" segments, bounding cost so a stored
// hash cannot make verification arbitrarily expensive.
func parseParams(versionPart, costPart string) (argonParams, error) {
	var version int
	if _, err := fmt.Sscanf(versionPart, "v=%d", &version); err != nil || version != argon2.Version {
		return argonParams{}, errors.New("unsupported argon2 version")
	}

	var memory, timeCost, threads uint32
	if _, err := fmt.Sscanf(costPart, "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return argonParams{}, errors.New("invalid argon2 parameters")
	}

	if timeCost < 1 || timeCost > 10 || memory < 8*1024 || memory > 256*1024 || threads < 1 || threads > 4 {
		return argonParams{}, errors.New("argon2 parameters out of range")
	}

	return argonParams{time: timeCost, memory: memory, threads: uint8(threads)}, nil
}
