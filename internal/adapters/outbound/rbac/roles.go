package rbac

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	rbacdomain "github.com/turahe/blog-api/internal/core/rbac/domain"
	rbacports "github.com/turahe/blog-api/internal/core/rbac/ports"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ rbacports.RoleRepository = (*RoleStore)(nil)

const (
	ptypePolicy   = "p"
	ptypeGrouping = "g"
)

// RoleStore writes role data to the relational tables and to casbin_rules in one
// transaction, then reloads the enforcer so authorization sees the change
// without a restart.
type RoleStore struct {
	db       *gorm.DB
	enforcer *Enforcer
	notifier PolicyNotifier
}

// PolicyNotifier tells other instances that the policy changed.
type PolicyNotifier interface {
	Notify(ctx context.Context)
}

// NewRoleStore returns a RoleStore over db and enforcer.
func NewRoleStore(db *gorm.DB, enforcer *Enforcer) *RoleStore {
	return &RoleStore{db: db, enforcer: enforcer}
}

// WithNotifier announces every committed write through n.
func (s *RoleStore) WithNotifier(n PolicyNotifier) *RoleStore {
	s.notifier = n
	return s
}

// write runs fn in a transaction, reloads the enforcer policy after commit, and
// announces the change to other instances.
func (s *RoleStore) write(ctx context.Context, fn func(tx *gorm.DB) error) error {
	if err := s.db.WithContext(ctx).Transaction(fn); err != nil {
		return err
	}

	if s.notifier != nil {
		defer s.notifier.Notify(ctx)
	}

	if err := s.enforcer.Reload(); err != nil {
		return fmt.Errorf("reload casbin policy: %w", err)
	}

	return nil
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

	return s.write(ctx, func(tx *gorm.DB) error {
		ids, err := roleIDs(tx, names)
		if err != nil {
			return err
		}

		uid, err := userRowID(tx, userID)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		for i, roleID := range ids {
			row := persistence.UserRoleModel{UserID: uid, RoleID: roleID, CreatedAt: now}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return fmt.Errorf("assign role: %w", err)
			}

			if err := addRule(tx, ptypeGrouping, userID.String(), names[i]); err != nil {
				return err
			}
		}

		return nil
	})
}

// ListRoles implements rbacports.RoleRepository.
func (s *RoleStore) ListRoles(ctx context.Context) ([]rbacdomain.Role, error) {
	db := s.db.WithContext(ctx)

	var models []persistence.RoleModel
	if err := db.Order("name").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}

	names := make([]string, len(models))
	for i, m := range models {
		names[i] = m.Name
	}

	perms, err := rolePermissions(db, names)
	if err != nil {
		return nil, err
	}

	roles := make([]rbacdomain.Role, len(models))
	for i, m := range models {
		roles[i] = mapRole(m, perms[m.Name])
	}

	return roles, nil
}

// FindRole implements rbacports.RoleRepository.
func (s *RoleStore) FindRole(ctx context.Context, name string) (rbacdomain.Role, error) {
	return findRole(s.db.WithContext(ctx), name)
}

// CreateRole implements rbacports.RoleRepository.
func (s *RoleStore) CreateRole(ctx context.Context, role rbacdomain.Role) (rbacdomain.Role, error) {
	err := s.write(ctx, func(tx *gorm.DB) error {
		if _, err := roleModel(tx, role.Name); err == nil {
			return rbacdomain.ErrRoleExists
		} else if !errors.Is(err, rbacdomain.ErrRoleNotFound) {
			return err
		}

		now := time.Now().UTC()
		model := persistence.RoleModel{UUID: uuid.New(), Name: role.Name, Description: optional(role.Description), CreatedAt: now, UpdatedAt: now}

		if err := tx.Create(&model).Error; err != nil {
			if persistence.IsUniqueViolation(err) {
				return rbacdomain.ErrRoleExists
			}

			return fmt.Errorf("create role: %w", err)
		}

		return grantPermissions(tx, model, role.Permissions, now)
	})
	if err != nil {
		return rbacdomain.Role{}, err
	}

	return s.FindRole(ctx, role.Name)
}

