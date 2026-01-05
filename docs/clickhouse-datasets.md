# ClickHouse Example Datasets

This document describes how to set up local ClickHouse databases with sample data for testing tbls.

## Public ClickHouse Playground

ClickHouse provides a public SQL Playground at `sql-clickhouse.clickhouse.com` with 35+ datasets.

### Connection (Native Protocol)

```bash
# Using clickhouse-client
clickhouse-client \
  --host sql-clickhouse.clickhouse.com \
  --secure \
  --user demo

# Using tbls (note: demo user has limited permissions)
./tbls scaffold --dsn "clickhouse://demo@sql-clickhouse.clickhouse.com:9440/default?secure=true"
```

**Note:** The `demo` user has read-only access and cannot query some system tables like `system.data_skipping_indices`, which tbls needs for full schema analysis.

## Local Dataset Setup

To fully test tbls with ClickHouse, you can import datasets locally.

### 1. NYC Taxi Dataset (Recommended for Quick Start)

~3 million rows sample, good for testing.

```sql
-- Create database
CREATE DATABASE IF NOT EXISTS nyc_taxi;

-- Create table
CREATE TABLE nyc_taxi.trips (
    trip_id             UInt32,
    pickup_datetime     DateTime,
    dropoff_datetime    DateTime,
    pickup_longitude    Nullable(Float64),
    pickup_latitude     Nullable(Float64),
    dropoff_longitude   Nullable(Float64),
    dropoff_latitude    Nullable(Float64),
    passenger_count     UInt8,
    trip_distance       Float32,
    fare_amount         Float32,
    tip_amount          Float32,
    total_amount        Float32,
    payment_type        Enum('CSH' = 1, 'CRE' = 2, 'NOC' = 3, 'DIS' = 4, 'UNK' = 5)
) ENGINE = MergeTree()
ORDER BY (pickup_datetime, dropoff_datetime);

-- Import from S3 (~3M rows)
INSERT INTO nyc_taxi.trips
SELECT * FROM s3(
    'https://datasets-documentation.s3.eu-west-3.amazonaws.com/nyc-taxi/trips_{0..2}.gz',
    'TabSeparatedWithNames'
);
```

### 2. OnTime (Flight Data)

US flight delay data, larger dataset.

```sql
-- Create database
CREATE DATABASE IF NOT EXISTS ontime;

-- Create table (simplified schema)
CREATE TABLE ontime.flights (
    Year UInt16,
    Quarter UInt8,
    Month UInt8,
    DayofMonth UInt8,
    DayOfWeek UInt8,
    FlightDate Date,
    Reporting_Airline String,
    Origin String,
    Dest String,
    DepDelay Int32,
    ArrDelay Int32,
    Cancelled UInt8,
    Diverted UInt8
) ENGINE = MergeTree()
ORDER BY (FlightDate, Reporting_Airline);

-- Import from S3 (full dataset, takes time)
INSERT INTO ontime.flights
SELECT * FROM s3(
    'https://clickhouse-public-datasets.s3.amazonaws.com/ontime/csv_by_year/*.csv.gz',
    CSVWithNames
) SETTINGS max_insert_threads = 40;
```

### 3. UK Price Paid (Real Estate)

UK property sales data.

```sql
CREATE DATABASE IF NOT EXISTS uk;

CREATE TABLE uk.price_paid (
    price UInt32,
    date Date,
    postcode1 LowCardinality(String),
    postcode2 LowCardinality(String),
    type Enum('terraced' = 1, 'semi-detached' = 2, 'detached' = 3, 'flat' = 4, 'other' = 0),
    is_new UInt8,
    duration Enum('freehold' = 1, 'leasehold' = 2, 'unknown' = 0),
    addr1 String,
    addr2 String,
    street LowCardinality(String),
    locality LowCardinality(String),
    town LowCardinality(String),
    district LowCardinality(String),
    county LowCardinality(String)
) ENGINE = MergeTree()
ORDER BY (postcode1, postcode2, date);

-- Import
INSERT INTO uk.price_paid
SELECT * FROM s3(
    'https://clickhouse-public-datasets.s3.eu-west-3.amazonaws.com/uk_price_paid/uk_price_paid.snappy.parquet'
);
```

