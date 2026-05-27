CREATE TABLE IF NOT EXISTS privatecaptcha.form_submit_logs
(
    user_id UInt32,
    org_id UInt32,
    form_id UInt32,
    status UInt8,
    timestamp DateTime
)
ENGINE = Null
