// Package service implements threaded comments: posting, editing, deletion, flags, and upvotes.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	"github.com/turahe/blog-api/internal/core/comment/ports"
)

const (
	maxUserAgentRunes  = 500
	maxAuthorNameRunes = 100
	repliesPerThread   = 20
)

// IDGenerator returns new UUIDs.
type IDGenerator interface {
	New() uuid.UUID
}

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// Config sets comment policy; zero durations and thresholds use defaults.
type Config struct {
	// GuestEnabled allows comments without a bearer token (name and email required).
	GuestEnabled bool
	// RequireApproval starts signed-in comments as pending; guest comments are always pending.
	RequireApproval bool
	// EditWindow is how long after creation an author may edit their comment.
	EditWindow time.Duration
	// FlagThreshold is the flag count that moves an approved comment to flagged.
	FlagThreshold int
	// Renderer produces ContentHTML; nil escapes the text instead of rendering markdown.
	Renderer ports.Renderer
}

// Service implements comment use cases.
type Service struct {
	repo  ports.Repository
	ids   IDGenerator
	clock Clock
	cfg   Config
}

// New returns a Service with defaults applied to cfg.
func New(repo ports.Repository, ids IDGenerator, clock Clock, cfg Config) *Service {
	if cfg.EditWindow <= 0 {
		cfg.EditWindow = 15 * time.Minute
	}

	if cfg.FlagThreshold < 1 {
		cfg.FlagThreshold = 3
	}

	if cfg.Renderer == nil {
		cfg.Renderer = escapeRenderer{}
	}

	if repo != nil {
		repo = renderingRepository{Repository: repo, renderer: cfg.Renderer}
	}

	return &Service{repo: repo, ids: ids, clock: clock, cfg: cfg}
}

// CreateInput is a new comment request; AuthorUUID is nil for guests.
type CreateInput struct {
	PostUUID    uuid.UUID
	ParentUUID  *uuid.UUID
	AuthorUUID  *uuid.UUID
	AuthorName  string
	AuthorEmail string
	Content     string
	// Honeypot is a hidden form field; bots fill it, and the comment is silently marked spam.
	Honeypot  string
	ClientIP  string
	UserAgent string
}

// Create validates and stores a comment; honeypot hits become spam and guest comments await approval.
func (s *Service) Create(ctx context.Context, in CreateInput) (commentdomain.Comment, error) {
	content, err := normalizeContent(in.Content)
	if err != nil {
		return commentdomain.Comment{}, err
	}

	comment := commentdomain.Comment{
		UUID:        s.ids.New(),
		PostUUID:    in.PostUUID,
		AuthorUUID:  in.AuthorUUID,
		Content:     content,
		ContentHTML: s.cfg.Renderer.Render(content),
		IPHash:      hashIdentity(in.ClientIP),
		UserAgent:   truncateRunes(strings.TrimSpace(in.UserAgent), maxUserAgentRunes),
	}

	policy, err := s.writablePolicy(ctx, in.PostUUID)
	if err != nil {
		return commentdomain.Comment{}, err
	}

	if in.AuthorUUID == nil {
		if policy == commentdomain.PolicyAuthenticated {
			return commentdomain.Comment{}, fmt.Errorf("%w: this post accepts comments from signed-in users only", commentdomain.ErrGuestDisabled)
		}

		if comment.AuthorName, comment.AuthorEmail, err = s.guestIdentity(in.AuthorName, in.AuthorEmail); err != nil {
			return commentdomain.Comment{}, err
		}
	}

	if in.ParentUUID != nil {
		depth, err := s.replyDepth(ctx, in.PostUUID, *in.ParentUUID)
		if err != nil {
			return commentdomain.Comment{}, err
		}

		comment.ParentUUID = in.ParentUUID
		comment.Depth = depth
	}

	switch {
	case strings.TrimSpace(in.Honeypot) != "":
		comment.Status = commentdomain.StatusSpam
	case comment.AuthorUUID == nil || s.cfg.RequireApproval:
		comment.Status = commentdomain.StatusPending
	default:
		comment.Status = commentdomain.StatusApproved
	}

	now := s.clock.Now()
	comment.CreatedAt = now
	comment.UpdatedAt = now

	return s.repo.Create(ctx, comment)
}

// ListForPost lists public comments on a public post: roots by default, or the direct
// replies of parentID. A post whose policy is disabled returns ErrCommentsDisabled.
func (s *Service) ListForPost(ctx context.Context, postID uuid.UUID, parentID *uuid.UUID, page, perPage int) (commentdomain.ListResult, error) {
	policy, err := s.repo.PostPolicy(ctx, postID)
	if err != nil {
		return commentdomain.ListResult{}, err
	}

	if err := policy.Readable(); err != nil {
		return commentdomain.ListResult{}, err
	}

	page, perPage = normalizePage(page, perPage)

	return s.repo.List(ctx, commentdomain.ListFilter{
		PostUUID:   &postID,
		ParentUUID: parentID,
		RootsOnly:  parentID == nil,
		Statuses:   commentdomain.PublicStatuses,
		Page:       page,
		PerPage:    perPage,
	})
}

