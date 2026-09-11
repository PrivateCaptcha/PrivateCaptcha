-- The salt rotation invalidates puzzles whose original validity is no longer retained.
UPDATE backend.properties
SET validity_interval = INTERVAL '24 hours',
    salt = gen_random_bytes(8)
WHERE validity_interval > INTERVAL '24 hours';
