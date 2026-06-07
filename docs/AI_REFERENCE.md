# sub2api Admin API AI Reference

本文档面向 AI、脚本和自动化调用方，按能力域梳理管理员 Key 可调用接口。

当前同步基准：sub2api `v0.1.133`。

源码依据：

- `backend/internal/server/middleware/admin_auth.go`
- `backend/internal/server/routes/admin.go`
- `backend/internal/server/routes/payment.go`
- `backend/internal/handler/admin/*`

## 认证

所有管理接口默认使用：

```http
x-api-key: admin-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

管理员 Key 只走 `x-api-key`。`Authorization: Bearer ...` 是管理员 JWT 认证路径，不是管理员 Key 路径。

认证通过后，服务端把调用者映射为数据库中的首个 active admin 用户。管理员 Key 没有 scope，没有只读模式，没有模块级权限隔离。

## 通用响应

大多数接口使用统一 JSON 响应包裹：

```json
{
  "success": true,
  "message": "optional",
  "data": {}
}
```

错误通常类似：

```json
{
  "success": false,
  "error": "INVALID_ADMIN_KEY",
  "message": "Invalid admin api key"
}
```

## 高风险接口

| Method | Path | 风险 |
|---|---|---|
| GET | `/api/v1/admin/accounts/data` | 导出上游账号 credentials 原文；`include_proxies=true` 时包含代理密码 |
| POST | `/api/v1/admin/accounts/data` | 批量导入账号和代理，写入 credentials |
| POST | `/api/v1/admin/accounts/batch-update-credentials` | 批量改写上游账号 credentials |
| GET | `/api/v1/admin/backups/{id}/download-url` | 获取备份下载 URL |
| POST | `/api/v1/admin/backups/{id}/restore` | 恢复备份；需要管理员密码二次校验 |
| POST | `/api/v1/admin/system/update` | 执行系统更新 |
| POST | `/api/v1/admin/system/rollback` | 执行系统回滚 |
| POST | `/api/v1/admin/system/restart` | 触发服务重启 |
| POST | `/api/v1/admin/payment/orders/{id}/refund` | 执行退款 |
| POST | `/api/v1/admin/usage/cleanup-tasks` | 创建用量清理任务，影响审计/计费记录 |
| POST | `/api/v1/admin/settings/admin-api-key/regenerate` | 轮换管理员 Key，旧 Key 失效 |
| DELETE | `/api/v1/admin/settings/admin-api-key` | 删除管理员 Key，Key 认证失效 |

## 运行说明

SubAdmin 的批量测试响应日志默认不保存。若显式开启保存，日志应写入由 `SUBADMIN_LOG_DIR` 指定的项目根目录日志目录。

## Endpoint Inventory

### Dashboard

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/v1/admin/dashboard/snapshot-v2` | 仪表盘快照 v2 |
| GET | `/api/v1/admin/dashboard/stats` | 总览统计 |
| GET | `/api/v1/admin/dashboard/realtime` | 实时指标 |
| GET | `/api/v1/admin/dashboard/trend` | 用量趋势 |
| GET | `/api/v1/admin/dashboard/models` | 模型统计 |
| GET | `/api/v1/admin/dashboard/groups` | 分组统计 |
| GET | `/api/v1/admin/dashboard/api-keys-trend` | API Key 用量趋势 |
| GET | `/api/v1/admin/dashboard/users-trend` | 用户用量趋势 |
| GET | `/api/v1/admin/dashboard/users-ranking` | 用户消费排行 |
| POST | `/api/v1/admin/dashboard/users-usage` | 批量查询用户用量 |
| POST | `/api/v1/admin/dashboard/api-keys-usage` | 批量查询 API Key 用量 |
| GET | `/api/v1/admin/dashboard/user-breakdown` | 用户分解统计 |
| POST | `/api/v1/admin/dashboard/aggregation/backfill` | 回填聚合数据 |

