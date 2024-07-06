CREATE TABLE IF NOT EXISTS privatecaptcha.user_limits
(
    user_id UInt32,
    limit UInt64,
    updated_at DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree
ORDER BY user_id;
