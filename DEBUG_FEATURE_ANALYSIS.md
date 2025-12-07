# DevOps Debug 功能分析与改进建议

本文档记录 eino-ext devops server debug 功能的现状分析和潜在改进方向。

## 问题 1: Debug 时能否传入参数设置 Chat Model 节点的模型配置

### 现状

**结论：不支持**

当前 `DebugRunRequest` 结构体（`devops/internal/apihandler/types/debug.go:34-38`）只接收以下参数：

```go
type DebugRunRequest struct {
    FromNode string `json:"from_node"`  // 起始节点
    Input    string `json:"input"`      // 输入数据（JSON 字符串）
    LogID    string `json:"log_id"`     // 日志 ID（可选）
}
```

在 `getInvokeOptions` 方法（`devops/internal/service/debug_run.go:144-151`）中，只为每个节点创建了 callback 选项用于状态收集：

```go
func (d *debugServiceImpl) getInvokeOptions(gi *model.GraphInfo, threadID string, stateCh chan *model.NodeDebugState) (opts []compose.Option, err error) {
    opts = make([]compose.Option, 0, len(gi.Nodes))
    for key, node := range gi.Nodes {
        opts = append(opts, newCallbackOption(key, threadID, node, stateCh))
    }
    return opts, nil
}
```

这些选项仅用于监控，**不涉及节点配置的动态覆盖**。

### 改进方案

#### 方案 A: 扩展 DebugRunRequest 结构（通用配置）

```go
type DebugRunRequest struct {
    FromNode    string                    `json:"from_node"`
    Input       string                    `json:"input"`
    LogID       string                    `json:"log_id"`
    NodeConfigs map[string]map[string]any `json:"node_configs,omitempty"` // 节点配置覆盖
}
```

然后在 `getInvokeOptions` 中添加配置选项：

```go
func (d *debugServiceImpl) getInvokeOptions(gi *model.GraphInfo, threadID string, stateCh chan *model.NodeDebugState, nodeConfigs map[string]map[string]any) (opts []compose.Option, err error) {
    opts = make([]compose.Option, 0, len(gi.Nodes))
    for key, node := range gi.Nodes {
        opts = append(opts, newCallbackOption(key, threadID, node, stateCh))

        // 如果该节点有配置覆盖，添加配置选项
        if config, ok := nodeConfigs[key]; ok {
            opts = append(opts, compose.WithConfig(config).DesignateNode(key))
        }
    }
    return opts, nil
}
```

#### 方案 B: 专门的模型配置字段

如果只针对 chat model 节点：

```go
type DebugRunRequest struct {
    FromNode       string                 `json:"from_node"`
    Input          string                 `json:"input"`
    LogID          string                 `json:"log_id"`
    ModelOverrides map[string]ModelConfig `json:"model_overrides,omitempty"`
}

type ModelConfig struct {
    Model       string   `json:"model,omitempty"`
    Temperature *float64 `json:"temperature,omitempty"`
    MaxTokens   *int     `json:"max_tokens,omitempty"`
    TopP        *float64 `json:"top_p,omitempty"`
    // 其他模型参数...
}
```

### 涉及的代码变更

| 文件 | 变更内容 |
|------|---------|
| `internal/apihandler/types/debug.go` | 扩展 `DebugRunRequest` 结构 |
| `internal/service/debug_run.go` | 修改 `DebugRun` 和 `getInvokeOptions` 方法 |
| `internal/apihandler/debug.go` | 修改 `StreamDebugRun` 传递新参数 |

---

## 问题 2: Debug 时是否支持断点和单步执行

### 现状

**结论：不支持**

当前的 debug 实现是 **Run-to-Completion（运行至完成）** 模式。

核心执行逻辑（`devops/internal/service/debug_run.go:122-139`）：

```go
safego.Go(ctx, func() {
    defer close(stateCh)
    defer close(errCh)

    r, e := devGraph.Compile()
    if e != nil {
        errCh <- e
        return
    }

    _, e = r.Invoke(ctx, input, opts...)  // 一次性执行到完成
    if e != nil {
        errCh <- e
        return
    }
})
```

### 当前支持的功能

| 功能 | 支持状态 |
|------|---------|
| 选择起始节点（`from_node`） | ✅ |
| 实时查看节点执行状态（SSE 流） | ✅ |
| 查看输入、输出、耗时、token 指标 | ✅ |
| 在某个节点暂停 | ❌ |
| 单步执行（点击下一步才继续） | ❌ |
| 设置断点 | ❌ |

### 改进方案

#### 方案 A: 节点级断点控制（推荐）

