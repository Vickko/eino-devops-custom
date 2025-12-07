# DevOps API Documentation

This document describes every HTTP and Server-Sent Events (SSE) endpoint exposed under `/eino/devops`. It is intended for frontend clients that interact with the local DevOps server when building and debugging graphs.

## Overview
- **Base URL:** `http://<host>:<port>/eino/devops`
- **Version endpoint value:** `0.1.7`
- **Transport:** All endpoints use JSON over HTTP except `/stream_log` and `/debug/v1/graphs/{graph_id}/threads/{thread_id}/stream`, which emit SSE responses (`text/event-stream`).
- **SSE connection guard:** The server limits all concurrent SSE streams to **10 total** (shared by log and debug streams). Requests above the limit receive a `400` response.

## Response & Error Contract
### HTTPResp & BaseResp
Every non-SSE HTTP response shares the following envelope (defined in `server.go`).

| Field | Type | Description |
|-------|------|-------------|
| `code` | `int` | Stored inside `BaseResp`. `0` means success. Non-zero values mirror HTTP status codes such as `400` or `500`. |
| `msg` | `string` | Human-readable description. Defaults to `"success"` when `code=0`, otherwise an HTTP status text or validation error message. |
| `data` | `any` | Payload that varies per endpoint. On failure it may contain a `BizError`. |

### BizError
Business errors are returned inside `data` while the envelope’s `code` is set to an HTTP error. Structure comes from `server.go`.

| Field | Type | Description |
|-------|------|-------------|
| `biz_code` | `int` | Application-specific error classification. Currently mirrors the HTTP code but can be extended. |
| `biz_msg` | `string` | Detailed failure message so clients can display or log it. |

### Error Handling Rules
- Validation failures (missing path/body parameters, wrong formats) return `code = 400` with a `BizError` message.
- Runtime or service failures (graph execution issues, container errors) return `code = 500` (or another matching status) with a `BizError`.
- Even when `code` is non-zero, the HTTP status line itself remains `200`. Always inspect the envelope to determine success.

### SSE Transport Shape (SSEResponse)
SSE payloads (defined in `server.go`) are emitted as:

| Field | Type | Description |
|-------|------|-------------|
| `eventType` | `string` | SSE `event:` value written to the stream. For debug streams this is one of `data`, `finish`, `error`. For log streams it is the log level (`INFO`, `WARN`, `ERROR`). |
| `data` | `string` | SSE `data:` value. For debug streams it is a JSON stringified `DebugRunEventMsg`. For log streams it is a plain log line. |

SSE frames are written as:

```
event: <eventType>
data: <data literal>

```

