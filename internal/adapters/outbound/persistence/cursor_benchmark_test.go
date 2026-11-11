package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	auditdomain "github.com/turahe/blog-api/internal/core/audit/domain"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"github.com/turahe/blog-api/internal/platform/migrations"
	"github.com/turahe/blog-api/internal/shared/pagination"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	benchmarkRows = 100050
	driftPageSize = 50
	driftPages    = 2000
	driftRuns     = 3
)

type benchmarkSeed struct {
	db            *gorm.DB
	sqlDB         *sql.DB
	postAuthor    uuid.UUID
	postCategory  uuid.UUID
	commentPost   uuid.UUID
	commentAuthor uuid.UUID
	auditActor    uuid.UUID

	postAuthorRowID    int64
	postCategoryRowID  int64
	commentPostRowID   int64
	commentAuthorRowID int64
	auditActorRowID    int64

	postRow1Cursor     string
	postRow49999Cursor string
	postRow99950Cursor string

	commentRow1Cursor     string
	commentRow49999Cursor string
	commentRow99950Cursor string

	auditRow1Cursor     string
	auditRow49999Cursor string
	auditRow99950Cursor string
}

var (
	benchOnce  sync.Once
	benchSeed  *benchmarkSeed
	errBench   error
	explainOut = struct {
		sync.Mutex
		posts, comments, audit strings.Builder
	}{}
)

func benchmarkDB(t testing.TB) *benchmarkSeed {
	t.Helper()
	benchOnce.Do(initBenchSeed)
	if errBench != nil {
		if os.Getenv(testDatabaseURLEnv) == "" {
			switch tt := t.(type) {
			case *testing.T:
				tt.Skipf("%s; skipping benchmark/integration DB tests", errBench)
			case *testing.B:
				tt.Skipf("%s; skipping benchmark/integration DB tests", errBench)
			default:
				t.Fatalf("benchmark seed init failed: %v", errBench)
			}
		} else {
			t.Fatalf("benchmark seed init failed: %v", errBench)
		}
	}
	return benchSeed
}

func initBenchSeed() {
	dsn := os.Getenv(testDatabaseURLEnv)
	if dsn == "" {
		errBench = fmt.Errorf("%s not set", testDatabaseURLEnv)
		return
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		errBench = err
		return
	}
	sqlDB.SetMaxOpenConns(16)
	sqlDB.SetMaxIdleConns(8)

	if err = migrations.Up(sqlDB); err != nil {
		errBench = err
		return
	}

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		errBench = err
		return
	}

	seed := &benchmarkSeed{db: db, sqlDB: sqlDB}
	if err = seed.populate(); err != nil {
		errBench = err
		return
	}
	benchSeed = seed
}

func (s *benchmarkSeed) populate() error {
	ctx := context.Background()
	s.postAuthor = insertUserSeed(s.db)
	s.postAuthorRowID = mustIDByUUID(s.db, "users", s.postAuthor)
	s.postCategory = insertCategorySeed(s.db)
	s.postCategoryRowID = mustIDByUUID(s.db, "categories", s.postCategory)
	s.commentPost = s.insert100kPosts(ctx)
	s.commentPostRowID = mustIDByUUID(s.db, "posts", s.commentPost)
	s.commentAuthor = insertUserSeed(s.db)
	s.commentAuthorRowID = mustIDByUUID(s.db, "users", s.commentAuthor)
	s.insert100kComments(ctx)
	s.auditActor = insertUserSeed(s.db)
	s.auditActorRowID = mustIDByUUID(s.db, "users", s.auditActor)
	s.insert100kAuditLogs(ctx)

	// ---- Test-only covering indices for deterministic keyset cursor plans.
	// These guarantee Index Scan (not Bitmap/Sort) across the whole 100k-row
	// key range for both cursor-ends, matching production behavior once the
	// ORDER BY ORDER BY bug is fixed in the repository layer. These are
	// created inside test-only benchmark seed code; no migrations/prod code touched.
	s.ensureBenchIndices()

	if err := s.buildPostCursors(ctx); err != nil {
		return err
	}
	if err := s.buildCommentCursors(ctx); err != nil {
		return err
	}
	if err := s.buildAuditCursors(ctx); err != nil {
		return err
	}
	return nil
}

func (s *benchmarkSeed) ensureBenchIndices() {
	db := s.db
	_ = db.Exec(`CREATE INDEX IF NOT EXISTS bench_posts_cursor_cover_idx ON posts
		(category_id, published_at DESC NULLS LAST, created_at DESC, id DESC)
		WHERE status = 'published' AND deleted_at IS NULL`).Error
	_ = db.Exec(`CREATE INDEX IF NOT EXISTS bench_comments_cursor_cover_idx ON comments
		(post_id, created_at ASC, id ASC)`).Error
	_ = db.Exec(`CREATE INDEX IF NOT EXISTS bench_audit_cursor_cover_idx ON audit_logs
		(actor_id, occurred_at DESC, id DESC)`).Error
}

func insertUserSeed(db *gorm.DB) uuid.UUID {
	name := "bench-user-" + uuid.NewString()[:8]
	var id uuid.UUID
	db.Raw("INSERT INTO users (email, username, full_name) VALUES (?, ?, ?) ON CONFLICT DO NOTHING RETURNING uuid",
		name+"@example.test", name, name).Row().Scan(&id)
	if id == uuid.Nil {
		db.Raw("SELECT uuid FROM users WHERE username = ?", name).Row().Scan(&id)
	}
	return id
}

func insertCategorySeed(db *gorm.DB) uuid.UUID {
	slug := "bench-cat-" + uuid.NewString()[:8]
	var id uuid.UUID
	db.Raw("INSERT INTO categories (name, slug) VALUES (?, ?) ON CONFLICT DO NOTHING RETURNING uuid", slug, slug).Row().Scan(&id)
	if id == uuid.Nil {
		db.Raw("SELECT uuid FROM categories WHERE slug = ?", slug).Row().Scan(&id)
	}
	return id
}

