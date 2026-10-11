ALTER TABLE backend.apikeys
    ADD CONSTRAINT apikeys_requests_nonnegative
    CHECK (requests_burst >= 0 AND requests_per_second >= 0) NOT VALID;
