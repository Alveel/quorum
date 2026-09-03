-- users.working_days bit layout: bit i = time.Weekday() value (Sun=0..Sat=6).
-- 62 = 0b0111110 = Mon,Tue,Wed,Thu,Fri.
ALTER TABLE users ADD COLUMN working_days smallint NOT NULL DEFAULT 62;
ALTER TABLE users ADD COLUMN active boolean NOT NULL DEFAULT true;

CREATE TABLE roles (
    id          serial PRIMARY KEY,
    name        text   NOT NULL UNIQUE,
    min_present int    NOT NULL DEFAULT 0 CHECK (min_present >= 0)
);

CREATE TABLE user_roles (
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id int  NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE holidays (
    date   date PRIMARY KEY,
    name   text NOT NULL,
    source text NOT NULL CHECK (source IN ('imported', 'manual'))
);

-- team_size / weekend_counts are superseded by per-member working_days + per-role minimums.
DELETE FROM settings WHERE key IN ('team_size', 'weekend_counts');
INSERT INTO settings (key, value) VALUES ('holiday_country', '"nl"'::jsonb)
    ON CONFLICT (key) DO NOTHING;
