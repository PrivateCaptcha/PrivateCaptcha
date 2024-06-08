CREATE UNLOGGED TABLE IF NOT EXISTS cache (
    id serial PRIMARY KEY,
    key text UNIQUE NOT NULL,
    value bytea NOT NULL,
    expires_at timestamp DEFAULT CURRENT_TIMESTAMP + INTERVAL '5 minutes' NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS index_cache_key ON cache (key);

CREATE OR REPLACE PROCEDURE delete_expired_cache() AS
$$
BEGIN
    DELETE FROM cache
    WHERE expires_at < NOW();

    COMMIT;
END;
$$ LANGUAGE plpgsql;
