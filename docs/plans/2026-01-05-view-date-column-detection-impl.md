# View 日期列自动探测 - 实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 自动探测 ClickHouse View 的日期分区列，为 stats 查询添加日期过滤

**Architecture:** 从 View 的 as_select 解析底层表，获取分区键，映射到 View 列名。用户配置优先，自动探测兜底。

**Tech Stack:** Go, logrus, ClickHouse system tables, regex

---

## Task 1: 添加 logrus 日志配置

**Files:**
- Create: `drivers/clickhouse/log.go`

**Step 1: 创建日志配置文件**

```go
package clickhouse

import (
	"os"

	"github.com/sirupsen/logrus"
)

var log = logrus.New()

func init() {
	log.SetOutput(os.Stderr)
	log.SetFormatter(&logrus.TextFormatter{
		DisableTimestamp: true,
	})

	// 根据环境变量设置日志级别
	if os.Getenv("DEBUG") == "1" || os.Getenv("TBLS_DEBUG") == "1" {
		log.SetLevel(logrus.DebugLevel)
	} else {
		log.SetLevel(logrus.WarnLevel)
	}
}
```

**Step 2: 验证编译通过**

Run: `go build ./drivers/clickhouse/...`
Expected: 无错误

**Step 3: Commit**

```bash
git add drivers/clickhouse/log.go
git commit -m "feat(clickhouse): add logrus logging configuration"
```

---

## Task 2: 更新配置结构

**Files:**
- Modify: `config/config.go`
- Modify: `drivers/drivers.go`

**Step 1: 在 config/config.go 中添加新配置项**

找到 `StatsConfig` 结构体，添加新字段：

```go
// StatsConfig is the config for stats collection
type StatsConfig struct {
	Enabled             bool             `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Include             []string         `yaml:"include,omitempty" json:"include,omitempty"`
	Exclude             []string         `yaml:"exclude,omitempty" json:"exclude,omitempty"`
	TopN                int              `yaml:"topN,omitempty" json:"topN,omitempty"`
	SampleSize          int              `yaml:"sampleSize,omitempty" json:"sampleSize,omitempty"`
	LargeTableThreshold int64            `yaml:"largeTableThreshold,omitempty" json:"largeTableThreshold,omitempty"`
	RecentDays          int              `yaml:"recentDays,omitempty" json:"recentDays,omitempty"`
	Inference           InferenceConfig  `yaml:"inference,omitempty" json:"inference,omitempty"`
	Checkpoint          CheckpointConfig `yaml:"checkpoint,omitempty" json:"checkpoint,omitempty"`
	// 新增字段
	DateColumn string                     `yaml:"dateColumn,omitempty" json:"dateColumn,omitempty"`
	Tables     map[string]TableStatsConfig `yaml:"tables,omitempty" json:"tables,omitempty"`
}

// TableStatsConfig is per-table stats configuration
type TableStatsConfig struct {
	DateColumn string `yaml:"dateColumn,omitempty" json:"dateColumn,omitempty"`
	Skip       bool   `yaml:"skip,omitempty" json:"skip,omitempty"`
}
```

**Step 2: 在 drivers/drivers.go 中更新 StatsConfig**

找到 `StatsConfig` 结构体，添加新字段：

```go
// StatsConfig holds configuration for stats collection
type StatsConfig struct {
	Include             []string
	Exclude             []string
	TopN                int
	SampleSize          int
	LargeTableThreshold int64
	RecentDays          int
	Progress            ProgressReporter
	Checkpoint          CheckpointUpdater
	// 新增字段
	DateColumn string
	Tables     map[string]TableStatsConfig
}