Additional notes:
- Headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`.
- The server throttles writes by 50 ms to keep clients responsive.
- Clients **must** close the HTTP connection when they are done so the SSE slot can be reused.

## Domain Data Models
### GraphMeta (common.go)
| Field | Type | Description |
|-------|------|-------------|
| `id` | `string` | Unique ID of the graph inside the DevOps container. |
| `name` | `string` | Human-readable graph name (as registered in the compose pipeline). |

### CanvasInfo & GraphSchema (`model/canvas.go`)
`CanvasInfo` wraps graph metadata that the IDE uses to render the canvas.

| Field | Type | Description |
|-------|------|-------------|
| `version` | `string` | Canvas schema version. Currently `1.0.0`. |
| `id` | `string` | Graph schema ID. |
| `name` | `string` | Graph name. |
| `component` | `string` | Top-level component type (value from `components.Component`). |
| `nodes` | `Node[]` | Collection of nodes; see below. |
| `edges` | `Edge[]` | Connections between nodes; see below. |
| `branches` | `Branch[]` | Conditional routing; see below. |
| `node_trigger_mode` | `string` | Either `AnyPredecessor` or `AllPredecessor`, describing how nodes fire. |
| `gen_local_state` | `GenLocalState` | Optional state generation config. |
| `input_type` | `JsonSchema` | Input schema for the graph. |
| `output_type` | `JsonSchema` | Output schema for the graph. |

`GenLocalState` contains:
- `is_set` (`bool`): whether local state is configured.
- `output_type` (`JsonSchema`): schema of the generated local state.

### Node (`model/canvas.go`)
| Field | Type | Description |
|-------|------|-------------|
| `key` | `string` | Unique key for referencing the node during debug runs (`from_node`). |
| `name` | `string` | Display name. |
| `type` | `string` | One of `start`, `end`, `branch`, `parallel`. |
| `component_schema` | `ComponentSchema` | Describes the component instance attached to this node. |
| `graph_schema` | `GraphSchema` | Nested graph when the node encapsulates a sub-graph. |
| `node_option` | `NodeOption` | Execution options (see below). |
| `allow_operate` | `bool` | Whether the node can be modified in the UI. |
| `extra` | `object` | Arbitrary metadata the IDE stores. |

### NodeOption
| Field | Type | Description |
|-------|------|-------------|
| `input_key` | `string` | Overrides the key used to retrieve inputs from context. Optional. |
| `output_key` | `string` | Overrides the key used to place outputs into context. Optional. |
| `used_state_pre_handler` | `bool` | Indicates a pre-handler consumes local state. Default `false`. |
| `used_state_post_handler` | `bool` | Indicates a post-handler emits local state. Default `false`. |

### Edge & Branch
| Structure | Fields |
|-----------|--------|
| **Edge** | `id` (`string`), `name` (`string`), `source_node_key` (`string`), `target_node_key` (`string`), `extra` (`object`). |
| **Branch** | `id` (`string`), `condition` (`Condition`), `source_node_key` (`string`), `target_node_keys` (`string[]`), `extra` (`object`). |
| **Condition** | `method` (`string`), `is_stream` (`bool`), `input_type` (`JsonSchema`). |

### ComponentSchema & Related Structures
| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | Display name of the component. |
| `component` | `string` | Component type identifier (ChatModel, Lambda, etc.). |
| `component_source` | `string` | Either `official` or `custom`. |
| `identifier` | `string` | Implementation reference (e.g., `eino-ext/model/ark`). |
| `input_type` / `output_type` | `JsonSchema` | Component IO schemas. |
| `method` | `string` | Generated function name when instantiating official components. |
| `slots` | `Slot[]` | Pluggable positions for other components. |
| `config` | `ConfigSchema` | Component configuration schema. |
| `extra_property` | `ExtraPropertySchema` | Optional additional properties. |
| `is_io_type_mutable` | `bool` | Whether IO schemas can be edited by users. |
| `version` | `string` | Component definition version. |

`ConfigSchema` holds `description` (string), `schema` (`JsonSchema`), and `config_input` (string representing the serialized config). `ExtraPropertySchema` mirrors that with `extra_property_input`.

`Slot` fields: `component` (string), `field_loc_path` (string path like `Config.Model`), `multiple` (bool), `required` (bool), `component_items` (`ComponentSchema[]`), `go_definition` (`GoDefinition`).

### JsonSchema and Supporting Types
`JsonSchema` mirrors standard JSON Schema concepts.

| Field | Type | Description |
|-------|------|-------------|
| `type` | `string` | One of `boolean`, `string`, `number`, `object`, `array`, `null`, `interface`. |
| `title` | `string` | Friendly name. |
| `description` | `string` | Field description. |
| `items` | `JsonSchema` | Schema of array items. |
| `properties` | `map<string, JsonSchema>` | Object properties. |
| `anyOf` | `JsonSchema[]` | Union schemas. |
| `additionalProperties` | `JsonSchema` | Schema for dynamic object keys. |
| `required` | `string[]` | Required property names. |
| `enum` | `any[]` | Enumerated options. |
| `propertyOrder` | `string[]` | UI display order. |
| `goDefinition` | `GoDefinition` | Reflection metadata. |

`GoDefinition` exposes `libraryRef` (`Library` with `version`, `module`, `pkgPath`), `typeName`, `kind`, and `isPtr`.

### DebugRunRequest (`types/debug.go`)
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `from_node` | `string` | Yes | Node key where execution should start. Must exist in the graph. |
| `input` | `string` | Yes | JSON-marshaled mock input passed to the node/graph. It must be a valid JSON string literal. |
| `log_id` | `string` | No | Optional correlation ID propagated to logs (`K_LOGID`). |

### DebugRunEventMsg, NodeDebugState, NodeDebugMetrics
| Field | Type | Description |
|-------|------|-------------|
| `type` | `string` | `data`, `finish`, or `error`. Matches SSE event type. |
| `debug_id` | `string` | Internal run identifier generated by the debug service. |
| `error` | `string` | Present when `type=error`. Plain-text reason. |
| `content` | `NodeDebugState` | Present when `type=data`. Captures node execution details. |

`NodeDebugState` fields: `node_key`, `input` (JSON string), `output` (JSON string), `error`, `error_type` (either `NodeError` or `SystemError`), and `metrics` (`NodeDebugMetrics`).

`NodeDebugMetrics` fields: `prompt_tokens`, `completion_tokens`, `invoke_time_ms`, `completion_time_ms`.

### SSEResponse (explicit definition)
While already described in the SSE section, the struct fields are:
- `eventType` (`string`): passed verbatim as the SSE event name.
- `data` (`string`): payload string written after `data:`.

## API Reference
Each endpoint description below includes method, path, parameters, sample requests, responses, and any SSE specifics.

### GET /eino/devops/ping
**Description:** Health probe that returns `"pong"` to verify the DevOps server is reachable.

**Request Parameters:** None.

**Sample Request:**
```shell
curl -X GET http://localhost:8080/eino/devops/ping
```

**Success Response:**
```json
{
  "code": 0,
  "msg": "success",
  "data": "pong"
}
```

**Failure Response:** Uses the standard `BizError` envelope (very rare).

### GET /eino/devops/version
**Description:** Returns the DevOps module version (`types.Version`).

**Sample Request:**
```shell
curl -X GET http://localhost:8080/eino/devops/version
```

**Success Response:**
```json
{
  "code": 0,
  "msg": "success",
  "data": "0.1.7"
}
```

### GET /eino/devops/stream_log
**Description:** Streams DevOps log messages in real time via SSE. Every log entry generated by `devops/internal/utils/log` is forwarded.

**Special Behavior:**
- SSE headers are set automatically.
- Event types are log levels: `INFO`, `WARN`, `ERROR`.
- Data lines contain the prefixed log line (e.g., `[INFO] 2024/06/01 12:00:00.123 message`).
- Occupies one of the 10 SSE slots until the client disconnects.

**Request Parameters:** None.

**Sample Request:**
```shell
curl -N http://localhost:8080/eino/devops/stream_log
```

**SSE Event Example:**
```
event: INFO
data: [INFO] 2024/06/01 12:00:00.123 node start -> preparing graph demo

