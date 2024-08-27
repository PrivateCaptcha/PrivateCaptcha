CREATE TABLE IF NOT EXISTS backend.deleted_records (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    data jsonb NOT NULL,
    deleted_at timestamptz NOT NULL DEFAULT current_timestamp,
    table_name varchar(200) NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT current_timestamp
);

CREATE OR REPLACE FUNCTION backend.deleted_record_insert() RETURNS trigger
    LANGUAGE plpgsql
AS $$
    BEGIN
        EXECUTE 'INSERT INTO backend.deleted_records (data, table_name) VALUES ($1, $2)'
        USING to_jsonb(OLD.*), TG_TABLE_NAME;

        RETURN OLD;
    END;
$$;