// TableStatsConfig is per-table stats configuration
type TableStatsConfig struct {
	DateColumn string
	Skip       bool
}
```

**Step 3: 验证编译通过**

Run: `go build ./...`
Expected: 无错误

**Step 4: Commit**

```bash
git add config/config.go drivers/drivers.go
git commit -m "feat(config): add dateColumn and tables config for stats"
```

---

## Task 3: 在 datasource 中传递新配置

**Files:**
- Modify: `datasource/datasource.go`

**Step 1: 更新 collectStatsWithProgress 函数中的 statsCfg**

找到 `statsCfg := drivers.StatsConfig{...}` 部分，添加新字段：

```go
statsCfg := drivers.StatsConfig{
	Include:             statsInclude,
	Exclude:             statsExclude,
	TopN:                cfg.Stats.TopN,
	SampleSize:          cfg.Stats.SampleSize,
	LargeTableThreshold: cfg.Stats.LargeTableThreshold,
	RecentDays:          cfg.Stats.RecentDays,
	Progress:            progressAdapter,
	Checkpoint:          checkpointAdapter,
	// 新增字段
	DateColumn: cfg.Stats.DateColumn,
	Tables:     convertTableStatsConfig(cfg.Stats.Tables),
}
```

**Step 2: 添加配置转换函数**

在文件末尾添加：

```go
// convertTableStatsConfig converts config.TableStatsConfig to drivers.TableStatsConfig
func convertTableStatsConfig(tables map[string]config.TableStatsConfig) map[string]drivers.TableStatsConfig {
	if tables == nil {
		return nil
	}
	result := make(map[string]drivers.TableStatsConfig)
	for k, v := range tables {
		result[k] = drivers.TableStatsConfig{
			DateColumn: v.DateColumn,
			Skip:       v.Skip,
		}
	}
	return result
}
```

**Step 3: 验证编译通过**

Run: `go build ./...`
Expected: 无错误

**Step 4: Commit**

```bash
git add datasource/datasource.go
git commit -m "feat(datasource): pass dateColumn and tables config to stats collector"
```

---

## Task 4: 实现 View 解析器

**Files:**
- Create: `drivers/clickhouse/view_resolver.go`

**Step 1: 创建 view_resolver.go**

```go
package clickhouse