### Users

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/v1/admin/users` | 用户列表 |
| POST | `/api/v1/admin/users` | 创建用户 |
| GET | `/api/v1/admin/users/{id}` | 用户详情 |
| PUT | `/api/v1/admin/users/{id}` | 更新用户 |
| DELETE | `/api/v1/admin/users/{id}` | 删除用户 |
| POST | `/api/v1/admin/users/{id}/auth-identities` | 手动绑定第三方认证身份 |
| POST | `/api/v1/admin/users/{id}/balance` | 修改用户余额 |
| GET | `/api/v1/admin/users/{id}/platform-quotas` | 用户平台配额 |
| PUT | `/api/v1/admin/users/{id}/platform-quotas` | 全量替换用户平台配额 |
| POST | `/api/v1/admin/users/{id}/platform-quotas/reset` | 重置用户平台配额窗口 |
| GET | `/api/v1/admin/users/{id}/api-keys` | 用户 API Key 列表 |
| GET | `/api/v1/admin/users/{id}/usage` | 用户用量 |
| GET | `/api/v1/admin/users/{id}/balance-history` | 用户余额历史 |
| POST | `/api/v1/admin/users/{id}/replace-group` | 替换用户独占分组 |
| GET | `/api/v1/admin/users/{id}/rpm-status` | 用户 RPM 状态 |
| POST | `/api/v1/admin/users/batch-concurrency` | 批量更新用户并发 |
| GET | `/api/v1/admin/users/{id}/attributes` | 用户属性值 |
| PUT | `/api/v1/admin/users/{id}/attributes` | 更新用户属性值 |
| GET | `/api/v1/admin/users/{id}/subscriptions` | 用户订阅列表 |

### Groups

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/v1/admin/groups` | 分组列表 |
| POST | `/api/v1/admin/groups` | 创建分组 |
| GET | `/api/v1/admin/groups/all` | 全部分组 |
| GET | `/api/v1/admin/groups/usage-summary` | 分组用量摘要 |
| GET | `/api/v1/admin/groups/capacity-summary` | 分组容量摘要 |
| PUT | `/api/v1/admin/groups/sort-order` | 更新分组排序 |
| GET | `/api/v1/admin/groups/{id}` | 分组详情 |
| PUT | `/api/v1/admin/groups/{id}` | 更新分组 |
| DELETE | `/api/v1/admin/groups/{id}` | 删除分组 |
| GET | `/api/v1/admin/groups/{id}/stats` | 分组统计 |
| GET | `/api/v1/admin/groups/{id}/rate-multipliers` | 分组模型倍率 |
| PUT | `/api/v1/admin/groups/{id}/rate-multipliers` | 批量设置分组模型倍率 |
| DELETE | `/api/v1/admin/groups/{id}/rate-multipliers` | 清空分组模型倍率 |
| GET | `/api/v1/admin/groups/{id}/models-list-candidates` | 分组模型候选列表 |
| PUT | `/api/v1/admin/groups/{id}/rpm-overrides` | 批量设置分组 RPM override |
| DELETE | `/api/v1/admin/groups/{id}/rpm-overrides` | 清空分组 RPM override |
| GET | `/api/v1/admin/groups/{id}/api-keys` | 分组 API Key |
| GET | `/api/v1/admin/groups/{id}/subscriptions` | 分组订阅列表 |

