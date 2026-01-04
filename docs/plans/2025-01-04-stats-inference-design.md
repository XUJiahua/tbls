# Stats Inference 自动推理设计

## 目标

在 tbls stats 功能基础上，添加自动推理能力。当 `stats.inference` 启用时，基于收集的统计信息自动推断元数据（主键、外键、枚举类型等），并将结果嵌入 Schema JSON。

## 推理能力

1. **主键/唯一标识符识别** - 列级
2. **外键关系推断** - 跨表
3. **枚举/字典列识别** - 列级
4. **数据分布分析** - 列级
5. **数据质量检测** - 列级
6. **业务洞察** - 跨表

## 配置

### 简单用法

```yaml
stats:
  enabled: true
  inference: true  # 使用默认阈值
```

### 自定义阈值

```yaml
stats:
  enabled: true
  inference:
    enabled: true
    enumMaxCardinality: 0.01       # 默认 0.01 (1%)
    enumMaxDistinct: 20            # 默认 20
    dictMaxCardinality: 0.05       # 默认 0.05 (5%)
    dictMaxDistinct: 100           # 默认 100
    foreignKeyMinConfidence: 0.7   # 默认 0.7
```

### Go 配置结构

```go
// config/config.go

type InferenceConfig struct {
    Enabled                 bool    `yaml:"enabled" json:"enabled"`
    EnumMaxCardinality      float64 `yaml:"enumMaxCardinality,omitempty" json:"enumMaxCardinality,omitempty"`
    EnumMaxDistinct         int     `yaml:"enumMaxDistinct,omitempty" json:"enumMaxDistinct,omitempty"`
    DictMaxCardinality      float64 `yaml:"dictMaxCardinality,omitempty" json:"dictMaxCardinality,omitempty"`
    DictMaxDistinct         int     `yaml:"dictMaxDistinct,omitempty" json:"dictMaxDistinct,omitempty"`
    ForeignKeyMinConfidence float64 `yaml:"foreignKeyMinConfidence,omitempty" json:"foreignKeyMinConfidence,omitempty"`
}

func DefaultInferenceConfig() InferenceConfig {
    return InferenceConfig{
        Enabled:                 false,
        EnumMaxCardinality:      0.01,
        EnumMaxDistinct:         20,
        DictMaxCardinality:      0.05,
        DictMaxDistinct:         100,
        ForeignKeyMinConfidence: 0.7,
    }
}

type StatsConfig struct {
    Enabled             bool             `yaml:"enabled" json:"enabled"`
    Include             []string         `yaml:"include,omitempty" json:"include,omitempty"`
    Exclude             []string         `yaml:"exclude,omitempty" json:"exclude,omitempty"`
    TopN                int              `yaml:"topN,omitempty" json:"topN,omitempty"`
    SampleSize          int              `yaml:"sampleSize,omitempty" json:"sampleSize,omitempty"`
    Inference           InferenceConfig  `yaml:"inference,omitempty" json:"inference,omitempty"`
}
```

## 数据结构

### 列级推理 (Column.Inferences)

```go
// schema/schema.go

type ColumnInferences struct {
    // 主键/唯一标识符识别
    IsPrimaryKey    bool    `json:"is_primary_key,omitempty"`
    IsUnique        bool    `json:"is_unique,omitempty"`

    // 枚举/字典列识别
    IsEnum          bool    `json:"is_enum,omitempty"`
    EnumType        string  `json:"enum_type,omitempty"`  // "enum" | "dictionary" | "high_cardinality"
    Cardinality     float64 `json:"cardinality,omitempty"`

    // 数据质量
    HasNulls        bool    `json:"has_nulls,omitempty"`
    IsComplete      bool    `json:"is_complete,omitempty"`

    // 数据分布
    Distribution    []DistributionItem `json:"distribution,omitempty"`
}

type DistributionItem struct {
    Value   string  `json:"value"`
    Count   int64   `json:"count"`
    Percent float64 `json:"percent"`
}

type Column struct {
    // ... existing fields
    Stats      *ColumnStats      `json:"stats,omitempty"`
    Inferences *ColumnInferences `json:"inferences,omitempty"`  // 新增
}
```

### Schema 顶层推理

```go
// schema/schema.go

type SchemaInferences struct {
    ForeignKeys      []InferredForeignKey `json:"foreign_keys,omitempty"`
    BusinessInsights []BusinessInsight    `json:"business_insights,omitempty"`
}

type InferredForeignKey struct {
    SourceTable  string  `json:"source_table"`
    SourceColumn string  `json:"source_column"`
    TargetTable  string  `json:"target_table"`
    TargetColumn string  `json:"target_column"`
    Confidence   float64 `json:"confidence"`
}

type BusinessInsight struct {
    Type        string   `json:"type"`
    Description string   `json:"description"`
    Value       float64  `json:"value"`
    Tables      []string `json:"tables"`
}

type Schema struct {
    // ... existing fields
    Inferences *SchemaInferences `json:"inferences,omitempty"`  // 新增
}
```

