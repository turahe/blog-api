package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"gorm.io/gorm"
)

// PostSEORepository stores per-post SEO overrides in PostgreSQL.
type PostSEORepository struct {
	db *gorm.DB
}

// NewPostSEORepository returns a PostSEORepository over db.
func NewPostSEORepository(db *gorm.DB) *PostSEORepository {
	return &PostSEORepository{db: db}
}

type postSEORow struct {
	SEOTitle           string     `gorm:"column:seo_title"`
	SEODescription     string     `gorm:"column:seo_description"`
	SEOKeywords        string     `gorm:"column:seo_keywords"`
	OGTitle            string     `gorm:"column:og_title"`
	OGDescription      string     `gorm:"column:og_description"`
	OGImageUUID        *uuid.UUID `gorm:"column:og_image_uuid"`
	OGURL              string     `gorm:"column:og_url"`
	TwitterCard        string     `gorm:"column:twitter_card"`
	TwitterTitle       string     `gorm:"column:twitter_title"`
	TwitterDescription string     `gorm:"column:twitter_description"`
	TwitterImageUUID   *uuid.UUID `gorm:"column:twitter_image_uuid"`
	TwitterCreator     string     `gorm:"column:twitter_creator"`
	CanonicalURL       string     `gorm:"column:canonical_url"`
	RobotsNoindex      bool       `gorm:"column:robots_noindex"`
	RobotsNofollow     bool       `gorm:"column:robots_nofollow"`
}

// Get returns the post's SEO, or the zero SEO when no row exists.
func (r *PostSEORepository) Get(ctx context.Context, postID uuid.UUID) (postdomain.SEO, error) {
	var rows []postSEORow

	err := conn(ctx, r.db).Raw(`
		SELECT s.seo_title, s.seo_description, s.seo_keywords::text AS seo_keywords, s.og_title, s.og_description,
			og.uuid AS og_image_uuid, s.og_url, s.twitter_card, s.twitter_title, s.twitter_description,
			tw.uuid AS twitter_image_uuid, s.twitter_creator, s.canonical_url, s.robots_noindex, s.robots_nofollow
		FROM post_seo s
		JOIN posts p ON p.id = s.post_id
		LEFT JOIN media_assets og ON og.id = s.og_image_id
		LEFT JOIN media_assets tw ON tw.id = s.twitter_image_id
		WHERE p.uuid = ?`, postID).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return postdomain.SEO{}, err
	}

	row := rows[0]
	seo := postdomain.SEO{
		Title:              row.SEOTitle,
		Description:        row.SEODescription,
		OGTitle:            row.OGTitle,
		OGDescription:      row.OGDescription,
		OGImageUUID:        row.OGImageUUID,
		OGURL:              row.OGURL,
		TwitterCard:        postdomain.TwitterCard(row.TwitterCard),
		TwitterTitle:       row.TwitterTitle,
		TwitterDescription: row.TwitterDescription,
		TwitterImageUUID:   row.TwitterImageUUID,
		TwitterCreator:     row.TwitterCreator,
		CanonicalURL:       row.CanonicalURL,
		RobotsNoindex:      row.RobotsNoindex,
		RobotsNofollow:     row.RobotsNofollow,
	}

	if err := json.Unmarshal([]byte(row.SEOKeywords), &seo.Keywords); err != nil {
		return postdomain.SEO{}, err
	}

	if len(seo.Keywords) == 0 {
		seo.Keywords = nil
	}

	return seo, nil
}

// Save upserts the post's SEO. An unknown post is postdomain.ErrNotFound and an unknown
// image is postdomain.ErrValidation.
func (r *PostSEORepository) Save(ctx context.Context, postID uuid.UUID, seo postdomain.SEO, at time.Time) error {
	db := conn(ctx, r.db)

	post, err := idByUUID(db, "posts", postID)
	if errors.Is(err, errUnknownReference) {
		return postdomain.ErrNotFound
	}

	if err != nil {
		return err
	}

	ogImage, err := optionalIDByUUID(db, "media_assets", seo.OGImageUUID)
	if err != nil {
		return invalidReference(err, postdomain.ErrValidation, "og_image_id")
	}

	twitterImage, err := optionalIDByUUID(db, "media_assets", seo.TwitterImageUUID)
	if err != nil {
		return invalidReference(err, postdomain.ErrValidation, "twitter_image_id")
	}

	keywords, err := json.Marshal(nonNil(seo.Keywords))
	if err != nil {
		return err
	}

	return db.Exec(`
		INSERT INTO post_seo (post_id, seo_title, seo_description, seo_keywords, og_title, og_description, og_image_id,
			og_url, twitter_card, twitter_title, twitter_description, twitter_image_id, twitter_creator, canonical_url,
			robots_noindex, robots_nofollow, created_at, updated_at)
		VALUES (?, ?, ?, ?::jsonb, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (post_id) DO UPDATE SET
			seo_title = EXCLUDED.seo_title, seo_description = EXCLUDED.seo_description,
			seo_keywords = EXCLUDED.seo_keywords, og_title = EXCLUDED.og_title,
			og_description = EXCLUDED.og_description, og_image_id = EXCLUDED.og_image_id, og_url = EXCLUDED.og_url,
			twitter_card = EXCLUDED.twitter_card, twitter_title = EXCLUDED.twitter_title,
			twitter_description = EXCLUDED.twitter_description, twitter_image_id = EXCLUDED.twitter_image_id,
			twitter_creator = EXCLUDED.twitter_creator, canonical_url = EXCLUDED.canonical_url,
			robots_noindex = EXCLUDED.robots_noindex, robots_nofollow = EXCLUDED.robots_nofollow,
			updated_at = EXCLUDED.updated_at`,
		post, seo.Title, seo.Description, string(keywords), seo.OGTitle, seo.OGDescription, ogImage, seo.OGURL,
		string(seo.TwitterCard), seo.TwitterTitle, seo.TwitterDescription, twitterImage, seo.TwitterCreator,
		seo.CanonicalURL, seo.RobotsNoindex, seo.RobotsNofollow, at, at).Error
}
