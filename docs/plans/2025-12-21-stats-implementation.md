# Stats Collection Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add database statistics collection (TableStats/ColumnStats) to tbls for enhancing LLM text2sql inference.

**Architecture:** Extend existing schema structs with optional Stats fields. Add StatsCollector interface for drivers. Implement ClickHouse stats collection with smart sampling (full scan for small tables, recent partitions for large tables).

**Tech Stack:** Go, ClickHouse SQL, existing tbls patterns

---

## Task 1: Add Stats Structs to Schema

**Files:**
- Modify: `schema/schema.go`

**Step 1: Add TopValue struct**

Add after line 36 (after Labels type):

```go
// TopValue represents a frequently occurring value in a column
type TopValue struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}
```

**Step 2: Add ColumnStats struct**

Add after TopValue:

```go
// ColumnStats holds column-level statistics
type ColumnStats struct {
	RowCount      int64      `json:"row_count"`
	NullCount     int64      `json:"null_count"`
	NullPercent   float64    `json:"null_percent"`
	DistinctCount int64      `json:"distinct_count"`
	Min           *float64   `json:"min,omitempty"`
	Max           *float64   `json:"max,omitempty"`
	Avg           *float64   `json:"avg,omitempty"`
	TopValues     []TopValue `json:"top_values,omitempty"`
}
```

**Step 3: Add TableStats struct**

Add after ColumnStats:

```go
// TableStats holds table-level statistics
type TableStats struct {
	RowCount   int64 `json:"row_count"`
	DataBytes  int64 `json:"data_bytes,omitempty"`
	IndexBytes int64 `json:"index_bytes,omitempty"`
}
```

**Step 4: Add Stats field to Column struct**

Modify Column struct (around line 118), add after `HideForER`:

```go
type Column struct {
	Name            string
	Type            string
	Nullable        bool
	Default         sql.NullString
	Comment         string
	ExtraDef        string
	Occurrences     sql.NullInt32
	Percents        sql.NullFloat64
	Labels          Labels
	ParentRelations []*Relation
	ChildRelations  []*Relation
	PK              bool
	FK              bool
	HideForER       bool
	Stats           *ColumnStats `json:"stats,omitempty"`
}
```

**Step 5: Add Stats field to Table struct**

Modify Table struct (around line 142), add after `External`:

```go
type Table struct {
	Name             string
	Type             string
	Comment          string
	Columns          []*Column
	Viewpoints       []*TableViewpoint
	Indexes          []*Index
	Constraints      []*Constraint
	Triggers         []*Trigger
	Def              string
	Labels           Labels
	ReferencedTables []*Table
	External         bool
	Stats            *TableStats `json:"stats,omitempty"`
}
```

**Step 6: Run tests to verify no breakage**

Run: `go build ./...`
Expected: Build succeeds with no errors

**Step 7: Commit**

```bash
git add schema/schema.go
git commit -m "feat(schema): add TableStats and ColumnStats structs"
```

---

## Task 2: Add StatsConfig to Config

**Files:**
- Modify: `config/config.go`

**Step 1: Add StatsConfig struct**

Add after DetectVirtualRelations struct (around line 127):

```go
// StatsConfig holds configuration for statistics collection
type StatsConfig struct {
	Enabled             bool     `yaml:"enabled" json:"enabled"`
	Include             []string `yaml:"include,omitempty" json:"include,omitempty"`
	Exclude             []string `yaml:"exclude,omitempty" json:"exclude,omitempty"`
	TopN                int      `yaml:"topN,omitempty" json:"topN,omitempty"`
	SampleSize          int      `yaml:"sampleSize,omitempty" json:"sampleSize,omitempty"`
	LargeTableThreshold int64    `yaml:"largeTableThreshold,omitempty" json:"largeTableThreshold,omitempty"`
	RecentDays          int      `yaml:"recentDays,omitempty" json:"recentDays,omitempty"`
}
```

**Step 2: Add Stats field to Config struct**

Add to Config struct (around line 58, after DisableOutputSchema):

```go
	Stats               StatsConfig            `yaml:"stats,omitempty" json:"stats,omitempty"`
```

