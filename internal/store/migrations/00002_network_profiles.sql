-- +goose Up
CREATE TABLE network_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    scope TEXT NOT NULL,
    environment_id TEXT,
    revision TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE network_profile_versions (
    revision TEXT PRIMARY KEY,
    profile_id TEXT NOT NULL REFERENCES network_profiles(id),
    config_json TEXT NOT NULL
);
CREATE INDEX network_profile_versions_owner ON network_profile_versions(profile_id);
CREATE TABLE proxy_credentials (
    id TEXT PRIMARY KEY,
    profile_id TEXT NOT NULL REFERENCES network_profiles(id),
    username TEXT NOT NULL,
    password TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX proxy_credentials_owner ON proxy_credentials(profile_id);
ALTER TABLE providers ADD COLUMN network_profile_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE providers DROP COLUMN network_profile_id;
DROP TABLE proxy_credentials;
DROP TABLE network_profile_versions;
DROP TABLE network_profiles;
