CREATE TABLE users (
                       id           text        PRIMARY KEY,
                       email        text        NOT NULL DEFAULT '',
                       display_name text        NOT NULL DEFAULT '',
                       created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE vacations (
                           id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
                           user_id    text        NOT NULL REFERENCES users(id),
                           start_date date        NOT NULL,
                           end_date   date        NOT NULL,
                           note       text        NOT NULL DEFAULT '',
                           status     text        NOT NULL DEFAULT 'approved'
                               CHECK (status IN ('approved', 'overridden', 'cancelled')),
                           created_at timestamptz NOT NULL DEFAULT now(),
                           created_by text        NOT NULL,
                           CONSTRAINT valid_range CHECK (end_date >= start_date)
);

CREATE INDEX ON vacations (user_id);
CREATE INDEX ON vacations (start_date, end_date) WHERE status IN ('approved', 'overridden');

CREATE TABLE settings (
                          key        text        PRIMARY KEY,
                          value      jsonb       NOT NULL,
                          updated_at timestamptz NOT NULL DEFAULT now(),
                          updated_by text        NOT NULL DEFAULT 'system'
);

INSERT INTO settings (key, value) VALUES
                                      ('min_present',    '8'::jsonb),
                                      ('team_size',      '15'::jsonb),
                                      ('weekend_counts', 'false'::jsonb);

CREATE TABLE audit_log (
                           id        uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
                           actor_id  text        NOT NULL,
                           action    text        NOT NULL,
                           target_id text        NOT NULL DEFAULT '',
                           payload   jsonb       NOT NULL DEFAULT '{}',
                           at        timestamptz NOT NULL DEFAULT now()
);