package responses

import (
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"
	auditdomain "github.com/turahe/blog-api/internal/core/audit/domain"
)

const (
	ownerIPv4Prefix = 24
	ownerIPv6Prefix = 48
)

// OwnerActivity is the owner's view of an audit entry: no raw IP, user agent,
// request id, or metadata.
func OwnerActivity(entry auditdomain.Entry) gin.H {
	return gin.H{
		"id":          entry.UUID,
		"category":    entry.Category,
		"action":      entry.Action,
		"result":      entry.Result,
		"ip_prefix":   ipPrefix(entry.IP),
		"device":      device(entry.UserAgent),
		"occurred_at": entry.OccurredAt,
	}
}

// AdminActivity is the full audit entry.
func AdminActivity(entry auditdomain.Entry) gin.H {
	return gin.H{
		"id":            entry.UUID,
		"action":        entry.Action,
		"category":      nullable(entry.Category),
		"result":        entry.Result,
		"status":        entry.Status,
		"actor_id":      entry.ActorID,
		"resource_type": nullable(entry.ResourceType),
		"resource_id":   entry.ResourceID,
		"changes":       entry.Changes,
		"metadata":      entry.Metadata,
		"ip":            nullable(entry.IP),
		"user_agent":    nullable(entry.UserAgent),
		"request_id":    nullable(entry.RequestID),
		"occurred_at":   entry.OccurredAt,
	}
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// ipPrefix coarsens an address to its /24 (IPv4) or /48 (IPv6) network.
func ipPrefix(raw string) *string {
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return nil
	}

	bits := ownerIPv6Prefix
	if addr.Unmap().Is4() {
		addr, bits = addr.Unmap(), ownerIPv4Prefix
	}

	prefix, err := addr.Prefix(bits)
	if err != nil {
		return nil
	}

	s := prefix.String()

	return &s
}

var (
	browsers = []struct{ token, name string }{
		{"Edg/", "Edge"},
		{"OPR/", "Opera"},
		{"Firefox/", "Firefox"},
		{"Chrome/", "Chrome"},
		{"Safari/", "Safari"},
		{"curl/", "curl"},
	}
	systems = []struct{ token, name string }{
		{"Android", "Android"},
		{"iPhone", "iOS"},
		{"iPad", "iOS"},
		{"Windows", "Windows"},
		{"Mac OS X", "macOS"},
		{"Linux", "Linux"},
	}
)

// device reduces a user agent to "Browser on OS".
func device(userAgent string) *string {
	if userAgent == "" {
		return nil
	}

	browser, system := "Unknown browser", ""

	for _, b := range browsers {
		if strings.Contains(userAgent, b.token) {
			browser = b.name
			break
		}
	}

	for _, s := range systems {
		if strings.Contains(userAgent, s.token) {
			system = s.name
			break
		}
	}

	if system != "" {
		browser += " on " + system
	}

	return &browser
}