### Accounts

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/v1/admin/accounts` | 上游账号列表 |
| POST | `/api/v1/admin/accounts` | 创建上游账号 |
| GET | `/api/v1/admin/accounts/{id}` | 账号详情 |
| PUT | `/api/v1/admin/accounts/{id}` | 更新账号 |
| DELETE | `/api/v1/admin/accounts/{id}` | 删除账号 |
| POST | `/api/v1/admin/accounts/{id}/test` | 测试账号 |
| POST | `/api/v1/admin/accounts/{id}/recover-state` | 恢复账号状态 |
| POST | `/api/v1/admin/accounts/{id}/refresh` | 刷新账号 token/状态 |
| POST | `/api/v1/admin/accounts/{id}/apply-oauth-credentials` | 落库重新授权 OAuth 凭据 |
| POST | `/api/v1/admin/accounts/{id}/set-privacy` | 设置账号隐私 |
| POST | `/api/v1/admin/accounts/{id}/refresh-tier` | 刷新账号 tier |
| GET | `/api/v1/admin/accounts/{id}/stats` | 账号统计 |
| POST | `/api/v1/admin/accounts/{id}/clear-error` | 清除账号错误 |
| GET | `/api/v1/admin/accounts/{id}/usage` | 账号用量 |
| GET | `/api/v1/admin/accounts/{id}/today-stats` | 账号今日统计 |
| POST | `/api/v1/admin/accounts/today-stats/batch` | 批量账号今日统计 |
| POST | `/api/v1/admin/accounts/{id}/clear-rate-limit` | 清除账号 rate limit |
| POST | `/api/v1/admin/accounts/{id}/reset-quota` | 重置账号 quota |
| GET | `/api/v1/admin/accounts/{id}/temp-unschedulable` | 获取临时不可调度状态 |
| DELETE | `/api/v1/admin/accounts/{id}/temp-unschedulable` | 清除临时不可调度状态 |
| POST | `/api/v1/admin/accounts/{id}/schedulable` | 设置账号是否可调度 |
| GET | `/api/v1/admin/accounts/{id}/models` | 账号可用模型 |
| POST | `/api/v1/admin/accounts/{id}/models/sync-upstream` | 同步上游模型 |
| POST | `/api/v1/admin/accounts/batch` | 批量创建账号 |
| GET | `/api/v1/admin/accounts/data` | 导出账号/代理数据，高风险 |
| POST | `/api/v1/admin/accounts/data` | 导入账号/代理数据 |
| POST | `/api/v1/admin/accounts/batch-update-credentials` | 批量更新账号凭据 |
| POST | `/api/v1/admin/accounts/batch-refresh-tier` | 批量刷新 tier |
| POST | `/api/v1/admin/accounts/bulk-update` | 批量更新账号 |
| POST | `/api/v1/admin/accounts/batch-clear-error` | 批量清错 |
| POST | `/api/v1/admin/accounts/batch-refresh` | 批量刷新账号 |
| GET | `/api/v1/admin/accounts/antigravity/default-model-mapping` | Antigravity 默认模型映射 |
| GET | `/api/v1/admin/accounts/{id}/scheduled-test-plans` | 账号定时测试计划 |

### OAuth

| Method | Path | 说明 |
|---|---|---|
| POST | `/api/v1/admin/accounts/generate-auth-url` | 生成 Claude OAuth URL |
| POST | `/api/v1/admin/accounts/generate-setup-token-url` | 生成 Claude setup token URL |
| POST | `/api/v1/admin/accounts/exchange-code` | 交换 Claude OAuth code |
| POST | `/api/v1/admin/accounts/exchange-setup-token-code` | 交换 Claude setup token code |
| POST | `/api/v1/admin/accounts/cookie-auth` | Claude cookie auth 导入 |
| POST | `/api/v1/admin/accounts/setup-token-cookie-auth` | Claude setup token cookie auth 导入 |
| POST | `/api/v1/admin/openai/generate-auth-url` | 生成 OpenAI OAuth URL |
| POST | `/api/v1/admin/openai/exchange-code` | 交换 OpenAI code |
| POST | `/api/v1/admin/openai/refresh-token` | 刷新 OpenAI token |
| POST | `/api/v1/admin/openai/accounts/{id}/refresh` | 刷新 OpenAI 账号 token |
| POST | `/api/v1/admin/openai/create-from-oauth` | 从 OAuth 创建 OpenAI 账号 |
| POST | `/api/v1/admin/gemini/oauth/auth-url` | 生成 Gemini OAuth URL |
| POST | `/api/v1/admin/gemini/oauth/exchange-code` | 交换 Gemini code |
| GET | `/api/v1/admin/gemini/oauth/capabilities` | Gemini OAuth 能力 |
| POST | `/api/v1/admin/antigravity/oauth/auth-url` | 生成 Antigravity OAuth URL |
| POST | `/api/v1/admin/antigravity/oauth/exchange-code` | 交换 Antigravity code |
| POST | `/api/v1/admin/antigravity/oauth/refresh-token` | 刷新 Antigravity token |

### Proxies

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/v1/admin/proxies` | 代理列表 |
| POST | `/api/v1/admin/proxies` | 创建代理 |
| GET | `/api/v1/admin/proxies/all` | 全部代理 |
| GET | `/api/v1/admin/proxies/data` | 导出代理数据，可能含密码 |
| POST | `/api/v1/admin/proxies/data` | 导入代理数据 |
| GET | `/api/v1/admin/proxies/{id}` | 代理详情 |
| PUT | `/api/v1/admin/proxies/{id}` | 更新代理 |
| DELETE | `/api/v1/admin/proxies/{id}` | 删除代理 |
| POST | `/api/v1/admin/proxies/{id}/test` | 测试代理 |
| POST | `/api/v1/admin/proxies/{id}/quality-check` | 质量检查 |
| GET | `/api/v1/admin/proxies/{id}/stats` | 代理统计 |
| GET | `/api/v1/admin/proxies/{id}/accounts` | 代理绑定账号 |
| POST | `/api/v1/admin/proxies/batch-delete` | 批量删除代理 |
| POST | `/api/v1/admin/proxies/batch` | 批量创建代理 |

