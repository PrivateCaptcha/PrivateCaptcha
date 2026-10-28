-- PostgreSQL does not support removing enum values. Invalidate pending new-address challenges.
UPDATE backend.sessions
SET challenge_kind = NULL,
    challenge_code = NULL,
    challenge_email = NULL,
    challenge_expires_at = NULL,
    version = version + 1
WHERE challenge_kind = 'email_change_new';