import (
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

// TableMeta stores table/view metadata from system.tables
type TableMeta struct {
	Name         string
	Engine       string
	PartitionKey string
	AsSelect     string
}

// getTableMeta queries system.tables for table metadata
func (ch *ClickHouse) getTableMeta(dbName, tableName string) (*TableMeta, error) {
	query := `
		SELECT
			name,
			engine,
			partition_key,
			as_select
		FROM system.tables
		WHERE database = ? AND name = ?
	`
	row := ch.db.QueryRow(query, dbName, tableName)

	var meta TableMeta
	var partitionKey, asSelect *string
	if err := row.Scan(&meta.Name, &meta.Engine, &partitionKey, &asSelect); err != nil {
		return nil, err
	}
	if partitionKey != nil {
		meta.PartitionKey = *partitionKey
	}
	if asSelect != nil {
		meta.AsSelect = *asSelect
	}
	return &meta, nil
}

// isView checks if the engine is a View type
func isView(engine string) bool {
	return engine == "View" || engine == "MaterializedView"
}

// extractUnderlyingTable extracts the first table name from FROM clause
func extractUnderlyingTable(asSelect string) string {
	// Pattern: FROM [db.]table [AS alias]
	// Handles: FROM table, FROM db.table, FROM `table`, FROM "table"
	re := regexp.MustCompile(`(?i)\bFROM\s+([` + "`" + `"]?\w+[` + "`" + `"]?\.)?([` + "`" + `"]?(\w+)[` + "`" + `"]?)`)
	matches := re.FindStringSubmatch(asSelect)
	if len(matches) >= 4 {
		return matches[3]
	}
	return ""
}

// findColumnMapping finds how sourceColumn is mapped in the SELECT clause
func findColumnMapping(asSelect, sourceColumn string) string {
	// Normalize whitespace
	normalized := regexp.MustCompile(`\s+`).ReplaceAllString(asSelect, " ")

	// Pattern 1: column AS alias (e.g., "__date AS dt" or "t.__date AS dt")
	re1 := regexp.MustCompile(`(?i)(\w+\.)?` + regexp.QuoteMeta(sourceColumn) + `\s+AS\s+(\w+)`)
	if matches := re1.FindStringSubmatch(normalized); len(matches) >= 3 {
		log.WithFields(logrus.Fields{
			"source_column": sourceColumn,
			"mapped_to":     matches[2],
			"pattern":       "column AS alias",
		}).Debug("found column mapping")
		return matches[2]
	}

	// Pattern 2: direct reference (e.g., "__date" or "t.__date" without AS)
	re2 := regexp.MustCompile(`(?i)(\w+\.)?` + regexp.QuoteMeta(sourceColumn) + `\b`)
	if re2.MatchString(normalized) {
		log.WithFields(logrus.Fields{
			"source_column": sourceColumn,
			"mapped_to":     sourceColumn,
			"pattern":       "direct reference",
		}).Debug("found column mapping")
		return sourceColumn
	}

	// Pattern 3: function wrapping with AS (e.g., "toDate(event_time) AS __date")
	// Check if sourceColumn appears as an alias after AS
	re3 := regexp.MustCompile(`(?i)\w+\([^)]+\)\s+AS\s+` + regexp.QuoteMeta(sourceColumn) + `\b`)
	if re3.MatchString(normalized) {
		log.WithFields(logrus.Fields{
			"source_column": sourceColumn,
			"mapped_to":     sourceColumn,
			"pattern":       "function AS alias",
		}).Debug("found column mapping")
		return sourceColumn
	}

	return ""
}

// resolveViewDateColumn resolves the date column for a View
func (ch *ClickHouse) resolveViewDateColumn(dbName string, meta *TableMeta, visited map[string]bool) string {
	if meta.AsSelect == "" {
		log.WithFields(logrus.Fields{
			"view": meta.Name,
		}).Debug("view has no as_select")
		return ""
	}

	// Extract underlying table
	underlyingTable := extractUnderlyingTable(meta.AsSelect)
	if underlyingTable == "" {
		log.WithFields(logrus.Fields{
			"view":      meta.Name,
			"as_select": truncateString(meta.AsSelect, 100),
		}).Warn("could not extract underlying table from view")
		return ""
	}

	log.WithFields(logrus.Fields{
		"view":             meta.Name,
		"underlying_table": underlyingTable,
	}).Debug("extracted underlying table")

	// Prevent infinite recursion
	if visited[underlyingTable] {
		log.WithFields(logrus.Fields{
			"view":             meta.Name,
			"underlying_table": underlyingTable,
		}).Warn("circular reference detected")
		return ""
	}
	visited[underlyingTable] = true

	// Get underlying table's date column (recursive)
	underlyingDateCol := ch.getDateColumnForTableInternal(dbName, underlyingTable, "", nil, visited)
	if underlyingDateCol == "" {
		log.WithFields(logrus.Fields{
			"view":             meta.Name,
			"underlying_table": underlyingTable,
		}).Debug("underlying table has no date column")
		return ""
	}

	// Find column mapping in SELECT clause
	mappedCol := findColumnMapping(meta.AsSelect, underlyingDateCol)
	if mappedCol == "" {
		log.WithFields(logrus.Fields{
			"view":                meta.Name,
			"underlying_date_col": underlyingDateCol,
			"suggestion":          "add stats.tables." + meta.Name + ".dateColumn in config",
		}).Warn("could not find column mapping in view, proceeding without date filter")
		return ""
	}

	log.WithFields(logrus.Fields{
		"view":                meta.Name,
		"underlying_date_col": underlyingDateCol,
		"view_date_col":       mappedCol,
	}).Debug("resolved view date column")

	return mappedCol
}

// truncateString truncates a string to maxLen with ellipsis
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
```

**Step 2: 验证编译通过**

Run: `go build ./drivers/clickhouse/...`
Expected: 无错误

**Step 3: Commit**

```bash
git add drivers/clickhouse/view_resolver.go
git commit -m "feat(clickhouse): add view resolver for date column detection"
```

---

## Task 5: 实现主入口函数 getDateColumnForTable

**Files:**
- Modify: `drivers/clickhouse/view_resolver.go`

**Step 1: 添加主入口函数**

在 `view_resolver.go` 末尾添加：

```go
// getDateColumnForTable returns the date column for filtering stats queries
// Priority: user config > auto-detect view > auto-detect table > empty
func (ch *ClickHouse) getDateColumnForTable(dbName, tableName string, cfg drivers.StatsConfig) string {
	return ch.getDateColumnForTableInternal(dbName, tableName, cfg.DateColumn, cfg.Tables, make(map[string]bool))
}

// getDateColumnForTableInternal is the internal implementation with recursion tracking
func (ch *ClickHouse) getDateColumnForTableInternal(
	dbName, tableName string,
	globalDateColumn string,
	tables map[string]drivers.TableStatsConfig,
	visited map[string]bool,
) string {
	// Priority 1: Table-level config
	if tables != nil {
		if tableConfig, ok := tables[tableName]; ok && tableConfig.DateColumn != "" {
			log.WithFields(logrus.Fields{
				"table":       tableName,
				"date_column": tableConfig.DateColumn,
				"source":      "table config",
			}).Debug("using configured date column")
			return tableConfig.DateColumn
		}
	}

	// Priority 2: Global config
	if globalDateColumn != "" {
		log.WithFields(logrus.Fields{
			"table":       tableName,
			"date_column": globalDateColumn,
			"source":      "global config",
		}).Debug("using configured date column")
		return globalDateColumn
	}

	// Priority 3 & 4: Auto-detect
	meta, err := ch.getTableMeta(dbName, tableName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"table": tableName,
			"error": err.Error(),
		}).Debug("could not get table metadata")
		return ""
	}

	// Priority 3: View - resolve from underlying table
	if isView(meta.Engine) {
		log.WithFields(logrus.Fields{
			"table":  tableName,
			"engine": meta.Engine,
		}).Debug("detected view, resolving date column")
		return ch.resolveViewDateColumn(dbName, meta, visited)
	}

	// Priority 4: Regular table - use partition key
	if meta.PartitionKey != "" {
		dateCol := extractDateColumn(meta.PartitionKey)
		if dateCol != "" {
			log.WithFields(logrus.Fields{
				"table":         tableName,
				"partition_key": meta.PartitionKey,
				"date_column":   dateCol,
				"source":        "partition key",
			}).Debug("detected date column from partition key")
			return dateCol
		}
	}

	log.WithFields(logrus.Fields{
		"table":      tableName,
		"engine":     meta.Engine,
		"suggestion": "add stats.tables." + tableName + ".dateColumn in config",
	}).Debug("could not detect date column")
	return ""
}
```

**Step 2: 添加缺少的 import**

确保文件顶部有：

```go
import (
	"regexp"

	"github.com/k1LoW/tbls/drivers"
	"github.com/sirupsen/logrus"
)
```

**Step 3: 验证编译通过**

Run: `go build ./drivers/clickhouse/...`
Expected: 无错误

**Step 4: Commit**

```bash
git add drivers/clickhouse/view_resolver.go
git commit -m "feat(clickhouse): add getDateColumnForTable main entry function"
```

---

## Task 6: 修改 stats.go 使用新的日期列探测

**Files:**
- Modify: `drivers/clickhouse/stats.go`

**Step 1: 删除旧的日志函数**

删除以下函数（约第 14-40 行）：
- `isDebug()`
- `logDebug()`
- `logSQL()`

**Step 2: 修改 CollectStats 函数**

找到 `CollectStats` 函数中的日期列获取逻辑，替换：

旧代码（约第 111-116 行）：
```go
// Determine sampling strategy
isLargeTable := table.Stats != nil && table.Stats.RowCount > cfg.LargeTableThreshold
partitionKey := ch.getPartitionKey(s.Name, table.Name)
if isLargeTable {
    logDebug("  Large table detected, will use sampling (partition key: %s)", partitionKey)
}
```

新代码：
```go
// Get date column for filtering
dateCol := ch.getDateColumnForTable(s.Name, table.Name, cfg)