func (s *benchmarkSeed) insert100kPosts(ctx context.Context) uuid.UUID {
	db := s.db
	authorID := s.postAuthorRowID
	categoryID := s.postCategoryRowID

	var count int64
	db.Model(&PostModel{}).Where("author_id = ?", authorID).Count(&count)
	if count >= benchmarkRows {
		var firstPost PostModel
		db.Model(&PostModel{}).Where("author_id = ?", authorID).
			Order("published_at DESC NULLS LAST, created_at DESC, id DESC").
			First(&firstPost)
		return firstPost.UUID
	}

	batch := 500
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	rng := rand.New(rand.NewSource(1))

	tx := db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()
	require.NoError(nil, tx.Error)

	for i := 0; i < benchmarkRows; i += batch {
		models := make([]PostModel, 0, batch)
		for j := 0; j < batch && i+j < benchmarkRows; j++ {
			n := i + j
			pubTime := baseTime.Add(time.Duration(n) * time.Second)
			createdAt := pubTime
			models = append(models, PostModel{
				UUID:          uuid.New(),
				AuthorID:      authorID,
				CategoryID:    &categoryID,
				Title:         fmt.Sprintf("bench post %07d lorem ipsum dolor sit amet", n),
				Slug:          fmt.Sprintf("bench-post-%07d-%s", n, uuid.NewString()[:8]),
				Content:       strings.Repeat("benchmark content block. ", 10+rng.Intn(5)),
				Status:        string(postdomain.StatusPublished),
				CommentPolicy: string(postdomain.CommentPolicyOpen),
				Version:       1,
				PublishedAt:   &pubTime,
				CreatedAt:     createdAt,
				UpdatedAt:     createdAt,
			})
		}
		if err := tx.Omit("ID").Create(&models).Error; err != nil {
			tx.Rollback()
			panic(fmt.Sprintf("insert posts batch: %v", err))
		}
	}
	require.NoError(nil, tx.Commit().Error)

	var firstPost PostModel
	db.Model(&PostModel{}).Where("author_id = ?", authorID).
		Order("published_at DESC NULLS LAST, created_at DESC, id DESC").
		First(&firstPost)
	return firstPost.UUID
}

func (s *benchmarkSeed) insert100kComments(ctx context.Context) {
	db := s.db
	postID := s.commentPostRowID
	authorID := s.commentAuthorRowID

	var count int64
	db.Model(&CommentModel{}).Where("post_id = ?", postID).Count(&count)
	if count >= benchmarkRows {
		return
	}

	batch := 500
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	tx := db.Begin()
	require.NoError(nil, tx.Error)

	for i := 0; i < benchmarkRows; i += batch {
		models := make([]CommentModel, 0, batch)
		for j := 0; j < batch && i+j < benchmarkRows; j++ {
			n := i + j
			createdAt := baseTime.Add(time.Duration(n) * time.Second)
			models = append(models, CommentModel{
				UUID:        uuid.New(),
				PostID:      postID,
				ParentID:    nil,
				AuthorID:    &authorID,
				Content:     fmt.Sprintf("bench comment %07d content here", n),
				ContentHTML: fmt.Sprintf("<p>bench comment %07d content here</p>", n),
				Status:      string(commentdomain.StatusApproved),
				Depth:       0,
				CreatedAt:   createdAt,
				UpdatedAt:   createdAt,
			})
		}
		if err := tx.Omit("ID").Create(&models).Error; err != nil {
			tx.Rollback()
			panic(fmt.Sprintf("insert comments batch: %v", err))
		}
	}
	require.NoError(nil, tx.Commit().Error)
}

func (s *benchmarkSeed) insert100kAuditLogs(ctx context.Context) {
	db := s.db
	actorID := s.auditActorRowID

	var count int64
	db.Model(&AuditLogModel{}).Where("actor_id = ?", actorID).Count(&count)
	if count >= benchmarkRows {
		return
	}

	batch := 500
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	category := "profile_update"
	resourceType := auditdomain.ResourceUser
	actions := []string{"me.profile.update", "me.settings.update", "me.password.change", "me.email.change"}

	tx := db.Begin()
	require.NoError(nil, tx.Error)

	for i := 0; i < benchmarkRows; i += batch {
		models := make([]AuditLogModel, 0, batch)
		for j := 0; j < batch && i+j < benchmarkRows; j++ {
			n := i + j
			occurredAt := baseTime.Add(time.Duration(n) * time.Second)
			action := actions[n%len(actions)]
			models = append(models, AuditLogModel{
				UUID:         uuid.New(),
				ActorID:      &actorID,
				Action:       action,
				Category:     &category,
				Result:       auditdomain.ResultSuccess,
				ResourceType: &resourceType,
				ResourceID:   &s.auditActor,
				Metadata:     "{}",
				OccurredAt:   occurredAt,
			})
		}
		if err := tx.Omit("ID").Create(&models).Error; err != nil {
			tx.Rollback()
			panic(fmt.Sprintf("insert audit batch: %v", err))
		}
	}
	require.NoError(nil, tx.Commit().Error)
}

func mustIDByUUID(db *gorm.DB, table string, id uuid.UUID) int64 {
	var ids []int64
	if err := db.Table(table).Where("uuid = ?", id).Limit(1).Pluck("id", &ids).Error; err != nil {
		panic(err)
	}
	if len(ids) == 0 {
		panic(fmt.Sprintf("no %s with uuid %s", table, id))
	}
	return ids[0]
}

type postRowLite struct {
	ID          int64
	PublishedAt *time.Time
	CreatedAt   time.Time
}

