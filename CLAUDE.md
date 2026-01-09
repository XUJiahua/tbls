# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a modified fork of tbls, a CI-friendly database documentation tool written in Go. This fork focuses on API/TUI use cases with added statistics collection, interactive exploration, and HTTP API capabilities.

Key features added in this fork:
- `explore` command: Interactive TUI for schema and statistics exploration
- `scaffold` command: Generate config files with interactive table selection
- `serve` command: HTTP API for schema analysis
- Statistics collection: Column-level statistics (null rates, cardinality, distribution)
- Checkpoint/resume: Long-running stats operations can be interrupted and resumed

## Common Commands

```bash
# Build (includes swagger generation)
make build

# Run all tests (requires database containers)
make test

# Run tests without database connections
make test-no-db

# Lint
make lint

# Run a single test
go test -v ./path/to/package -run TestName

# Set up test databases (requires Docker)
make db
```

## CLI Commands

```bash
# Interactive schema exploration TUI
tbls explore metadata.json

# Generate config from DSN with interactive table selection
tbls scaffold --dsn "postgres://user:pass@localhost:5432/db" --interactive

# Start HTTP API server
tbls serve --addr :8080

# Output schema in various formats (json, yaml, md, dot, mermaid, plantuml, xlsx)
tbls out -t json -o schema.json

# List schema resources
tbls ls

# Measure documentation coverage
tbls coverage
```

## Architecture

### Core Packages

- **schema/**: Core data models (`Schema`, `Table`, `Column`, `Relation`, `Constraint`, `Index`, `Trigger`). The `Schema` struct is the central representation passed between all components.

- **drivers/**: Database-specific implementations. Each driver implements the `Driver` interface:
  ```go
  type Driver interface {
      Analyze(*schema.Schema) error  // Populate schema from database
      Info() (*schema.Driver, error) // Get database metadata
  }
  ```
  Supported: PostgreSQL, MySQL, MariaDB, SQLite, BigQuery, Cloud Spanner, DynamoDB, MongoDB, ClickHouse, Snowflake, Redshift, MSSQL, Databricks.

- **datasource/**: Orchestrates driver selection based on DSN scheme. Key functions:
  - `Analyze()`: Basic schema analysis
  - `AnalyzeWithStats()`: Schema analysis with column statistics collection
  Handles special datasources (JSON files, HTTP, GitHub, external plugins).

- **output/**: Output format implementations. Each format implements the `Output` interface:
  ```go
  type Output interface {
      OutputSchema(wr io.Writer, s *schema.Schema) error
      OutputTable(wr io.Writer, s *schema.Table) error
  }
  ```
  Formats: Markdown (md/), JSON, YAML, DOT (Graphviz), Mermaid, PlantUML, Excel (xlsx), Config.

- **config/**: YAML configuration parsing. Key struct is `Config` which defines DSN, lint rules, relations, comments, viewpoints, and output settings.

- **stats/**: Statistics collection and progress tracking:
  - `progress.go`: Progress reporting interfaces (`ProgressReporter`, `TaskStore`)
  - `checkpoint.go`: Checkpoint/resume functionality for long-running operations
  - `adapter.go`: Database adapter for stats queries

- **cmd/**: Cobra CLI commands:
  - `explore.go` / `explore_tui.go`: Interactive TUI for schema exploration
  - `scaffold.go` / `scaffold_tui.go`: Config generation with TUI
  - `serve.go` / `serve_types.go`: HTTP API server (Swagger documented)
  - `out.go`, `ls.go`, `coverage.go`: Schema output commands

### Data Flow

1. CLI parses config (`.tbls.yml`) and command flags
2. `datasource.Analyze()` or `AnalyzeWithStats()` selects driver based on DSN scheme
3. Driver queries database catalog and populates `schema.Schema`
4. (Optional) Stats collector gathers column statistics with checkpoint support
5. Config applies modifications (relations, comments, filters, viewpoints)
6. Output renderer generates documentation or TUI displays results

### HTTP API (serve command)

The `serve` command provides REST endpoints documented via Swagger:
- Schema analysis endpoints
- Async stats collection with task status tracking
- Checkpoint resume capabilities

Swagger docs are generated in `docs/` directory via `swag init`.

### External Drivers

tbls supports plugin drivers via executables named `tbls-driver-*` in PATH. The driver receives the DSN and must output schema JSON to stdout.

### Build Tags

Database-specific code uses build tags: `mysql`, `postgres`, `sqlite`, `bq`, `spanner`, `dynamo`, `mongodb`, `mssql`, `clickhouse`, `snowflake`, `redshift`, `databricks`, `mariadb`.

## Configuration

Default config files (checked in order): `.tbls.yml`, `tbls.yml`, `.tbls.yaml`, `tbls.yaml`

Key config sections:
- `dsn`: Database connection string
- `docPath`: Output directory (default: `dbdoc`)
- `er`: ER diagram settings (format, skip, etc.)
- `lint`: Linting rules
- `relations`: Additional relationship definitions not in DB
- `comments`: Override/add comments to tables/columns
- `viewpoints`: Organize tables into logical groups
