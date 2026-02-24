-- Stats test tables (shared schema with ClickHouse)
-- Deterministic data: 10000 rows each, same generation logic as clickhouse/02_stats.sql

DROP TABLE IF EXISTS stats_numbers;
DROP TABLE IF EXISTS stats_basic;

CREATE TABLE stats_basic (
    id serial PRIMARY KEY,
    name varchar(100) NOT NULL,
    email varchar(255),
    age integer,
    score double precision NOT NULL,
    status varchar(20) NOT NULL,
    category varchar(50) NOT NULL,
    created_at timestamp NOT NULL,
    updated_at timestamp,
    note text,
    payload jsonb NOT NULL
);

CREATE TABLE stats_numbers (
    id serial PRIMARY KEY,
    int_val integer NOT NULL,
    float_val double precision NOT NULL,
    nullable_int integer,
    small_distinct integer NOT NULL,
    created_at timestamp NOT NULL
);

INSERT INTO stats_basic (name, email, age, score, status, category, created_at, updated_at, note, payload)
SELECT
    'user_' || lpad(i::text, 5, '0'),
    CASE WHEN i % 10 = 0 THEN NULL ELSE 'user_' || lpad(i::text, 5, '0') || '@test.com' END,
    CASE WHEN i % 100 < 15 THEN NULL ELSE 18 + (i % 63) END,
    (i % 1000) / 10.0,
    CASE i % 5
        WHEN 0 THEN 'active'
        WHEN 1 THEN 'inactive'
        WHEN 2 THEN 'pending'
        WHEN 3 THEN 'suspended'
        WHEN 4 THEN 'deleted'
    END,
    'cat_' || lpad((i % 20)::text, 2, '0'),
    '2024-01-01 00:00:00'::timestamp + (i || ' minutes')::interval,
    CASE WHEN i % 5 = 0 THEN NULL ELSE '2024-01-01 00:00:00'::timestamp + (i || ' minutes')::interval + interval '1 hour' END,
    CASE WHEN i % 10 < 3 THEN NULL ELSE 'Note text for row ' || i END,
    ('{"key": ' || i || '}')::jsonb
FROM generate_series(1, 10000) AS s(i);

INSERT INTO stats_numbers (int_val, float_val, nullable_int, small_distinct, created_at)
SELECT
    (i % 1000) - 500,
    i * 1.5,
    CASE WHEN i % 5 = 0 THEN NULL ELSE i % 100 END,
    i % 10,
    '2024-01-01 00:00:00'::timestamp + (i || ' minutes')::interval
FROM generate_series(1, 10000) AS s(i);

ANALYZE stats_basic;
ANALYZE stats_numbers;
