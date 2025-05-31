CREATE TABLE IF NOT EXISTS privatecaptcha.request_logs
(
    user_id UInt32,
    org_id UInt32,
    property_id UInt32,
    fingerprint UInt64,
    timestamp DateTime
)
ENGINE = Null
