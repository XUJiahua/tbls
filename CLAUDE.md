# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

tbls is a CI-friendly database documentation tool written in Go. It automatically generates GitHub Flavored Markdown documentation from database schemas and works as a schema linter.

## Common Commands

```bash
# Build
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

# Generate sample documentation
make doc

# Verify documentation matches database schema
make testdoc
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

- **datasource/**: Orchestrates driver selection based on DSN scheme. Handles special datasources (JSON files, HTTP, GitHub, external plugins).

- **output/**: Output format implementations. Each format implements the `Output` interface:
  ```go
  type Output interface {
      OutputSchema(wr io.Writer, s *schema.Schema) error
      OutputTable(wr io.Writer, s *schema.Table) error
  }
  ```
  Formats: Markdown (md/), JSON, YAML, DOT (Graphviz), Mermaid, PlantUML, Excel (xlsx), Config.

- **config/**: YAML configuration parsing. Key struct is `Config` which defines DSN, lint rules, relations, comments, viewpoints, and output settings.

- **cmd/**: Cobra CLI commands (`doc`, `lint`, `diff`, `coverage`, `out`, `ls`).

### Data Flow

1. CLI parses config (`.tbls.yml`) and command flags
2. `datasource.Analyze()` selects driver based on DSN scheme
3. Driver queries database catalog and populates `schema.Schema`
4. Config applies modifications (relations, comments, filters, viewpoints)
5. Output renderer generates documentation in chosen format

### Linting Rules

Defined in `config/lint.go`. Each rule implements the `Rule` interface:
```go
type Rule interface {
    IsEnabled() bool
    Check(schema *schema.Schema, exclude []string) []RuleWarn
}
```

Available rules: `requireTableComment`, `requireColumnComment`, `requireIndexComment`, `requireConstraintComment`, `requireTriggerComment`, `requireTableLabels`, `unrelatedTable`, `columnCount`, `requireColumns`, `duplicateRelations`, `requireForeignKeyIndex`, `labelStyleBigQuery`, `requireViewpoints`.

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
