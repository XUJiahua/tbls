-- Stats test tables (shared schema with PostgreSQL)
-- Deterministic data: 10000 rows each, same generation logic as postgres_stats.sql

DROP TABLE IF EXISTS testdb.stats_numbers;
DROP TABLE IF EXISTS testdb.stats_basic;

CREATE TABLE testdb.stats_basic (
    id UInt64,
    name String,
    email Nullable(String),
    age Nullable(Int32),
    score Float64,
    status LowCardinality(String),
    category String,
    created_at DateTime,
    updated_at Nullable(DateTime),
    note Nullable(String),
    payload String
) ENGINE = MergeTree() ORDER BY id;

CREATE TABLE testdb.stats_numbers (
    id UInt64,
    int_val Int32,
    float_val Float64,
    nullable_int Nullable(Int32),
    small_distinct UInt8,
    created_at DateTime
) ENGINE = MergeTree() ORDER BY id;

INSERT INTO testdb.stats_basic (id, name, email, age, score, status, category, created_at, updated_at, note, payload)
SELECT
    number + 1 AS i,
    concat('user_', lpad(toString(number + 1), 5, '0')),
    if((number + 1) % 10 = 0, NULL, concat('user_', lpad(toString(number + 1), 5, '0'), '@test.com')),
    if((number + 1) % 100 < 15, NULL, toInt32(18 + ((number + 1) % 63))),
    ((number + 1) % 1000) / 10.0,
    multiIf(
        (number + 1) % 5 = 0, 'active',
        (number + 1) % 5 = 1, 'inactive',
        (number + 1) % 5 = 2, 'pending',
        (number + 1) % 5 = 3, 'suspended',
        'deleted'
    ),
    concat('cat_', lpad(toString((number + 1) % 20), 2, '0')),
    toDateTime('2024-01-01 00:00:00') + (number + 1) * 60,
    if((number + 1) % 5 = 0, NULL, toDateTime('2024-01-01 00:00:00') + (number + 1) * 60 + 3600),
    if((number + 1) % 10 < 3, NULL, concat('Note text for row ', toString(number + 1))),
    concat('{"key": ', toString(number + 1), '}')
FROM numbers(10000);

INSERT INTO testdb.stats_numbers (id, int_val, float_val, nullable_int, small_distinct, created_at)
SELECT
    number + 1 AS i,
    toInt32(((number + 1) % 1000) - 500),
    (number + 1) * 1.5,
    if((number + 1) % 5 = 0, NULL, toInt32((number + 1) % 100)),
    toUInt8((number + 1) % 10),
    toDateTime('2024-01-01 00:00:00') + (number + 1) * 60
FROM numbers(10000);
