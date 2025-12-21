# tbls serve

`tbls serve` starts an HTTP server that provides a `/schema` endpoint to analyze databases and return schema information as JSON.

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

Analyze a database and return the schema as JSON.

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
  }
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

#### Response

**Success (200 OK):**

Returns the database schema as JSON:

```json
{
  "name": "mydb",
  "tables": [
    {
      "name": "users",
      "type": "table",
      "columns": [
        {
          "name": "id",
          "type": "INTEGER",
          "nullable": false
        }
      ],
      "indexes": [...],
      "constraints": [...],
      "def": "CREATE TABLE ..."
    }
  ],
  "relations": [...],
  "driver": {
    "name": "postgres",
    "database_version": "15.0"
  }
}
```

**Error (400 Bad Request):**

```json
{
  "error": "dsn.url is required"
}
```

**Error (500 Internal Server Error):**

```json
{
  "error": "failed to connect to database: ..."
}
```

## Examples

### Basic Usage

```bash
# Start the server
tbls serve -a :8080

# In another terminal, query a SQLite database
curl -X POST http://localhost:8080/schema \
  -H "Content-Type: application/json" \
  -d '{"dsn": {"url": "sqlite:///path/to/db.sqlite"}}'
```

### PostgreSQL with Filtering

```bash
curl -X POST http://localhost:8080/schema \
  -H "Content-Type: application/json" \
  -d '{
    "dsn": {"url": "postgres://user:pass@localhost:5432/mydb"},
    "include": ["users", "orders", "products"],
    "exclude": ["*_archive"]
  }'
```

### MySQL with Additional Relations

```bash
curl -X POST http://localhost:8080/schema \
  -H "Content-Type: application/json" \
  -d '{
    "dsn": {"url": "mysql://user:pass@localhost:3306/mydb"},
    "relations": [
      {
        "table": "orders",
        "columns": ["customer_id"],
        "parentTable": "customers",
        "parentColumns": ["id"],
        "cardinality": "zero or more",
        "parentCardinality": "exactly one"
      }
    ]
  }'
```

### ClickHouse

```bash
curl -X POST http://localhost:8080/schema \
  -H "Content-Type: application/json" \
  -d '{
    "dsn": {"url": "clickhouse://localhost:9000/mydb"}
  }'
```

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