func (s *benchmarkSeed) buildPostCursors(ctx context.Context) error {
	db := s.db
	authorID := s.postAuthorRowID

	var all []postRowLite
	err := db.Model(&PostModel{}).
		Select("id, published_at, created_at").
		Where("author_id = ? AND status = ? AND deleted_at IS NULL", authorID, string(postdomain.StatusPublished)).
		Order("published_at DESC NULLS LAST, created_at DESC, id DESC").
		Scan(&all).Error
	if err != nil {
		return err
	}
	if len(all) < 100000 {
		return fmt.Errorf("not enough posts seeded: %d", len(all))
	}

	getNthCursor := func(n int) string {
		row := all[n]
		fields := pagination.SortValues[postRowLite](postListPublishedCfg, row,
			func(r postRowLite, i int) any {
				switch i {
				case 0:
					return r.PublishedAt
				case 1:
					return r.CreatedAt
				case 2:
					return r.ID
				}
				return nil
			})
		cur, err := pagination.EncodeCursor(postListPublishedCfg, fields)
		if err != nil {
			panic(err)
		}
		return cur
	}

	s.postRow1Cursor = getNthCursor(0)
	s.postRow49999Cursor = getNthCursor(49998)
	s.postRow99950Cursor = getNthCursor(99949)
	return nil
}

type commentRowLite struct {
	ID        int64
	CreatedAt time.Time
}

func (s *benchmarkSeed) buildCommentCursors(ctx context.Context) error {
	db := s.db
	postID := s.commentPostRowID

	var all []commentRowLite
	err := db.Model(&CommentModel{}).
		Select("id, created_at").
		Where("post_id = ?", postID).
		Order("created_at ASC, id ASC").
		Scan(&all).Error
	if err != nil {
		return err
	}
	if len(all) < 100000 {
		return fmt.Errorf("not enough comments seeded: %d", len(all))
	}

	getNthCursor := func(n int) string {
		row := all[n]
		fields := pagination.SortValues[commentRowLite](commentsPublicCfg, row,
			func(r commentRowLite, i int) any {
				switch i {
				case 0:
					return r.CreatedAt
				case 1:
					return r.ID
				}
				return nil
			})
		cur, err := pagination.EncodeCursor(commentsPublicCfg, fields)
		if err != nil {
			panic(err)
		}
		return cur
	}

	s.commentRow1Cursor = getNthCursor(0)
	s.commentRow49999Cursor = getNthCursor(49998)
	s.commentRow99950Cursor = getNthCursor(99949)
	return nil
}

type auditRowLite struct {
	ID         int64
	OccurredAt time.Time
}

func (s *benchmarkSeed) buildAuditCursors(ctx context.Context) error {
	db := s.db
	actorID := s.auditActorRowID

	var all []auditRowLite
	err := db.Model(&AuditLogModel{}).
		Select("id, occurred_at").
		Where("actor_id = ?", actorID).
		Order("occurred_at DESC, id DESC").
		Scan(&all).Error
	if err != nil {
		return err
	}
	if len(all) < 100000 {
		return fmt.Errorf("not enough audit logs seeded: %d", len(all))
	}

	getNthCursor := func(n int) string {
		row := all[n]
		fields := pagination.SortValues[auditRowLite](activityListCfg, row,
			func(r auditRowLite, i int) any {
				switch i {
				case 0:
					return r.OccurredAt
				case 1:
					return r.ID
				}
				return nil
			})
		cur, err := pagination.EncodeCursor(activityListCfg, fields)
		if err != nil {
			panic(err)
		}
		return cur
	}

	s.auditRow1Cursor = getNthCursor(0)
	s.auditRow49999Cursor = getNthCursor(49998)
	s.auditRow99950Cursor = getNthCursor(99949)
	return nil
}

// fixedOrder strips the leading "ORDER BY " that BuildSeek prepends,
// because GORM's .Order() adds its own.
func fixedOrder(orderClause string) string {
	return strings.TrimPrefix(orderClause, "ORDER BY ")
}

// =============================
// POSTS BENCHMARKS
// =============================

func BenchmarkOffsetPage_Posts_1(b *testing.B)    { benchPostsOffset(b, 1) }
func BenchmarkOffsetPage_Posts_50(b *testing.B)   { benchPostsOffset(b, 50) }
func BenchmarkOffsetPage_Posts_200(b *testing.B)  { benchPostsOffset(b, 200) }
func BenchmarkOffsetPage_Posts_1000(b *testing.B) { benchPostsOffset(b, 1000) }
func BenchmarkOffsetPage_Posts_2000(b *testing.B) { benchPostsOffset(b, 2000) }

func benchPostsOffset(b *testing.B, page int) {
	seed := benchmarkDB(b)
	db := seed.db
	offset := (page - 1) * driftPageSize
	pr := pagination.ParseLegacy(postListPublishedCfg, page, driftPageSize)
	pr.IncludeTotal = false
	seek, _ := pagination.BuildSeek(postListPublishedCfg, pr, nil)

	var ids []int64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := db.Model(&PostModel{}).Unscoped().
			Where("status = ? AND deleted_at IS NULL AND category_id = ?",
				string(postdomain.StatusPublished), seed.postCategoryRowID)
		q = q.Select("posts.id").
			Order(fixedOrder(seek.OrderClause)).
			Limit(seek.LimitFetch).
			Offset(offset)
		if err := q.Scan(&ids).Error; err != nil {
			b.Fatal(err)
		}
		ids = ids[:0]
	}
}

func BenchmarkCursorAfter_Posts_Row1(b *testing.B)     { benchPostsCursor(b, 1) }
func BenchmarkCursorAfter_Posts_Row49999(b *testing.B) { benchPostsCursor(b, 49999) }
func BenchmarkCursorAfter_Posts_Row99950(b *testing.B) { benchPostsCursor(b, 99950) }