// UpdateRole implements rbacports.RoleRepository.
func (s *RoleStore) UpdateRole(ctx context.Context, name, description string) (rbacdomain.Role, error) {
	db := s.db.WithContext(ctx)

	result := db.Model(&persistence.RoleModel{}).Where("name = ?", name).
		Updates(map[string]any{"description": optional(description), "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return rbacdomain.Role{}, fmt.Errorf("update role: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return rbacdomain.Role{}, fmt.Errorf("%w: %s", rbacdomain.ErrRoleNotFound, name)
	}

	return findRole(db, name)
}

// DeleteRole implements rbacports.RoleRepository. user_roles and role_permissions
// rows go with the role through ON DELETE CASCADE.
func (s *RoleStore) DeleteRole(ctx context.Context, name string) error {
	return s.write(ctx, func(tx *gorm.DB) error {
		model, err := roleModel(tx, name)
		if err != nil {
			return err
		}

		if err := tx.Delete(&persistence.RoleModel{}, model.ID).Error; err != nil {
			return fmt.Errorf("delete role: %w", err)
		}

		if err := tx.Where("ptype = ? AND v0 = ?", ptypePolicy, name).Delete(&ruleRow{}).Error; err != nil {
			return fmt.Errorf("delete role grants: %w", err)
		}

		if err := tx.Where("ptype = ? AND v1 = ?", ptypeGrouping, name).Delete(&ruleRow{}).Error; err != nil {
			return fmt.Errorf("delete role assignments: %w", err)
		}

		return nil
	})
}

// SetRolePermissions implements rbacports.RoleRepository.
func (s *RoleStore) SetRolePermissions(ctx context.Context, name string, keys []string) (rbacdomain.Role, error) {
	err := s.write(ctx, func(tx *gorm.DB) error {
		model, err := roleModel(tx, name)
		if err != nil {
			return err
		}

		if err := tx.Where("role_id = ?", model.ID).Delete(&persistence.RolePermissionModel{}).Error; err != nil {
			return fmt.Errorf("clear role permissions: %w", err)
		}

		if err := tx.Where("ptype = ? AND v0 = ?", ptypePolicy, name).Delete(&ruleRow{}).Error; err != nil {
			return fmt.Errorf("clear role grants: %w", err)
		}

		now := time.Now().UTC()
		if err := tx.Model(&persistence.RoleModel{}).Where("id = ?", model.ID).Update("updated_at", now).Error; err != nil {
			return fmt.Errorf("touch role: %w", err)
		}

		return grantPermissions(tx, model, keys, now)
	})
	if err != nil {
		return rbacdomain.Role{}, err
	}

	return s.FindRole(ctx, name)
}

// ListPermissions implements rbacports.RoleRepository.
func (s *RoleStore) ListPermissions(ctx context.Context) ([]rbacdomain.Permission, error) {
	var models []persistence.PermissionModel
	if err := s.db.WithContext(ctx).Order("key").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}

	out := make([]rbacdomain.Permission, len(models))
	for i, m := range models {
		out[i] = rbacdomain.Permission{UUID: m.UUID, Key: m.Key, Description: deref(m.Description)}
	}

	return out, nil
}

// CheckPermissions implements rbacports.RoleRepository.
func (s *RoleStore) CheckPermissions(ctx context.Context, keys []string) error {
	_, err := permissionIDs(s.db.WithContext(ctx), keys)
	return err
}

// UserRoles implements rbacports.RoleRepository.
func (s *RoleStore) UserRoles(ctx context.Context, userID uuid.UUID) ([]string, error) {
	db := s.db.WithContext(ctx)

	uid, err := userRowID(db, userID)
	if err != nil {
		return nil, err
	}

	names := []string{}
	err = db.Table("roles").
		Joins("INNER JOIN user_roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ?", uid).
		Order("roles.name").
		Pluck("roles.name", &names).Error
	if err != nil {
		return nil, fmt.Errorf("list user roles: %w", err)
	}

	return names, nil
}

// RevokeRole implements rbacports.RoleRepository.
func (s *RoleStore) RevokeRole(ctx context.Context, userID uuid.UUID, name string) error {
	return s.write(ctx, func(tx *gorm.DB) error {
		model, err := roleModel(tx, name)
		if err != nil {
			return err
		}

		uid, err := userRowID(tx, userID)
		if err != nil {
			return err
		}

		if err := tx.Where("user_id = ? AND role_id = ?", uid, model.ID).Delete(&persistence.UserRoleModel{}).Error; err != nil {
			return fmt.Errorf("revoke role: %w", err)
		}

		err = tx.Where("ptype = ? AND v0 = ? AND v1 = ?", ptypeGrouping, userID.String(), name).Delete(&ruleRow{}).Error
		if err != nil {
			return fmt.Errorf("revoke role grant: %w", err)
		}

		return nil
	})
}

