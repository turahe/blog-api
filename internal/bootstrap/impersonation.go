package bootstrap

import (
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	outboundrbac "github.com/turahe/blog-api/internal/adapters/outbound/rbac"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	"github.com/turahe/blog-api/internal/core/event"
	impports "github.com/turahe/blog-api/internal/core/impersonation/ports"
	impservice "github.com/turahe/blog-api/internal/core/impersonation/service"
	rbacdomain "github.com/turahe/blog-api/internal/core/rbac/domain"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
	"github.com/turahe/blog-api/internal/platform/system"
)

// NewImpersonationService wires staff impersonation. perms and auth may be nil where sessions
// are only expired (app scheduler).
func NewImpersonationService(
	cfg config.Config, db *database.Database, events event.Unit, perms impports.Permissions, auth *authservice.AuthService,
) *impservice.Service {
	var (
		stepUp impports.StepUp
		tokens impports.Tokens
	)

	if auth != nil {
		stepUp, tokens = auth, auth
	}

	return impservice.New(persistence.NewImpersonationRepository(db.GORM), persistence.NewUserRepository(db.GORM),
		perms, stepUp, tokens, system.UUIDGenerator{}, system.Clock{},
		impservice.Config{TTL: cfg.ImpersonationTTL, AdminRole: rbacdomain.ProtectedRole}).WithEvents(events)
}

// impersonationPermissions checks permissions with the live enforcer and reads each user's
// grants from the role store.
type impersonationPermissions struct {
	*outboundrbac.Enforcer
	*outboundrbac.RoleStore
}