// Determine if we should use date filtering
// For views, always use date filter if available (since row count is 0)
// For tables, only use if large
useDataFilter := dateCol != "" && (isView(table.Type) || (table.Stats != nil && table.Stats.RowCount > cfg.LargeTableThreshold))

if useDataFilter {
    log.WithFields(logrus.Fields{
        "table":       table.Name,
        "date_column": dateCol,
        "recent_days": cfg.RecentDays,
    }).Debug("using date filter for stats collection")
}
```

**Step 3: 修改 getColumnStats 调用**

找到调用 `getColumnStats` 的地方，更新参数：

旧代码：
```go
colStats, err := ch.getColumnStats(s.Name, table.Name, col.Name, col.Type, isLargeTable, partitionKey, cfg)
```

新代码：
```go
colStats, err := ch.getColumnStats(s.Name, table.Name, col.Name, col.Type, useDataFilter, dateCol, cfg)
```

**Step 4: 修改 getColumnStats 函数签名和实现**

旧签名：
```go
func (ch *ClickHouse) getColumnStats(dbName, tableName, colName, colType string, isLargeTable bool, partitionKey string, cfg drivers.StatsConfig) (*schema.ColumnStats, error)
```

新签名：
```go
func (ch *ClickHouse) getColumnStats(dbName, tableName, colName, colType string, useDateFilter bool, dateColumn string, cfg drivers.StatsConfig) (*schema.ColumnStats, error)
```

修改函数内部 WHERE 子句构建：

旧代码：
```go
// Build WHERE clause for large table sampling
whereClause := ""
if isLargeTable {
    dateCol := extractDateColumn(partitionKey)
    if dateCol != "" {
        whereClause = fmt.Sprintf("WHERE %s >= today() - %d", backquote(dateCol), cfg.RecentDays)
    }
}
```

新代码：
```go
// Build WHERE clause for date filtering
whereClause := ""
if useDateFilter && dateColumn != "" {
    whereClause = fmt.Sprintf("WHERE %s >= today() - %d", backquote(dateColumn), cfg.RecentDays)
    log.WithFields(logrus.Fields{
        "table":        tableName,
        "column":       colName,
        "where_clause": whereClause,
    }).Debug("applying date filter")
}
```

**Step 5: 替换所有 logDebug 为 log.WithFields().Debug()**

将所有 `logDebug(...)` 调用替换为 logrus 格式。

**Step 6: 替换所有 logSQL 为 log.Debug()**

将所有 `logSQL(query)` 替换为：
```go
log.WithField("query", strings.TrimSpace(query)).Debug("executing SQL")
```

**Step 7: 删除 getPartitionKey 函数**

该函数已经被 `getTableMeta` 替代，可以删除。

**Step 8: 验证编译通过**

Run: `go build ./drivers/clickhouse/...`
Expected: 无错误

**Step 9: Commit**

```bash
git add drivers/clickhouse/stats.go
git commit -m "refactor(clickhouse): use new date column detection in stats collection"
```

---

## Task 7: 处理 table.Type 字段

**Files:**
- Modify: `drivers/clickhouse/stats.go`
- Modify: `drivers/clickhouse/clickhouse.go` (如果需要)

**Step 1: 检查 schema.Table 是否有 Type 字段**

如果 `table.Type` 不存在，需要在 CollectStats 中获取表类型：

```go
// 在循环开始处添加
tableMeta, _ := ch.getTableMeta(s.Name, table.Name)
tableEngine := ""
if tableMeta != nil {
    tableEngine = tableMeta.Engine
}

