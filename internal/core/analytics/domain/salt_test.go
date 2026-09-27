package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSaltDaysUseUTC(t *testing.T) {
	t.Parallel()

	jakarta := time.FixedZone("WIB", 7*60*60)

	tests := []struct {
		name          string
		now           time.Time
		day, keepFrom string
	}{
		{name: "UTC midday", now: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), day: "2026-09-25", keepFrom: "2026-09-24"},
		{name: "local date ahead of UTC", now: time.Date(2026, 9, 26, 3, 0, 0, 0, jakarta), day: "2026-09-25", keepFrom: "2026-09-24"},
		{name: "month boundary", now: time.Date(2026, 10, 1, 0, 30, 0, 0, time.UTC), day: "2026-10-01", keepFrom: "2026-09-30"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.day, SaltDay(tt.now))
			assert.Equal(t, tt.keepFrom, SaltKeepFrom(tt.now))
		})
	}
}
