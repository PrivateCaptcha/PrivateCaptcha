ALTER TABLE privatecaptcha.request_logs_1mo
    MODIFY TTL timestamp + INTERVAL 3 YEAR;
