ALTER TABLE privatecaptcha.request_logs ADD COLUMN IF NOT EXISTS rule_id UInt32 DEFAULT 0;
