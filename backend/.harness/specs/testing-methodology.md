---
last_updated: 2026-07-31
status: active
owner: backend-team
---

# 测试与调试方法学

## 概述

本文档定义测试与调试的通用方法学规范，适用于任何前后端分离的 Web 项目。项目特定数据（账户、端点、脚本路径等）见 [e2e-test-plan.md](e2e-test-plan.md)。

核心原则：
- **测试验证正确性**：从静态分析到全链路 E2E，四层互补
- **调试定位问题**：从日志到命令行到网络抓包，工具链闭环
- **测试与调试不可分**：测试发现问题 → 调试定位根因 → 修复 → 回归验证

## 测试金字塔（四层策略）

```
        ╱╲
       ╱ L4 ╲        Playwright 全链路 E2E（少量，高价值）
      ╱──────╲       真实浏览器 + 真实后端 + 真实 DB
     ╱   L3   ╲     API 契约 + 深度场景（中量）
    ╱──────────╲    curl / supertest / Go integration test
   ╱     L2     ╲  单元 + 组件测试（大量）
  ╱──────────────╲ Go test + mock / Vue 组件测试
 ╱       L1       ╲ 静态分析 + 编译检查
╱──────────────────╲ go vet / eslint / stylelint / tsc
```

### 各层职责与边界

| 层级 | 验证什么 | 不验证什么 |
|------|---------|-----------|
| L1 静态 | 类型安全、代码规范、架构约束 | 运行时行为 |
| L2 单元 | 纯函数、服务编排逻辑、边界条件 | 真实网络、真实浏览器 |
| L3 API | 接口契约、状态码、SSE 事件序列、DB 状态 | 前端是否正确消费响应 |
| L4 E2E | 用户视角完整链路、UI 状态、跨端联动 | 内部实现细节 |

### 层间互补原则

- L3 证明"后端说了正确的话"
- L4 证明"前端听懂了并正确展示了"
- 两层缺一不可：API 通过 ≠ 用户可用

---

## 调试工具箱

测试发现问题后，需要系统化的调试手段定位根因。以下按场景分类，从轻量到重量排列。

### 1. 日志诊断

日志是生产环境和本地调试的第一道防线。

#### 日志级别与使用场景

| 级别 | 何时用 | 典型场景 |
|------|-------|---------|
| `Debug` | 开发调试、临时排查 | 请求参数、中间状态、分支走向 |
| `Info` | 关键业务事件 | 登录成功、文章发布、配置变更 |
| `Warn` | 预期内的失败 | 登录失败、限流触发、降级路径 |
| `Error` | 非预期错误 | DB/Redis 故障、panic recovered |

> 业务校验失败（401/403/404/422）用 `Warn`，不是 `Error`——它们是预期内的客户端错误，不是系统故障。

#### 日志调试技巧

| 技巧 | 说明 | 示例 |
|------|------|------|
| **request_id 追踪** | 每个请求自动注入唯一 ID，grep 即可追踪完整生命周期 | `grep "a1b2c3d4" logs/app.log` |
| **context 传递** | 使用 `slog.XxxContext(ctx, ...)` 让 request_id/user_id 自动注入 | `slog.InfoContext(ctx, "event", "key", val)` |
| **临时 Debug 日志** | 排查时添加 `slog.Debug`，通过环境变量 `LOG_LEVEL=debug` 开启，修完即删 | `slog.DebugContext(ctx, "rewrite result", "query", rewritten)` |
| **结构化字段** | 用键值对而非字符串拼接，便于 jq/grep 过滤 | `slog.Info("login", "user_id", id)` 而非 `slog.Info("login user " + id)` |
| **PII 保护** | 医疗/金融等敏感场景：不记录用户名明文、消息内容、密码/token、完整 API Key | 记 `user_id` 不记 `username`；记 `api_key_masked` 不记原始 key |

#### 日志查看命令

```bash
# 实时跟踪
tail -f logs/app.log

# 按 request_id 过滤完整请求链路
cat logs/app.log | jq 'select(.request_id=="abc123")'

# 按级别过滤
cat logs/app.log | jq 'select(.level=="ERROR")'

# 按时间窗口过滤
cat logs/app.log | jq 'select(.time >= "2026-07-31T10:00:00" and .time < "2026-07-31T11:00:00")'

# 统计错误类型分布
cat logs/app.log | jq -r 'select(.level=="ERROR") | .msg' | sort | uniq -c | sort -rn
```

### 2. 命令行工具

#### HTTP 请求调试

| 工具 | 用途 | 示例 |
|------|------|------|
| `curl` / `curl.exe` | API 请求、SSE 流抓取、状态码验证 | `curl -s -w "\n%{http_code}" http://localhost:5230/healthz` |
| `jq` | JSON 响应格式化与字段提取 | `curl -s ... \| jq '.data[].id'` |
| `httpie` | 更友好的 curl 替代（可选） | `http POST :5230/api/auth/login username=admin1 password=Pass1234` |