// GetThread returns a public comment with the first page of its public replies.
func (s *Service) GetThread(ctx context.Context, id uuid.UUID) (commentdomain.Thread, error) {
	comment, err := s.getPublic(ctx, id)
	if err != nil {
		return commentdomain.Thread{}, err
	}

	replies, err := s.repo.List(ctx, commentdomain.ListFilter{
		ParentUUID: &comment.UUID,
		Statuses:   commentdomain.PublicStatuses,
		Page:       1,
		PerPage:    repliesPerThread,
	})
	if err != nil {
		return commentdomain.Thread{}, err
	}

	return commentdomain.Thread{Comment: comment, Replies: replies}, nil
}

// ListMine lists the caller's comments in any status except deleted, newest first.
func (s *Service) ListMine(ctx context.Context, userID uuid.UUID, page, perPage int) (commentdomain.ListResult, error) {
	page, perPage = normalizePage(page, perPage)

	return s.repo.List(ctx, commentdomain.ListFilter{
		AuthorUUID: &userID,
		Statuses: []commentdomain.Status{
			commentdomain.StatusPending, commentdomain.StatusApproved, commentdomain.StatusFlagged,
			commentdomain.StatusSpam, commentdomain.StatusRejected,
		},
		NewestFirst: true,
		Page:        page,
		PerPage:     perPage,
	})
}

// Update edits the caller's own comment inside the edit window.
func (s *Service) Update(ctx context.Context, actorID, id uuid.UUID, content string) (commentdomain.Comment, error) {
	content, err := normalizeContent(content)
	if err != nil {
		return commentdomain.Comment{}, err
	}

	comment, err := s.getOwned(ctx, actorID, id)
	if err != nil {
		return commentdomain.Comment{}, err
	}

	if comment.Status == commentdomain.StatusSpam || comment.Status == commentdomain.StatusRejected {
		return commentdomain.Comment{}, commentdomain.ErrNotEditable
	}

	now := s.clock.Now()
	if now.After(comment.CreatedAt.Add(s.cfg.EditWindow)) {
		return commentdomain.Comment{}, commentdomain.ErrEditWindowClosed
	}

	if _, err := s.writablePolicy(ctx, comment.PostUUID); err != nil {
		return commentdomain.Comment{}, err
	}

	comment.Content = content
	comment.ContentHTML = s.cfg.Renderer.Render(content)
	comment.EditedAt = &now
	comment.UpdatedAt = now

	return s.repo.Update(ctx, comment)
}

// Delete soft-deletes the caller's own comment. Deleting twice is a no-op.
func (s *Service) Delete(ctx context.Context, actorID, id uuid.UUID) error {
	comment, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if !comment.OwnedBy(actorID) {
		return commentdomain.ErrForbidden
	}

	if comment.Status == commentdomain.StatusDeleted {
		return nil
	}

	now := s.clock.Now()
	comment.Status = commentdomain.StatusDeleted
	comment.DeletedAt = &now
	comment.DeletedByUUID = &actorID
	comment.UpdatedAt = now
	_, err = s.repo.Update(ctx, comment)

	return err
}

// FlagInput is a flag request; ReporterUUID is nil for guests, who are identified by IP and user agent.
type FlagInput struct {
	CommentUUID  uuid.UUID
	ReporterUUID *uuid.UUID
	Reason       string
	Details      string
	ClientIP     string
	UserAgent    string
}

// Flag reports an approved comment. Repeat flags from the same identity are accepted but not counted.
func (s *Service) Flag(ctx context.Context, in FlagInput) error {
	reason := strings.TrimSpace(in.Reason)
	if !slices.Contains(commentdomain.FlagReasons, reason) {
		return fmt.Errorf("%w: unknown reason_code", commentdomain.ErrValidation)
	}

	details := strings.TrimSpace(in.Details)
	if utf8.RuneCountInString(details) > commentdomain.MaxFlagDetailsRunes {
		return fmt.Errorf("%w: details too long", commentdomain.ErrValidation)
	}

	comment, err := s.repo.GetByID(ctx, in.CommentUUID)
	if err != nil {
		return err
	}

	if comment.Status != commentdomain.StatusApproved {
		return commentdomain.ErrNotFound
	}

	if err := s.readablePost(ctx, comment.PostUUID); err != nil {
		return err
	}

	flag := commentdomain.Flag{
		CommentUUID:  comment.UUID,
		ReporterUUID: in.ReporterUUID,
		Reason:       reason,
		Details:      details,
		CreatedAt:    s.clock.Now(),
	}
	if in.ReporterUUID == nil {
		flag.ReporterIPHash = hashIdentity(in.ClientIP + "|" + in.UserAgent)
	}

	_, err = s.repo.AddFlag(ctx, flag, s.cfg.FlagThreshold)

	return err
}