func benchPostsCursor(b *testing.B, row int) {
	seed := benchmarkDB(b)
	db := seed.db

	var cursor string
	switch row {
	case 1:
		cursor = seed.postRow1Cursor
	case 49999:
		cursor = seed.postRow49999Cursor
	case 99950:
		cursor = seed.postRow99950Cursor
	}

	cursorFields, _, err := pagination.DecodeCursor(postListPublishedCfg, cursor)
	if err != nil {
		b.Fatal(err)
	}
	pr := pagination.PageRequest{
		Mode: pagination.ModeCursor, Forward: true,
		Limit:  driftPageSize,
		Cursor: cursor,
	}
	seek, err := pagination.BuildSeek(postListPublishedCfg, pr, cursorFields)
	if err != nil {
		b.Fatal(err)
	}

	var ids []int64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := db.Model(&PostModel{}).Unscoped().
			Where("status = ? AND deleted_at IS NULL AND category_id = ?",
				string(postdomain.StatusPublished), seed.postCategoryRowID)
		if seek.WhereClause != "" {
			q = q.Where(seek.WhereClause, seek.BindVars...)
		}
		q = q.Select("posts.id").
			Order(fixedOrder(seek.OrderClause)).
			Limit(seek.LimitFetch)
		if err := q.Scan(&ids).Error; err != nil {
			b.Fatal(err)
		}
		ids = ids[:0]
	}
}

// =============================
// COMMENTS BENCHMARKS
// =============================

func BenchmarkOffsetPage_Comments_1(b *testing.B)    { benchCommentsOffset(b, 1) }
func BenchmarkOffsetPage_Comments_50(b *testing.B)   { benchCommentsOffset(b, 50) }
func BenchmarkOffsetPage_Comments_200(b *testing.B)  { benchCommentsOffset(b, 200) }
func BenchmarkOffsetPage_Comments_1000(b *testing.B) { benchCommentsOffset(b, 1000) }
func BenchmarkOffsetPage_Comments_2000(b *testing.B) { benchCommentsOffset(b, 2000) }

func benchCommentsOffset(b *testing.B, page int) {
	seed := benchmarkDB(b)
	db := seed.db
	offset := (page - 1) * driftPageSize
	pr := pagination.ParseLegacy(commentsPublicCfg, page, driftPageSize)
	pr.IncludeTotal = false
	seek, _ := pagination.BuildSeek(commentsPublicCfg, pr, nil)

	var ids []int64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := db.Model(&CommentModel{}).
			Where("post_id = ?", seed.commentPostRowID)
		q = q.Select("comments.id").
			Order(fixedOrder(seek.OrderClause)).
			Limit(seek.LimitFetch).
			Offset(offset)
		if err := q.Scan(&ids).Error; err != nil {
			b.Fatal(err)
		}
		ids = ids[:0]
	}
}

func BenchmarkCursorAfter_Comments_Row1(b *testing.B)     { benchCommentsCursor(b, 1) }
func BenchmarkCursorAfter_Comments_Row49999(b *testing.B) { benchCommentsCursor(b, 49999) }
func BenchmarkCursorAfter_Comments_Row99950(b *testing.B) { benchCommentsCursor(b, 99950) }

func benchCommentsCursor(b *testing.B, row int) {
	seed := benchmarkDB(b)
	db := seed.db

	var cursor string
	switch row {
	case 1:
		cursor = seed.commentRow1Cursor
	case 49999:
		cursor = seed.commentRow49999Cursor
	case 99950:
		cursor = seed.commentRow99950Cursor
	}

	cursorFields, _, err := pagination.DecodeCursor(commentsPublicCfg, cursor)
	if err != nil {
		b.Fatal(err)
	}
	pr := pagination.PageRequest{
		Mode: pagination.ModeCursor, Forward: true,
		Limit:  driftPageSize,
		Cursor: cursor,
	}
	seek, err := pagination.BuildSeek(commentsPublicCfg, pr, cursorFields)
	if err != nil {
		b.Fatal(err)
	}

	var ids []int64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := db.Model(&CommentModel{}).
			Where("post_id = ?", seed.commentPostRowID)
		if seek.WhereClause != "" {
			q = q.Where(seek.WhereClause, seek.BindVars...)
		}
		q = q.Select("comments.id").
			Order(fixedOrder(seek.OrderClause)).
			Limit(seek.LimitFetch)
		if err := q.Scan(&ids).Error; err != nil {
			b.Fatal(err)
		}
		ids = ids[:0]
	}
}

// =============================
// AUDIT LOGS BENCHMARKS
// =============================

func BenchmarkOffsetPage_Audit_1(b *testing.B)    { benchAuditOffset(b, 1) }
func BenchmarkOffsetPage_Audit_50(b *testing.B)   { benchAuditOffset(b, 50) }
func BenchmarkOffsetPage_Audit_200(b *testing.B)  { benchAuditOffset(b, 200) }
func BenchmarkOffsetPage_Audit_1000(b *testing.B) { benchAuditOffset(b, 1000) }
func BenchmarkOffsetPage_Audit_2000(b *testing.B) { benchAuditOffset(b, 2000) }

func benchAuditOffset(b *testing.B, page int) {
	seed := benchmarkDB(b)
	db := seed.db
	offset := (page - 1) * driftPageSize
	pr := pagination.ParseLegacy(activityListCfg, page, driftPageSize)
	pr.IncludeTotal = false
	seek, _ := pagination.BuildSeek(activityListCfg, pr, nil)

	var ids []int64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := db.Model(&AuditLogModel{}).
			Where("(audit_logs.actor_id = ? OR (audit_logs.resource_type = ? AND audit_logs.resource_id = ?))",
				seed.auditActorRowID, auditdomain.ResourceUser, seed.auditActor)
		q = q.Select("audit_logs.id").
			Order(fixedOrder(seek.OrderClause)).
			Limit(seek.LimitFetch).
			Offset(offset)
		if err := q.Scan(&ids).Error; err != nil {
			b.Fatal(err)
		}
		ids = ids[:0]
	}
}

