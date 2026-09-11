ALTER TABLE privatecaptcha.request_logs ADD COLUMN IF NOT EXISTS puzzle_id UInt64 DEFAULT 0;
ALTER TABLE privatecaptcha.request_logs ADD COLUMN IF NOT EXISTS expires_at DateTime DEFAULT toDateTime(0);
ALTER TABLE privatecaptcha.request_logs ADD COLUMN IF NOT EXISTS ip_family UInt8 DEFAULT 0;
ALTER TABLE privatecaptcha.request_logs ADD COLUMN IF NOT EXISTS ip_prefix UInt64 DEFAULT 0;
ALTER TABLE privatecaptcha.request_logs ADD COLUMN IF NOT EXISTS browser LowCardinality(String) DEFAULT '';
ALTER TABLE privatecaptcha.request_logs ADD COLUMN IF NOT EXISTS browser_major UInt16 DEFAULT 0;
ALTER TABLE privatecaptcha.request_logs ADD COLUMN IF NOT EXISTS os LowCardinality(String) DEFAULT '';
ALTER TABLE privatecaptcha.request_logs ADD COLUMN IF NOT EXISTS device LowCardinality(String) DEFAULT '';

ALTER TABLE privatecaptcha.verify_logs ADD COLUMN IF NOT EXISTS expires_at DateTime DEFAULT toDateTime(0);
ALTER TABLE privatecaptcha.verify_logs ADD COLUMN IF NOT EXISTS user_agent LowCardinality(String) DEFAULT 'unknown';
