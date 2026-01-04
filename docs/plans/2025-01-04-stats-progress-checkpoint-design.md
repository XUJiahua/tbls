# Stats 进度报告与断点续传设计

## 背景

开启 stats 收集后，整个执行过程可能很耗时（每个 column 需要 2 个 SQL 查询）。需要：
1. 进度报告 - 让用户了解执行状态
2. 断点续传 - 中断后可从上次位置继续

需同时支持 CMD 和 REST API 两种模式。

## 整体架构

```
┌─────────────────────────────────────────────────────┐
│                  ProgressReporter                    │
│  (接口：CMD 实现终端输出，REST 实现状态存储)          │
└─────────────────────────────────────────────────────┘
                          │
┌─────────────────────────────────────────────────────┐
│                 CheckpointManager                    │
│  - 读取/写入检查点文件                               │
│  - 验证 TTL 和 schema 变化                          │
│  - 文件位置: {docPath}/.tbls-stats-checkpoint.json  │
└─────────────────────────────────────────────────────┘
                          │
┌─────────────────────────────────────────────────────┐
│              AnalyzeWithStats (改造)                 │
│  - 接收 ProgressReporter 和 CheckpointManager       │
│  - 每完成一个阶段更新检查点并报告进度                 │
└─────────────────────────────────────────────────────┘
```

**阶段流转：**
- `analyzing` → `collecting_stats` → `inferring` → `completed`
- 任意阶段失败 → `failed`
- 用户取消 → `cancelled`

## 检查点文件格式

**文件路径：** `{docPath}/.tbls-stats-checkpoint.json`

```json
{
  "version": 1,
  "task_id": "uuid-xxx",
  "dsn_hash": "sha256-of-dsn",
  "schema_hash": "sha256-of-table-names-and-columns",
  "updated_at": "2025-01-04T10:30:00Z",
  "stage": "collecting_stats",
  "progress": {
    "completed_tables": ["users"],
    "current_table": "orders",
    "completed_columns": ["id", "customer_id", "amount"],
    "total_columns": 15
  },
  "partial_result": {
    "tables": {
      "users": { /* 完整 stats */ },
      "orders": {
        "columns": {
          "id": { /* stats */ },
          "customer_id": { /* stats */ }
        }
      }
    }
  }
}
```

**字段说明：**
- `dsn_hash` - 用于验证是同一个数据库
- `schema_hash` - 表名+列名的哈希，schema 变化时自动失效
- `partial_result` - 已完成表的统计结果，恢复时直接使用

**失效判断逻辑：**
1. `updated_at` 超过 TTL（默认 24h）→ 失效
2. `schema_hash` 不匹配 → 失效
3. `stats.checkpoint.force: true` → 强制忽略

## CMD 模式实现

**终端输出示例：**

```
$ tbls doc --stats

[1/4] Analyzing schema...
[2/4] Collecting stats... orders.customer_id (45/200 columns)
[3/4] Running inference...
[4/4] Completed

Generated: dbdoc/
```

**中断恢复示例：**

```
$ tbls doc --stats
# Ctrl+C 中断

$ tbls doc --stats
Resuming from checkpoint (last updated 2 hours ago)
[2/4] Collecting stats... (45/200 columns)  # 从中断点继续
...
```

**强制重新开始：**

```
$ tbls doc --stats --force-stats
Ignoring checkpoint, starting fresh...
[1/4] Analyzing schema...
...
```

**命令行参数：**
- `--force-stats` - 强制忽略检查点重新开始
- `--no-checkpoint` - 禁用检查点功能

## REST API 模式实现

### 1. 提交任务（异步）

```
POST /schema
Content-Type: application/json

{
  "dsn": { "url": "clickhouse://..." },
  "stats": { "enabled": true }
}

Response 202 Accepted:
{
  "task_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "pending"
}
```

### 2. 查询进度

```
GET /schema/status/:task_id

Response 200 (进行中):
{
  "task_id": "550e8400-...",
  "status": "running",
  "stage": "collecting_stats",
  "progress": {
    "current_table": "orders",
    "current_column": "customer_id",
    "completed_columns": 45,
    "total_columns": 200
  },
  "resumed_from_checkpoint": true,
  "started_at": "2025-01-04T10:30:00Z"
}

Response 200 (完成):
{
  "task_id": "550e8400-...",
  "status": "completed",
  "result": { /* schema JSON */ }
}

Response 200 (失败):
{
  "task_id": "550e8400-...",
  "status": "failed",
  "error": "connection refused"
}
```

