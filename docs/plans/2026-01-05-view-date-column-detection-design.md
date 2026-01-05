# View 日期列自动探测设计

## 背景

ClickHouse 的 stats 收集功能对大表需要添加日期过滤来提升性能。当前实现存在问题：

1. **View 的 `total_rows` 为 0**：`system.tables` 对 View 返回 0，导致不被识别为大表
2. **View 没有 `partition_key`**：无法自动获取分区键
3. **日期过滤不生效**：View 查询没有带上日期条件，导致全表扫描

## 目标

自动探测 View 的日期分区列，为 stats 查询添加日期过滤，减少查询耗时。

## 设计方案

### 整体流程

```
┌─────────────────────────────────────────────────────────────┐
│                    getDateColumnForTable                     │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  1. 检查用户配置 stats.tables.<table>.dateColumn            │
│     └─ 有配置 → 直接返回                                     │
│                                                             │
│  2. 检查全局配置 stats.dateColumn                            │
│     └─ 有配置 → 直接返回                                     │
│                                                             │
│  3. 查询 system.tables 获取 engine, partition_key, as_select │
│                                                             │
│  4. 是普通表？(engine = MergeTree 系列)                      │
│     └─ 是 → extractDateColumn(partition_key) → 返回         │
│                                                             │
│  5. 是 View？(engine = 'View')                              │
│     ├─ 解析 as_select 提取底层表名                           │
│     ├─ 递归获取底层表的分区键日期列                           │
│     ├─ 在 SELECT 子句中查找该列的映射                        │
│     └─ 找到 → 返回映射后的列名                               │
│                                                             │
│  6. 都没找到                                                 │
│     └─ 打印 WARN 日志，返回空                                │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### View 解析逻辑

```
┌─────────────────────────────────────────────────────────────┐
│                   resolveViewDateColumn                      │
├─────────────────────────────────────────────────────────────┤
│ 输入: viewName, asSelect (View 的 SQL 定义)                  │
│ 输出: 映射后的日期列名 或 空字符串                            │
└─────────────────────────────────────────────────────────────┘

Step 1: 提取底层表名
  正则: FROM\s+([`"]?\w+[`"]?\.)?([`"]?\w+[`"]?)

  匹配示例:
    "SELECT ... FROM log_table"           → log_table
    "SELECT ... FROM db.log_table"        → log_table
    "SELECT ... FROM `log_table` AS t"    → log_table

Step 2: 获取底层表分区键的日期列
  递归调用 getDateColumnForTable(底层表名)
    → 返回如 "__date" 或 "event_date"

Step 3: 在 SELECT 子句中查找列映射
  搜索模式 (按优先级):

  A) 直接同名: __date
     正则: \b__date\b
     结果: __date

  B) 带表别名: t.__date 或 t.__date AS dt
     正则: \w+\.__date(\s+AS\s+(\w+))?
     结果: 有 AS 用别名，否则用 __date

  C) 函数包装: toDate(col) AS __date
     正则: \w+\([^)]+\)\s+AS\s+(\w+)
     结果: 使用 AS 后的别名
```

### 边界处理

- **多表 JOIN**：只取第一个 FROM 表（主表）
- **子查询**：暂不支持，打日志跳过
- **找不到映射**：打日志，返回空

## 配置设计

```yaml
stats:
  enabled: true
  topN: 20
  sampleSize: 10000

  # 现有配置
  largeTableThreshold: 1000000  # 大表阈值（行数）
  recentDays: 30                # 过滤最近 N 天

  # 新增配置
  dateColumn: ""                # 全局日期列名（可选，覆盖自动探测）

  # 表级别覆盖（新增）
  tables:
    log_api_hkg_product_v2_view:
      dateColumn: __date        # 指定该表的日期列
    log_order_hkg_product_v2_view:
      dateColumn: dt            # 不同表可以不同
    some_small_table:
      skip: true                # 跳过该表的 stats 收集
```

### 配置优先级

1. `stats.tables.<table>.dateColumn` (表级别指定)
2. `stats.dateColumn` (全局指定)
3. 自动探测 View 底层表映射
4. 自动探测普通表 partition_key
5. 放弃，不加日期过滤

### 新增配置项

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `stats.dateColumn` | string | `""` | 全局日期列名 |
| `stats.tables.<name>.dateColumn` | string | `""` | 表级别日期列 |
| `stats.tables.<name>.skip` | bool | `false` | 跳过该表 |

## 代码结构

```
drivers/clickhouse/
├── log.go           # logrus 日志配置 (新增)
├── view_resolver.go # View 解析逻辑 (新增)
└── stats.go         # 修改，调用 getDateColumnForTable
```

### 新增函数

```go
// log.go
var log = logrus.New()
func init() { /* 配置 logrus */ }

// view_resolver.go

// TableMeta 存储表/视图的元数据
type TableMeta struct {
    Name         string
    Engine       string
    PartitionKey string
    AsSelect     string
}

// getTableMeta 查询 system.tables 获取元数据
func (ch *ClickHouse) getTableMeta(dbName, tableName string) (*TableMeta, error)

// resolveViewDateColumn 解析 View 找到日期列映射
func (ch *ClickHouse) resolveViewDateColumn(dbName string, meta *TableMeta) string

// extractUnderlyingTable 从 as_select 提取底层表名
func extractUnderlyingTable(asSelect string) string

// findColumnMapping 在 SELECT 子句中查找列映射
func findColumnMapping(asSelect, sourceColumn string) string

// stats.go

// getDateColumnForTable 获取表的日期过滤列（主入口）
func (ch *ClickHouse) getDateColumnForTable(
    dbName, tableName string,
    cfg drivers.StatsConfig,
) string
```

### 改动文件

| 文件 | 改动 |
|------|------|
| `drivers/clickhouse/log.go` | 新增，logrus 配置 |
| `drivers/clickhouse/view_resolver.go` | 新增，View 解析 |
| `drivers/clickhouse/stats.go` | 修改，删除旧日志函数，调用新入口 |
| `drivers/drivers.go` | 修改，StatsConfig 新增字段 |
| `config/config.go` | 修改，新增配置项 |

## 日志设计

使用 logrus 结构化日志：

```go
// log.go
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

    if os.Getenv("DEBUG") == "1" || os.Getenv("TBLS_DEBUG") == "1" {
        log.SetLevel(logrus.DebugLevel)
    } else {
        log.SetLevel(logrus.WarnLevel)
    }
}
```

### 日志示例

```bash
# DEBUG=1 时
DEBU Table log_data: detected date column from partition_key
     partition_key="toYYYYMM(__date)" date_column="__date"

DEBU View log_api_hkg_product_v2_view: resolving date column
     engine="View" underlying_table="log_api_hkg_product_v2"

# 失败时 (始终显示)
WARN View complex_view: could not detect date column
     reason="column mapping not found"
     suggestion="add stats.tables.complex_view.dateColumn"
```

## 错误处理

| 情况 | 处理 |
|------|------|
| View 底层表找不到 | WARN 日志，跳过日期过滤 |
| 列映射找不到 | WARN 日志，跳过日期过滤 |
| 用户配置的列不存在 | WARN 日志，跳过日期过滤 |
| SQL 解析异常 | WARN 日志，跳过日期过滤 |

**原则**：永远不因为日期列探测失败而中断 stats 收集流程。

## 预期覆盖率

- 简单 View（直接映射）：~95%
- 复杂 View（JOIN/子查询）：~60%
- 整体：~80%

未覆盖场景通过用户配置 `stats.tables.<name>.dateColumn` 兜底。
