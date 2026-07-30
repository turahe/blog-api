package persistence

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	"gorm.io/gorm"
)

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (userdomain.User, error) {
	var model UserModel
	err := r.db.WithContext(ctx).
		Where("lower(email) = ?", strings.ToLower(email)).
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return userdomain.User{}, err
	}
	if err != nil {
		return userdomain.User{}, err
	}
	return mapUser(model), nil
}

func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (userdomain.User, error) {
	var model UserModel
	err := r.db.WithContext(ctx).First(&model, "id = ?", id).Error
	if err != nil {
		return userdomain.User{}, err
	}
	return mapUser(model), nil
}

func (r *UserRepository) FindByUsernameOrEmail(ctx context.Context, identity string) (userdomain.User, error) {
	identity = strings.ToLower(strings.TrimSpace(identity))
	var model UserModel
	err := r.db.WithContext(ctx).
		Where("lower(email) = ? OR lower(username) = ?", identity, identity).
		First(&model).Error
	if err != nil {
		return userdomain.User{}, err
	}
	return mapUser(model), nil
}

func (r *UserRepository) RecordLogin(ctx context.Context, id uuid.UUID, at time.Time) error {
	return r.db.WithContext(ctx).Model(&UserModel{}).Where("id = ?", id).Updates(map[string]any{
		"last_login_at": at,
		"login_count":   gorm.Expr("login_count + 1"),
		"updated_at":    at,
	}).Error
}

func (r *UserRepository) Create(ctx context.Context, user userdomain.User) (userdomain.User, error) {
	model := UserModel{
		ID:        user.ID,
		Email:     user.Email,
		Username:  user.Username,
		FullName:  user.FullName,
		Status:    string(user.Status),
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}
	if user.PasswordHash != "" {
		model.PasswordHash = &user.PasswordHash
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return userdomain.User{}, err
	}
	return mapUser(model), nil
}

func (r *UserRepository) UpdatePassword(ctx context.Context, id uuid.UUID, hash string, changedAt time.Time) error {
	return r.db.WithContext(ctx).Model(&UserModel{}).Where("id = ?", id).Updates(map[string]any{
		"password_hash":       hash,
		"password_changed_at": changedAt,
		"updated_at":          changedAt,
	}).Error
}

func (r *UserRepository) List(ctx context.Context, page, perPage int) ([]userdomain.User, int64, error) {
	q := r.db.WithContext(ctx).Model(&UserModel{}).Where("deleted_at IS NULL")
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var models []UserModel
	offset := (page - 1) * perPage
	if err := q.Order("created_at DESC").Limit(perPage).Offset(offset).Find(&models).Error; err != nil {
		return nil, 0, err
	}
	users := make([]userdomain.User, 0, len(models))
	for _, model := range models {
		users = append(users, mapUser(model))
	}
	return users, total, nil
}

func (r *UserRepository) ListRoleNames(ctx context.Context, userID uuid.UUID) ([]string, error) {
	var names []string
	err := r.db.WithContext(ctx).
		Table("roles").
		Select("roles.name").
		Joins("INNER JOIN user_roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ?", userID).
		Pluck("roles.name", &names).Error
	return names, err
}

func mapUser(model UserModel) userdomain.User {
	user := userdomain.User{
		ID:                model.ID,
		Email:             model.Email,
		Username:          model.Username,
		FullName:          model.FullName,
		Status:            userdomain.Status(model.Status),
		EmailVerifiedAt:   model.EmailVerifiedAt,
		PasswordChangedAt: model.PasswordChangedAt,
		LastLoginAt:       model.LastLoginAt,
		LoginCount:        model.LoginCount,
		CreatedAt:         model.CreatedAt,
		UpdatedAt:         model.UpdatedAt,
	}
	if model.PasswordHash != nil {
		user.PasswordHash = *model.PasswordHash
	}
	if model.DeletedAt.Valid {
		t := model.DeletedAt.Time
		user.DeletedAt = &t
	}
	return user
}

type SessionRepository struct {
	db *gorm.DB
}

func NewSessionRepository(db *gorm.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

func (r *SessionRepository) Create(ctx context.Context, session authdomain.RefreshSession) (authdomain.RefreshSession, error) {
	model := RefreshSessionModel{
		ID:        session.ID,
		UserID:    session.UserID,
		FamilyID:  session.FamilyID,
		TokenHash: session.TokenHash,
		ExpiresAt: session.ExpiresAt,
		CreatedAt: session.CreatedAt,
	}
	if session.UserAgent != "" {
		model.UserAgent = &session.UserAgent
	}
	if session.IPAddress != "" {
		model.IPAddress = &session.IPAddress
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return authdomain.RefreshSession{}, err
	}
	return session, nil
}

func (r *SessionRepository) FindByTokenHash(ctx context.Context, hash string) (authdomain.RefreshSession, error) {
	var model RefreshSessionModel
	if err := r.db.WithContext(ctx).Where("token_hash = ?", hash).First(&model).Error; err != nil {
		return authdomain.RefreshSession{}, err
	}
	return mapSession(model), nil
}

func (r *SessionRepository) Revoke(ctx context.Context, id uuid.UUID, at time.Time) error {
	return r.db.WithContext(ctx).Model(&RefreshSessionModel{}).Where("id = ?", id).
		Update("revoked_at", at).Error
}

func (r *SessionRepository) RevokeFamily(ctx context.Context, userID, familyID uuid.UUID, at time.Time) error {
	return r.db.WithContext(ctx).Model(&RefreshSessionModel{}).
		Where("user_id = ? AND family_id = ? AND revoked_at IS NULL", userID, familyID).
		Update("revoked_at", at).Error
}

func (r *SessionRepository) Replace(ctx context.Context, oldID, newID uuid.UUID, at time.Time) error {
	return r.db.WithContext(ctx).Model(&RefreshSessionModel{}).Where("id = ?", oldID).Updates(map[string]any{
		"revoked_at":  at,
		"replaced_by": newID,
	}).Error
}

func (r *SessionRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) error {
	return r.db.WithContext(ctx).Model(&RefreshSessionModel{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", at).Error
}

func mapSession(model RefreshSessionModel) authdomain.RefreshSession {
	session := authdomain.RefreshSession{
		ID:         model.ID,
		UserID:     model.UserID,
		FamilyID:   model.FamilyID,
		TokenHash:  model.TokenHash,
		ExpiresAt:  model.ExpiresAt,
		RevokedAt:  model.RevokedAt,
		ReplacedBy: model.ReplacedBy,
		CreatedAt:  model.CreatedAt,
	}
	if model.UserAgent != nil {
		session.UserAgent = *model.UserAgent
	}
	if model.IPAddress != nil {
		session.IPAddress = *model.IPAddress
	}
	return session
}
