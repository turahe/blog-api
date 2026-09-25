-- +goose Up
-- Sanitized HTML rendered from comments.content on create and edit. Rows written
-- before this column existed keep '' and are rendered when read.
ALTER TABLE comments ADD COLUMN content_html text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE comments DROP COLUMN IF EXISTS content_html;
