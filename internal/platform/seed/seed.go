// Package seed idempotently creates default roles, permissions, the initial administrator,
// and the default notification templates.
package seed

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	outboundrbac "github.com/turahe/blog-api/internal/adapters/outbound/rbac"
	notificationtemplate "github.com/turahe/blog-api/internal/core/notification/template"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	"github.com/turahe/blog-api/internal/platform/security/password"
	"gorm.io/gorm"
)

// Options sets the initial administrator; empty fields use development defaults.
type Options struct {
	AdminEmail    string
	AdminUsername string
	AdminPassword string
	AdminName     string
}

const (
	roleAdmin     = "admin"
	roleEditor    = "editor"
	roleAuthor    = "author"
	roleModerator = "moderator"
	permPostRead  = "post.read"

	// permAdminAccess gates POST /api/v1/admin/auth/login.
	permAdminAccess = "admin.access"

	permCommentModerate = "comment.moderate"
	permCommentDelete   = "comment.delete"

	permRevisionsView    = "post.revisions.view"
	permRevisionsViewAll = "post.revisions.view_all"
	permRevisionsRestore = "post.revisions.restore"
	permSEOView          = "post.seo.view"
	permSEOEdit          = "post.seo.edit"
	permSlugEdit         = "post.slug.edit"
)

var roleDescriptions = map[string]string{
	roleAdmin:     "Full platform administrator",
	roleEditor:    "Content editor",
	roleAuthor:    "Content author",
	roleModerator: "Comment moderator",
}

var rolePermissions = map[string][]string{
	roleAdmin: {
		permAdminAccess,
		"user.read", "user.create", "user.update", "user.profile.read", "user.profile.edit",
		"user.password.admin_reset", "user.activity.read_all", "role.read", "role.manage",
		permPostRead, "post.create", "post.update", "post.publish", "post.delete",
		permRevisionsView, permRevisionsViewAll, permRevisionsRestore,
		permSEOView, permSEOEdit, permSlugEdit,
		"category.read", "category.create", "category.update", "category.delete",
		"tag.create", "tag.update", "tag.delete",
		"settings.read", "settings.update", "settings.history.read",
		"media.create", "media.delete",
		permCommentModerate, permCommentDelete,
		"*",
	},
	roleEditor: {
		permAdminAccess,
		"user.read",
		permPostRead, "post.create", "post.update", "post.publish", "post.delete",
		permRevisionsView, permRevisionsViewAll, permRevisionsRestore,
		permSEOView, permSEOEdit, permSlugEdit,
		"category.read", "category.create", "category.update", "category.delete",
		"tag.create", "tag.update", "tag.delete",
		"media.create", "media.delete",
		permCommentModerate,
	},
	roleAuthor: {
		permAdminAccess,
		permPostRead, "post.create", "post.update",
		permRevisionsView, permRevisionsRestore,
		permSEOView, permSEOEdit, permSlugEdit,
		"media.create",
	},
	roleModerator: {
		permAdminAccess,
		permPostRead,
		permCommentModerate, permCommentDelete,
	},
}

// Run idempotently seeds roles, permissions, Casbin policies, and the initial administrator.
func Run(ctx context.Context, db *gorm.DB, opts Options) error {
	opts = opts.withDefaults()

	enforcer, err := seedRBAC(ctx, db)
	if err != nil {
		return err
	}

	admin, err := ensureAdmin(ctx, db, opts)
	if err != nil {
		return err
	}

	if err := assignRole(ctx, db, admin.ID, roleAdmin); err != nil {
		return err
	}

	if err := enforcer.AddRoleForUser(ctx, admin.UUID, roleAdmin); err != nil {
		return err
	}

	if err := persistence.NewNotificationTemplateRepository(db).SeedDefaults(ctx, notificationtemplate.Defaults()); err != nil {
		return fmt.Errorf("seed notification templates: %w", err)
	}

	return enforcer.Save()
}

func (o Options) withDefaults() Options {
	if o.AdminEmail == "" {
		o.AdminEmail = "admin@example.com"
	}

	if o.AdminUsername == "" {
		o.AdminUsername = "admin"
	}

	if o.AdminPassword == "" {
		// bearer:disable go_gosec_secrets_secrets
		o.AdminPassword = "ChangeMeNow!123"
	}

	if o.AdminName == "" {
		o.AdminName = "Administrator"
	}

	return o
}

