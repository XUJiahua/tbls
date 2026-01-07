# 移除 docPath 和文件生成功能设计

## 目标

将 tbls 从文件生成工具转变为纯数据 API，前端根据 JSON 数据自行渲染文档和 ER 图。

## 保留的核心功能

### 命令行接口

```
tbls serve              # API 服务器（主要模式）
tbls out [DSN]          # 输出 JSON/YAML 到 stdout
tbls scaffold [DSN]     # 生成配置文件
tbls ls [DSN]           # 列出表名
tbls coverage [DSN]     # 覆盖率检查
tbls version            # 版本信息
tbls completion         # Shell 补全
```

### 输出格式（`tbls out -t`）

- `json`（默认）
- `yaml`
- `config`（生成配置）

### 移除的命令

- `tbls doc` — 文件生成
- `tbls diff` — 文档对比（依赖 md 输出）

### 移除的输出格式

- `md`, `dot`, `svg`, `png`, `jpg`, `plantuml`, `mermaid`, `xlsx`

## 配置结构精简

### 移除的配置字段

```yaml
docPath: dbdoc
format:
  adjust: false
  sort: false
  number: false
  showOnlyFirstParagraph: false
  hideColumnsWithoutValues: []
er:
  skip: false
  format: svg
  comment: false
  hideDef: false
  distance: 1
  font: ""
templates:
  md: {}
dict: {}
baseUrl: ""
```

### 保留的配置字段

```yaml
dsn: clickhouse://...
name: ""
desc: ""
labels: []
include: []
exclude: []
requiredVersion: ""
detectVirtualRelations:
  enabled: false
  strategy: ""
stats:
  enabled: true
  # ... (保持不变)
```

## 文件删除清单

### 删除的目录/文件

```
cmd/doc.go
cmd/diff.go
output/md/
output/gviz/
output/dot/
output/plantuml/
output/mermaid/
output/xlsx/
sample/
testdata/*md*.golden
testdata/*dot*.golden
testdata/*plantuml*.golden
testdata/*mermaid*.golden
testdata/templates/
```

### 保留的目录/文件

```
cmd/out.go (简化)
cmd/serve.go (简化)
cmd/scaffold.go (简化)
cmd/ls.go
cmd/coverage.go
cmd/root.go (简化)
output/json/
output/yaml/
output/config/ (简化)
testdata/json_output*.golden
testdata/yaml_output*.golden
```

## 实现步骤

### 阶段 1：删除命令和输出包

1. 删除 `cmd/doc.go`
2. 删除 `cmd/diff.go`
3. 修改 `cmd/root.go` — 移除 doc/diff 命令注册
4. 删除输出包目录
5. 修改 `cmd/out.go` — 只保留 json/yaml/config 格式

### 阶段 2：精简配置

1. 修改 `config/config.go` — 移除字段
2. 修改 `output/config/config.go` — 适配新结构
3. 更新 `cmd/scaffold.go`
4. 更新 `cmd/serve.go` 和 `cmd/serve_types.go`

### 阶段 3：清理测试和示例

1. 删除 `sample/` 目录
2. 删除相关 golden 文件
3. 更新测试文件

### 阶段 4：验证

1. `make test-no-db`
2. `make build`
3. 手动测试命令
