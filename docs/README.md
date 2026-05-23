# sub2api Admin API 文档

这个目录整理了 `x-api-key` 管理员 Key 可访问的管理端接口。

文件说明：

- `openapi.yaml`：OpenAPI 3.0 规格文件，可被 Swagger UI、Redoc、Postman、Insomnia、OpenAPI Generator 等工具读取。
- `index.html`：Swagger UI 单页文档，打开后可浏览接口、填写参数并实际发起请求。
- `AI_REFERENCE.md`：面向 AI/自动化脚本阅读的分组接口清单、权限说明和风险标注。

默认不会保存批量测试响应日志；如果需要留档，请放到项目根目录下的本地日志目录。

认证方式：

所有管理接口默认使用请求头：

```http
x-api-key: admin-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

Swagger UI 右上角 `Authorize` 中填入完整管理员 Key 即可试用。

注意事项：

- 管理员 Key 是全局高权限密钥，没有细粒度 scope。
- `GET /api/v1/admin/accounts/data` 会导出上游账号 credentials 原文。
- 系统更新、回滚、重启、备份下载、支付退款、用量清理等接口有强副作用，试用前应确认目标环境。
