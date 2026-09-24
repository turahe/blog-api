-- +goose Up
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS avatar_id bigint REFERENCES media_assets(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS users_avatar_id_idx ON users (avatar_id);

ALTER TABLE categories
    ADD COLUMN IF NOT EXISTS image_id bigint REFERENCES media_assets(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS categories_image_id_idx ON categories (image_id);

ALTER TABLE posts
    ADD COLUMN IF NOT EXISTS cover_image_media_id bigint REFERENCES media_assets(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS posts_cover_image_media_id_idx ON posts (cover_image_media_id);

CREATE TABLE post_media (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    post_id bigint NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    media_asset_id bigint NOT NULL REFERENCES media_assets(id) ON DELETE CASCADE,
    kind text NOT NULL
        CHECK (kind IN ('cover', 'inline_image', 'attachment')),
    sort_order integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (post_id, media_asset_id, kind)
);
CREATE INDEX post_media_post_sort_idx ON post_media (post_id, sort_order);
CREATE INDEX post_media_media_asset_id_idx ON post_media (media_asset_id);
CREATE INDEX post_media_post_kind_idx ON post_media (post_id, kind);

-- +goose Down
DROP TABLE IF EXISTS post_media;
ALTER TABLE posts DROP COLUMN IF EXISTS cover_image_media_id;
ALTER TABLE categories DROP COLUMN IF EXISTS image_id;
ALTER TABLE users DROP COLUMN IF EXISTS avatar_id;
