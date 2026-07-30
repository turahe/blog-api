package system

import (
	"time"

	"github.com/google/uuid"
)

type Clock struct{}

func (Clock) Now() time.Time { return time.Now().UTC() }

type UUIDGenerator struct{}

func (UUIDGenerator) New() uuid.UUID { return uuid.New() }