**Step 3: Add default values in setDefault()**

Add to setDefault() function (around line 293, before return):

```go
	// Stats defaults
	if c.Stats.TopN == 0 {
		c.Stats.TopN = 10
	}
	if c.Stats.SampleSize == 0 {
		c.Stats.SampleSize = 10000
	}
	if c.Stats.LargeTableThreshold == 0 {
		c.Stats.LargeTableThreshold = 1000000
	}
	if c.Stats.RecentDays == 0 {
		c.Stats.RecentDays = 30
	}
```

**Step 4: Add ShouldCollectStats helper method**

Add after FilterTables method:

```go
// ShouldCollectStats returns true if stats should be collected for the given table
func (c *Config) ShouldCollectStats(tableName string) bool {
	if !c.Stats.Enabled {
		return false
	}
	// Check exclude first
	if len(c.Stats.Exclude) > 0 && match(c.Stats.Exclude, tableName) {
		return false
	}
	// If include is specified, table must match
	if len(c.Stats.Include) > 0 {
		return match(c.Stats.Include, tableName)
	}
	// Default: collect for all tables
	return true
}
```

**Step 5: Run tests**

Run: `go test ./config/... -v`
Expected: All tests pass

**Step 6: Commit**

```bash
git add config/config.go
git commit -m "feat(config): add StatsConfig for statistics collection"
```

---

## Task 3: Add StatsCollector Interface

**Files:**
- Modify: `drivers/drivers.go`

**Step 1: Add StatsCollector interface**

Add after Driver interface:

```go
// StatsCollector is an optional interface for drivers that support statistics collection
type StatsCollector interface {
	CollectStats(s *schema.Schema, cfg StatsConfig) error
}

// StatsConfig is passed to StatsCollector
type StatsConfig struct {
	Include             []string
	Exclude             []string
	TopN                int
	SampleSize          int
	LargeTableThreshold int64
	RecentDays          int
}
```

**Step 2: Run build**

Run: `go build ./...`
Expected: Build succeeds

**Step 3: Commit**

```bash
git add drivers/drivers.go
git commit -m "feat(drivers): add StatsCollector interface"
```

---

## Task 4: Implement ClickHouse Stats Collection

**Files:**
- Create: `drivers/clickhouse/stats.go`

**Step 1: Create stats.go with package and imports**

```go
package clickhouse

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/k1LoW/tbls/drivers"
	"github.com/k1LoW/tbls/schema"
	"github.com/samber/lo"
)

// numericTypeRe matches ClickHouse numeric types
var numericTypeRe = regexp.MustCompile(`(?i)^(U?Int\d+|Float\d+|Decimal.*|Nullable\((U?Int\d+|Float\d+|Decimal.*)\))`)
```

**Step 2: Add CollectStats method**

```go
// CollectStats collects statistics for all tables in the schema
func (ch *ClickHouse) CollectStats(s *schema.Schema, cfg drivers.StatsConfig) error {
	// Get table stats from system.tables
	tableStats, err := ch.getTableStats(s.Name)
	if err != nil {
		return err
	}

	for _, table := range s.Tables {
		// Check if this table should have stats collected
		if !shouldCollectStats(table.Name, cfg.Include, cfg.Exclude) {
			continue
		}

		// Set table-level stats
		if stats, ok := tableStats[table.Name]; ok {
			table.Stats = stats
		}

		// Determine sampling strategy
		isLargeTable := table.Stats != nil && table.Stats.RowCount > cfg.LargeTableThreshold
		partitionKey := ch.getPartitionKey(s.Name, table.Name)

		// Collect column stats
		for _, col := range table.Columns {
			colStats, err := ch.getColumnStats(s.Name, table.Name, col.Name, col.Type, isLargeTable, partitionKey, cfg)
			if err != nil {
				// Log error but continue with other columns
				continue
			}
			col.Stats = colStats
		}
	}

	return nil
}
```

**Step 3: Add helper function shouldCollectStats**