func seedRBAC(ctx context.Context, db *gorm.DB) (*outboundrbac.Enforcer, error) {
	for role, desc := range roleDescriptions {
		if err := ensureRole(ctx, db, role, desc); err != nil {
			return nil, err
		}
	}

	for _, perms := range rolePermissions {
		for _, key := range perms {
			if key == "*" {
				continue
			}

			if err := ensurePermission(ctx, db, key); err != nil {
				return nil, err
			}
		}
	}

	if err := mirrorRolePermissions(ctx, db); err != nil {
		return nil, err
	}

	enforcer, err := outboundrbac.NewEnforcer(db)
	if err != nil {
		return nil, err
	}

	for role, perms := range rolePermissions {
		for _, perm := range perms {
			if err := enforcer.AddPermissionForRole(ctx, role, perm); err != nil {
				return nil, err
			}
		}
	}

	return enforcer, nil
}

func ensureAdmin(ctx context.Context, db *gorm.DB, opts Options) (userdomain.User, error) {
	users := persistence.NewUserRepository(db)

	admin, err := users.FindByEmail(ctx, opts.AdminEmail)
	if err == nil {
		return admin, nil
	}

	if !errors.Is(err, userdomain.ErrNotFound) {
		return userdomain.User{}, fmt.Errorf("lookup admin: %w", err)
	}

	hash, err := password.New().Hash(opts.AdminPassword)
	if err != nil {
		return userdomain.User{}, err
	}

	now := time.Now().UTC()

	admin, err = users.Create(ctx, userdomain.User{
		UUID: uuid.New(), Email: strings.ToLower(opts.AdminEmail), Username: opts.AdminUsername,
		FullName: opts.AdminName, PasswordHash: hash, Status: userdomain.StatusActive,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return userdomain.User{}, fmt.Errorf("create admin: %w", err)
	}

	return admin, nil
}

func assignRole(ctx context.Context, db *gorm.DB, userID int64, roleName string) error {
	var role persistence.RoleModel
	if err := db.WithContext(ctx).Where("name = ?", roleName).First(&role).Error; err != nil {
		return err
	}

	assignment := persistence.UserRoleModel{UserID: userID, RoleID: role.ID, CreatedAt: time.Now().UTC()}

	return db.WithContext(ctx).Where("user_id = ? AND role_id = ?", userID, role.ID).
		FirstOrCreate(&assignment).Error
}

func ensureRole(ctx context.Context, db *gorm.DB, name, description string) error {
	var role persistence.RoleModel

	err := db.WithContext(ctx).Where("name = ?", name).First(&role).Error
	if err == nil {
		return nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	now := time.Now().UTC()
	desc := description

	return db.WithContext(ctx).Create(&persistence.RoleModel{
		UUID: uuid.New(), Name: name, Description: &desc, CreatedAt: now, UpdatedAt: now,
	}).Error
}

// mirrorRolePermissions records the seeded grants in role_permissions so the
// relational tables agree with the Casbin policy.
func mirrorRolePermissions(ctx context.Context, db *gorm.DB) error {
	for role, perms := range rolePermissions {
		for _, key := range perms {
			if key == "*" {
				continue
			}

			err := db.WithContext(ctx).Exec(`
				INSERT INTO role_permissions (role_id, permission_id, created_at)
				SELECT r.id, p.id, now() FROM roles r, permissions p
				WHERE r.name = ? AND p.key = ?
				ON CONFLICT DO NOTHING`, role, key).Error
			if err != nil {
				return fmt.Errorf("mirror %s permission %s: %w", role, key, err)
			}
		}
	}

	return nil
}

func ensurePermission(ctx context.Context, db *gorm.DB, key string) error {
	var perm persistence.PermissionModel

	err := db.WithContext(ctx).Where("key = ?", key).First(&perm).Error
	if err == nil {
		return nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	return db.WithContext(ctx).Create(&persistence.PermissionModel{
		UUID: uuid.New(), Key: key, CreatedAt: time.Now().UTC(),
	}).Error
}