### 4. GitHub Events

GitHub activity data.

```sql
CREATE DATABASE IF NOT EXISTS github;

CREATE TABLE github.events (
    file_time DateTime,
    event_type Enum('CommitCommentEvent' = 1, 'CreateEvent' = 2, 'DeleteEvent' = 3, 'ForkEvent' = 4, 'GollumEvent' = 5, 'IssueCommentEvent' = 6, 'IssuesEvent' = 7, 'MemberEvent' = 8, 'PublicEvent' = 9, 'PullRequestEvent' = 10, 'PullRequestReviewCommentEvent' = 11, 'PushEvent' = 12, 'ReleaseEvent' = 13, 'SponsorshipEvent' = 14, 'WatchEvent' = 15, 'GistEvent' = 16, 'FollowEvent' = 17, 'DownloadEvent' = 18, 'PullRequestReviewEvent' = 19, 'ForkApplyEvent' = 20, 'Event' = 21, 'TeamAddEvent' = 22),
    actor_login LowCardinality(String),
    repo_name LowCardinality(String),
    created_at DateTime,
    updated_at DateTime,
    action Enum('none' = 0, 'created' = 1, 'added' = 2, 'edited' = 3, 'deleted' = 4, 'opened' = 5, 'closed' = 6, 'reopened' = 7, 'assigned' = 8, 'unassigned' = 9, 'labeled' = 10, 'unlabeled' = 11, 'review_requested' = 12, 'review_request_removed' = 13, 'synchronize' = 14, 'started' = 15, 'published' = 16, 'update' = 17, 'create' = 18, 'fork' = 19, 'merged' = 20),
    comment_id UInt64,
    body String,
    ref LowCardinality(String),
    number UInt32,
    title String
) ENGINE = MergeTree()
ORDER BY (event_type, repo_name, created_at);
```

## Using Altinity Datasets Tool

For easier dataset management, use the [altinity-datasets](https://github.com/Altinity/altinity-datasets) Python tool:

```bash
# Install
pip install altinity-datasets

# List available datasets
altinity-datasets list

# Load a dataset to local ClickHouse
altinity-datasets load nyc_taxi --host localhost --port 9000

# Load with custom credentials
altinity-datasets load ontime --host localhost --user default --password mypassword
```

## Testing tbls with Local ClickHouse

Once data is imported, test tbls:

```bash
# Using native protocol (recommended)
./tbls doc --dsn "clickhouse://default:@localhost:9000/nyc_taxi"

# Using HTTP protocol
./tbls doc --dsn "clickhouse+http://default:@localhost:8123/nyc_taxi"

# Generate scaffold config
./tbls scaffold --dsn "clickhouse://default:@localhost:9000/nyc_taxi"
```

## Available Databases on Playground

The following databases are available on `sql-clickhouse.clickhouse.com`:

| Database | Description |
|----------|-------------|
| `amazon` | Amazon product reviews |
| `bluesky` | Bluesky social network data |
| `github` | GitHub events |
| `hackernews` | Hacker News stories and comments |
| `imdb` | IMDB movie database |
| `nyc_taxi` | NYC taxi trips |
| `ontime` | US flight delays |
| `pypi` | Python package downloads |
| `stackoverflow` | Stack Overflow Q&A |
| `uk` | UK property prices |
| `wiki` | Wikipedia pageviews |
| `youtube` | YouTube video statistics |

## References

- [ClickHouse Example Datasets](https://clickhouse.com/docs/getting-started/example-datasets)
- [NYC Taxi Data](https://clickhouse.com/docs/getting-started/example-datasets/nyc-taxi)
- [OnTime Flight Data](https://clickhouse.com/docs/getting-started/example-datasets/ontime)
- [Altinity Datasets](https://github.com/Altinity/altinity-datasets)
- [ClickHouse SQL Playground](https://sql.clickhouse.com/)
