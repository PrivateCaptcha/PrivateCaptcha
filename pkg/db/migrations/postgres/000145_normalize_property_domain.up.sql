UPDATE backend.properties
SET domain = LOWER(domain)
WHERE domain <> LOWER(domain);
