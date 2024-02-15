CREATE TABLE IF NOT EXISTS privatecaptcha.request_logs
(
    user_id UInt32,
    property_id UInt32,
    fingerprint String,
    timestamp DateTime
)
ENGINE = Null