### Settings

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/v1/admin/settings` | 获取全局设置 |
| PUT | `/api/v1/admin/settings` | 更新全局设置 |
| POST | `/api/v1/admin/settings/test-smtp` | 测试 SMTP |
| POST | `/api/v1/admin/settings/send-test-email` | 发送测试邮件 |
| GET | `/api/v1/admin/settings/email-templates` | 邮件模板列表 |
| GET | `/api/v1/admin/settings/email-templates/{event}/{locale}` | 邮件模板详情 |
| PUT | `/api/v1/admin/settings/email-templates/{event}/{locale}` | 更新邮件模板 |
| POST | `/api/v1/admin/settings/email-templates/{event}/{locale}/restore-official` | 恢复官方模板 |
| POST | `/api/v1/admin/settings/email-template-preview` | 预览邮件模板 |
| GET | `/api/v1/admin/settings/admin-api-key` | 管理员 Key 状态 |
| POST | `/api/v1/admin/settings/admin-api-key/regenerate` | 生成/重置管理员 Key |
| DELETE | `/api/v1/admin/settings/admin-api-key` | 删除管理员 Key |
| GET | `/api/v1/admin/settings/overload-cooldown` | 529 冷却配置 |
| PUT | `/api/v1/admin/settings/overload-cooldown` | 更新 529 冷却配置 |
| GET | `/api/v1/admin/settings/rate-limit-429-cooldown` | 429 回避配置 |
| PUT | `/api/v1/admin/settings/rate-limit-429-cooldown` | 更新 429 回避配置 |
| GET | `/api/v1/admin/settings/stream-timeout` | 流超时配置 |
| PUT | `/api/v1/admin/settings/stream-timeout` | 更新流超时配置 |
| GET | `/api/v1/admin/settings/rectifier` | 请求整流器配置 |
| PUT | `/api/v1/admin/settings/rectifier` | 更新请求整流器配置 |
| GET | `/api/v1/admin/settings/beta-policy` | Beta policy 配置 |
| PUT | `/api/v1/admin/settings/beta-policy` | 更新 Beta policy 配置 |
| GET | `/api/v1/admin/settings/web-search-emulation` | Web Search 模拟配置 |
| PUT | `/api/v1/admin/settings/web-search-emulation` | 更新 Web Search 模拟配置 |
| POST | `/api/v1/admin/settings/web-search-emulation/test` | 测试 Web Search 模拟 |
| POST | `/api/v1/admin/settings/web-search-emulation/reset-usage` | 重置 Web Search 用量 |

### Data, Backup, System

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/v1/admin/data-management/agent/health` | agent 健康 |
| GET | `/api/v1/admin/data-management/config` | 数据管理配置 |
| PUT | `/api/v1/admin/data-management/config` | 更新数据管理配置 |
| GET | `/api/v1/admin/data-management/sources/{source_type}/profiles` | source profiles |
| POST | `/api/v1/admin/data-management/sources/{source_type}/profiles` | 创建 source profile |
| PUT | `/api/v1/admin/data-management/sources/{source_type}/profiles/{profile_id}` | 更新 source profile |
| DELETE | `/api/v1/admin/data-management/sources/{source_type}/profiles/{profile_id}` | 删除 source profile |
| POST | `/api/v1/admin/data-management/sources/{source_type}/profiles/{profile_id}/activate` | 激活 source profile |
| POST | `/api/v1/admin/data-management/s3/test` | 测试 S3 |
| GET | `/api/v1/admin/data-management/s3/profiles` | S3 profiles |
| POST | `/api/v1/admin/data-management/s3/profiles` | 创建 S3 profile |
| PUT | `/api/v1/admin/data-management/s3/profiles/{profile_id}` | 更新 S3 profile |
| DELETE | `/api/v1/admin/data-management/s3/profiles/{profile_id}` | 删除 S3 profile |
| POST | `/api/v1/admin/data-management/s3/profiles/{profile_id}/activate` | 激活 S3 profile |
| POST | `/api/v1/admin/data-management/backups` | 创建数据管理备份任务 |
| GET | `/api/v1/admin/data-management/backups` | 数据管理备份任务列表 |
| GET | `/api/v1/admin/data-management/backups/{job_id}` | 数据管理备份任务详情 |
| GET | `/api/v1/admin/backups/s3-config` | 备份 S3 配置 |
| PUT | `/api/v1/admin/backups/s3-config` | 更新备份 S3 配置 |
| POST | `/api/v1/admin/backups/s3-config/test` | 测试备份 S3 |
| GET | `/api/v1/admin/backups/schedule` | 定时备份配置 |
| PUT | `/api/v1/admin/backups/schedule` | 更新定时备份配置 |
| POST | `/api/v1/admin/backups` | 创建备份 |
| GET | `/api/v1/admin/backups` | 备份列表 |
| GET | `/api/v1/admin/backups/{id}` | 备份详情 |
| DELETE | `/api/v1/admin/backups/{id}` | 删除备份 |
| GET | `/api/v1/admin/backups/{id}/download-url` | 备份下载 URL |
| POST | `/api/v1/admin/backups/{id}/restore` | 恢复备份 |
| GET | `/api/v1/admin/system/version` | 当前版本 |
| GET | `/api/v1/admin/system/check-updates` | 检查更新 |
| POST | `/api/v1/admin/system/update` | 执行更新 |
| POST | `/api/v1/admin/system/rollback` | 执行回滚 |
| POST | `/api/v1/admin/system/restart` | 重启服务 |

