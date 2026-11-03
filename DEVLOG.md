# subAdmin 开发日志

## 2026-07-28 账号页性能优化

### 背景

账号页加载慢，根因是 sub2api 的 `GET /admin/accounts` 返回数据过大：

- 单页 20 条 ~340KB，100 条 ~1.7MB
- 主要膨胀来源：`groups` 全量对象跨账号重复序列化、`extra` 含 21 个键（大部分前端不用）
- 批量测试"全选筛选账号"需逐页遍历所有账号收集 ID，26 页 × 1.7MB = ~45MB 传到浏览器
- sub2api 有 `lite=1` 参数但实际不裁剪响应（只加入 ETag）
- sub2api 没有 batch usage API（`POST /accounts/usage/batch` 返回 404）
- 用量数据已在列表响应的 `extra.codex_*_percent` 字段中，无需单独 `/usage` 请求

### 约束

- **不能修改 sub2api 源码**，必须保持官方 Docker 镜像 `weishaw/sub2api:0.1.165`
- 所有优化在 subAdmin 层实现

### 改动

#### 1. 后端：Lite 响应裁剪

文件：`backend/cmd/subadmin/main.go`

当 `lite=1` 查询参数出现在 accounts 代理请求上时，后端在 `sanitizeJSONForBrowser` 之后、返回浏览器之前裁剪响应：

| 字段 | 裁剪前 | 裁剪后 |
|------|--------|--------|
| `groups` | 完整对象（含 allow_\*、concurrency 等） | `{id, name}` |
| `account_groups` | 含 `created_at`、嵌套完整 group | 仅 `account_id, group_id, priority` + 嵌套 group 裁为 `{id, name}` |
| `extra` | 21 个键 | 6 个键：`codex_primary_used_percent, codex_5h_used_percent, codex_secondary_used_percent, codex_7d_used_percent, email, privacy_mode` |

新增函数：
- `liteTrimAccountList(data []byte) []byte` — 主入口
- `findItemsSlice(root any) []any` — 兼容 `data.items` 和 `items` 两种结构
- `liteTrimGroups`, `liteTrimAccountGroups`, `liteTrimExtra` — 各字段裁剪

日志：裁剪后输出 `accounts lite trimmed` 日志含 `before/after/saved_pct`

效果：20 条 340KB → 36KB（**减少 89%**）

#### 2. 后端：服务端 Filtered-IDs 端点

文件：`backend/cmd/subadmin/main.go`

新增 `POST /api/sites/:id/accounts/filtered-ids`，接收空 body，将 URL 上的上游查询参数透传给 sub2api 的 accounts API，服务端逐页收集所有 ID，仅返回：

```json
{"ids": [1, 2, ...], "total": 292}
```

- 内部使用 lite 模式分页（只解析 `id` 字段）
- 上限无硬限制，靠 sub2api 分页自然终止
- 路由组：`accounts/filtered-ids`（已加入 `routeGroup` 函数）

效果：292 个 ID 从 3 页 × 190KB = ~570KB → 1.5KB，耗时 ~440ms vs 客户端 ~970ms

#### 3. 前端：主页面 Lite 加载

文件：`web/src/App.vue`

`loadAccounts` 函数的 `buildAccountQuery` 后追加 `params.set('lite', '1')`。

前端验证过 lite 裁剪后的字段覆盖了所有已用字段（`id, name, status, platform, type, schedulable, concurrency, current_concurrency, error_message, last_used_at, overload_until, rate_limited_at, rate_limit_reset_at, temp_unschedulable_until, temp_unschedulable_reason, session_window_status, notes, groups[].{id,name}, extra.usage%, extra.email`）。

#### 4. 前端：批量 ID 收集快速路径

文件：`web/src/App.vue`

`selectAllFilteredAccounts` 现有两条路径：

- **快速路径**（无本地过滤器）：调 `filtered-ids` 端点，一次请求拿回全部 ID
- **慢速路径**（有本地过滤器）：回退到 `lite=1` 客户端分页

新增 `hasLocalFilters()` 判断 `scheduleQuickFilter` 和 `groupFilterStates` 是否激活。

### 效果

