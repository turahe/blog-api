-- +goose Up
-- Per-post search and social overrides. A row exists only once a post's SEO was edited;
-- rendering falls back to values derived from the post and the site settings for every
-- empty field. Text is stored normalised and without markup.
CREATE TABLE post_seo (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    post_id bigint NOT NULL UNIQUE REFERENCES posts(id) ON DELETE CASCADE,
    seo_title text NOT NULL DEFAULT '' CHECK (char_length(seo_title) <= 200),
    seo_description text NOT NULL DEFAULT '' CHECK (char_length(seo_description) <= 500),
    seo_keywords jsonb NOT NULL DEFAULT '[]',
    og_title text NOT NULL DEFAULT '' CHECK (char_length(og_title) <= 200),
    og_description text NOT NULL DEFAULT '' CHECK (char_length(og_description) <= 500),
    og_image_id bigint REFERENCES media_assets(id) ON DELETE SET NULL,
    og_url text NOT NULL DEFAULT '' CHECK (char_length(og_url) <= 2048),
    twitter_card text NOT NULL DEFAULT ''
        CHECK (twitter_card IN ('', 'summary', 'summary_large_image', 'app', 'player')),
    twitter_title text NOT NULL DEFAULT '' CHECK (char_length(twitter_title) <= 200),
    twitter_description text NOT NULL DEFAULT '' CHECK (char_length(twitter_description) <= 500),
    twitter_image_id bigint REFERENCES media_assets(id) ON DELETE SET NULL,
    twitter_creator text NOT NULL DEFAULT '' CHECK (char_length(twitter_creator) <= 16),
    canonical_url text NOT NULL DEFAULT '' CHECK (char_length(canonical_url) <= 2048),
    robots_noindex boolean NOT NULL DEFAULT false,
    robots_nofollow boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX post_seo_og_image_idx ON post_seo (og_image_id) WHERE og_image_id IS NOT NULL;
CREATE INDEX post_seo_twitter_image_idx ON post_seo (twitter_image_id) WHERE twitter_image_id IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS post_seo;
