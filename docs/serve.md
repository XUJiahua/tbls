# tbls serve

`tbls serve` starts an HTTP server that provides a `/schema` endpoint to analyze databases and return schema information as JSON.

> **OpenAPI/Swagger**: The server provides Swagger UI at `/swagger/index.html`. The OpenAPI specification is generated from code annotations using [swaggo/swag](https://github.com/swaggo/swag). See [swagger.yaml](swagger.yaml) for the generated spec.

## Usage

```bash
# Start server on default port 8080
tbls serve

# Start server on custom port
tbls serve -a :3000
tbls serve --addr :3000
```

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--addr` | `-a` | `:8080` | Server listen address |

## API Endpoints

### POST /schema

Analyze a database asynchronously. Returns a task ID immediately for progress polling.

#### Request

**Content-Type:** `application/json`

The request body accepts the same configuration as `.tbls.yml`, with `dsn.url` being required:

```json
{
  "dsn": {
    "url": "postgres://user:pass@localhost:5432/dbname",
    "headers": {}
  },
  "name": "My Database",
  "desc": "Database description",
  "include": ["users", "posts"],
  "exclude": ["*_backup", "*_temp"],
  "distance": 1,
  "format": {
    "sort": true,
    "adjust": false
  },
  "relations": [
    {
      "table": "posts",
      "columns": ["author_id"],
      "parentTable": "users",
      "parentColumns": ["id"]
    }
  ],
  "comments": [
    {
      "table": "users",
      "tableComment": "User accounts",
      "columnComments": {
        "id": "Primary key",
        "email": "User email address"
      }
    }
  ],
  "detectVirtualRelations": {
    "enabled": true,
    "strategy": "default"
  },
  "force": false
}
```

#### Required Fields

| Field | Description |
|-------|-------------|
| `dsn.url` | Database connection string (required) |

#### Optional Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Override database name |
| `desc` | string | Database description |
| `include` | []string | Tables to include (supports wildcards) |
| `exclude` | []string | Tables to exclude (supports wildcards) |
| `distance` | int | Relation distance for filtering |
| `format.sort` | bool | Sort tables and columns alphabetically |
| `format.adjust` | bool | Adjust column widths |
| `relations` | array | Additional relations to add |
| `comments` | array | Additional comments to add |
| `detectVirtualRelations.enabled` | bool | Enable virtual relation detection |
| `detectVirtualRelations.strategy` | string | Naming strategy (default, rails, laravel) |
| `force` | bool | Force stats collection, ignoring checkpoint |

#### Response

**Accepted (202 Accepted):**

Returns a task ID for polling:

```json
{
  "task_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "pending"
}
```

**Error (400 Bad Request):**

```json
{
  "error": "dsn.url is required"
}
```

### GET /schema/status/:task_id

Get the status and progress of a schema analysis task.

#### Response

**Running (200 OK):**

```json
{
  "task_id": "550e8400-e29b-41d4-a716-446655440000",
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
```

**Completed (200 OK):**

```json
{
  "task_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "completed",
  "stage": "completed",
  "started_at": "2025-01-04T10:30:00Z",
  "completed_at": "2025-01-04T10:35:00Z",
  "result": {
    "name": "mydb",
    "tables": [...],
    "relations": [...],
    "driver": {...}
  }
}
```

**Failed (200 OK):**

```json
{
  "task_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "failed",
  "stage": "failed",
  "error": "connection refused",
  "started_at": "2025-01-04T10:30:00Z",
  "completed_at": "2025-01-04T10:30:05Z"
}
```

**Not Found (404 Not Found):**

```json
{
  "error": "task not found"
}
```

#### Task Status Values

| Status | Description |
|--------|-------------|
| `pending` | Task queued, not yet started |
| `running` | Task in progress |
| `completed` | Task finished successfully |
| `failed` | Task failed with error |
| `cancelled` | Task was cancelled |

#### Stage Values

| Stage | Description |
|-------|-------------|
| `analyzing` | Analyzing database schema |
| `collecting_stats` | Collecting column statistics |
| `inferring` | Running inference on statistics |
| `completed` | All stages completed |
| `failed` | Processing failed |
| `cancelled` | Task was cancelled |

### DELETE /schema/:task_id

Cancel a running task. The current progress is saved to a checkpoint file.

#### Response

**Success (200 OK):**

```json
{
  "task_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "cancelled",
  "checkpoint_saved": true
}
```

**Not Found (404 Not Found):**

```json
{
  "error": "task not found"
}
```

**Bad Request (400 Bad Request):**

```json
{
  "error": "task is not running",
  "status": "completed",
  "task_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

## Examples

### Basic Usage (Async)

```bash
# Start the server
tbls serve -a :8080

# Submit a task
RESPONSE=$(curl -s -X POST http://localhost:8080/schema \
  -H "Content-Type: application/json" \
  -d '{"dsn": {"url": "sqlite:///path/to/db.sqlite"}}')

TASK_ID=$(echo $RESPONSE | jq -r '.task_id')
echo "Task ID: $TASK_ID"

# Poll for status
curl -s http://localhost:8080/schema/status/$TASK_ID | jq
```

### Polling Until Completion

```bash
#!/bin/bash
TASK_ID="$1"

while true; do
  RESPONSE=$(curl -s http://localhost:8080/schema/status/$TASK_ID)
  STATUS=$(echo $RESPONSE | jq -r '.status')

  case $STATUS in
    "completed")
      echo "Task completed!"
      echo $RESPONSE | jq '.result'
      break
      ;;
    "failed")
      echo "Task failed: $(echo $RESPONSE | jq -r '.error')"
      exit 1
      ;;
    "cancelled")
      echo "Task was cancelled"
      exit 0
      ;;
    *)
      STAGE=$(echo $RESPONSE | jq -r '.stage')
      PROGRESS=$(echo $RESPONSE | jq -r '.progress.completed_columns // 0')
      TOTAL=$(echo $RESPONSE | jq -r '.progress.total_columns // 0')
      echo "[$STAGE] Progress: $PROGRESS/$TOTAL columns"
      sleep 2
      ;;
  esac
done
```

### Cancel a Running Task

```bash
curl -X DELETE http://localhost:8080/schema/$TASK_ID
```

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

### Force Stats Collection (Ignore Checkpoint)

```bash
curl -X POST http://localhost:8080/schema \
  -H "Content-Type: application/json" \
  -d '{
    "dsn": {"url": "clickhouse://localhost:9000/mydb"},
    "stats": {"enabled": true},
    "force": true
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
| `stats.checkpoint.enabled` | bool | true | Enable checkpoint/resume |
| `stats.checkpoint.ttl` | string | "24h" | Checkpoint validity duration |
| `stats.checkpoint.force` | bool | false | Ignore existing checkpoint |

## Supported Databases

The `/schema` endpoint supports all databases that tbls supports:

- PostgreSQL (`postgres://`)
- MySQL (`mysql://`)
- MariaDB (`maria://`, `mariadb://`)
- SQLite (`sqlite://`)
- Microsoft SQL Server (`sqlserver://`)
- ClickHouse (`clickhouse://`)
- Snowflake (`snowflake://`)
- BigQuery (`bq://`, `bigquery://`)
- Cloud Spanner (`span://`, `spanner://`)
- Amazon DynamoDB (`dynamodb://`)
- MongoDB (`mongodb://`)
- Databricks (`databricks://`)
