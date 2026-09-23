CREATE TABLE notifications (
  id TEXT PRIMARY KEY,
  payload_json TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  sent_at INTEGER,
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at INTEGER NOT NULL,
  lease_until INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_notifications_pending ON notifications(sent_at,next_attempt_at,lease_until);
