-- TaskBridge initial database schema


-- =========================================================
-- Clients
-- 每台 Win / Linux / macOS / AutoDL / Codex 一个客户端身份
-- =========================================================

CREATE TABLE clients (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,

    token_hash TEXT NOT NULL UNIQUE,
    scopes TEXT NOT NULL,

    created_at INTEGER NOT NULL,
    revoked_at INTEGER
);

CREATE INDEX idx_clients_token_hash
ON clients(token_hash);


-- =========================================================
-- Tasks
-- tb run / Codex task 的核心状态
-- =========================================================

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,

    client_id TEXT,

    name TEXT NOT NULL,
    kind TEXT,

    host TEXT,
    platform TEXT,
    cwd TEXT,
    command TEXT,

    status TEXT NOT NULL,

    created_at INTEGER NOT NULL,
    started_at INTEGER NOT NULL,
    last_heartbeat INTEGER NOT NULL,
    finished_at INTEGER,

    exit_code INTEGER,
    duration_ms INTEGER,

    metadata_json TEXT,
    error_summary TEXT
);

CREATE INDEX idx_tasks_status
ON tasks(status);

CREATE INDEX idx_tasks_last_heartbeat
ON tasks(last_heartbeat);

CREATE INDEX idx_tasks_status_heartbeat
ON tasks(status, last_heartbeat);

CREATE INDEX idx_tasks_client_id
ON tasks(client_id);


-- =========================================================
-- Questions
-- Codex Human-in-the-loop
-- =========================================================

CREATE TABLE questions (
    id TEXT PRIMARY KEY,

    client_id TEXT,
    task_id TEXT,

    session_id TEXT,
    turn_id TEXT,

    question_type TEXT NOT NULL,
    question TEXT NOT NULL,
    options_json TEXT,

    status TEXT NOT NULL,
    answer TEXT,

    created_at INTEGER NOT NULL,
    expires_at INTEGER,
    answered_at INTEGER
);

CREATE INDEX idx_questions_status
ON questions(status);

CREATE INDEX idx_questions_task_id
ON questions(task_id);

CREATE INDEX idx_questions_session_id
ON questions(session_id);

CREATE INDEX idx_questions_status_expires
ON questions(status, expires_at);


-- =========================================================
-- Events
-- 用于审计、去重和调试
-- =========================================================

CREATE TABLE events (
    id TEXT PRIMARY KEY,

    task_id TEXT,
    client_id TEXT,

    event_type TEXT NOT NULL,
    payload_json TEXT,

    created_at INTEGER NOT NULL
);

CREATE INDEX idx_events_task_id
ON events(task_id);

CREATE INDEX idx_events_event_type
ON events(event_type);

CREATE INDEX idx_events_created_at
ON events(created_at);