// ToggleUpvote adds or removes the caller's upvote on an approved comment.
func (s *Service) ToggleUpvote(ctx context.Context, voterID, id uuid.UUID) (bool, int, error) {
	comment, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return false, 0, err
	}

	if comment.Status != commentdomain.StatusApproved {
		return false, 0, commentdomain.ErrNotFound
	}

	if _, err := s.writablePolicy(ctx, comment.PostUUID); err != nil {
		return false, 0, err
	}

	return s.repo.ToggleUpvote(ctx, comment.UUID, voterID, s.clock.Now())
}

func (s *Service) guestIdentity(rawName, rawEmail string) (string, string, error) {
	if !s.cfg.GuestEnabled {
		return "", "", commentdomain.ErrGuestDisabled
	}

	name := strings.TrimSpace(rawName)

	email := strings.ToLower(strings.TrimSpace(rawEmail))
	if name == "" || email == "" {
		return "", "", fmt.Errorf("%w: author_name and author_email are required for guest comments", commentdomain.ErrValidation)
	}

	if utf8.RuneCountInString(name) > maxAuthorNameRunes {
		return "", "", fmt.Errorf("%w: author_name too long", commentdomain.ErrValidation)
	}

	return name, email, nil
}

// replyDepth validates that parentID is an approved comment on postID and returns the reply's depth.
func (s *Service) replyDepth(ctx context.Context, postID, parentID uuid.UUID) (int, error) {
	parent, err := s.repo.GetByID(ctx, parentID)
	if errors.Is(err, commentdomain.ErrNotFound) {
		return 0, commentdomain.ErrParentInvalid
	}

	if err != nil {
		return 0, err
	}

	if parent.PostUUID != postID || parent.Status != commentdomain.StatusApproved {
		return 0, commentdomain.ErrParentInvalid
	}

	if parent.Depth+1 > commentdomain.MaxDepth {
		return 0, commentdomain.ErrDepthExceeded
	}

	return parent.Depth + 1, nil
}

func (s *Service) getPublic(ctx context.Context, id uuid.UUID) (commentdomain.Comment, error) {
	comment, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return commentdomain.Comment{}, err
	}

	if !comment.Public() {
		return commentdomain.Comment{}, commentdomain.ErrNotFound
	}

	if err := s.readablePost(ctx, comment.PostUUID); err != nil {
		return commentdomain.Comment{}, err
	}

	return comment, nil
}

// readablePost returns ErrNotFound when the comment's post is not public, and
// ErrCommentsDisabled when its policy hides comments.
func (s *Service) readablePost(ctx context.Context, postID uuid.UUID) error {
	policy, err := s.repo.PostPolicy(ctx, postID)
	if errors.Is(err, commentdomain.ErrPostNotFound) {
		return commentdomain.ErrNotFound
	}

	if err != nil {
		return err
	}

	return policy.Readable()
}

// writablePolicy returns the policy of a public post that accepts comment writes.
func (s *Service) writablePolicy(ctx context.Context, postID uuid.UUID) (commentdomain.Policy, error) {
	policy, err := s.repo.PostPolicy(ctx, postID)
	if err != nil {
		return "", err
	}

	return policy, policy.Writable()
}

func (s *Service) getOwned(ctx context.Context, actorID, id uuid.UUID) (commentdomain.Comment, error) {
	comment, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return commentdomain.Comment{}, err
	}

	if !comment.OwnedBy(actorID) {
		return commentdomain.Comment{}, commentdomain.ErrForbidden
	}

	if comment.Status == commentdomain.StatusDeleted {
		return commentdomain.Comment{}, commentdomain.ErrNotFound
	}

	return comment, nil
}

func normalizeContent(raw string) (string, error) {
	content := strings.TrimSpace(raw)
	if content == "" {
		return "", fmt.Errorf("%w: content required", commentdomain.ErrValidation)
	}

	if utf8.RuneCountInString(content) > commentdomain.MaxContentRunes {
		return "", fmt.Errorf("%w: content too long", commentdomain.ErrValidation)
	}

	return content, nil
}

func normalizePage(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}

	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	return page, perPage
}

func hashIdentity(value string) string {
	if value == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:])
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}

	return string([]rune(value)[:limit])
}
