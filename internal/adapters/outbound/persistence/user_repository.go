package persistence

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	"github.com/turahe/blog-api/internal/shared/pagination"
	"gorm.io/gorm"
)

var usersAdminCfg = pagination.CursorConfig{
	Kind: "users_admin",
	Sort: []pagination.SortField{
		{Name: "created_at", Column: "created_at", Dir: pagination.Desc, Type: pagination.TypeTime},
		{Name: "id", Column: "id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

// UserRepository implements the user and auth user repository ports.
type UserRepository struct {
	db *gorm.DB
}

// NewUserRepository returns a UserRepository backed by db.
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// FindByEmail returns the user with the email (case-insensitive) or userdomain.ErrNotFound.
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (userdomain.User, error) {
	return r.findOne(ctx, "find user by email", "lower(email) = ?", strings.ToLower(email))
}

// FindByID returns the user with the given UUID or userdomain.ErrNotFound.
func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (userdomain.User, error) {
	return r.findOne(ctx, "find user by id", "uuid = ?", id)
}

// FindByUsernameOrEmail matches identity against email or username (case-insensitive),
// returning userdomain.ErrNotFound when no user matches.
func (r *UserRepository) FindByUsernameOrEmail(ctx context.Context, identity string) (userdomain.User, error) {
	identity = strings.ToLower(strings.TrimSpace(identity))

	return r.findOne(ctx, "find user by identity", "lower(email) = ? OR lower(username) = ?", identity, identity)
}

func (r *UserRepository) findOne(ctx context.Context, op, query string, args ...any) (userdomain.User, error) {
	var model UserModel

	err := conn(ctx, r.db).Where(query, args...).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return userdomain.User{}, userdomain.ErrNotFound
	}

	if err != nil {
		return userdomain.User{}, fmt.Errorf("%s: %w", op, err)
	}

	return mapUser(model), nil
}

// RecordLogin stamps last_login_at and increments login_count.
func (r *UserRepository) RecordLogin(ctx context.Context, id uuid.UUID, at time.Time) error {
	return conn(ctx, r.db).Model(&UserModel{}).Where("uuid = ?", id).Updates(map[string]any{
		"last_login_at": at,
		"login_count":   gorm.Expr("login_count + 1"),
		"updated_at":    at,
	}).Error
}

// Create inserts a user and returns it with its assigned id.
func (r *UserRepository) Create(ctx context.Context, user userdomain.User) (userdomain.User, error) {
	model := UserModel{
		UUID:            user.UUID,
		Email:           user.Email,
		Username:        user.Username,
		FullName:        user.FullName,
		Status:          string(user.Status),
		EmailVerifiedAt: user.EmailVerifiedAt,
		CreatedAt:       user.CreatedAt,
		UpdatedAt:       user.UpdatedAt,
	}
	if user.PasswordHash != "" {
		model.PasswordHash = &user.PasswordHash
	}

	if err := conn(ctx, r.db).Create(&model).Error; err != nil {
		switch uniqueConstraint(err) {
		case "users_email_unique":
			return userdomain.User{}, authdomain.ErrEmailTaken
		case "users_username_unique":
			return userdomain.User{}, userdomain.ErrUsernameTaken
		}

		return userdomain.User{}, err
	}

	return mapUser(model), nil
}

// UpdatePassword stores a new password hash.
func (r *UserRepository) UpdatePassword(ctx context.Context, id uuid.UUID, hash string, changedAt time.Time) error {
	return conn(ctx, r.db).Model(&UserModel{}).Where("uuid = ?", id).Updates(map[string]any{
		"password_hash":       hash,
		"password_changed_at": changedAt,
		"updated_at":          changedAt,
	}).Error
}

// UpdateEmail sets the address and marks it verified; a live duplicate is authdomain.ErrEmailTaken.
func (r *UserRepository) UpdateEmail(ctx context.Context, id uuid.UUID, email string, verifiedAt time.Time) error {
	err := conn(ctx, r.db).Model(&UserModel{}).Where("uuid = ?", id).Updates(map[string]any{
		"email":             email,
		"email_verified_at": verifiedAt,
		"updated_at":        verifiedAt,
	}).Error
	if isUniqueViolation(err) {
		return authdomain.ErrEmailTaken
	}

	return err
}

// List returns a page of users.
func (r *UserRepository) List(ctx context.Context, filter userdomain.ListFilter) (userdomain.ListResult, error) {
	pr := filter.PageRequest
	if pr.Limit <= 0 {
		pr.Limit = pagination.DefaultPerPage
	}
	var cursorFields map[string]any
	if pr.Mode == pagination.ModeCursor && pr.Cursor != "" {
		decoded, _, err := pagination.DecodeCursor(usersAdminCfg, pr.Cursor)
		if err != nil {
			return userdomain.ListResult{}, err
		}
		if err := pagination.ValidateCursor(usersAdminCfg, decoded); err != nil {
			return userdomain.ListResult{}, err
		}
		cursorFields = decoded
	}
	seek, err := pagination.BuildSeek(usersAdminCfg, pr, cursorFields)
	if err != nil {
		return userdomain.ListResult{}, err
	}

	q := conn(ctx, r.db).Model(&UserModel{}).Where("deleted_at IS NULL")
	if seek.WhereClause != "" {
		q = q.Where(seek.WhereClause, seek.BindVars...)
	}

	var out userdomain.ListResult
	if pr.IncludeTotal {
		var total int64
		if err := q.Count(&total).Error; err != nil {
			return userdomain.ListResult{}, err
		}
		out.Total = &total
	}

	var models []UserModel
	db := q.Order(seek.OrderClause).Limit(seek.LimitFetch)
	if pr.Mode == pagination.ModeOffset {
		db = db.Offset(pr.Offset)
	}
	if err := db.Find(&models).Error; err != nil {
		return userdomain.ListResult{}, err
	}
	if seek.ReverseDisplay {
		for i, j := 0, len(models)-1; i < j; i, j = i+1, j-1 {
			models[i], models[j] = models[j], models[i]
		}
	}
	items := make([]userdomain.User, 0, len(models))
	for _, model := range models {
		items = append(items, mapUser(model))
	}
	page, hasNext, hasPrev := pagination.TruncatePage(items, pr.Limit, pr.Forward, pr.Cursor != "")
	out.Items = page
	out.HasNextPage = hasNext
	out.HasPreviousPage = hasPrev
	out.Limit = pr.Limit
	if pr.Mode == pagination.ModeOffset {
		out.OffsetPage = pr.Page
		out.OffsetPerPage = pr.Limit
	}
	if len(page) > 0 {
		if out.HasNextPage {
			fields := pagination.SortValues[userdomain.User](usersAdminCfg, page[len(page)-1],
				func(row userdomain.User, i int) any {
					switch i {
					case 0:
						return row.CreatedAt
					case 1:
						return row.ID
					}
					return nil
				})
			cur, err := pagination.EncodeCursor(usersAdminCfg, fields)
			if err != nil {
				return userdomain.ListResult{}, err
			}
			out.NextCursor = cur
		}
		if out.HasPreviousPage {
			fields := pagination.SortValues[userdomain.User](usersAdminCfg, page[0],
				func(row userdomain.User, i int) any {
					switch i {
					case 0:
						return row.CreatedAt
					case 1:
						return row.ID
					}
					return nil
				})
			cur, err := pagination.EncodeCursor(usersAdminCfg, fields)
			if err != nil {
				return userdomain.ListResult{}, err
			}
			out.PreviousCursor = cur
		}
	}
	return out, nil
}

// ListRoleNames returns the names of the roles assigned to the user.
func (r *UserRepository) ListRoleNames(ctx context.Context, userID uuid.UUID) ([]string, error) {
	var names []string

	err := conn(ctx, r.db).
		Table("roles").
		Select("roles.name").
		Joins("INNER JOIN user_roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = "+idOf("users"), userID).
		Pluck("roles.name", &names).Error

	return names, err
}

func mapUser(model UserModel) userdomain.User {
	user := userdomain.User{
		ID:                model.ID,
		UUID:              model.UUID,
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

// SessionRepository implements authports.SessionRepository.
type SessionRepository struct {
	db *gorm.DB
}

// NewSessionRepository returns a SessionRepository backed by db.
func NewSessionRepository(db *gorm.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

// Create stores a refresh session and returns it with its assigned ids.
func (r *SessionRepository) Create(ctx context.Context, session authdomain.RefreshSession) (authdomain.RefreshSession, error) {
	userID, err := idByUUID(conn(ctx, r.db), "users", session.UserUUID)
	if err != nil {
		return authdomain.RefreshSession{}, err
	}

	model := RefreshSessionModel{
		UUID:      session.UUID,
		UserID:    userID,
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

	if err := conn(ctx, r.db).Create(&model).Error; err != nil {
		return authdomain.RefreshSession{}, err
	}

	session.ID = model.ID
	session.UUID = model.UUID

	return session, nil
}

// FindByTokenHash returns the session with the hash or authdomain.ErrInvalidToken.
func (r *SessionRepository) FindByTokenHash(ctx context.Context, hash string) (authdomain.RefreshSession, error) {
	var model RefreshSessionModel

	err := conn(ctx, r.db).
		Select(withRefs("refresh_sessions",
			uuidRef("users", "refresh_sessions.user_id", "user_uuid"),
			uuidRef("refresh_sessions", "refresh_sessions.replaced_by", "replaced_by_uuid"),
		)).
		Where("token_hash = ?", hash).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return authdomain.RefreshSession{}, authdomain.ErrInvalidToken
	}

	if err != nil {
		return authdomain.RefreshSession{}, fmt.Errorf("find session by token hash: %w", err)
	}

	return mapSession(model), nil
}

// Revoke marks one session revoked.
func (r *SessionRepository) Revoke(ctx context.Context, id uuid.UUID, at time.Time) error {
	return conn(ctx, r.db).Model(&RefreshSessionModel{}).Where("uuid = ?", id).
		Update("revoked_at", at).Error
}

// RevokeFamily revokes every live session in a rotation family (refresh token reuse).
func (r *SessionRepository) RevokeFamily(ctx context.Context, userID, familyID uuid.UUID, at time.Time) error {
	return conn(ctx, r.db).Model(&RefreshSessionModel{}).
		Where("user_id = "+idOf("users")+" AND family_id = ? AND revoked_at IS NULL", userID, familyID).
		Update("revoked_at", at).Error
}

// Replace revokes oldID and links it to its rotated successor newID.
func (r *SessionRepository) Replace(ctx context.Context, oldID, newID uuid.UUID, at time.Time) error {
	return conn(ctx, r.db).Model(&RefreshSessionModel{}).Where("uuid = ?", oldID).Updates(map[string]any{
		"revoked_at":  at,
		"replaced_by": gorm.Expr(idOf("refresh_sessions"), newID),
	}).Error
}

// RevokeAllForUser revokes every live session of the user.
func (r *SessionRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) error {
	return conn(ctx, r.db).Model(&RefreshSessionModel{}).
		Where("user_id = "+idOf("users")+" AND revoked_at IS NULL", userID).
		Update("revoked_at", at).Error
}

// PruneExpiredBefore deletes sessions that expired before cutoff and returns how many.
func (r *SessionRepository) PruneExpiredBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM refresh_sessions WHERE expires_at < ?`, cutoff)
	if res.Error != nil {
		return 0, fmt.Errorf("prune refresh sessions: %w", res.Error)
	}

	return res.RowsAffected, nil
}

func mapSession(model RefreshSessionModel) authdomain.RefreshSession {
	session := authdomain.RefreshSession{
		ID:             model.ID,
		UUID:           model.UUID,
		UserUUID:       model.UserUUID,
		FamilyID:       model.FamilyID,
		TokenHash:      model.TokenHash,
		ExpiresAt:      model.ExpiresAt,
		RevokedAt:      model.RevokedAt,
		ReplacedByUUID: model.ReplacedByUUID,
		CreatedAt:      model.CreatedAt,
	}
	if model.UserAgent != nil {
		session.UserAgent = *model.UserAgent
	}

	if model.IPAddress != nil {
		session.IPAddress = *model.IPAddress
	}

	return session
}
