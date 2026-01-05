# tbls scaffold

`tbls scaffold` generates a complete configuration file with all parameters filled in, helping users to quickly set up and fine-tune their tbls configuration.

## Purpose

1. **See all available options** - Generate a config with all parameters explicitly listed
2. **Minimize manual input** - Auto-fill default values and infer relations
3. **Quick start** - Get a fully documented config from just a DSN

## Usage

```bash
# Generate config from existing config file
tbls scaffold -c .tbls.yml

# Generate config from DSN (no existing config)
tbls scaffold --dsn "postgres://user:pass@localhost:5432/mydb"

# Specify output file
tbls scaffold -c .tbls.yml -o my-config.yml

# Force overwrite without prompt
tbls scaffold -c .tbls.yml -f
```

## CLI Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--config` | `-c` | Existing config file path |
| `--dsn` | | Database connection string (required if `--config` is not specified) |
| `--out` | `-o` | Output file path (default: overwrite config file or `.tbls.yml`) |
| `--force` | `-f` | Force overwrite without prompt |

## Generated Config Structure

The scaffolded config includes all parameters with explicit values:

### DSN and Basic Settings

```yaml
dsn:
  url: postgres://user:pass@localhost:5432/mydb
docPath: dbdoc
```

### Format Settings

```yaml
format:
  adjust: false
  sort: false
  number: false
  showOnlyFirstParagraph: false
```

### ER Diagram Settings

```yaml
er:
  skip: false
  format: svg
  comment: false
  hideDef: false
  distance: 1
```

### Lint Rules

All 13 lint rules with their default values:

```yaml
lint:
  requireTableComment:
    enabled: false
    allOrNothing: false
  requireColumnComment:
    enabled: false
    allOrNothing: false
  requireIndexComment:
    enabled: false
    allOrNothing: false
  requireConstraintComment:
    enabled: false
    allOrNothing: false
  requireTriggerComment:
    enabled: false
    allOrNothing: false
  requireTableLabels:
    enabled: false
    allOrNothing: false
  unrelatedTable:
    enabled: false
    allOrNothing: false
  columnCount:
    enabled: false
    max: 0
  requireColumns:
    enabled: false
  duplicateRelations:
    enabled: false
  requireForeignKeyIndex:
    enabled: false
  labelStyleBigQuery:
    enabled: false
  requireViewpoints:
    enabled: false
```

### Comments

All tables, columns, indexes, constraints, and triggers are listed:

```yaml
comments:
  - table: users
    tableComment: "Users table"  # From database
    columnComments:
      id: "Primary key"          # From database
      name: ""                   # Empty if not set in database
      email: "User email"
    indexComments:
      users_pkey: ""
    constraintComments:
      users_email_key: ""
    triggerComments: {}
```

### Relations

Includes both existing foreign keys and inferred relations:

```yaml
relations:
  # Existing FK from database
  - table: posts
    columns:
      - user_id
    parentTable: users
    parentColumns:
      - id
  # Inferred from naming pattern (xxx_id -> xxxs.id)
  - table: comments
    columns:
      - author_id
    parentTable: authors
    parentColumns:
      - id
    def: "Inferred Relation"
```

### Stats Settings

Statistics collection configuration with all options:

```yaml
stats:
  enabled: false
  topN: 10
  sampleSize: 10000
  largeTableThreshold: 1000000
  recentDays: 30
  dateColumn: ""  # Global date column for partition filtering
  inference:
    enabled: false
    enumMaxCardinality: 0.01
    enumMaxDistinct: 20
    dictMaxCardinality: 0.05
    dictMaxDistinct: 100
    foreignKeyMinConfidence: 0.7
  checkpoint:
    enabled: false
    ttl: "24h"
    force: false
  tables:
    users:
      dateColumn: ""  # Per-table date column override
      skip: false
    posts:
      dateColumn: "created_at"
      skip: false
    logs:
      skip: true  # Skip stats collection for this table
```

#### Stats Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `enabled` | `false` | Enable statistics collection |
| `topN` | `10` | Number of top values to collect per column |
| `sampleSize` | `10000` | Maximum rows to sample |
| `largeTableThreshold` | `1000000` | Row count threshold for large table sampling |
| `recentDays` | `30` | Days to look back for large table sampling |
| `dateColumn` | `""` | Global date column for partition filtering |

#### Inference Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `enabled` | `false` | Enable stats-based inference |
| `enumMaxCardinality` | `0.01` | Max cardinality ratio for enum detection |
| `enumMaxDistinct` | `20` | Max distinct values for enum detection |
| `dictMaxCardinality` | `0.05` | Max cardinality ratio for dictionary detection |
| `dictMaxDistinct` | `100` | Max distinct values for dictionary detection |
| `foreignKeyMinConfidence` | `0.7` | Min confidence for FK inference |

#### Checkpoint Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `enabled` | `false` | Enable checkpoint/resume functionality |
| `ttl` | `"24h"` | Checkpoint cache duration |
| `force` | `false` | Force re-collection ignoring cache |

### Viewpoints (Example)

Viewpoints are not auto-generated, but an example is provided:

```yaml
# viewpoints:
#   - name: example-viewpoint
#     desc: "Description of this viewpoint"
#     tables:
#       - table1
#       - table2
#     groups:
#       - name: group1
#         desc: "Description of this group"
#         tables:
#           - table1
```

## Relation Inference

The scaffold command uses the `default` naming strategy to infer relations:

| Column Pattern | Inferred Parent |
|---------------|-----------------|
| `user_id` | `users.id` |
| `post_id` | `posts.id` |
| `category_id` | `categories.id` |

The strategy:
1. Extract prefix from `xxx_id` pattern
2. Pluralize the prefix to get parent table name
3. Use `id` as parent column name
4. Only add if both table and column exist
5. Skip if relation already exists in database

## File Output Behavior

When the target file exists:

1. **With `--force`**: Overwrite without prompt
2. **Without `--force`**: Prompt user
   - `y` / `yes` - Overwrite the file
   - `s` / `scaffold` - Save as `.tbls.scaffold.yml`
   - Other - Abort operation

## Implementation Details

### Files

- `cmd/scaffold.go` - Main implementation
- `cmd/scaffold_test.go` - Unit tests

### Key Functions

- `generateScaffoldConfig()` - Creates the scaffolded config from existing config and schema
- `buildCommentsFromSchema()` - Extracts comments from database schema
- `buildRelationsFromSchema()` - Combines existing relations with inferred ones
- `writeScaffoldOutput()` - Handles file output with overwrite prompt
