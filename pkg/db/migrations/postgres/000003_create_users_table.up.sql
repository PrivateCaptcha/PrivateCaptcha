CREATE TABLE IF NOT EXISTS users(
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL,
    subscription_id INTEGER DEFAULT NULL,
    created_at TIMESTAMPTZ DEFAULT current_timestamp,
    updated_at TIMESTAMPTZ DEFAULT current_timestamp,
    deleted_at TIMESTAMPTZ NULL DEFAULT NULL,
    FOREIGN KEY(subscription_id) REFERENCES subscriptions(id) ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS index_user_email ON users(email);

 CREATE OR REPLACE TRIGGER deleted_record_insert AFTER DELETE ON users
    FOR EACH ROW EXECUTE FUNCTION deleted_record_insert();
