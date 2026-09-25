-- +goose Up
-- One random salt per UTC day for anonymous visitor hashes, shared by every API replica. Salts
-- older than yesterday are deleted, after which that day's hashes cannot be recomputed.
CREATE TABLE analytics_salts (
    day date PRIMARY KEY,
    salt bytea NOT NULL CHECK (octet_length(salt) = 32),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS analytics_salts;
