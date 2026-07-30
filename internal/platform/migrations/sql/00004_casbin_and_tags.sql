-- +goose Up
CREATE TABLE IF NOT EXISTS casbin_rules (
    id bigserial PRIMARY KEY,
    ptype varchar(100),
    v0 varchar(255),
    v1 varchar(255),
    v2 varchar(255),
    v3 varchar(255),
    v4 varchar(255),
    v5 varchar(255)
);
CREATE INDEX IF NOT EXISTS idx_casbin_rules_ptype ON casbin_rules (ptype);
CREATE INDEX IF NOT EXISTS idx_casbin_rules_v0 ON casbin_rules (v0);

-- +goose Down
DROP TABLE IF EXISTS casbin_rules;