```

**Error Handling:**
- When the SSE limit is exceeded, the server responds with:
```json
{
  "code": 400,
  "msg": "",
  "data": {
    "biz_code": 400,
    "biz_msg": "too many connections"
  }
}
```
- Network issues simply close the SSE stream.

### GET /eino/devops/debug/v1/input_types
**Description:** Lists all registered input types as JSON Schemas. These schemas are built from Go types registered via `model.RegisterType` and are used to render IDE forms.

**Request Parameters:** None.

**Success Response Structure:**
`data.types` is an array of `JsonSchema` objects.

**Sample Response:**
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "types": [
      {
        "title": "schema.Message",
        "description": "",
        "type": "object",
        "properties": {
          "role": {"type": "string", "enum": ["user", "assistant"]},
          "content": {"type": "string", "description": "message text"}
        },
        "required": ["role", "content"],
        "goDefinition": {
          "typeName": "schema.Message",
          "kind": "struct",
          "isPtr": false,
          "libraryRef": {
            "module": "github.com/cloudwego/eino",
            "pkgPath": "github.com/cloudwego/eino/schema",
            "version": "latest"
          }
        }
      }
    ]
  }
}
```

**Failure Response:** Standard `BizError` envelope if schema generation fails.

### GET /eino/devops/debug/v1/graphs
**Description:** Lists all graphs known to the DevOps container.

**Request Parameters:** None.

