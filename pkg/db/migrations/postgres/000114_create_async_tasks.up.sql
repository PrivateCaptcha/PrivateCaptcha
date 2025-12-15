CREATE TABLE IF NOT EXISTS backend.async_tasks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    handler TEXT NOT NULL,
    input jsonb NOT NULL,
    output jsonb,
    user_id INT REFERENCES backend.users(id) ON DELETE CASCADE,
    reference_id TEXT NOT NULL,
    processing_attempts INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    scheduled_at TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    processed_at TIMESTAMPTZ DEFAULT NULL
);