**新增请求字段**：

```go
type DebugRunRequest struct {
    FromNode     string   `json:"from_node"`
    Input        string   `json:"input"`
    LogID        string   `json:"log_id"`
    BreakAtNodes []string `json:"break_at_nodes,omitempty"` // 断点节点列表
    StepMode     bool     `json:"step_mode,omitempty"`      // 单步模式
}
```

**新增 API 端点**：

```
POST /eino/devops/debug/v1/graphs/{graph_id}/threads/{thread_id}/continue
POST /eino/devops/debug/v1/graphs/{graph_id}/threads/{thread_id}/step
POST /eino/devops/debug/v1/graphs/{graph_id}/threads/{thread_id}/stop
```

**服务层改造**：

```go
type DebugService interface {
    CreateDebugThread(ctx context.Context, graphID string) (threadID string, err error)
    DebugRun(ctx context.Context, m *model.DebugRunMeta, userInput string) (debugID string, stateCh chan *model.NodeDebugState, errCh chan error, err error)

    // 新增方法
    Continue(ctx context.Context, graphID, threadID, debugID string) error
    Step(ctx context.Context, graphID, threadID, debugID string) error
    Stop(ctx context.Context, graphID, threadID, debugID string) error
}
```

#### 方案 B: Callback 控制机制

在 callback handler 中加入暂停控制：

```go
type callbackHandler struct {
    nodeKey   string
    stateCh   chan *model.NodeDebugState
    threadID  string
    node      compose.GraphNodeInfo

    // 新增控制字段
    breakpoints map[string]bool    // 断点节点
    stepMode    bool               // 单步模式
    pauseChan   chan struct{}      // 暂停信号
    resumeChan  chan struct{}      // 恢复信号
}

func (c *callbackHandler) OnStart(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
    // 检查是否需要暂停
    if c.shouldPause() {
        // 发送暂停事件到客户端
        c.stateCh <- &model.NodeDebugState{
            NodeKey: c.nodeKey,
            Status:  "paused",
        }

        // 阻塞等待恢复信号
        select {
        case <-c.resumeChan:
            // 继续执行
        case <-ctx.Done():
            return ctx
        }
    }

    // ... 原有逻辑
}

func (c *callbackHandler) shouldPause() bool {
    return c.stepMode || c.breakpoints[c.nodeKey]
}
```

#### 方案 C: 逐节点执行（最简单但功能受限）

不修改 eino 核心执行逻辑，而是：

1. 将图拆解为节点执行序列
2. 每次只执行一个节点
3. 返回结果后等待用户指令

```go
func (d *debugServiceImpl) StepDebugRun(ctx context.Context, rm *model.DebugRunMeta, currentNode string, input string) (nextNode string, output *model.NodeDebugState, err error) {
    // 只执行单个节点
    // 返回下一个待执行节点
}
```

### 涉及的代码变更

| 文件 | 变更内容 |
|------|---------|
| `internal/apihandler/types/debug.go` | 新增请求/响应类型 |
| `internal/service/debug_run.go` | 新增断点控制逻辑 |
| `internal/service/call_option.go` | 修改 callback handler |
| `internal/apihandler/debug.go` | 新增 Continue/Step/Stop handler |
| `internal/apihandler/server.go` | 注册新路由 |
| `internal/model/debug_run.go` | 新增调试会话状态管理 |

### 实现复杂度对比

| 方案 | 复杂度 | 优点 | 缺点 |
|------|--------|------|------|
| A: 节点级断点 | 高 | 功能完整，用户体验好 | 需要状态管理，并发处理复杂 |
| B: Callback 控制 | 中 | 侵入性小，与现有架构契合 | 需要处理超时和取消 |
| C: 逐节点执行 | 低 | 实现简单 | 无法处理并发节点、分支逻辑受限 |

---

## 附录：当前 Debug API 概览

| 端点 | 方法 | 描述 |
|------|------|------|
| `/debug/v1/graphs` | GET | 列出所有图 |
| `/debug/v1/graphs/{graph_id}/canvas` | GET | 获取图的画布信息 |
| `/debug/v1/graphs/{graph_id}/threads` | POST | 创建调试线程 |
| `/debug/v1/graphs/{graph_id}/threads/{thread_id}/stream` | POST | 流式执行调试（SSE） |
| `/debug/v1/input_types` | GET | 列出已注册的输入类型 |

---

## 后续行动

- [ ] 确定优先级：模型配置覆盖 vs 断点功能
- [ ] 评估 eino 核心库是否需要配合修改
- [ ] 设计详细的 API 规范
- [ ] 实现并测试
