-- +goose Up
ALTER TABLE categories
    ADD COLUMN IF NOT EXISTS lft integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rgt integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS depth integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS sort_order integer NOT NULL DEFAULT 0;

-- Flat backfill ordered by name (valid nested-set numbers). parent_id may
-- disagree with lft/rgt until CategoryService.RebuildAll runs at bootstrap
-- (Task 5) and rewrites bounds from adjacency.
WITH ordered AS (
    SELECT id, row_number() OVER (ORDER BY name ASC, id ASC) AS n
    FROM categories
)
UPDATE categories c
SET
    lft = (o.n * 2 - 1),
    rgt = (o.n * 2),
    depth = 0,
    sort_order = (o.n - 1)::integer
FROM ordered o
WHERE c.id = o.id;

-- +goose Down
ALTER TABLE categories
    DROP COLUMN IF EXISTS lft,
    DROP COLUMN IF EXISTS rgt,
    DROP COLUMN IF EXISTS depth,
    DROP COLUMN IF EXISTS sort_order;