```go
func shouldCollectStats(tableName string, include, exclude []string) bool {
	// Check exclude first
	for _, pattern := range exclude {
		if matchPattern(pattern, tableName) {
			return false
		}
	}
	// If include is specified, table must match
	if len(include) > 0 {
		for _, pattern := range include {
			if matchPattern(pattern, tableName) {
				return true
			}
		}
		return false
	}
	return true
}

func matchPattern(pattern, name string) bool {
	// Simple wildcard matching
	if strings.Contains(pattern, "*") {
		pattern = strings.ReplaceAll(pattern, "*", ".*")
		re, err := regexp.Compile("^" + pattern + "$")
		if err != nil {
			return false
		}
		return re.MatchString(name)
	}
	return pattern == name
}
```

**Step 4: Add getTableStats method**

```go
func (ch *ClickHouse) getTableStats(dbName string) (map[string]*schema.TableStats, error) {
	result := make(map[string]*schema.TableStats)

	rows, err := ch.db.Query(`
		SELECT
			name,
			total_rows,
			total_bytes,
			0 as index_bytes
		FROM system.tables
		WHERE database = ?
	`, dbName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			name       string
			rowCount   uint64
			dataBytes  uint64
			indexBytes uint64
		)
		if err := rows.Scan(&name, &rowCount, &dataBytes, &indexBytes); err != nil {
			continue
		}
		result[name] = &schema.TableStats{
			RowCount:   int64(rowCount),
			DataBytes:  int64(dataBytes),
			IndexBytes: int64(indexBytes),
		}
	}

	return result, nil
}
```

**Step 5: Add getPartitionKey method**

```go
func (ch *ClickHouse) getPartitionKey(dbName, tableName string) string {
	var partitionKey string
	row := ch.db.QueryRow(`
		SELECT partition_key
		FROM system.tables
		WHERE database = ? AND name = ?
	`, dbName, tableName)
	_ = row.Scan(&partitionKey)
	return partitionKey
}

// extractDateColumn attempts to extract date column from partition key expression
func extractDateColumn(partitionKey string) string {
	// Match patterns like toYYYYMM(date), toDate(timestamp), etc.
	re := regexp.MustCompile(`to\w+\((\w+)\)`)
	matches := re.FindStringSubmatch(partitionKey)
	if len(matches) > 1 {
		return matches[1]
	}
	// If simple column name
	if !strings.Contains(partitionKey, "(") && partitionKey != "" {
		return strings.TrimSpace(partitionKey)
	}
	return ""
}
```

**Step 6: Add getColumnStats method**

```go
func (ch *ClickHouse) getColumnStats(dbName, tableName, colName, colType string, isLargeTable bool, partitionKey string, cfg drivers.StatsConfig) (*schema.ColumnStats, error) {
	stats := &schema.ColumnStats{}
	isNumeric := numericTypeRe.MatchString(colType)

	// Build WHERE clause for large table sampling
	whereClause := ""
	if isLargeTable {
		dateCol := extractDateColumn(partitionKey)
		if dateCol != "" {
			whereClause = fmt.Sprintf("WHERE %s >= today() - %d", dateCol, cfg.RecentDays)
		}
	}

	// Build query based on column type
	var query string
	if isNumeric {
		query = fmt.Sprintf(`
			SELECT
				count() as row_count,
				countIf(%s IS NULL) as null_count,
				countDistinct(%s) as distinct_count,
				min(%s) as min_val,
				max(%s) as max_val,
				avg(%s) as avg_val
			FROM %s.%s
			%s
			LIMIT %d
		`, colName, colName, colName, colName, colName, dbName, tableName, whereClause, cfg.SampleSize)
	} else {
		query = fmt.Sprintf(`
			SELECT
				count() as row_count,
				countIf(%s IS NULL) as null_count,
				countDistinct(%s) as distinct_count,
				0 as min_val,
				0 as max_val,
				0 as avg_val
			FROM %s.%s
			%s
			LIMIT %d
		`, colName, colName, dbName, tableName, whereClause, cfg.SampleSize)
	}

	row := ch.db.QueryRow(query)
	var (
		rowCount      int64
		nullCount     int64
		distinctCount int64
		minVal        float64
		maxVal        float64
		avgVal        float64
	)
	if err := row.Scan(&rowCount, &nullCount, &distinctCount, &minVal, &maxVal, &avgVal); err != nil {
		return nil, err
	}

	stats.RowCount = rowCount
	stats.NullCount = nullCount
	stats.DistinctCount = distinctCount
	if rowCount > 0 {
		stats.NullPercent = float64(nullCount) / float64(rowCount) * 100
	}

	if isNumeric {
		stats.Min = &minVal
		stats.Max = &maxVal
		stats.Avg = &avgVal
	}

	// Get top N values
	topValues, err := ch.getTopValues(dbName, tableName, colName, whereClause, cfg.TopN)
	if err == nil {
		stats.TopValues = topValues
	}

	return stats, nil
}
```

