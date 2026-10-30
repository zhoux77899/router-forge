-- +goose Up
CREATE TABLE environments (
    id TEXT PRIMARY KEY, name TEXT NOT NULL, type TEXT NOT NULL,
    config_json TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE TABLE providers (
    id TEXT PRIMARY KEY, name TEXT NOT NULL, base_url TEXT NOT NULL,
    credential_id TEXT, created_at TEXT NOT NULL
);
CREATE TABLE provider_credentials (
    id TEXT PRIMARY KEY, provider_id TEXT NOT NULL REFERENCES providers(id),
    api_key TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE TABLE models (
    id TEXT PRIMARY KEY, provider_id TEXT NOT NULL REFERENCES providers(id),
    name TEXT NOT NULL, protocol TEXT NOT NULL, config_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE evaluation_suites (
    id TEXT PRIMARY KEY, config_json TEXT NOT NULL
);
CREATE TABLE evaluation_runs (
    id TEXT PRIMARY KEY, state TEXT NOT NULL, snapshot_json TEXT NOT NULL,
    created_at TEXT NOT NULL, started_at TEXT, finished_at TEXT, error TEXT NOT NULL DEFAULT ''
);
CREATE TABLE evaluation_tasks (
    id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES evaluation_runs(id),
    name TEXT NOT NULL, state TEXT NOT NULL, UNIQUE(run_id, name)
);
CREATE TABLE evaluation_attempts (
    id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES evaluation_tasks(id),
    run_id TEXT NOT NULL REFERENCES evaluation_runs(id), state TEXT NOT NULL,
    job_name TEXT NOT NULL UNIQUE, result_json TEXT NOT NULL
);
CREATE TABLE job_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT NOT NULL REFERENCES evaluation_runs(id),
    attempt_id TEXT, timestamp TEXT NOT NULL, kind TEXT NOT NULL, message TEXT NOT NULL,
    UNIQUE(run_id, attempt_id, timestamp, kind, message)
);
CREATE INDEX events_run_id ON job_events(run_id, id);
CREATE TABLE llm_requests (
    id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES evaluation_runs(id),
    task_id TEXT NOT NULL REFERENCES evaluation_tasks(id),
    attempt_id TEXT NOT NULL REFERENCES evaluation_attempts(id),
    provider_id TEXT NOT NULL REFERENCES providers(id), model_id TEXT NOT NULL REFERENCES models(id),
    protocol TEXT NOT NULL, kind TEXT NOT NULL, requested_model TEXT NOT NULL, actual_model TEXT NOT NULL,
    upstream_request_id TEXT, upstream_response_id TEXT, started_at TEXT NOT NULL, finished_at TEXT,
    duration_ms INTEGER, first_byte_ms INTEGER, http_status INTEGER, state TEXT NOT NULL,
    error_class TEXT, input_tokens INTEGER, output_tokens INTEGER, cache_read_tokens INTEGER,
    cache_write_tokens INTEGER, reasoning_tokens INTEGER, total_tokens INTEGER,
    usage_source TEXT NOT NULL, usage_completeness TEXT NOT NULL,
    cost_amount TEXT, currency TEXT, cost_source TEXT NOT NULL, record_json TEXT NOT NULL
);
CREATE INDEX requests_run_time ON llm_requests(run_id, started_at DESC);
CREATE INDEX requests_attempt ON llm_requests(attempt_id);

-- +goose Down
DROP TABLE llm_requests;
DROP TABLE job_events;
DROP TABLE evaluation_attempts;
DROP TABLE evaluation_tasks;
DROP TABLE evaluation_runs;
DROP TABLE evaluation_suites;
DROP TABLE models;
DROP TABLE provider_credentials;
DROP TABLE providers;
DROP TABLE environments;
