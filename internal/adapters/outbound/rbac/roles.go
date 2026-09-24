package rbac

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	rbacdomain "github.com/turahe/blog-api/internal/core/rbac/domain"
	rbacports "github.com/turahe/blog-api/internal/core/rbac/ports"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ rbacports.RoleAssigner = (*RoleStore)(nil)

// RoleStore writes role data to the relational tables and mirrors it into the
// Casbin enforcer so authorization sees changes without a restart.
type RoleStore struct {
	db       *gorm.DB
	enforcer *Enforcer
}

// NewRoleStore returns a RoleStore over db and enforcer.
func NewRoleStore(db *gorm.DB, enforcer *Enforcer) *RoleStore {
	return &RoleStore{db: db, enforcer: enforcer}
}

// CheckRoles implements rbacports.RoleAssigner.
func (s *RoleStore) CheckRoles(ctx context.Context, names []string) error {
	_, err := roleIDs(s.db.WithContext(ctx), names)
	return err
}

// AssignRoles implements rbacports.RoleAssigner.
func (s *RoleStore) AssignRoles(ctx context.Context, userID uuid.UUID, names []string) error {
	names = dedupe(names)
	if len(names) == 0 {
		return nil
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids, err := roleIDs(tx, names)
		if err != nil {
			return err
		}

		var userIDs []int64
		if err := tx.Table("users").Where("uuid = ?", userID).Limit(1).Pluck("id", &userIDs).Error; err != nil {
			return fmt.Errorf("resolve user id: %w", err)
		}

		if len(userIDs) == 0 {
			return fmt.Errorf("user %s not found", userID)
		}

		now := time.Now().UTC()
		for _, roleID := range ids {
			row := persistence.UserRoleModel{UserID: userIDs[0], RoleID: roleID, CreatedAt: now}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return fmt.Errorf("assign role: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	for _, name := range names {
		if err := s.enforcer.AddRoleForUser(ctx, userID, name); err != nil {
			return fmt.Errorf("sync role %q to enforcer: %w", name, err)
		}
	}

	return nil
}

// roleIDs resolves every name to a role id, failing with ErrRoleNotFound on the first unknown name.
func roleIDs(db *gorm.DB, names []string) ([]int64, error) {
	names = dedupe(names)
	if len(names) == 0 {
		return nil, nil
	}

	var roles []persistence.RoleModel
	if err := db.Where("name IN ?", names).Find(&roles).Error; err != nil {
		return nil, fmt.Errorf("load roles: %w", err)
	}

	byName := make(map[string]int64, len(roles))
	for _, role := range roles {
		byName[role.Name] = role.ID
	}

	ids := make([]int64, 0, len(names))
	for _, name := range names {
		id, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("%w: %s", rbacdomain.ErrRoleNotFound, name)
		}

		ids = append(ids, id)
	}

	return ids, nil
}

func dedupe(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name != "" && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}

	return out
}