#### SSE 流式调试

```bash
# 抓取完整 SSE 事件序列
curl -N -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"message":"头痛怎么办","department_id":4}' \
  http://localhost:5230/api/chat/conversations/xxx/stream

# 仅看事件类型
curl -N ... 2>/dev/null | grep "^event:" | sort | uniq -c
```

#### 数据库直查

| 场景 | 命令 |
|------|------|
| 连接数据库 | `psql postgres://user:pass@localhost:5432/dbname` |
| 验证数据写入 | `SELECT id, feedback, created_at FROM messages WHERE conversation_id = 'xxx' ORDER BY created_at;` |
| 检查迁移状态 | `goose -dir migrations postgres "$DSN" status` |
| 查看表结构 | `\d table_name` |
| 检查索引 | `\di` |
| 查看锁 | `SELECT * FROM pg_locks WHERE NOT granted;` |
| 查看活跃连接 | `SELECT count(*) FROM pg_stat_activity;` |

#### Redis 调试

| 场景 | 命令 |
|------|------|
| 连接 | `redis-cli` |
| 查看限流状态 | `GET rl:chat:{user_id}` |
| 查看分布式锁 | `GET chat_pending:{uid}:{convID}` |
| 查看 TTL | `TTL key_name` |
| 清除测试数据 | `DEL key_name`（谨慎使用） |
| 查看 Pub/Sub 频道 | `PUBSUB CHANNELS` |

### 3. 网络抓包与代理

| 工具 | 用途 | 适用场景 |
|------|------|---------|
| **浏览器 DevTools Network** | 查看 XHR/Fetch/SSE 请求、响应头、状态码 | 前端调试首选 |
| **mitmproxy** | HTTP/HTTPS 中间人代理，可修改请求/响应 | 模拟异常响应、注入延迟 |
| **Wireshark** | 底层 TCP/HTTP 包分析 | 连接断开、TLS 握手问题 |
| **curl -v** | 查看请求/响应头完整交互 | 快速验证 HTTP 行为 |

#### 前端网络调试要点

```
DevTools → Network → 筛选:
  - Fetch/XHR: 普通 API 请求
  - EventSource: SSE 流式连接
  - 按状态码过滤: 4xx/5xx 快速定位错误
  - 查看 Response Headers: Content-Type, X-Request-ID
```

### 4. 进程与运行时调试

| 工具 | 用途 | 示例 |
|------|------|------|
| **pprof** | CPU/内存/goroutine 性能分析 | 开发模式注册 `net/http/pprof`，`go tool pprof http://localhost:5230/debug/pprof/heap` |
| **air** | Go 热重载，修改代码自动重启 | `air` 自动检测文件变更 |
| **dlv** | Go 源码级调试器 | `dlv debug ./cmd/server`，设断点、单步、查看变量 |
| **go test -run** | 运行特定测试用例 | `go test -run TestAuthGate_ProtectedEndpoints -v ./tests/` |
| **go test -tags debug** | 运行依赖外部服务的诊断测试 | `go test -tags debug ./tests/e2e_api/` |

#### 进程状态检查

```bash
# 检查服务是否运行
curl -s http://localhost:5230/healthz | jq .

# 检查端口占用
lsof -i :5230    # macOS/Linux
netstat -ano | findstr :5230   # Windows

# 查看 Go 进程
ps aux | grep server

# Docker 容器状态
docker compose ps
docker compose logs -f postgres redis
```

### 5. 前端调试

| 工具 | 用途 |
|------|------|
| **Vue DevTools** | 组件树、props/state、事件追踪 |
| **Console 面板** | `console.warn`/`console.error` 输出（禁止 `console.log`/`console.debug`） |
| **Elements 面板** | DOM 结构、CSS 样式、移动端适配检查 |
| **Sources 面板** | 断点调试、源码映射 |
| **Application 面板** | localStorage、Session Storage、Cookie 检查 |
| **Lighthouse** | 性能、可访问性、最佳实践审计 |

#### 移动端调试

```
Chrome DevTools → Toggle Device Toolbar (Ctrl+Shift+M)
  - 设备: iPhone 12 Pro (390×844) 或自定义 375×812
  - deviceScaleFactor: 3
  - Network throttling: Slow 3G / Fast 3G 模拟弱网
```

### 6. 调试策略：按问题类型选择工具

