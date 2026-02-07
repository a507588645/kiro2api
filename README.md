# Kiro2API

将 Anthropic/OpenAI API 请求转换为 Kiro/AWS CodeWhisperer API 的代理服务，支持流式响应、工具调用、OAuth 授权与多账号轮换。

---

## 快速开始

### 方式一：源码运行

```bash
git clone https://github.com/a507588645/kiro2api.git
cd kiro2api
go build -o kiro2api main.go
./kiro2api
```

### 方式二：Docker

```bash
docker build -t kiro2api .
docker run -p 8080:8080 --env-file .env kiro2api
```

---

## 基础配置（按你的环境变量重写）

在 `.env` 中填写（以下即你的配置）：

```bash
KIRO_CLIENT_TOKEN=密码
SESSION_POOL_MAX_SIZE=3
SESSION_POOL_MAX_RETRIES=5
LOG_LEVEL=DEBUG
SESSION_POOL_COOLDOWN=60s
SESSION_POOL_ENABLED=true
OAUTH_ENABLED=true
OAUTH_TOKEN_FILE=/app/data/oauth_tokens.json
KIRO_UI_PASSWORD=登陆密码
```

注意：仍需提供上游认证配置 `KIRO_AUTH_TOKEN`（JSON 字符串或文件路径），否则无法正常获取凭证。

示例：

```bash
# JSON 方式
KIRO_AUTH_TOKEN='[{"auth":"Social","refreshToken":"your_refresh_token"}]'

# 或文件路径
KIRO_AUTH_TOKEN=./auth_config.json
```

---

## Web 管理界面

- 访问：`http://localhost:8080/`
- 如果设置了 `KIRO_UI_PASSWORD`，将启用 Basic Auth 保护 `/`、`/static`、`/api`、`/oauth`。

---

## API 端点

### Anthropic 兼容

- `POST /v1/messages`
- `POST /v1/messages/count_tokens`
- `GET /v1/models`

### OpenAI 兼容

- `POST /v1/chat/completions`

---

## OAuth 与账号导入

启用 `OAUTH_ENABLED=true` 后，可访问：

- `GET /oauth`：OAuth 授权页面
- `POST /api/import-accounts`：导入账号 JSON

---

## 机器码管理（UI）

Dashboard 支持为账号绑定机器码（UUID 或 64 位 HEX），并在请求时自动使用绑定值。

---

## License

MIT
