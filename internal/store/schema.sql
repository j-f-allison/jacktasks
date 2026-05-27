CREATE TABLE IF NOT EXISTS projects (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  deleted_at   INTEGER,
  archived     INTEGER NOT NULL DEFAULT 0,
  arrived_at   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS categories (
  id              TEXT PRIMARY KEY,
  name            TEXT NOT NULL,
  project_id      TEXT REFERENCES projects(id),
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL,
  deleted_at      INTEGER,
  archived        INTEGER NOT NULL DEFAULT 0,
  arrived_at      INTEGER NOT NULL DEFAULT 0,
  target_minutes  INTEGER,
  target_period   TEXT,
  schedule_mask   INTEGER
);

CREATE TABLE IF NOT EXISTS sessions (
  id                   TEXT PRIMARY KEY,
  project_id           TEXT REFERENCES projects(id),
  category_id          TEXT NOT NULL REFERENCES categories(id),
  planned_duration_min INTEGER NOT NULL,
  actual_duration_sec  INTEGER NOT NULL,
  started_at           INTEGER NOT NULL,
  ended_at             INTEGER NOT NULL,
  end_notes            TEXT,
  status               TEXT NOT NULL,
  created_at           INTEGER NOT NULL,
  device_id            TEXT NOT NULL,
  arrived_at           INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS captures (
  id                 TEXT PRIMARY KEY,
  session_id         TEXT NOT NULL REFERENCES sessions(id),
  text               TEXT NOT NULL,
  captured_at        INTEGER NOT NULL,
  cleared            INTEGER NOT NULL DEFAULT 0,
  sent_to_reminders  INTEGER NOT NULL DEFAULT 0,
  created_at         INTEGER NOT NULL,
  updated_at         INTEGER NOT NULL DEFAULT 0,
  arrived_at         INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sync_state (
  table_name    TEXT PRIMARY KEY,
  last_pull_at  INTEGER,
  last_push_at  INTEGER
);

CREATE TABLE IF NOT EXISTS config (
  key    TEXT PRIMARY KEY,
  value  TEXT
);

CREATE INDEX IF NOT EXISTS idx_categories_project ON categories(project_id);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_id);
CREATE INDEX IF NOT EXISTS idx_captures_session ON captures(session_id);
CREATE INDEX IF NOT EXISTS idx_categories_updated ON categories(updated_at);
CREATE INDEX IF NOT EXISTS idx_projects_updated ON projects(updated_at);
