# SubAdmin

SubAdmin 是一个面向 sub2api 的轻量级管理面板，目标是提供更方便的上游账号导入、筛选、批量管理和 API 文档查看能力。

项目重点是安全地管理 sub2api 管理端能力：浏览器只访问 SubAdmin 自己的后端，不直接接触 sub2api 的管理员 Key。所有 sub2api 管理接口请求都由 SubAdmin 后端从服务器侧发出。

## 当前状态

项目仍处于早期开发阶段，目前已经完成：

- Go 后端基础服务
- SQLite 基础表结构初始化
- 单密钥登录
- Session Cookie 鉴权
- 最小可访问 Web 页面
- sub2api 源码作为 Git submodule 引入

正在规划和开发中的能力：

- 多 sub2api 站点配置和切换
- sub2api 管理员 Key 加密保存
- 站点连通性测试
- 上游账号列表、筛选和搜索
- 上游账号批量管理
- 上传导入和导入模板
- 登录后内置查看 API 文档

## 技术栈

- 后端：Go
- 数据库：SQLite
- 前端：Vue 3 + Vite
- 运行方式：Go 服务提供 Web 管理界面和 API

生产环境中，前端会构建为静态资源，并由 Go 后端统一提供访问。

## 安全设计

SubAdmin 的安全边界如下：

- 浏览器不会拿到 sub2api 管理员 Key。
- sub2api 管理员 Key 后续会加密保存到 SQLite。
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

后续 SubAdmin 会把这些文档整合进登录后的管理面板。

## 开发计划

开发计划见：

```text
PLAN.md
```

当前下一阶段重点：

- 站点配置管理
- sub2api 管理员 Key 加密存储
- 站点连通性测试

## 注意事项

- 不要把真实 `SUBADMIN_LOGIN_SECRET` 写入 Git。
- 不要把 SQLite 数据库提交到 Git。
- 不要在浏览器端保存 sub2api 管理员 Key。
- 当前项目仍在早期开发阶段，危险批量操作上线前需要额外确认和审计日志。
