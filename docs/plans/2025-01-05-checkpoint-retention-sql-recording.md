# Checkpoint Retention & SQL Recording Design

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Keep checkpoints after completion for cache reuse, and record SQL queries in stats output for debugging.

**Architecture:** Modify checkpoint lifecycle to retain completed checkpoints as cache (invalidated by schema change or TTL). Add `Queries []string` field to stats structs to capture all executed SQL.

**Tech Stack:** Go, ClickHouse driver, existing checkpoint infrastructure

---

## Requirements

1. **Checkpoint Retention**: Don't delete checkpoint on success. Use completed checkpoint as cache.
   - Cache hit: same DSN + same schema hash + within TTL + stage=completed
   - Cache miss: re-collect stats
   - On cache hit: log "Using cached stats from [timestamp]", skip collection

2. **Checkpoint Scope**: Keep current behavior (only for stats collection)

3. **SQL Recording**: Record all SQL queries in output
   - `TableStats.Queries []string` - table-level queries
   - `ColumnStats.Queries []string` - column-level queries (stats + top values)
   - Always included in output (not debug-only)

## Data Structure Changes

### schema/schema.go

```go
type TableStats struct {
    RowCount   int64    `json:"row_count"`
    DataBytes  int64    `json:"data_bytes,omitempty"`
    IndexBytes int64    `json:"index_bytes,omitempty"`
    Queries    []string `json:"queries,omitempty"`  // NEW
}

type ColumnStats struct {
    // ... existing fields ...
    TopValues []TopValue `json:"top_values,omitempty"`
    Queries   []string   `json:"queries,omitempty"`  // NEW
}
```

## Checkpoint Flow

```
stats collection → success → save checkpoint with StageCompleted (don't delete)
                          ↓
next request → load checkpoint → valid & completed? → log, use cached, skip collection
                               → invalid/incomplete? → collect fresh/resume
```

## Edge Cases

| Scenario | Action |
|----------|--------|
| No checkpoint exists | Collect stats, save with StageCompleted |
| Checkpoint exists, schema changed | Collect fresh, overwrite |
| Checkpoint exists, TTL expired | Collect fresh, overwrite |
| Checkpoint exists, valid, completed | Log, load cached, skip collection |
| Checkpoint exists, valid, incomplete | Resume (existing behavior) |
