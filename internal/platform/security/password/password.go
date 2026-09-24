// Package password hashes and verifies passwords with bcrypt.
package password

import (
	"golang.org/x/crypto/bcrypt"
)

// Hasher implements authports.PasswordHasher with bcrypt.
type Hasher struct {
	cost int
}

// New returns a Hasher; a cost outside bcrypt's valid range uses bcrypt.DefaultCost.
func New(cost int) *Hasher {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		cost = bcrypt.DefaultCost
	}

	return &Hasher{cost: cost}
}

// Hash returns the bcrypt hash of the password.
func (h *Hasher) Hash(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}

// Compare reports whether password matches hash.
func (h *Hasher) Compare(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