**Step 7: Add getTopValues method**

```go
func (ch *ClickHouse) getTopValues(dbName, tableName, colName, whereClause string, topN int) ([]schema.TopValue, error) {
	query := fmt.Sprintf(`
		SELECT
			toString(%s) as value,
			count() as cnt
		FROM %s.%s
		%s
		GROUP BY %s
		ORDER BY cnt DESC
		LIMIT %d
	`, colName, dbName, tableName, whereClause, colName, topN)

	rows, err := ch.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []schema.TopValue
	for rows.Next() {
		var (
			value string
			count int64
		)
		if err := rows.Scan(&value, &count); err != nil {
			continue
		}
		result = append(result, schema.TopValue{
			Value: value,
			Count: count,
		})
	}

	return result, nil
}
```

**Step 8: Run build**

Run: `go build ./...`
Expected: Build succeeds

**Step 9: Commit**

```bash
git add drivers/clickhouse/stats.go
git commit -m "feat(clickhouse): implement StatsCollector for ClickHouse"
```

---

## Task 5: Integrate Stats Collection in Datasource

**Files:**
- Modify: `datasource/datasource.go`

**Step 1: Add import for config package**

The config package is already imported. No change needed.

**Step 2: Create new AnalyzeWithStats function**

Add after Analyze function (around line 163):

```go
// AnalyzeWithStats analyzes database and optionally collects statistics
func AnalyzeWithStats(dsn config.DSN, cfg *config.Config) (*schema.Schema, error) {
	s, err := Analyze(dsn)
	if err != nil {
		return nil, err
	}

	// Collect stats if enabled
	if cfg != nil && cfg.Stats.Enabled {
		if err := collectStats(s, dsn, cfg); err != nil {
			// Log error but don't fail - stats are optional
			// TODO: consider adding proper logging
			_ = err
		}
	}

	return s, nil
}

func collectStats(s *schema.Schema, dsn config.DSN, cfg *config.Config) error {
	urlstr := dsn.URL
	u, err := dburl.Parse(urlstr)
	if err != nil {
		return err
	}

	statsCfg := drivers.StatsConfig{
		Include:             cfg.Stats.Include,
		Exclude:             cfg.Stats.Exclude,
		TopN:                cfg.Stats.TopN,
		SampleSize:          cfg.Stats.SampleSize,
		LargeTableThreshold: cfg.Stats.LargeTableThreshold,
		RecentDays:          cfg.Stats.RecentDays,
	}

	switch u.Driver {
	case "clickhouse":
		db, err := dburl.Open(urlstr)
		if err != nil {
			return err
		}
		defer db.Close()

		driver := clickhouse.New(db)
		return driver.CollectStats(s, statsCfg)
	default:
		// Stats not supported for this driver
		return nil
	}
}
```

**Step 3: Run build**

Run: `go build ./...`
Expected: Build succeeds

**Step 4: Commit**

```bash
git add datasource/datasource.go
git commit -m "feat(datasource): integrate stats collection in AnalyzeWithStats"
```

---

## Task 6: Update Serve Command to Support Stats

**Files:**
- Modify: `cmd/serve.go`

**Step 1: Find where schema is analyzed and update to use AnalyzeWithStats**