// 修改 useDataFilter 判断
useDataFilter := dateCol != "" && (isView(tableEngine) || (table.Stats != nil && table.Stats.RowCount > cfg.LargeTableThreshold))
```

**Step 2: 验证编译通过**

Run: `go build ./drivers/clickhouse/...`
Expected: 无错误

**Step 3: Commit**

```bash
git add drivers/clickhouse/stats.go
git commit -m "fix(clickhouse): handle table engine type for date filter decision"
```

---

## Task 8: 添加表级别 skip 支持

**Files:**
- Modify: `drivers/clickhouse/stats.go`

**Step 1: 在 CollectStats 循环中添加 skip 检查**

在表循环的开头添加：

```go
for _, table := range s.Tables {
    // Check if this table should be skipped via config
    if cfg.Tables != nil {
        if tableConfig, ok := cfg.Tables[table.Name]; ok && tableConfig.Skip {
            log.WithFields(logrus.Fields{
                "table": table.Name,
            }).Debug("skipping table (configured)")
            continue
        }
    }

    // Check if this table should have stats collected (existing logic)
    if !shouldCollectStats(table.Name, cfg.Include, cfg.Exclude) {
        // ... existing code
    }
    // ... rest of the loop
}
```

**Step 2: 验证编译通过**

Run: `go build ./drivers/clickhouse/...`
Expected: 无错误

**Step 3: Commit**

```bash
git add drivers/clickhouse/stats.go
git commit -m "feat(clickhouse): add table-level skip support in stats config"
```

---

## Task 9: 完整测试

**Step 1: 编译整个项目**

Run: `go build -o tbls .`
Expected: 无错误

**Step 2: 运行测试**

Run: `go test ./drivers/clickhouse/... -v`
Expected: 测试通过

**Step 3: 手动测试**

```bash
# 测试 View 日期列探测
DEBUG=1 ./tbls out -c ./.tbls-evofinder.yml -t json 2>&1 | head -50
```

Expected: 看到类似日志：
```
DEBU detected view, resolving date column    table=log_api_hkg_product_v2_view engine=View
DEBU extracted underlying table              view=log_api_hkg_product_v2_view underlying_table=log_api_hkg_product_v2
DEBU resolved view date column               view=log_api_hkg_product_v2_view view_date_col=__date
DEBU using date filter for stats collection  table=log_api_hkg_product_v2_view date_column=__date recent_days=30
```

**Step 4: 最终 Commit**

```bash
git add -A
git commit -m "feat(clickhouse): complete view date column auto-detection

- Add logrus structured logging
- Add dateColumn and tables config options
- Auto-detect date column from View's underlying table
- Support table-level skip and dateColumn override
- Apply date filter for both large tables and views"
```

---

## 完成检查清单

- [ ] Task 1: logrus 日志配置
- [ ] Task 2: 配置结构更新
- [ ] Task 3: datasource 传递配置
- [ ] Task 4: View 解析器
- [ ] Task 5: 主入口函数
- [ ] Task 6: stats.go 重构
- [ ] Task 7: table.Type 处理
- [ ] Task 8: skip 支持
- [ ] Task 9: 完整测试