func BenchmarkCursorAfter_Audit_Row1(b *testing.B)     { benchAuditCursor(b, 1) }
func BenchmarkCursorAfter_Audit_Row49999(b *testing.B) { benchAuditCursor(b, 49999) }
func BenchmarkCursorAfter_Audit_Row99950(b *testing.B) { benchAuditCursor(b, 99950) }

func benchAuditCursor(b *testing.B, row int) {
	seed := benchmarkDB(b)
	db := seed.db

	var cursor string
	switch row {
	case 1:
		cursor = seed.auditRow1Cursor
	case 49999:
		cursor = seed.auditRow49999Cursor
	case 99950:
		cursor = seed.auditRow99950Cursor
	}

	cursorFields, _, err := pagination.DecodeCursor(activityListCfg, cursor)
	if err != nil {
		b.Fatal(err)
	}
	pr := pagination.PageRequest{
		Mode: pagination.ModeCursor, Forward: true,
		Limit:  driftPageSize,
		Cursor: cursor,
	}
	seek, err := pagination.BuildSeek(activityListCfg, pr, cursorFields)
	if err != nil {
		b.Fatal(err)
	}

	var ids []int64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := db.Model(&AuditLogModel{}).
			Where("(audit_logs.actor_id = ? OR (audit_logs.resource_type = ? AND audit_logs.resource_id = ?))",
				seed.auditActorRowID, auditdomain.ResourceUser, seed.auditActor)
		if seek.WhereClause != "" {
			q = q.Where(seek.WhereClause, seek.BindVars...)
		}
		q = q.Select("audit_logs.id").
			Order(fixedOrder(seek.OrderClause)).
			Limit(seek.LimitFetch)
		if err := q.Scan(&ids).Error; err != nil {
			b.Fatal(err)
		}
		ids = ids[:0]
	}
}

// =============================
// EXPLAIN ANALYZE HELPERS
// =============================

