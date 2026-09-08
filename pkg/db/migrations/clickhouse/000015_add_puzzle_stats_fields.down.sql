ALTER TABLE privatecaptcha.verify_logs DROP COLUMN IF EXISTS expires_at;

ALTER TABLE privatecaptcha.request_logs DROP COLUMN IF EXISTS device;
ALTER TABLE privatecaptcha.request_logs DROP COLUMN IF EXISTS os;
ALTER TABLE privatecaptcha.request_logs DROP COLUMN IF EXISTS browser_major;
ALTER TABLE privatecaptcha.request_logs DROP COLUMN IF EXISTS browser;
ALTER TABLE privatecaptcha.request_logs DROP COLUMN IF EXISTS ip_prefix;
ALTER TABLE privatecaptcha.request_logs DROP COLUMN IF EXISTS ip_family;
ALTER TABLE privatecaptcha.request_logs DROP COLUMN IF EXISTS expires_at;
ALTER TABLE privatecaptcha.request_logs DROP COLUMN IF EXISTS puzzle_id;