## 推理规则

### 1. 主键/唯一标识符识别

| 条件 | 推断结果 |
|------|----------|
| `distinct_count == row_count` 且 `null_count == 0` | `IsPrimaryKey: true` |
| `distinct_count == row_count` 且 `null_count > 0` | `IsUnique: true` |

### 2. 枚举/字典列识别

| 条件 | EnumType |
|------|----------|
| `cardinality <= enumMaxCardinality` 且 `distinct <= enumMaxDistinct` | `"enum"` |
| `cardinality <= dictMaxCardinality` 且 `distinct <= dictMaxDistinct` | `"dictionary"` |
| 其他 | `"high_cardinality"` |

### 3. 数据分布

将 `top_values` 转换为带百分比的 `Distribution`：
```
percent = (count / row_count) * 100
```

### 4. 数据质量

| 条件 | 推断结果 |
|------|----------|
| `null_count > 0` | `HasNulls: true` |
| `null_count == 0` | `IsComplete: true` |

### 5. 外键推断

匹配条件：
1. 列名匹配：`xxx_id` 模式，目标表存在 `xxx` 或 `xxxs`
2. 值域匹配：源列 `min/max` 在目标列范围内
3. 基数匹配：源列 `distinct_count` ≤ 目标列 `distinct_count`

置信度：
```
confidence = 0.4 (列名) + 0.3 (值域) + 0.3 (基数)
```

仅输出 `confidence >= foreignKeyMinConfidence` 的结果。

### 6. 业务洞察

| Type | 计算方式 |
|------|----------|
| `avg_per_entity` | `A.row_count / B.row_count` (A 有外键指向 B) |
| `entity_coverage` | `A.fk_column.distinct / B.row_count` |

## 架构

### 文件结构

```
schema/
  schema.go          # 添加 Inferences 相关结构体
  inference.go       # 新增：推理逻辑

config/
  config.go          # 添加 InferenceConfig
```

### 推理器

```go
// schema/inference.go

type Inferrer struct {
    config *config.InferenceConfig
}

func NewInferrer(cfg *config.InferenceConfig) *Inferrer

func (i *Inferrer) RunInference(s *Schema) error
```

### 调用流程

```
datasource.AnalyzeWithStats()
    │
    ├── driver.Analyze(schema)
    ├── driver.CollectStats(schema, cfg)
    └── inferrer.RunInference(schema)    # 新增
            ├── inferColumnLevel()
            ├── inferForeignKeys()
            └── inferBusinessInsights()
```

### datasource 集成

```go
// datasource/datasource.go

func AnalyzeWithStats(dsn string, cfg *config.Config) (*schema.Schema, error) {
    s, err := Analyze(dsn, cfg)
    if err != nil {
        return nil, err
    }

    if cfg.Stats.Enabled {
        if collector, ok := driver.(StatsCollector); ok {
            collector.CollectStats(s, &cfg.Stats)
        }

        // 新增
        if cfg.Stats.Inference.Enabled {
            inferrer := schema.NewInferrer(&cfg.Stats.Inference)
            inferrer.RunInference(s)
        }
    }

    return s, nil
}
```

## 输出示例

```json
{
  "name": "ecommerce",
  "tables": [
    {
      "name": "customers",
      "columns": [
        {
          "name": "id",
          "type": "UInt64",
          "stats": {
            "row_count": 1000,
            "distinct_count": 1000
          },
          "inferences": {
            "is_primary_key": true,
            "is_complete": true,
            "cardinality": 1.0,
            "enum_type": "high_cardinality"
          }
        },
        {
          "name": "country",
          "type": "String",
          "stats": {
            "row_count": 1000,
            "null_count": 50,
            "distinct_count": 7,
            "top_values": [
              {"value": "US", "count": 400}
            ]
          },
          "inferences": {
            "is_enum": true,
            "enum_type": "enum",
            "cardinality": 0.007,
            "has_nulls": true,
            "distribution": [
              {"value": "US", "count": 400, "percent": 40.0}
            ]
          }
        }
      ]
    },
    {
      "name": "orders",
      "columns": [
        {
          "name": "customer_id",
          "type": "UInt64",
          "stats": {
            "row_count": 5000,
            "distinct_count": 800,
            "min": 1,
            "max": 1000
          },
          "inferences": {
            "is_complete": true,
            "cardinality": 0.16,
            "enum_type": "high_cardinality"
          }
        }
      ]
    }
  ],
  "inferences": {
    "foreign_keys": [
      {
        "source_table": "orders",
        "source_column": "customer_id",
        "target_table": "customers",
        "target_column": "id",
        "confidence": 1.0
      }
    ],
    "business_insights": [
      {
        "type": "avg_per_entity",
        "description": "Average orders per customer",
        "value": 5.0,
        "tables": ["orders", "customers"]
      },
      {
        "type": "entity_coverage",
        "description": "Customer activity rate",
        "value": 0.8,
        "tables": ["orders", "customers"]
      }
    ]
  }
}
```