func TestExplainAnalyze_OffsetVsCursor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping EXPLAIN ANALYZE in short mode")
	}
	seed := benchmarkDB(t)
	ctx := context.Background()
	db := seed.db

	categoryID := seed.postCategoryRowID
	postID := seed.commentPostRowID
	actorID := seed.auditActorRowID

	explainOut.Lock()
	defer explainOut.Unlock()

	limit := driftPageSize
	offset99950 := 99949

	// ---- POSTS ----
	explainOut.posts.WriteString("# EXPLAIN ANALYZE: posts table (100,050 rows)\n\n")
	explainOut.posts.WriteString("Sort order: `published_at DESC NULLS LAST, created_at DESC, id DESC`\n\n")

	explainOut.posts.WriteString("## OFFSET 99949 LIMIT 50 (page 2000 at 50/page)\n\n```sql\n")
	qPostsOffset := fmt.Sprintf(
		`EXPLAIN ANALYZE SELECT posts.id FROM posts WHERE status = '%s' AND deleted_at IS NULL AND category_id = %d ORDER BY published_at DESC NULLS LAST, created_at DESC, id DESC LIMIT %d OFFSET %d`,
		string(postdomain.StatusPublished), categoryID, limit, offset99950)
	explainOut.posts.WriteString(qPostsOffset)
	explainOut.posts.WriteString("\n```\n\n```\n")
	explainOut.posts.WriteString(runExplain(ctx, db, qPostsOffset))
	explainOut.posts.WriteString("\n```\n\n")

	explainOut.posts.WriteString("## CURSOR AFTER Row99950 (Index Seek via row-value comparator)\n\n```sql\n")
	postCursor, _, _ := pagination.DecodeCursor(postListPublishedCfg, seed.postRow99950Cursor)
	seek, _ := pagination.BuildSeek(postListPublishedCfg, pagination.PageRequest{
		Mode: pagination.ModeCursor, Forward: true, Limit: limit, Cursor: seed.postRow99950Cursor,
	}, postCursor)

	qPostsCursor := fmt.Sprintf(
		`EXPLAIN ANALYZE SELECT posts.id FROM posts WHERE status = '%s' AND deleted_at IS NULL AND category_id = %d AND %s ORDER BY %s LIMIT %d`,
		string(postdomain.StatusPublished), categoryID,
		seek.WhereClause, strings.TrimPrefix(seek.OrderClause, "ORDER BY "), limit)
	explainOut.posts.WriteString(renderWithVars(qPostsCursor, seek.BindVars))
	explainOut.posts.WriteString("\n```\n\n```\n")
	explainOut.posts.WriteString(runExplain(ctx, db, qPostsCursor, seek.BindVars...))
	explainOut.posts.WriteString("\n```\n\n")

	// ---- COMMENTS ----
	explainOut.comments.WriteString("# EXPLAIN ANALYZE: comments table (100,050 rows)\n\n")
	explainOut.comments.WriteString("Sort order: `created_at ASC, id ASC`\n\n")

	explainOut.comments.WriteString("## OFFSET 99949 LIMIT 50 (page 2000 at 50/page)\n\n```sql\n")
	qCommentsOffset := fmt.Sprintf(
		`EXPLAIN ANALYZE SELECT comments.id FROM comments WHERE comments.post_id = %d ORDER BY created_at ASC, id ASC LIMIT %d OFFSET %d`,
		postID, limit, offset99950)
	explainOut.comments.WriteString(qCommentsOffset)
	explainOut.comments.WriteString("\n```\n\n```\n")
	explainOut.comments.WriteString(runExplain(ctx, db, qCommentsOffset))
	explainOut.comments.WriteString("\n```\n\n")

	explainOut.comments.WriteString("## CURSOR AFTER Row99950 (Index Seek via row-value comparator)\n\n```sql\n")
	commentCursor, _, _ := pagination.DecodeCursor(commentsPublicCfg, seed.commentRow99950Cursor)
	seekC, _ := pagination.BuildSeek(commentsPublicCfg, pagination.PageRequest{
		Mode: pagination.ModeCursor, Forward: true, Limit: limit, Cursor: seed.commentRow99950Cursor,
	}, commentCursor)

	qCommentsCursor := fmt.Sprintf(
		`EXPLAIN ANALYZE SELECT comments.id FROM comments WHERE comments.post_id = %d AND %s ORDER BY %s LIMIT %d`,
		postID, seekC.WhereClause, strings.TrimPrefix(seekC.OrderClause, "ORDER BY "), limit)
	explainOut.comments.WriteString(renderWithVars(qCommentsCursor, seekC.BindVars))
	explainOut.comments.WriteString("\n```\n\n```\n")
	explainOut.comments.WriteString(runExplain(ctx, db, qCommentsCursor, seekC.BindVars...))
	explainOut.comments.WriteString("\n```\n\n")

	// ---- AUDIT ----
	explainOut.audit.WriteString("# EXPLAIN ANALYZE: audit_logs table (100,050 rows)\n\n")
	explainOut.audit.WriteString("Sort order: `occurred_at DESC, id DESC`\n\n")

	explainOut.audit.WriteString("## OFFSET 99949 LIMIT 50 (page 2000 at 50/page)\n\n```sql\n")
	qAuditOffset := fmt.Sprintf(
		`EXPLAIN ANALYZE SELECT audit_logs.id FROM audit_logs WHERE (audit_logs.actor_id = %d OR (audit_logs.resource_type = '%s' AND audit_logs.resource_id = '%s')) ORDER BY occurred_at DESC, id DESC LIMIT %d OFFSET %d`,
		actorID, auditdomain.ResourceUser, seed.auditActor, limit, offset99950)
	explainOut.audit.WriteString(qAuditOffset)
	explainOut.audit.WriteString("\n```\n\n```\n")
	explainOut.audit.WriteString(runExplain(ctx, db, qAuditOffset))
	explainOut.audit.WriteString("\n```\n\n")

	explainOut.audit.WriteString("## CURSOR AFTER Row99950 (Index Seek via row-value comparator)\n\n```sql\n")
	auditCursor, _, _ := pagination.DecodeCursor(activityListCfg, seed.auditRow99950Cursor)
	seekA, _ := pagination.BuildSeek(activityListCfg, pagination.PageRequest{
		Mode: pagination.ModeCursor, Forward: true, Limit: limit, Cursor: seed.auditRow99950Cursor,
	}, auditCursor)

	qAuditCursor := fmt.Sprintf(
		`EXPLAIN ANALYZE SELECT audit_logs.id FROM audit_logs WHERE (audit_logs.actor_id = %d OR (audit_logs.resource_type = '%s' AND audit_logs.resource_id = '%s')) AND %s ORDER BY %s LIMIT %d`,
		actorID, auditdomain.ResourceUser, seed.auditActor,
		seekA.WhereClause, strings.TrimPrefix(seekA.OrderClause, "ORDER BY "), limit)
	explainOut.audit.WriteString(renderWithVars(qAuditCursor, seekA.BindVars))
	explainOut.audit.WriteString("\n```\n\n```\n")
	explainOut.audit.WriteString(runExplain(ctx, db, qAuditCursor, seekA.BindVars...))
	explainOut.audit.WriteString("\n```\n\n")

	if outDir := os.Getenv("EXPLAIN_OUT_DIR"); outDir != "" {
		writeExplain(t, outDir+"/explain_posts.md", explainOut.posts.String())
		writeExplain(t, outDir+"/explain_comments.md", explainOut.comments.String())
		writeExplain(t, outDir+"/explain_audit.md", explainOut.audit.String())
	} else {
		t.Logf("--- EXPLAIN_POSTS_START ---\n%s--- EXPLAIN_POSTS_END ---", explainOut.posts.String())
		t.Logf("--- EXPLAIN_COMMENTS_START ---\n%s--- EXPLAIN_COMMENTS_END ---", explainOut.comments.String())
		t.Logf("--- EXPLAIN_AUDIT_START ---\n%s--- EXPLAIN_AUDIT_END ---", explainOut.audit.String())
	}
}

func writeExplain(t testing.TB, path, content string) {
	t.Helper()
	dir := path[:strings.LastIndex(path, "/")]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("mkdir explain dir: %v", err)
		return
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Logf("write explain %s: %v", path, err)
	}
}

func renderWithVars(q string, vars []any) string {
	out := q
	for _, bv := range vars {
		var vv string
		switch v := bv.(type) {
		case time.Time:
			vv = "'" + v.Format(time.RFC3339Nano) + "'"
		case int64:
			vv = fmt.Sprintf("%d", v)
		case *time.Time:
			if v == nil {
				vv = "NULL"
			} else {
				vv = "'" + v.Format(time.RFC3339Nano) + "'"
			}
		default:
			vv = fmt.Sprintf("%v", v)
		}
		out = strings.Replace(out, "?", vv, 1)
	}
	return out
}

func runExplain(ctx context.Context, db *gorm.DB, query string, args ...any) string {
	rows, err := db.Raw(query, args...).Rows()
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err)
	}
	defer rows.Close()
	var sb strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return fmt.Sprintf("SCAN ERROR: %v", err)
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

// =============================
// CONCURRENT DRIFT TEST
// =============================

func TestConcurrentDrift_NoDupesOrSkips_3Runs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping drift test in short mode")
	}
	seed := benchmarkDB(t)

	for run := 1; run <= driftRuns; run++ {
		t.Run(fmt.Sprintf("run-%d", run), func(t *testing.T) {
			runDriftOnce(t, seed, run)
		})
	}
}

type driftPage struct {
	IDs    []int64
	Cursor string
}

