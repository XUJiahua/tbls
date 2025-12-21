# Database Statistics Collection Design

## Goal

Extend tbls to collect database statistics (TableStats/ColumnStats) to enhance schema documentation for LLM text2sql inference.

## Data Structures

### schema/schema.go

```go
// TableStats holds table-level statistics
type TableStats struct {
    RowCount   int64 `json:"row_count"`
    DataBytes  int64 `json:"data_bytes,omitempty"`
    IndexBytes int64 `json:"index_bytes,omitempty"`
}

// ColumnStats holds column-level statistics
type ColumnStats struct {
    // Universal stats (all column types)
    RowCount      int64   `json:"row_count"`
    NullCount     int64   `json:"null_count"`
    NullPercent   float64 `json:"null_percent"`
    DistinctCount int64   `json:"distinct_count"`

    // Numeric types only (nil for non-numeric)
    Min *float64 `json:"min,omitempty"`
    Max *float64 `json:"max,omitempty"`
    Avg *float64 `json:"avg,omitempty"`

    // Sample values (all types)
    TopValues []TopValue `json:"top_values,omitempty"`
}

type TopValue struct {
    Value string `json:"value"`
    Count int64  `json:"count"`
}

// Extend existing structs
type Table struct {
    // ... existing fields
    Stats *TableStats `json:"stats,omitempty"`
}

type Column struct {
    // ... existing fields
    Stats *ColumnStats `json:"stats,omitempty"`
}
```

## Configuration

### config/config.go

```go
type StatsConfig struct {
    Enabled             bool     `yaml:"enabled" json:"enabled"`
    Include             []string `yaml:"include,omitempty" json:"include,omitempty"`
    Exclude             []string `yaml:"exclude,omitempty" json:"exclude,omitempty"`
    TopN                int      `yaml:"topN,omitempty" json:"topN,omitempty"`
    SampleSize          int      `yaml:"sampleSize,omitempty" json:"sampleSize,omitempty"`
    LargeTableThreshold int64    `yaml:"largeTableThreshold,omitempty" json:"largeTableThreshold,omitempty"`
    RecentDays          int      `yaml:"recentDays,omitempty" json:"recentDays,omitempty"`
}

type Config struct {
    // ... existing fields
    Stats StatsConfig `yaml:"stats,omitempty" json:"stats,omitempty"`
}
```

### .tbls.yml Example

```yaml
dsn: clickhouse://localhost:9000/mydb

stats:
  enabled: true
  include: ["users", "orders"]
  exclude: ["*_log", "*_backup"]
  topN: 10                      # default: 10
  sampleSize: 10000             # default: 10000
  largeTableThreshold: 1000000  # default: 1000000 (1M rows)
  recentDays: 30                # default: 30
```

## Architecture

### StatsCollector Interface

```go
// StatsCollector is an optional interface for drivers that support stats collection
type StatsCollector interface {
    CollectStats(s *schema.Schema, cfg *config.StatsConfig) error
}
```

### File Changes

```
schema/schema.go          # Add TableStats, ColumnStats structs
config/config.go          # Add StatsConfig struct
drivers/clickhouse/
  clickhouse.go           # Implement StatsCollector interface
  stats.go                # Stats collection logic
datasource/datasource.go  # Call CollectStats if driver supports it
```

### Call Flow

1. `datasource.Analyze()` extracts schema (existing logic)
2. Check `config.Stats.Enabled`
3. If driver implements `StatsCollector`, call `CollectStats()`
4. Stats populated into `schema.Table.Stats` / `schema.Column.Stats`

## ClickHouse Implementation

### Table-Level Stats (from system.tables)

```sql
SELECT
    name,
    total_rows,
    total_bytes,
    primary_key,
    partition_key
FROM system.tables
WHERE database = 'mydb'
```

### Column-Level Stats - Numeric Types

```sql
SELECT
    count() as total,
    countDistinct(col) as distinct_count,
    countIf(col IS NULL) as null_count,
    min(col) as min_val,
    max(col) as max_val,
    avg(col) as avg_val
FROM table
[WHERE date_col >= today() - 30]  -- for large tables
LIMIT {sampleSize}
```

### Column-Level Stats - Non-Numeric Types

```sql
SELECT
    count() as total,
    countDistinct(col) as distinct_count,
    countIf(col IS NULL) as null_count
FROM table
[WHERE date_col >= today() - 30]  -- for large tables
LIMIT {sampleSize}
```

### Top N Values (all types)

```sql
SELECT
    toString(col) as value,
    count() as cnt
FROM table
[WHERE date_col >= today() - 30]  -- for large tables
GROUP BY col
ORDER BY cnt DESC
LIMIT {topN}
```

### Sampling Strategy

| Table Size | Strategy |
|------------|----------|
| rows <= largeTableThreshold | Full table scan with LIMIT sampleSize |
| rows > largeTableThreshold | Filter by recent partition (recentDays) + LIMIT sampleSize |

### Partition Key Detection

1. Query `system.tables.partition_key` for partition expression
2. Parse date column name (e.g., `toYYYYMM(event_date)` -> `event_date`)
3. Large tables without partition key fall back to LIMIT sampling

### Type Detection

Numeric types (support min/max/avg):
- Int8, Int16, Int32, Int64, Int128, Int256
- UInt8, UInt16, UInt32, UInt64, UInt128, UInt256
- Float32, Float64
- Decimal, Decimal32, Decimal64, Decimal128, Decimal256

Non-numeric types (only distinct count + topN):
- String, FixedString
- Date, Date32, DateTime, DateTime64
- UUID
- Enum8, Enum16
- Array, Tuple, Map, etc.

## Output Example

```json
{
  "name": "mydb",
  "tables": [
    {
      "name": "orders",
      "type": "table",
      "comment": "Orders table",
      "stats": {
        "row_count": 5000000,
        "data_bytes": 1073741824,
        "index_bytes": 268435456
      },
      "columns": [
        {
          "name": "id",
          "type": "UInt64",
          "nullable": false,
          "stats": {
            "row_count": 5000000,
            "null_count": 0,
            "null_percent": 0,
            "distinct_count": 5000000,
            "min": 1,
            "max": 5000000,
            "avg": 2500000
          }
        },
        {
          "name": "status",
          "type": "String",
          "nullable": false,
          "stats": {
            "row_count": 5000000,
            "null_count": 0,
            "null_percent": 0,
            "distinct_count": 4,
            "top_values": [
              {"value": "completed", "count": 2500000},
              {"value": "pending", "count": 1500000},
              {"value": "shipped", "count": 800000},
              {"value": "cancelled", "count": 200000}
            ]
          }
        },
        {
          "name": "amount",
          "type": "Decimal(10,2)",
          "nullable": false,
          "stats": {
            "row_count": 5000000,
            "null_count": 0,
            "null_percent": 0,
            "distinct_count": 48523,
            "min": 9.99,
            "max": 9999.99,
            "avg": 156.78,
            "top_values": [
              {"value": "99.00", "count": 15234},
              {"value": "199.00", "count": 12456}
            ]
          }
        }
      ]
    }
  ]
}
```

## Value for LLM text2sql

- `status` column: distinct=4 + top_values -> recognized as enum, usable in WHERE clauses
- `amount` column: min/max/avg -> understand value range, avoid unreasonable queries
- `id` column: distinct=row_count -> identified as primary key/unique identifier
