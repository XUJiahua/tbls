# Stats Inference - 基于统计信息的元数据推理

本文档描述如何利用 tbls 收集的列级统计信息进行元数据推理和数据洞察。

## 配置 Stats 收集

在 `.tbls.yml` 中启用统计信息收集：

```yaml
dsn: clickhouse+http://user:password@host:port/database

stats:
  enabled: true
  topN: 20           # 收集前 N 个枚举值
  sampleSize: 10000  # 采样大小
```

生成带统计信息的 JSON：

```bash
./tbls out -c .tbls.yml -t json > metadata.json
```

## 统计信息字段说明

### 列级统计 (Column Stats)

| 字段 | 说明 |
|------|------|
| `row_count` | 总行数 |
| `null_count` | 空值数量 |
| `null_percent` | 空值百分比 |
| `distinct_count` | 唯一值数量 |
| `min` | 最小值（数值类型） |
| `max` | 最大值（数值类型） |
| `avg` | 平均值（数值类型） |
| `top_values` | 前 N 个高频值及其计数 |

### 表级统计 (Table Stats)

| 字段 | 说明 |
|------|------|
| `row_count` | 表总行数 |
| `data_bytes` | 数据占用字节数 |

## 推理能力

### 1. 主键/唯一标识符识别

**规则**：当 `distinct_count == row_count` 时，该列可能是主键或唯一约束列。

```
判断条件: distinct_count / row_count = 100%
```

**示例**：
| 列 | distinct_count | row_count | 推断 |
|---|---|---|---|
| `customers.customer_id` | 10 | 10 | 主键 (100% 唯一) |
| `customers.email` | 10 | 10 | 候选键 (可作为唯一约束) |

### 2. 外键关系推断

**规则**：当两列具有相同的值域范围和相似的 distinct_count 时，可能存在外键关系。

```
判断条件:
  - 列名相似（如 xxx_id）
  - min/max 值域匹配
  - distinct_count 匹配或为子集
```

**示例**：
| 列 | 值范围 | distinct | 关联表 |
|---|---|---|---|
| `orders.customer_id` | 1-10 | 10 | → `customers.customer_id` |
| `orders.product_id` | 1-10 | 10 | → `products.product_id` |

### 3. 枚举/字典列识别

**规则**：低基数列（distinct_count 远小于 row_count）适合作为枚举类型或字典表。

```
判断条件: distinct_count / row_count < 5%（可调整阈值）
```

**分类**：

| 基数范围 | 建议 |
|----------|------|
| 2-5 | 枚举类型 (Enum) |
| 5-20 | 字典表候选 |
| 20-100 | 考虑索引优化 |
| >100 | 高基数，正常列 |

**示例**：
| 列 | distinct | row_count | 类型建议 |
|---|---|---|---|
| `orders.status` | 3 | 20 | 枚举 (15%) |
| `products.category` | 3 | 10 | 枚举 (30%) |
| `customers.country` | 7 | 10 | 字典表 (70%) |

### 4. 数据分布分析

通过 `top_values` 分析数据分布：

**订单状态分布**：
```
completed: 15 (75%) - 大多数订单已完成
shipped:    3 (15%) - 配送中
pending:    2 (10%) - 待处理
```

**产品类别分布**：
```
Electronics: 6 (60%) - 电子产品为主
Shoes:       2 (20%)
Clothing:    2 (20%)
```

### 5. 数据质量检测

| 检测项 | 规则 | 说明 |
|--------|------|------|
| 空值检测 | `null_percent > 0` | 识别可空列 |
| 完整性 | `null_percent = 0` | 非空约束验证 |
| 重复检测 | `distinct_count < row_count` 且预期唯一 | 主键重复 |
| 范围检测 | `min/max` 超出预期范围 | 异常值检测 |

### 6. 索引优化建议

基于统计信息生成索引建议：

```
低基数 + 高频过滤 → 位图索引
高基数 + 范围查询 → B-Tree 索引
外键列 → 常规索引（JOIN 优化）
```

**示例建议**：
```sql
-- 低基数列，适合过滤
CREATE INDEX idx_orders_status ON orders(status);

-- 外键查询优化
CREATE INDEX idx_orders_customer ON orders(customer_id);
CREATE INDEX idx_orders_product ON orders(product_id);

-- 分类筛选
CREATE INDEX idx_products_category ON products(category);
```

### 7. 业务洞察

从统计信息推断业务特征：

| 指标 | 计算方式 | 示例 |
|------|----------|------|
| 平均订单数 | orders.row_count / customers.row_count | 2 单/客户 |
| 客单价 | orders.total_amount.avg | 808.5 |
| 产品活跃度 | orders.product_id.distinct / products.row_count | 100% |
| 客户活跃度 | orders.customer_id.distinct / customers.row_count | 100% |

## 实际应用场景

### Schema 逆向工程

当数据库缺少约束定义时，利用统计信息推断：
- 主键约束
- 外键关系
- 非空约束
- 唯一约束

### 数据建模优化

- 识别适合分区的列（日期、状态等）
- 发现适合字典编码的列
- 优化存储格式

### 数据治理

- 数据完整性检查
- 异常值检测
- 数据分布监控

## 命令参考

```bash
# 生成带统计信息的 JSON
./tbls out -c .tbls.yml -t json > schema.json

# 生成带统计信息的文档
./tbls doc -c .tbls.yml

# 查看表列表
./tbls ls -c .tbls.yml
```
