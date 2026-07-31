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
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	"github.com/turahe/blog-api/internal/platform/security/password"
	"gorm.io/gorm"
)

type Options struct {
	AdminEmail    string
	AdminUsername string
	AdminPassword string
	AdminName     string
}

var rolePermissions = map[string][]string{
	"admin": {
		"user.read", "user.create", "user.update",
		"post.read", "post.create", "post.update", "post.publish", "post.archive",
		"tag.create", "tag.update", "tag.delete",
		"settings.read", "settings.update",
		"media.create", "media.delete",
		"comments.moderate",
		"*",
	},
	"editor": {
		"user.read",
		"post.read", "post.create", "post.update", "post.publish", "post.archive",
		"tag.create", "tag.update", "tag.delete",
		"media.create", "media.delete",
		"comments.moderate",
	},
	"author": {
		"post.read", "post.create", "post.update",
		"media.create",
	},
	"moderator": {
		"post.read",
		"comments.moderate",
	},
}

func Run(ctx context.Context, db *gorm.DB, opts Options) error {
	if opts.AdminEmail == "" {
		opts.AdminEmail = "admin@example.com"
	}
	if opts.AdminUsername == "" {
		opts.AdminUsername = "admin"
	}
	if opts.AdminPassword == "" {
		opts.AdminPassword = "ChangeMeNow!123"
	}
	if opts.AdminName == "" {
		opts.AdminName = "Administrator"
	}

	hasher := password.New(0)
	users := persistence.NewUserRepository(db)

	for role, desc := range map[string]string{
		"admin": "Full platform administrator", "editor": "Content editor",
		"author": "Content author", "moderator": "Comment moderator",
	} {
		if err := ensureRole(ctx, db, role, desc); err != nil {
			return err
		}
	}

	for _, perms := range rolePermissions {
		for _, key := range perms {
			if key == "*" {
				continue
			}
			if err := ensurePermission(ctx, db, key); err != nil {
				return err
			}
		}
	}

	enforcer, err := outboundrbac.NewEnforcer(db)
	if err != nil {
		return fmt.Errorf("casbin: %w", err)
	}
	for role, perms := range rolePermissions {
		for _, perm := range perms {
			if err := enforcer.AddPermissionForRole(ctx, role, perm); err != nil {
				return err
			}
		}
	}

	admin, err := users.FindByEmail(ctx, opts.AdminEmail)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		hash, hashErr := hasher.Hash(opts.AdminPassword)
		if hashErr != nil {
			return hashErr
		}
		now := time.Now().UTC()
		admin, err = users.Create(ctx, userdomain.User{
			ID: uuid.New(), Email: strings.ToLower(opts.AdminEmail), Username: opts.AdminUsername,
			FullName: opts.AdminName, PasswordHash: hash, Status: userdomain.StatusActive,
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return fmt.Errorf("create admin: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("lookup admin: %w", err)
	}

	var role persistence.RoleModel
	if err := db.WithContext(ctx).Where("name = ?", "admin").First(&role).Error; err != nil {
		return err
	}
	assignment := persistence.UserRoleModel{UserID: admin.ID, RoleID: role.ID, CreatedAt: time.Now().UTC()}
	if err := db.WithContext(ctx).Where("user_id = ? AND role_id = ?", admin.ID, role.ID).
		FirstOrCreate(&assignment).Error; err != nil {
		return err
	}
	if err := enforcer.AddRoleForUser(ctx, admin.ID, "admin"); err != nil {
		return err
	}
	return enforcer.Save()
}

func ensureRole(ctx context.Context, db *gorm.DB, name, description string) error {
	var role persistence.RoleModel
	err := db.WithContext(ctx).Where("name = ?", name).First(&role).Error
	if err == nil {
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	now := time.Now().UTC()
	desc := description
	return db.WithContext(ctx).Create(&persistence.RoleModel{
		ID: uuid.New(), Name: name, Description: &desc, CreatedAt: now, UpdatedAt: now,
	}).Error
}

func ensurePermission(ctx context.Context, db *gorm.DB, key string) error {
	var perm persistence.PermissionModel
	err := db.WithContext(ctx).Where("key = ?", key).First(&perm).Error
	if err == nil {
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	return db.WithContext(ctx).Create(&persistence.PermissionModel{
		ID: uuid.New(), Key: key, CreatedAt: time.Now().UTC(),
	}).Error
}
