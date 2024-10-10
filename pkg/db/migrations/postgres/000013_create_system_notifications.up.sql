CREATE TABLE IF NOT EXISTS backend.system_notifications(
    id SERIAL PRIMARY KEY,
    message TEXT NOT NULL,
    start_date TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    end_date TIMESTAMPTZ DEFAULT NULL,
    user_id INTEGER DEFAULT NULL,
    is_active BOOLEAN DEFAULT TRUE
);