### Business Management

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/v1/admin/announcements` | 公告列表 |
| POST | `/api/v1/admin/announcements` | 创建公告 |
| GET | `/api/v1/admin/announcements/{id}` | 公告详情 |
| PUT | `/api/v1/admin/announcements/{id}` | 更新公告 |
| DELETE | `/api/v1/admin/announcements/{id}` | 删除公告 |
| GET | `/api/v1/admin/announcements/{id}/read-status` | 阅读状态 |
| GET | `/api/v1/admin/redeem-codes` | 兑换码列表 |
| GET | `/api/v1/admin/redeem-codes/stats` | 兑换码统计 |
| GET | `/api/v1/admin/redeem-codes/export` | 导出兑换码 |
| GET | `/api/v1/admin/redeem-codes/{id}` | 兑换码详情 |
| POST | `/api/v1/admin/redeem-codes/create-and-redeem` | 创建并兑换 |
| POST | `/api/v1/admin/redeem-codes/generate` | 生成兑换码 |
| DELETE | `/api/v1/admin/redeem-codes/{id}` | 删除兑换码 |
| POST | `/api/v1/admin/redeem-codes/batch-delete` | 批量删除兑换码 |
| POST | `/api/v1/admin/redeem-codes/{id}/expire` | 使兑换码过期 |
| GET | `/api/v1/admin/promo-codes` | 优惠码列表 |
| POST | `/api/v1/admin/promo-codes` | 创建优惠码 |
| GET | `/api/v1/admin/promo-codes/{id}` | 优惠码详情 |
| PUT | `/api/v1/admin/promo-codes/{id}` | 更新优惠码 |
| DELETE | `/api/v1/admin/promo-codes/{id}` | 删除优惠码 |
| GET | `/api/v1/admin/promo-codes/{id}/usages` | 优惠码使用记录 |
| GET | `/api/v1/admin/subscriptions` | 订阅列表 |
| GET | `/api/v1/admin/subscriptions/{id}` | 订阅详情 |
| GET | `/api/v1/admin/subscriptions/{id}/progress` | 订阅进度 |
| POST | `/api/v1/admin/subscriptions/assign` | 分配订阅 |
| POST | `/api/v1/admin/subscriptions/bulk-assign` | 批量分配订阅 |
| POST | `/api/v1/admin/subscriptions/{id}/extend` | 延长订阅 |
| POST | `/api/v1/admin/subscriptions/{id}/reset-quota` | 重置订阅 quota |
| DELETE | `/api/v1/admin/subscriptions/{id}` | 撤销订阅 |

### Usage, Rules, Channels, Risk, Payment, Ops

这几组接口数量较多，完整路径以 `openapi.yaml` 为准。能力摘要：

- Usage：查询用量记录和统计、搜索用户/API Key、创建/取消清理任务。
- UserAttributes：管理用户属性定义、排序、批量读取。
- ErrorPassthrough：管理错误透传规则。
- TLSFingerprintProfiles：管理 TLS 指纹模板。
- APIKeys：更新用户 API Key 分组，重置限速用量。
- ScheduledTests：创建/更新/删除定时测试计划，查看测试结果。
- Channels：创建/查看/更新/删除渠道，查询模型默认定价和定价目录模型同步。
- ChannelMonitors：管理渠道监控和模板，手动运行监控，查看历史。
- RiskControl：管理风控配置、测试风控 key、查看日志、解封、删除 flagged hash。
- Affiliates：查看邀请/返利/转账记录，管理专属返利用户配置。
- AdminPayment：管理支付配置、订单、退款、套餐、provider。
- Ops：查询实时监控、告警、错误日志、请求明细、系统日志，并可执行 resolve/cleanup。

## 推荐调用约束

- 写操作尽量带 `Idempotency-Key`。
- 不要把管理员 Key 暴露给前端浏览器。
- 对高风险路径增加来源限制或二次认证。
- 轮换管理员 Key 后，立即更新所有集成方配置。
- 自动化脚本应默认禁用系统更新、重启、备份下载、账号导出、退款、用量清理，除非用户显式确认。
