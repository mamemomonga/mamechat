ALTER TABLE channels
  ADD COLUMN IF NOT EXISTS suspended_at TIMESTAMPTZ;

ALTER TABLE channels
  ADD COLUMN IF NOT EXISTS suspend_retention_hours INTEGER;