Look for the call to `datasource.Analyze` and replace with `datasource.AnalyzeWithStats`:

```go
// Before:
s, err := datasource.Analyze(c.DSN)

// After:
s, err := datasource.AnalyzeWithStats(c.DSN, c)
```

**Step 2: Run build**

Run: `go build ./...`
Expected: Build succeeds

**Step 3: Commit**

```bash
git add cmd/serve.go
git commit -m "feat(serve): use AnalyzeWithStats to include statistics"
```

---

## Task 7: Add Integration Test

**Files:**
- Create: `drivers/clickhouse/stats_test.go`

**Step 1: Create test file with basic structure**

```go
//go:build clickhouse

package clickhouse

import (
	"database/sql"
	"os"
	"testing"

	"github.com/k1LoW/tbls/drivers"
	"github.com/k1LoW/tbls/schema"
)

func TestCollectStats(t *testing.T) {
	dsn := os.Getenv("TBLS_TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("TBLS_TEST_CLICKHOUSE_DSN not set")
	}

	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ch := New(db)

	s := &schema.Schema{
		Name: "default",
	}

	// First analyze to get tables
	if err := ch.Analyze(s); err != nil {
		t.Fatal(err)
	}

	// Then collect stats
	cfg := drivers.StatsConfig{
		TopN:                10,
		SampleSize:          10000,
		LargeTableThreshold: 1000000,
		RecentDays:          30,
	}

	if err := ch.CollectStats(s, cfg); err != nil {
		t.Fatal(err)
	}

	// Verify stats were collected
	for _, table := range s.Tables {
		if table.Stats == nil {
			t.Errorf("table %s has no stats", table.Name)
			continue
		}
		if table.Stats.RowCount < 0 {
			t.Errorf("table %s has invalid row count", table.Name)
		}
	}
}
```

**Step 2: Run test (if ClickHouse available)**

Run: `TBLS_TEST_CLICKHOUSE_DSN="clickhouse://localhost:9000/default" go test -v -tags clickhouse ./drivers/clickhouse/... -run TestCollectStats`
Expected: Test passes (or skips if no ClickHouse)

**Step 3: Commit**

```bash
git add drivers/clickhouse/stats_test.go
git commit -m "test(clickhouse): add integration test for stats collection"
```

---

## Task 8: Update Documentation

**Files:**
- Modify: `docs/serve.md`

**Step 1: Add stats configuration example to serve.md**

Add a new section after the existing examples:

```markdown
### With Statistics Collection

```bash
curl -X POST http://localhost:8080/schema \
  -H "Content-Type: application/json" \
  -d '{
    "dsn": {"url": "clickhouse://localhost:9000/mydb"},
    "stats": {
      "enabled": true,
      "topN": 10,
      "sampleSize": 10000,
      "largeTableThreshold": 1000000,
      "recentDays": 30
    }
  }'
```

#### Stats Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `stats.enabled` | bool | false | Enable statistics collection |
| `stats.include` | []string | all | Tables to collect stats for (supports wildcards) |
| `stats.exclude` | []string | none | Tables to exclude (supports wildcards) |
| `stats.topN` | int | 10 | Number of top values to collect per column |
| `stats.sampleSize` | int | 10000 | Maximum rows to sample |
| `stats.largeTableThreshold` | int | 1000000 | Row count threshold for large table sampling |
| `stats.recentDays` | int | 30 | Days to look back for large table sampling |
```

**Step 2: Commit**

```bash
git add docs/serve.md
git commit -m "docs: add stats configuration to serve documentation"
```

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | Add Stats structs to schema | `schema/schema.go` |
| 2 | Add StatsConfig to config | `config/config.go` |
| 3 | Add StatsCollector interface | `drivers/drivers.go` |
| 4 | Implement ClickHouse stats | `drivers/clickhouse/stats.go` |
| 5 | Integrate in datasource | `datasource/datasource.go` |
| 6 | Update serve command | `cmd/serve.go` |
| 7 | Add integration test | `drivers/clickhouse/stats_test.go` |
| 8 | Update documentation | `docs/serve.md` |

Total: 8 tasks, ~8 commits