func findRole(db *gorm.DB, name string) (rbacdomain.Role, error) {
	model, err := roleModel(db, name)
	if err != nil {
		return rbacdomain.Role{}, err
	}

	perms, err := rolePermissions(db, []string{name})
	if err != nil {
		return rbacdomain.Role{}, err
	}

	return mapRole(model, perms[name]), nil
}

func roleModel(db *gorm.DB, name string) (persistence.RoleModel, error) {
	var model persistence.RoleModel

	err := db.Where("name = ?", name).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model, fmt.Errorf("%w: %s", rbacdomain.ErrRoleNotFound, name)
	}

	if err != nil {
		return model, fmt.Errorf("load role: %w", err)
	}

	return model, nil
}

// rolePermissions reads each role's grants from casbin_rules, the policy the
// enforcer evaluates, so the reported set is exactly what authorization uses.
func rolePermissions(db *gorm.DB, names []string) (map[string][]string, error) {
	out := make(map[string][]string, len(names))
	if len(names) == 0 {
		return out, nil
	}

	var rows []ruleRow
	if err := db.Where("ptype = ? AND v0 IN ?", ptypePolicy, names).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load role grants: %w", err)
	}

	for _, row := range rows {
		if !slices.Contains(out[row.V0], row.V1) {
			out[row.V0] = append(out[row.V0], row.V1)
		}
	}

	for name := range out {
		sort.Strings(out[name])
	}

	return out, nil
}

func grantPermissions(tx *gorm.DB, role persistence.RoleModel, keys []string, now time.Time) error {
	ids, err := permissionIDs(tx, keys)
	if err != nil {
		return err
	}

	for i, permID := range ids {
		row := persistence.RolePermissionModel{RoleID: role.ID, PermissionID: permID, CreatedAt: now}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return fmt.Errorf("grant permission: %w", err)
		}

		if err := addRule(tx, ptypePolicy, role.Name, keys[i]); err != nil {
			return err
		}
	}

	return nil
}

// addRule inserts a two-field Casbin rule unless it already exists.
func addRule(tx *gorm.DB, ptype, v0, v1 string) error {
	var count int64
	if err := tx.Model(&ruleRow{}).Where("ptype = ? AND v0 = ? AND v1 = ?", ptype, v0, v1).Count(&count).Error; err != nil {
		return fmt.Errorf("check casbin rule: %w", err)
	}

	if count > 0 {
		return nil
	}

	if err := tx.Create(policyRow(ptype, []string{v0, v1})).Error; err != nil {
		return fmt.Errorf("write casbin rule: %w", err)
	}

	return nil
}

func userRowID(db *gorm.DB, userID uuid.UUID) (int64, error) {
	var ids []int64
	if err := db.Table("users").Where("uuid = ? AND deleted_at IS NULL", userID).Limit(1).Pluck("id", &ids).Error; err != nil {
		return 0, fmt.Errorf("resolve user id: %w", err)
	}

	if len(ids) == 0 {
		return 0, rbacdomain.ErrUserNotFound
	}

	return ids[0], nil
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

// permissionIDs resolves every key to a permission id, in order, failing with
// ErrPermissionNotFound on the first unregistered key.
func permissionIDs(db *gorm.DB, keys []string) ([]int64, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	var perms []persistence.PermissionModel
	if err := db.Where("key IN ?", keys).Find(&perms).Error; err != nil {
		return nil, fmt.Errorf("load permissions: %w", err)
	}

	byKey := make(map[string]int64, len(perms))
	for _, p := range perms {
		byKey[p.Key] = p.ID
	}

	ids := make([]int64, 0, len(keys))
	for _, key := range keys {
		id, ok := byKey[key]
		if !ok {
			return nil, fmt.Errorf("%w: %s", rbacdomain.ErrPermissionNotFound, key)
		}

		ids = append(ids, id)
	}

	return ids, nil
}

func mapRole(m persistence.RoleModel, perms []string) rbacdomain.Role {
	if perms == nil {
		perms = []string{}
	}

	return rbacdomain.Role{
		UUID: m.UUID, Name: m.Name, Description: deref(m.Description), Permissions: perms,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func optional(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
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