| 场景 | 优化前 | 优化后 |
|------|--------|--------|
| 页面加载 20 条 | 340KB | 36KB |
| 全选 ID（无本地过滤器） | ~970ms, ~570KB | ~440ms, 1.5KB |
| 全选 ID（有本地过滤器） | ~970ms, ~570KB | ~970ms, ~60KB |

### 部署

```bash
cd ~/subAdmin/backend && go build -o /tmp/subadmin-new ./cmd/subadmin/
systemctl stop subadmin && cp /tmp/subadmin-new ~/subAdmin/backend/subadmin && systemctl start subadmin
cd ~/subAdmin/web && npm run build   # 前端 dist 被 backend 直接 serve
systemctl restart subadmin
```

### 未完成 / 后续

- **异步用量加载 + 占位符**：当前用量数据已在 lite 响应的 `extra.codex_*_percent` 中，页面渲染即有数据，暂无异步需求。若后续 extra 中用量字段也裁掉（极端精简），则需前端先渲染占位符再补数据。
- **`writeBatchUsage` 端点**：已实现 `POST /api/sites/:id/accounts/usage/batch`，但 sub2api 上游无此 API（返回 404）。代码保留，待上游支持后自动生效。
- **服务端过滤 + 本地过滤**：`filtered-ids` 端点仅支持上游查询参数，不支持 `scheduleQuickFilter` 和 `groupFilterStates`（这些是前端本地状态）。如需支持，可将本地过滤规则序列化后传给后端。
- **Lite 模式对 group-quota / error-accounts 视图**：这些走独立的 API 路径，不受 lite 影响。

---

## subAdmin 架构备忘

### 关键路径

| 路径 | 说明 |
|------|------|
| `backend/cmd/subadmin/main.go` | 单文件 Go 后端，含路由、代理、Job 逻辑（~3600 行） |
| `web/src/App.vue` | 主前端文件（~2700 行），含账号页、批量测试、过滤器 |
| `web/src/accountDisplay.ts` | 账号格式化工具 |
| `web/src/apiClient.ts` | API 调用封装（自动 JSON、401 处理、超时） |
| `web/src/views/` | SearchView、ErrorAccountsView、GroupQuotaView 等 |
| `backend/internal/sites/sites.go` | Site 管理服务，含 `AdminGET`/`AdminPOSTJSON` 代理方法 |
| `backend/internal/auth/` | Session 认证 |
| `backend/internal/config/config.go` | 配置加载 |

### 后端账号代理流程

1. 前端请求 `api/sites/:id/accounts?...`
2. `mux.HandleFunc("/api/sites/", ...)` 匹配
3. `sites.IDFromPath` 提取 `id=5, action="accounts"`
4. `siteService.AdminGET` 代理到 sub2api `GET /api/v1/admin/accounts?...`
5. `sanitizeJSONForBrowser` 脱敏（credentials → `[redacted]`）
6. 如有 `lite=1`，`liteTrimAccountList` 裁剪响应
7. 返回给浏览器

### 认证

- 登录密钥：`.env` 中 `SUBADMIN_LOGIN_SECRET`
- Session cookie：`subadmin_session`，HttpOnly，路径跟随 `SUBADMIN_BASE_PATH`
- 当前 base path：`/subadmin`，API 实际在 `/api/*`（cookie 路径与 API 路径不一致，浏览器内因为 SPA 同源所以正常工作，但 curl 测试需手动设 Cookie header）

### sub2api 上游接口

| 端点 | 说明 |
|------|------|
| `GET /admin/accounts` | 账号列表（含 groups、extra、usage 百分比） |
| `GET /admin/accounts/:id/usage` | 单账号用量（subAdmin 前端未使用，用量在 extra 中） |
| `POST /admin/accounts/today-stats/batch` | 批量今日统计（sub2api 支持） |
| `POST /admin/accounts/usage/batch` | 批量用量（**sub2api 不支持，返回 404**） |
| `POST /admin/accounts/search-by-names` | 按名搜索 |
| `POST /admin/accounts/batch` | 批量创建账号（import 用） |
| `POST /admin/accounts/refresh` | 批量刷新 token |

### 运维

```bash
# 状态
systemctl status subadmin
# 日志
journalctl -u subadmin -f
# 重建
cd ~/subAdmin/backend && go build -o ~/subAdmin/backend/subadmin ./cmd/subadmin/
cd ~/subAdmin/web && npm run build
systemctl restart subadmin
```
