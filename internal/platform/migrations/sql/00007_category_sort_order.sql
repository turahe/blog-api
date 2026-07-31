-- +goose Up
ALTER TABLE categories
    ADD COLUMN IF NOT EXISTS sort_order integer NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS categories_parent_sort_idx ON categories (parent_id, sort_order);

-- +goose Down
DROP INDEX IF EXISTS categories_parent_sort_idx;
ALTER TABLE categories DROP COLUMN IF EXISTS sort_order;