| 问题类型 | 第一步 | 第二步 | 第三步 |
|---------|--------|--------|--------|
| API 返回错误状态码 | curl 复现 + 查日志 request_id | 检查 DB 数据状态 | dlv 断点 service 层 |
| SSE 流异常 | curl -N 抓取事件序列 | DevTools Network 看 EventSource | 日志 grep stream 相关 |
| 前端 UI 不更新 | DevTools Elements 检查 DOM | Console 看 warn/error | Network 检查 API 响应 |
| 数据不一致 | psql 直查对比 | 检查事务边界 | 日志 grep 事务相关 |
| 性能慢 | pprof CPU profile | 检查 DB 慢查询 (`pg_stat_statements`) | Network 瀑布图 |
| 并发/竞态 | `go test -race` | Redis 锁 TTL 检查 | 日志 grep lock 相关 |
| 限流误触发 | Redis 限流 key 检查 | 日志 grep rate_limit | curl 验证 Retry-After |

---

## Layer 3: API 契约 + 深度场景测试

### 方法
- PowerShell + curl.exe，JSON 用文件传递避免转义
- Go integration test（内存 mock 全端口，覆盖服务编排）
- DB 直查验证持久化正确性

### 覆盖维度
- 认证鉴权：正向/反向/越权/token 篡改/过期
- CRUD 状态机：合法转换 + 违规跳转
- 边缘输入：空值、超长、特殊字符、SQL 注入、XSS、emoji
- 安全变体：关键词空格/零宽插入、拼音替代、英文表达、prompt 注入
- 并发：乐观锁、分布式锁、限流精确触发
- 流式：SSE 事件顺序、中断恢复、孤儿消息清理
- 降级链路：改写降级、OOD 拒答、LLM 不可用 503

### 设计原则
- 每个测试 = 一个可独立验证的断言
- 边缘用例从信任边界推导，不凭直觉
- 安全测试用"攻击者视角"：绕过 > 触发

## Layer 4: Playwright 全链路 E2E

### 定位
模拟真实用户在手机浏览器上的完整操作路径。不验证 API 响应体，只验证用户看到的 UI 状态。

### 环境要求
- 前端：vite dev server
- 后端：air hot-reload
- 基础设施：PostgreSQL + Redis + Worker (Docker)
- 浏览器：headless Chromium, 移动端 viewport

### 核心规范

1. **用户视角断言**：不断言 HTTP 响应，断言 DOM 可见文本/元素状态
2. **等待策略**：`page.waitForSelector` / `expect(locator).toBeVisible()`，禁止固定 sleep
3. **SSE 流式断言**：等待 `done` 事件对应的 UI 终态（光标消失、按钮恢复），不逐 token 断言
4. **数据隔离**：每个 spec 文件通过 API seed 独立数据，不依赖其他测试的副作用
5. **失败诊断**：截图 + trace 自动保存，`page.on('console')` 捕获前端错误

### 与 L3 的分工

| 验证点 | L3 (API) | L4 (Playwright) |
|--------|----------|-----------------|
| SSE 事件序列正确 | ✅ 抓包断言 | ❌ 不关心 |
| 前端正确渲染 token | ❌ 看不到 | ✅ DOM 断言 |
| 并发锁 | ✅ 状态码 | ✅ Toast 可见 |
| 危机事件 | ✅ 事件存在 | ✅ Dialog 弹出 |
| 引用字段 | ❌ 后端正确即可 | ✅ 卡片渲染 |
| 降级链路 | ✅ 检索 query 断言 | ✅ 回答相关性 |

---

## 测试数据管理

- 种子数据：通过 seed 命令或迁移脚本初始化
- 向量化验证：chunk 记录 is_active + embedding 非空
- E2E 数据：每个 spec 通过 API seed，测试后不清理（幂等设计）
- 敏感数据：测试账户密码仅存于 spec 文档，不硬编码在脚本中

## Bug 分类与修复流程

| 级别 | 定义 | 响应 |
|------|------|------|
| 🔴 阻塞 | 迁移/schema 不匹配、服务不可启动 | 立即修复，阻塞后续测试 |
| 🔴 高危安全 | 关键词绕过、数据误分类、越权 | 立即修复 |
| 🟡 中等 | 输入校验缺失、降级链路断裂、前端反馈层缺失 | 当轮修复 |
| 🟢 低 | 状态码不一致、硬编码色值、UI 微调 | 当轮修复或记录 |

## 回归验证清单

```bash
# 后端
go build ./... && go vet ./... && go test ./internal/... -count=1

# 前端
npm run build && npm run lint && npm run lint:style

# E2E（需启动全栈）
npx playwright test --project=mobile-chrome
```

## 已知限制与升级路径

| 限制 | 当前状态 | 升级路径 |
|------|---------|---------|
| 日志级别 | 硬编码 Info，无运行时切换 | `LOG_LEVEL` 环境变量支持 |
| 链路追踪 | request_id 单进程 | OpenTelemetry / W3C TraceContext |
| pprof | 未集成 | 开发模式注册 `net/http/pprof` |
| 弱网测试 | Playwright 基本覆盖 | `page.route` 模拟 + BrowserStack |
| iOS Safari SSE | 未真机验证 | BrowserStack / 真机测试 |
