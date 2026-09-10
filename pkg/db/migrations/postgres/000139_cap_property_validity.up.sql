UPDATE backend.properties
SET validity_interval = INTERVAL '24 hours'
WHERE validity_interval > INTERVAL '24 hours';