**Success Response:**
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "graphs": [
      {"id": "graph_demo", "name": "demo"},
      {"id": "workflow_support", "name": "Support Workflow"}
    ]
  }
}
```

**Failure Response:** Returns `400` if internal discovery fails (unlikely) with a `BizError`.

### GET /eino/devops/debug/v1/graphs/{graph_id}/canvas
**Description:** Fetches (or lazily creates) the canvas definition for a specific graph.

**Path Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `graph_id` | `string` | Yes | Graph identifier returned by `/graphs`. |

**Success Response:** `data.canvas_info` equals `CanvasInfo`.

**Sample Response:**
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "canvas_info": {
      "version": "1.0.0",
      "id": "graph_demo",
      "name": "Demo",
      "component": "Workflow",
      "nodes": [
        {
          "key": "start",
          "name": "Start",
          "type": "start",
          "allow_operate": true,
          "component_schema": {
            "name": "Prompt",
            "component": "Lambda",
            "component_source": "official",
            "identifier": "eino/prompt",
            "output_type": {"type": "string"},
            "input_type": {"type": "string"},
            "config": {
              "description": "Prompt text",
              "schema": {"type": "object"},
              "config_input": "{\"prompt\":\"Hello\"}"
            }
          }
        }
      ],
      "edges": [
        {"id": "edge_1", "source_node_key": "start", "target_node_key": "end"}
      ],
      "branches": [],
      "node_trigger_mode": "AnyPredecessor",
      "input_type": {
        "type": "object",
        "properties": {"messages": {"type": "array"}}
      },
      "output_type": {"type": "string"}
    }
  }
}
```

**Failure Responses:**
- `400` when `graph_id` is missing or the canvas cannot be created.
- Payload contains `BizError` with the original service error message.

### POST /eino/devops/debug/v1/graphs/{graph_id}/threads
**Description:** Creates a debug thread for a specific graph. Threads partition concurrent debugging sessions.

**Path Parameters:** same as above (`graph_id` required).

**Request Body:** None.

**Success Response:**
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "thread_id": "thread-7f4a98b4"
  }
}
```

**Failure Responses:**
- `400` when `graph_id` is not provided.
- `500` when the debug service cannot provision the thread. Example:
```json
{
  "code": 500,
  "msg": "Internal Server Error",
  "data": {
    "biz_code": 500,
    "biz_msg": "failed to allocate debug thread"
  }
}
```

### POST /eino/devops/debug/v1/graphs/{graph_id}/threads/{thread_id}/stream
**Description:** Runs a mock debug execution starting from a chosen node and streams node states via SSE. This is the primary interface used by the IDE to step through a graph.

**Path Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `graph_id` | `string` | Yes | Graph ID the thread belongs to. |
| `thread_id` | `string` | Yes | Thread returned by the previous endpoint. |

**Request Body:** JSON `DebugRunRequest`.

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `from_node` | `string` | Yes | Must match `Node.key` in the canvas. Validation rejects empty strings. |
| `input` | `string` | Yes | JSON string containing the user input. Should align with the graph’s `input_type`. |
| `log_id` | `string` | No | Optional correlation ID that will appear in backend logs. |

**Sample Request:**
```shell
curl -N \
  -H "Content-Type: application/json" \
  -X POST \
  http://localhost:8080/eino/devops/debug/v1/graphs/graph_demo/threads/thread-7f4a98b4/stream \
  -d '{
    "from_node": "start",
    "input": "{\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}]}",
    "log_id": "cli-20240601-0001"
  }'
```

**SSE Event Types & Payloads:**
- `data`: `data` field contains a JSON `DebugRunEventMsg` with `content` populated.
- `finish`: indicates the state channel closed cleanly. `content` is omitted.
- `error`: includes the `error` message. Emitted for validation failures discovered after the stream starts or runtime errors.

**SSE Example Sequence:**
```
event: data
data: {"type":"data","debug_id":"dbg-123","content":{"node_key":"start","input":"{...}","output":"{\"text\":\"hi\"}","metrics":{"prompt_tokens":12,"invoke_time_ms":80}}}

event: data
data: {"type":"data","debug_id":"dbg-123","content":{"node_key":"call_llm","error":"timeout","error_type":"NodeError"}}

event: finish
data: {"type":"finish","debug_id":"dbg-123"}
```

**Failure Before Streaming Starts:**
- Missing `graph_id`, `thread_id`, or `from_node` causes an immediate JSON error response with `code=400`.
- Any other setup issue yields `code=500` with `BizError`.

**Runtime Errors After Stream Starts:**
- Errors are sent as SSE `error` events:
```
event: error
data: {"type":"error","debug_id":"dbg-123","error":"invalid input payload"}
```

## Additional Notes for Clients
- Always inspect `code` in the JSON envelope before relying on `data`.
- SSE clients should automatically reconnect on network failures but respect the 10-connection ceiling.
- When sending `DebugRunRequest.input`, prefer compact JSON strings to avoid escaping mistakes.
- Node keys, graph IDs, and thread IDs are case-sensitive.
- Log streams and debug streams share the same SSE limiter; releasing log streams after consumption helps free capacity for debugging sessions.
