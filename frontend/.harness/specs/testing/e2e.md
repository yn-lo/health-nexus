# 真实环境 E2E 测试指南（Playwright）

针对已部署的线上环境做端到端验证，用于排查本地单测/契约测试覆盖不到的真实问题。

## 环境信息

| 项 | 值 |
|----|----|
| 站点 | https://hn.ynlo.top |
| 管理员账号 | `admin` / `Pass1234` |
| 患者端 | `/chat` |
| 医护/管理端 | `/staff` |
| API 基路径 | `/api/` |

## 核心要求

1. **必须使用有头浏览器**（headed），便于观察真实环境下的页面行为：
   ```bash
   playwright-cli open https://hn.ynlo.top
   ```
   若全局命令不可用，用 `npx playwright cli ...`。
2. **模拟真实用户流程**：登录 → 进入页面 → 输入问题 → 发送 → 等待回答，不做接口 mock，全部走线上真实 API。
3. **三件套排查法**：每步操作后收集
   - `playwright-cli console` — 前端 JS 报错/警告
   - `playwright-cli requests` — API 请求的状态码与响应（重点看 `/api/chat/*`、`/api/auth/*`）
   - `playwright-cli snapshot` — 页面 DOM 实际状态

## 标准测试流程

```bash
playwright-cli open https://hn.ynlo.top
playwright-cli snapshot                          # 找到登录表单 ref
playwright-cli fill <用户名ref> "admin"
playwright-cli fill <密码ref> "Pass1234"
playwright-cli click <登录按钮ref>
playwright-cli console                           # 检查登录是否报错
playwright-cli goto https://hn.ynlo.top/chat     # 进入聊天页（或按实际路由）
playwright-cli fill <输入框ref> "你好，请问发烧怎么办？"
playwright-cli click <发送按钮ref>
playwright-cli requests                          # 检查 chat API 请求是否发出、状态码、响应体
playwright-cli console                           # 检查前端是否报错
playwright-cli snapshot                          # 确认回答是否渲染到页面
```

## 常见问题定位顺序

1. 请求未发出 → 前端路由/表单提交逻辑问题，看 console。
2. 请求 4xx/5xx → 看请求详情（`playwright-cli request <n>`），核对鉴权 cookie/token 与后端错误信息。
3. 请求成功但页面无回答 → 看响应体结构是否与前端解析一致（SSE/流式响应需确认前端是否按流读取）。
4. 长时间 pending → 后端 RAG/LLM 调用超时或被墙，看服务端日志。

## 收尾

```bash
playwright-cli close
```

问题结论需附：失败步骤、console 错误、对应 API 请求/响应证据。
