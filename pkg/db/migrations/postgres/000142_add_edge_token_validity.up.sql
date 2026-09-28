ALTER TABLE backend.properties ADD COLUMN edge_token_validity_interval INTERVAL NOT NULL DEFAULT INTERVAL '0 seconds';