### 3. 取消任务

```
DELETE /schema/:task_id

Response 200:
{
  "task_id": "550e8400-...",
  "status": "cancelled",
  "checkpoint_saved": true
}
```

### 4. 强制重新开始

```
POST /schema?force=true
```

**任务存储：** 内存 map（进程重启后任务丢失，但检查点文件仍在）

## 配置项

**YAML 配置：**

```yaml
stats:
  enabled: true
  topN: 20
  sampleSize: 10000
  inference: true

  # 检查点配置
  checkpoint:
    enabled: true          # 默认 true，是否启用断点续传
    ttl: 24h               # 检查点有效期，默认 24 小时
    force: false           # 强制忽略检查点重新开始
```

**优先级：** 命令行 > 配置文件 > 默认值

**默认值：**
- `checkpoint.enabled`: `true`
- `checkpoint.ttl`: `24h`
- `checkpoint.force`: `false`

## 核心接口与数据结构

### ProgressReporter 接口

```go
type Stage string

const (
    StageAnalyzing       Stage = "analyzing"
    StageCollectingStats Stage = "collecting_stats"
    StageInferring       Stage = "inferring"
    StageCompleted       Stage = "completed"
    StageFailed          Stage = "failed"
    StageCancelled       Stage = "cancelled"
)

type Progress struct {
    Stage            Stage
    CurrentTable     string
    CurrentColumn    string
    CompletedColumns int
    TotalColumns     int
}

type ProgressReporter interface {
    // 报告进度
    Report(p Progress)
    // 检查是否被取消
    IsCancelled() bool
}
```

### 两种实现

```go
// CMD 模式 - 输出到终端
type CLIProgressReporter struct {
    writer    io.Writer
    cancelled atomic.Bool
}

// REST 模式 - 存储到内存供轮询
type TaskProgressReporter struct {
    taskID    string
    store     *TaskStore  // 并发安全的 map
    cancelled atomic.Bool
}
```

### CheckpointManager 接口

```go
type CheckpointManager interface {
    // 加载检查点，返回 nil 表示无有效检查点
    Load(docPath string, schemaHash string) (*Checkpoint, error)
    // 保存检查点
    Save(docPath string, cp *Checkpoint) error
    // 删除检查点（任务完成后）
    Delete(docPath string) error
}
```

## 错误处理

| 场景 | 处理方式 |
|------|---------|
| 数据库连接失败/断开 | 立即停止，保存检查点，返回错误 |
| 查询超时 | 立即停止，保存检查点，返回错误 |
| 权限不足 | 立即停止，返回明确错误信息 |
| 用户取消 | 保存检查点，优雅退出 |
| 检查点文件损坏 | 忽略检查点，从头开始，记录警告 |

**策略：** 任何数据库相关错误都立即停止并报告，不静默跳过。检查点机制保证用户可以在修复问题后继续。

**信号处理（CMD 模式）：**

```go
// 捕获 SIGINT/SIGTERM
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

go func() {
    <-sigCh
    reporter.Cancel()  // 设置 cancelled = true
}()

// 在收集循环中检查
for _, col := range table.Columns {
    if reporter.IsCancelled() {
        checkpoint.Save()
        return ErrCancelled
    }

    stats, err := collectColumnStats(col)
    if err != nil {
        checkpoint.Save()
        return fmt.Errorf("failed to collect stats for %s.%s: %w",
            table.Name, col.Name, err)
    }
    col.Stats = stats
    checkpoint.Update(col)
}
```

## 文件结构

**新增/修改的文件：**

```
stats/
├── progress.go        # ProgressReporter 接口及实现
├── checkpoint.go      # CheckpointManager 实现
└── checkpoint_test.go

config/
└── config.go          # 新增 CheckpointConfig 结构

cmd/
├── doc.go             # 添加 --force-stats, --no-checkpoint 参数
└── serve.go           # 改为异步模式，新增 status/cancel 接口

datasource/
└── datasource.go      # AnalyzeWithStats 接收 ProgressReporter
```
