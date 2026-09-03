DELETE FROM settings WHERE key = 'holiday_country';
INSERT INTO settings (key, value) VALUES
    ('team_size', '15'::jsonb),
    ('weekend_counts', 'false'::jsonb)
    ON CONFLICT (key) DO NOTHING;

DROP TABLE holidays;
DROP TABLE user_roles;
DROP TABLE roles;
ALTER TABLE users DROP COLUMN active;
ALTER TABLE users DROP COLUMN working_days;
