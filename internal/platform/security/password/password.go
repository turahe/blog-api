// Package password hashes and verifies passwords with Argon2id.
package password

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/turahe/pkg/crypto"
)

const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024 // KiB (64 MiB)
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
	argonSaltLen uint32 = 16
)

// issueParams is the cost used for new hashes. It is stricter than
// crypto.DefaultArgon2Params so existing hashes and new ones stay at the same work factor.
var issueParams = crypto.Argon2Params{
	Memory:      argonMemory,
	Iterations:  argonTime,
	Parallelism: argonThreads,
	SaltLength:  argonSaltLen,
	KeyLength:   argonKeyLen,
}

// Hasher implements authports.PasswordHasher with Argon2id.
type Hasher struct{}

// New returns a Hasher using Argon2id (t=3, m=64 MiB, p=2).
func New() *Hasher {
	return &Hasher{}
}

// Hash returns the PHC-encoded Argon2id hash of the password.
func (h *Hasher) Hash(password string) (string, error) {
	return crypto.HashArgon2idWithParams([]byte(password), issueParams)
}

// Compare reports whether password matches a PHC-encoded Argon2id hash.
// Parameters outside this API's bounds are rejected before key derivation so a
// stored value cannot force more work than t=10, 256 MiB, and 4 lanes.
func (h *Hasher) Compare(encoded, password string) bool {
	if !acceptableHash(encoded) {
		return false
	}

	ok, err := crypto.VerifyArgon2id(encoded, []byte(password))

	return err == nil && ok
}

// acceptableHash enforces the verification budget. github.com/turahe/pkg/crypto
// allows up to 1 GiB and 64 iterations, which is too much work for a login check.
func acceptableHash(encoded string) bool {
	if len(encoded) > 512 {
		return false
	}

	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}

	var memory, timeCost, threads uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false
	}

	if parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", memory, timeCost, threads) {
		return false
	}

	if timeCost < 1 || timeCost > 10 || memory < 8*1024 || memory > 256*1024 || threads < 1 || threads > 4 {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false
	}

	sum, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(sum) != int(argonKeyLen) {
		return false
	}

	return true
}
