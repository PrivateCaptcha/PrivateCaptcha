CREATE ROLE pc_backend_role;
GRANT SELECT, INSERT, DELETE ON privatecaptcha.* TO pc_backend_role;
GRANT ALTER DELETE ON privatecaptcha.* to pc_backend_role;
GRANT ALTER UPDATE(_row_exists) ON privatecaptcha.* to pc_backend_role;

CREATE USER captchasrv IDENTIFIED BY 'uwnhNn4YW01';
GRANT pc_backend_role TO captchasrv;
