CREATE TABLE IF NOT EXISTS privatecaptcha.verify_logs
(
    user_id UInt32,
    org_id UInt32,
    property_id UInt32,
    puzzle_id UInt64,
    status UInt8,
    timestamp DateTime
)
ENGINE = Null
