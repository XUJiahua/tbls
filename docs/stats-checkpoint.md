# Stats 进度报告与断点续传

当启用 stats 收集时，tbls 支持进度报告和断点续传功能，适用于大型数据库的长时间统计收集任务。

## 功能概述

- **进度报告**：实时显示当前处理的表和列，以及整体进度
- **断点续传**：中断后可从上次位置继续，避免重复工作
- **信号处理**：支持 Ctrl+C 优雅退出并保存检查点

## 配置

### YAML 配置

```yaml
dsn: clickhouse+http://user:password@host:port/database

stats:
  enabled: true
  topN: 20
  sampleSize: 10000

  # 断点续传配置
  checkpoint:
    enabled: true    # 启用断点续传（默认 true）
    ttl: 24h         # 检查点有效期（默认 24 小时）
    force: false     # 强制忽略检查点重新开始
```

### 配置项说明

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `checkpoint.enabled` | bool | `true` | 是否启用断点续传 |
| `checkpoint.ttl` | string | `"24h"` | 检查点有效期，支持 Go duration 格式 |
| `checkpoint.force` | bool | `false` | 强制忽略现有检查点，重新开始 |

## 命令行使用

### 基本用法

```bash
# 启用 stats 收集，自动启用进度报告和断点续传
tbls doc -c .tbls.yml

# 输出示例：
# [1/4] Analyzing schema...
# [2/4] Collecting stats... orders.customer_id (45/200 columns)
# [3/4] Running inference...
# [4/4] Completed
```

### 中断与恢复

```bash
# 首次运行，中途按 Ctrl+C 中断
tbls doc -c .tbls.yml
# [2/4] Collecting stats... orders.amount (50/200 columns)
# ^C
# Cancelled (checkpoint saved)

# 再次运行，从上次位置继续
tbls doc -c .tbls.yml
# Resuming from checkpoint (last updated 5 minutes ago)
# [2/4] Collecting stats... orders.status (51/200 columns)
# ...
```

### 命令行参数

| 参数 | 说明 |
|------|------|
| `--force-stats` | 强制忽略检查点，重新收集统计信息 |
| `--no-checkpoint` | 禁用断点续传功能 |

```bash
# 强制重新开始
tbls doc -c .tbls.yml --force-stats

# 禁用断点续传
tbls doc -c .tbls.yml --no-checkpoint
```

## 进度阶段

Stats 收集分为四个阶段：

| 阶段 | 说明 |
|------|------|
| `analyzing` | 分析数据库 schema 结构 |
| `collecting_stats` | 收集列级统计信息（最耗时） |
| `inferring` | 基于统计信息进行推理 |
| `completed` | 处理完成 |

进度报告在 `collecting_stats` 阶段会显示列级别的详细进度。

## 检查点文件

### 文件位置

检查点保存在 `{docPath}/.tbls-stats-checkpoint.json`，例如：

```
dbdoc/.tbls-stats-checkpoint.json
```

### 文件格式

```json
{
  "version": 1,
  "dsn_hash": "sha256...",
  "schema_hash": "sha256...",
  "updated_at": "2025-01-04T10:30:00Z",
  "stage": "collecting_stats",
  "progress": {
    "completed_tables": ["users", "products"],
    "current_table": "orders",
    "completed_columns": ["id", "user_id", "product_id"],
    "total_columns": 15
  },
  "partial_result": {
    "tables": {
      "users": {
        "stats": {"row_count": 1000},
        "columns": {
          "id": {"stats": {"row_count": 1000, "distinct_count": 1000}}
        }
      }
    }
  }
}
```

### 检查点失效条件

检查点在以下情况会被自动忽略：

1. **超过 TTL**：检查点最后更新时间超过配置的 TTL（默认 24 小时）
2. **Schema 变化**：数据库表结构发生变化（表名或列名变更）
3. **DSN 变化**：连接的数据库地址发生变化
4. **强制重新开始**：使用 `--force-stats` 或配置 `checkpoint.force: true`

## REST API 模式

REST API 使用异步模式，通过轮询获取进度。详见 [serve.md](serve.md)。

### 提交任务

```bash
curl -X POST http://localhost:8080/schema \
  -H "Content-Type: application/json" \
  -d '{
    "dsn": {"url": "clickhouse://localhost:9000/mydb"},
    "stats": {"enabled": true}
  }'

# 响应
# {"task_id": "550e8400-...", "status": "pending"}
```

### 查询进度

```bash
curl http://localhost:8080/schema/status/550e8400-...

# 响应
# {
#   "task_id": "550e8400-...",
#   "status": "running",
#   "stage": "collecting_stats",
#   "progress": {
#     "current_table": "orders",
#     "current_column": "customer_id",
#     "completed_columns": 45,
#     "total_columns": 200
#   }
# }
```

### 取消任务

```bash
curl -X DELETE http://localhost:8080/schema/550e8400-...

# 响应
# {"task_id": "550e8400-...", "status": "cancelled", "checkpoint_saved": true}
```

## 错误处理

| 错误类型 | 处理方式 |
|----------|----------|
| 数据库连接失败 | 保存检查点，返回错误 |
| 查询超时 | 保存检查点，返回错误 |
| 权限不足 | 立即返回错误 |
| 用户取消 (Ctrl+C) | 保存检查点，优雅退出 |
| 检查点文件损坏 | 忽略检查点，从头开始 |

所有数据库相关错误都会立即停止并保存当前进度，不会静默跳过失败的列或表。

## 最佳实践

1. **大型数据库**：对于列数超过 100 的数据库，建议启用断点续传
2. **网络不稳定**：在网络环境不稳定时，减小 TTL 以避免使用过期的检查点
3. **开发测试**：开发过程中可使用 `--force-stats` 确保每次都重新收集
4. **生产环境**：保持默认配置，利用断点续传减少重复工作

## 相关文档

- [Stats 推理功能](stats-inference.md) - 基于统计信息的元数据推理
- [REST API 文档](serve.md) - HTTP 服务接口说明
