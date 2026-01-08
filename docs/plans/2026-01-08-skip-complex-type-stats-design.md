# Skip Complex Type Stats Analysis Design

**Goal:** Skip deep statistical analysis for ClickHouse complex types (Array, Map, Tuple, Object('json'), etc.) to avoid extremely slow queries with meaningless results.

**Problem:** Complex types like `Object('json')` take 230+ seconds for `countDistinct()` and produce meaningless results.

---

## Solution

For column types that don't match numeric/date/string patterns, only collect basic counts:

| Operation | numeric/date/string | Other types |
|-----------|---------------------|-------------|
| `count()` | Yes | Yes |
| `countIf(IS NULL)` | Yes | Yes |
| `countDistinct()` | Yes | Skip |
| min/max/avg etc. | Yes | Skip |
| Top Values | Yes | Skip |

## Implementation

### 1. Schema Change

Add field to `ColumnStats` in `schema/schema.go`:

```go
type ColumnStats struct {
    // ... existing fields ...

    // SkippedComplexAnalysis indicates that deep analysis was skipped
    // because the column type is not numeric/date/string
    SkippedComplexAnalysis bool `json:"skipped_complex_analysis,omitempty"`
}
```

### 2. Minimal Query

In `drivers/clickhouse/stats.go`, add minimal stats query for complex types:

```go
minimalQuery := fmt.Sprintf(`
    SELECT
        count() as row_count,
        countIf(%s IS NULL) as null_count
    %s
`, quotedCol, fromClause)
```

### 3. Logic Change in getColumnStats

- `default` case uses `minimalQuery` instead of `fallbackQuery`
- Set `stats.SkippedComplexAnalysis = true`
- Skip top values query for complex types

### 4. LowCardinality Support

Extend type regexes to recognize `LowCardinality(...)` wrapper:

```go
// Match LowCardinality(String), LowCardinality(Nullable(String)), etc.
stringTypeRe = regexp.MustCompile(`(?i)^(LowCardinality\()?(String|FixedString.*|UUID|Nullable\((String|FixedString.*|UUID)\))\)?`)
```

Apply same pattern to numericTypeRe and dateTypeRe.

### 5. Logging

Log when skipping complex analysis:

```go
log.WithFields(logrus.Fields{
    "table":  tableName,
    "column": colName,
    "type":   colType,
}).Debug("skipping complex analysis for non-basic type")
```

## Files to Modify

1. `schema/schema.go` - Add `SkippedComplexAnalysis` field
2. `drivers/clickhouse/stats.go` - Add minimal query, update type regexes, skip top values
3. `drivers/clickhouse/stats_test.go` - Add test cases for complex types

## Testing

- Verify complex types (Array, Map, Object) use minimal query
- Verify LowCardinality wrapped types are correctly detected
- Verify SkippedComplexAnalysis flag is set in output
- Verify top values query is skipped for complex types
