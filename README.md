# SubAdmin

SubAdmin 是一个面向 sub2api 的轻量级管理面板，目标是提供更方便的上游账号导入、筛选、批量管理和 API 文档查看能力。

项目重点是安全地管理 sub2api 管理端能力：浏览器只访问 SubAdmin 自己的后端，不直接接触 sub2api 的管理员 Key。所有 sub2api 管理接口请求都由 SubAdmin 后端从服务器侧发出。

## 当前状态

项目当前已经可用于安全的只读管理和低风险账号维护。目前已经具备：

- 单密钥登录和 HttpOnly Session Cookie
- 多 sub2api 站点管理、默认站点、连接测试
- sub2api 管理员 Key 服务端加密保存
- 账号列表查询、筛选、脱敏详情和移动端卡片展示
- 批量账号检测和批量令牌刷新，均已接入持久化任务
- 导入页支持粘贴/小文件账号内容并生成安全预览；确认站点信息后可通过任务批量导入账号
- 导入执行使用 sub2api `/api/v1/admin/accounts/batch`，导入模型列表统一写入账号 `credentials.model_mapping`
- 导入模板支持保存和套用非敏感默认设置，不保存账号凭据
- 任务页支持查看任务历史、进度、结果详情、运行中任务取消和失败项重试
- 审计页支持查看站点写操作、任务动作和导入摘要
- 统计页默认展示近 24 小时数据，用户并发和账号并发可单独刷新
- 统计页、账号页、站点页、任务页、文档页的顶层导航
- 受保护的 OpenAPI、Swagger UI 和 AI Reference 文档

导入执行前会强制展示目标站点信息并要求确认；SubAdmin 后端负责代发上游写请求，浏览器不接触 sub2api 管理员 Key。

下一步重点：

- 继续打磨导入结果展示和失败定位
- 继续保持浏览器不接触 sub2api 管理员 Key

## 技术栈

- 后端：Go
- 数据库：SQLite
- 前端：Vue 3 + Vite
- 运行方式：Go 服务提供 Web 管理界面和 API

生产环境中，前端会构建为静态资源，并由 Go 后端统一提供访问。

## 安全设计

SubAdmin 的安全边界如下：

- 浏览器不会拿到 sub2api 管理员 Key。
- sub2api 管理员 Key 加密保存到 SQLite。
- 登录使用单一管理密钥。
- 登录成功后使用 `HttpOnly` Session Cookie。
- SQLite 中只保存 session token 的哈希，不保存明文 token。
- 对 sub2api 的管理 API 请求由 SubAdmin 后端代发。

当前 session cookie 默认设置：

- `HttpOnly`
- `Secure`
- `SameSite=Lax`
- `Path=/`

## 目录结构

```text
subAdmin/
  AGENTS.md          AI 协作约束说明
  PLAN.md            开发计划
  README.md          项目说明
  backend/           Go 后端
  web/               Vue 前端
  docs/              sub2api 管理 API 文档
  sub2api/           sub2api 上游源码 submodule
  data/              本地运行数据，默认不提交 Git
```

## 环境变量

当前后端支持以下环境变量：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `SUBADMIN_ADDR` | `127.0.0.1:8787` | SubAdmin 后端监听地址 |
| `SUBADMIN_DB_PATH` | `../data/subadmin.db` | SQLite 数据库路径 |
| `SUBADMIN_LOGIN_SECRET` | 空 | 登录管理密钥，生产环境必须设置 |
| `SUBADMIN_SECRET_KEY` | 空 | 敏感数据加密密钥，站点管理功能需要设置 |
| `SUBADMIN_BASE_PATH` | `/` | 应用对外基础路径，也用于 Session Cookie Path |
| `SUBADMIN_COOKIE_SECURE` | `true` | 是否只允许安全连接携带 Cookie |
| `SUBADMIN_SESSION_TTL` | `24h` | Session 有效期 |
| `SUBADMIN_LOG_DIR` | `data/logs` | 本地日志目录，批量测试响应日志保存到该目录下 |
| `SUBADMIN_LOG_LEVEL` | `info` | 应用日志等级，支持 `debug`、`info`、`warn`、`error` |
| `SUBADMIN_LOG_MAX_MB` | `10` | 单个 `subadmin.log` 文件最大大小，单位 MiB |
| `SUBADMIN_LOG_MAX_BACKUPS` | `5` | 日志轮转后保留的历史文件数量 |

应用日志使用 JSON Lines 格式，同时写到标准输出和 `SUBADMIN_LOG_DIR/subadmin.log`。默认 `info` 等级；本机开发测试可设置 `SUBADMIN_LOG_LEVEL=debug` 记录更详细的请求、任务和操作信息。每个请求会返回并记录 `X-Request-ID`，业务日志会自动带上同一个 `request_id` 方便串联排查。

示例：

```bash
SUBADMIN_LOGIN_SECRET='change-me' \
SUBADMIN_ADDR='127.0.0.1:8787' \
./subadmin
```

## 运行

进入后端目录：

```bash
cd backend
```

构建：

```bash
go build ./cmd/subadmin
```

运行：

```bash
SUBADMIN_LOGIN_SECRET='change-me' ./subadmin
```

健康检查：

```text
http://127.0.0.1:8787/healthz
```

## API 文档

`docs/` 目录中保存了 sub2api 管理接口文档，包括：

- `docs/openapi.yaml`：OpenAPI 3.0 文档
- `docs/index.html`：Swagger UI 页面
- `docs/AI_REFERENCE.md`：AI 友好参考文档

这些文档已经整合进登录后的管理面板。

## 开发计划

开发计划见：

```text
PLAN.md
```

当前下一阶段重点：

- 导入预览
- 导入执行前的确认、任务跟踪和审计日志基础
- 高风险账号操作的安全工作流

## 注意事项

- 不要把真实 `SUBADMIN_LOGIN_SECRET` 写入 Git。
- 不要把 SQLite 数据库提交到 Git。
- 不要在浏览器端保存 sub2api 管理员 Key。
- 批量测试响应日志默认不保存；如需保存，请使用项目根目录下的本地日志目录，例如 `data/logs`。
- 导入执行和高风险批量操作上线前需要预览、明确确认、任务跟踪和审计日志。
