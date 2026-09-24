-- +goose Up
CREATE TABLE media_assets (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    storage_key text NOT NULL,
    original_filename text NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL DEFAULT 0,
    width integer,
    height integer,
    checksum_sha256 text,
    disk text NOT NULL
        CHECK (disk IN ('s3', 'r2', 'minio', 'do_spaces')),
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'ready', 'failed')),
    uploaded_by bigint REFERENCES users(id) ON DELETE SET NULL,
    tags text[] NOT NULL DEFAULT '{}',
    presign_expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE UNIQUE INDEX media_assets_storage_key_unique ON media_assets (storage_key);
CREATE INDEX media_assets_uploaded_by_idx ON media_assets (uploaded_by);
CREATE INDEX media_assets_status_idx ON media_assets (status) WHERE deleted_at IS NULL;
CREATE INDEX media_assets_deleted_at_idx ON media_assets (deleted_at);

-- +goose Down
DROP TABLE IF EXISTS media_assets;
