CREATE TABLE IF NOT EXISTS backend.user_settings (
    id SERIAL PRIMARY KEY,
    user_id INT NOT NULL REFERENCES backend.users(id) ON DELETE CASCADE,
    weekly_report BOOLEAN NOT NULL DEFAULT FALSE,
    monthly_report BOOLEAN NOT NULL DEFAULT FALSE,
    notifications_email TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id)
);