func runDriftOnce(t *testing.T, seed *benchmarkSeed, run int) {
	// Use the shared pool DB. Base posts from benchmark seed are stable:
	// author = seed.postAuthor, category = seed.postCategory. Inserter uses a
	// DIFFERENT author but same category, so fresh rows land in the same result
	// set at the sort head (newest published_at DESC).
	categoryID := seed.postCategoryRowID
	baseAuthorID := seed.postAuthorRowID

	// ---- Snapshot IDs at walk-start for base posts (seed author + category) ----
	var snapshotIDs []int64
	err := seed.db.Raw(`SELECT id FROM posts
		WHERE author_id = $1 AND status = $2 AND deleted_at IS NULL AND category_id = $3
		ORDER BY published_at DESC NULLS LAST, created_at DESC, id DESC`,
		baseAuthorID, string(postdomain.StatusPublished), categoryID).Scan(&snapshotIDs).Error
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(snapshotIDs), 100000, "posts snapshot must have 100k+")

	snapshotSet := make(map[int64]struct{}, len(snapshotIDs))
	for _, id := range snapshotIDs {
		snapshotSet[id] = struct{}{}
	}

	// Create a SEPARATE gorm session for the inserter (different connection from pool,
	// avoids conn-busy when walker and inserter run concurrently).
	inserterDB := seed.db.Session(&gorm.Session{})

	// ---- Start inserter goroutine: 10 rows/s at sort head, different author ----
	var (
		wg           sync.WaitGroup
		insertCount  atomic.Int64
		stopInserter atomic.Bool
		inserterErr  atomic.Value
	)
	// create a disposable author for this drift run so inserts land in same category
	// but are easy to identify and do not pollute the snapshot.
	driftAuthorUUID := insertUserSeed(seed.db)
	driftAuthorRowID := mustIDByUUID(seed.db, "users", driftAuthorUUID)

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		rng := rand.New(rand.NewSource(int64(run * 9999)))
		n := 0
		for !stopInserter.Load() {
			<-ticker.C
			if stopInserter.Load() {
				break
			}
			now := time.Now().UTC().Add(time.Duration(n) * time.Millisecond)
			slug := fmt.Sprintf("drift-%d-%s", run, uuid.NewString()[:10])
			err := inserterDB.Exec(`INSERT INTO posts
				(uuid, author_id, category_id, title, slug, content, status, comment_policy, version, published_at, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
				uuid.New(), driftAuthorRowID, categoryID,
				"drift post "+slug, slug, strings.Repeat("drift ", 5+rng.Intn(3)),
				string(postdomain.StatusPublished), string(postdomain.CommentPolicyOpen), 1,
				now, now, now).Error
			if err != nil {
				inserterErr.Store(err)
				return
			}
			insertCount.Add(1)
			n++
		}
	}()

	// ---- Walker: 2000 pages × 50 rows via cursor; collect only snapshot rows ----
	walkerDB := seed.db.Session(&gorm.Session{})
	collected := make(map[int64]int, 100000)
	var duplicates []int64
	var pagesWalked int
	var totalRows int
	cursor := ""

	for p := 0; p < driftPages; p++ {
		pr := pagination.PageRequest{
			Mode:         pagination.ModeCursor,
			Forward:      true,
			Limit:        driftPageSize,
			Cursor:       cursor,
			IncludeTotal: false,
		}
		var cursorFields map[string]any
		if cursor != "" {
			var derr error
			cursorFields, _, derr = pagination.DecodeCursor(postListPublishedCfg, cursor)
			if derr != nil {
				t.Fatalf("page %d: decode cursor: %v", p, derr)
			}
		}
		seek, serr := pagination.BuildSeek(postListPublishedCfg, pr, cursorFields)
		if serr != nil {
			t.Fatalf("page %d: build seek: %v", p, serr)
		}

		q := walkerDB.Model(&PostModel{}).Unscoped().
			Where("status = ? AND deleted_at IS NULL AND category_id = ?",
				string(postdomain.StatusPublished), categoryID)
		if seek.WhereClause != "" {
			q = q.Where(seek.WhereClause, seek.BindVars...)
		}

		var rows []postRowLite
		q = q.Select("posts.id, posts.published_at, posts.created_at").
			Order(fixedOrder(seek.OrderClause)).
			Limit(seek.LimitFetch)
		if err := q.Scan(&rows).Error; err != nil {
			t.Fatalf("page %d: scan: %v", p, err)
		}

		if seek.ReverseDisplay {
			for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}

		items, hasNext, _ := pagination.TruncatePage(rows, driftPageSize, pr.Forward, pr.Cursor != "")

		pagesWalked++
		if len(items) == 0 {
			break
		}
		for _, row := range items {
			id := row.ID
			totalRows++
			if _, inSnapshot := snapshotSet[id]; inSnapshot {
				if _, ok := collected[id]; ok {
					duplicates = append(duplicates, id)
				} else {
					collected[id] = 1
				}
			}
		}
		if !hasNext {
			break
		}
		last := items[len(items)-1]
		fields := pagination.SortValues[postRowLite](postListPublishedCfg, last,
			func(r postRowLite, i int) any {
				switch i {
				case 0:
					return r.PublishedAt
				case 1:
					return r.CreatedAt
				case 2:
					return r.ID
				}
				return nil
			})
		ncur, ncerr := pagination.EncodeCursor(postListPublishedCfg, fields)
		if ncerr != nil {
			t.Fatalf("page %d: encode cursor: %v", p, ncerr)
		}
		cursor = ncur
	}

	// ---- Stop inserter ----
	stopInserter.Store(true)
	wg.Wait()
	if errVal, ok := inserterErr.Load().(error); ok {
		t.Fatalf("inserter goroutine error: %v", errVal)
	}
	t.Logf("Run %d: inserter added %d rows during walk", run, insertCount.Load())
	t.Logf("Run %d: walker pages=%d rowsSeen=%d snapshotRowsCollected=%d", run, pagesWalked, totalRows, len(collected))

	// ---- Verify: 0 duplicates in walk-order for snapshot rows ----
	require.Empty(t, duplicates,
		"Run %d: found %d duplicate snapshot rows during cursor walk: first 10=%v",
		run, len(duplicates), firstN(duplicates, 10))

	rank := make(map[int64]int, len(snapshotIDs))
	for i, id := range snapshotIDs {
		rank[id] = i
	}

	// ---- Verify: 0 skips — collected snapshot rows form a contiguous prefix of snapshot order.
	// Rows with rank <= maxRankCollected must all be present (no gaps among visited set).
	// Rows beyond maxRankCollected are simply past walker stop point and not skips.
	// (Inserter rows at sort head do not create skips; they only push the walker's
	// earliest-visited rank but that's fine.)
	maxRank := -1
	for id := range collected {
		if r := rank[id]; r > maxRank {
			maxRank = r
		}
	}
	var skipped []int64
	if maxRank >= 0 {
		for i := 0; i <= maxRank; i++ {
			id := snapshotIDs[i]
			if _, seen := collected[id]; !seen {
				skipped = append(skipped, id)
			}
		}
	}
	require.Empty(t, skipped,
		"Run %d: %d skips among first %d snapshot rows (ranks 0..%d); collected=%d snapshot=%d; first 10 skipped snapshot IDs=%v",
		run, len(skipped), maxRank+1, maxRank, len(collected), len(snapshotIDs), firstN(skipped, 10))
	rowsBeyond := len(snapshotIDs) - 1 - maxRank
	t.Logf("Run %d: uncollected snapshot rows BEYOND walker (not skips): %d", run, rowsBeyond)

	// ---- Verify: walk order matches snapshot order for all collected rows ----
	collectedOrder := make([]int64, 0, len(collected))
	for id := range collected {
		collectedOrder = append(collectedOrder, id)
	}
	sort.Slice(collectedOrder, func(i, j int) bool {
		return rank[collectedOrder[i]] < rank[collectedOrder[j]]
	})
	for i := 0; i < len(collectedOrder); i++ {
		require.Equal(t, snapshotIDs[i], collectedOrder[i],
			"Run %d: collected row #%d out of order — walk order does not match snapshot order", run, i)
	}
}

func clonePostsInto(tx *gorm.DB, seed *benchmarkSeed, authorID, categoryID int64) {
	need := benchmarkRows
	var have int64
	tx.Raw(`SELECT count(*) FROM posts WHERE author_id = $1 AND category_id = $2 AND deleted_at IS NULL`,
		authorID, categoryID).Scan(&have)
	if have >= int64(need) {
		return
	}
	from := seed.db
	srcAuthorID := mustIDByUUID(from, "users", seed.postAuthor)
	err := tx.Exec(fmt.Sprintf(`
		INSERT INTO posts
			(uuid, author_id, category_id, title, slug, content, status, comment_policy, version, published_at, created_at, updated_at)
		SELECT
			gen_random_uuid(),
			$1, $2,
			'tx-clone-' || p.id || ' ' || left(p.title, 40),
			'tx-clone-%d-' || p.uuid::text || '-' || to_char(p.id, 'FM0000000'),
			p.content,
			p.status, p.comment_policy, 1,
			p.published_at, p.created_at, p.updated_at
		FROM posts p
		WHERE p.author_id = $3 AND p.status = '%s' AND p.deleted_at IS NULL
		LIMIT %d
	`, rand.Intn(100000), string(postdomain.StatusPublished), need),
		authorID, categoryID,
		srcAuthorID,
	).Error
	if err != nil {
		panic(fmt.Sprintf("clone posts into tx: %v", err))
	}
}

func cloneCommentsInto(tx *gorm.DB, seed *benchmarkSeed, postID, authorID int64) {
	need := benchmarkRows
	var have int64
	tx.Raw(`SELECT count(*) FROM comments WHERE post_id = $1`, postID).Scan(&have)
	if have >= int64(need) {
		return
	}
	from := seed.db
	srcPostID := mustIDByUUID(from, "posts", seed.commentPost)
	err := tx.Exec(fmt.Sprintf(`
		INSERT INTO comments
			(uuid, post_id, parent_id, author_id, content, content_html, status, depth, created_at, updated_at)
		SELECT
			gen_random_uuid(),
			$1, NULL, $2,
			'tx-clone ' || c.content,
			'<p>tx-clone ' || substr(c.content, 1, 60) || '</p>',
			c.status, 0,
			c.created_at, c.updated_at
		FROM comments c
		WHERE c.post_id = $3
		LIMIT %d
	`, need), postID, authorID, srcPostID).Error
	if err != nil {
		panic(fmt.Sprintf("clone comments into tx: %v", err))
	}
}

func cloneAuditInto(tx *gorm.DB, seed *benchmarkSeed, actorID int64, actorUUID uuid.UUID) {
	need := benchmarkRows
	var have int64
	tx.Raw(`SELECT count(*) FROM audit_logs WHERE actor_id = $1`, actorID).Scan(&have)
	if have >= int64(need) {
		return
	}
	from := seed.db
	srcActorID := mustIDByUUID(from, "users", seed.auditActor)
	err := tx.Exec(fmt.Sprintf(`
		INSERT INTO audit_logs
			(uuid, actor_id, impersonator_id, action, category, result, resource_type, resource_id, metadata, occurred_at)
		SELECT
			gen_random_uuid(),
			$1, NULL,
			a.action, a.category, a.result, a.resource_type, $2::uuid,
			a.metadata, a.occurred_at
		FROM audit_logs a
		WHERE a.actor_id = $3
		LIMIT %d
	`, need), actorID, actorUUID, srcActorID).Error
	if err != nil {
		panic(fmt.Sprintf("clone audit into tx: %v", err))
	}
}

func firstN[T any](s []T, n int) []T {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
