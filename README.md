# Kiro2API

一个用 Go 编写的 Anthropic Claude API 兼容代理服务，将 Anthropic/OpenAI API 请求转换为 Kiro API 请求。

## 免责声明

本项目仅供研究使用，Use at your own risk。使用本项目所导致的任何后果由使用人承担，与本项目无关。
本项目与 AWS/KIRO/Anthropic/Claude 等官方无关，本项目不代表官方立场。

## 功能特性

- **Anthropic API 兼容**: 完整支持 Anthropic Claude API 格式 (`/v1/messages`)
- **OpenAI API 兼容**: 支持 OpenAI 格式 (`/v1/chat/completions`)
- **流式响应**: 支持 SSE (Server-Sent Events) 流式输出
- **Token 自动刷新**: 自动管理和刷新 OAuth Token
- **多凭据支持**: 支持配置多个凭据，自动故障转移
- **会话池**: 支持会话池和智能重试机制
- **OAuth 授权**: 支持 Web 页面 OAuth 授权流程
- **Thinking 模式**: 支持 Claude 的 extended thinking 功能
- **工具调用**: 完整支持 function calling / tool use
- **多模型支持**: 支持 Sonnet、Opus、Haiku 系列模型
- **Web 管理界面**: 实时监控 Token 状态和使用情况

## 支持的 API 端点

| 端点 | 方法 | 描述 |
|------|------|------|
| `/v1/models` | GET | 获取可用模型列表 |
| `/v1/messages` | POST | Anthropic API 代理 |
| `/v1/messages/count_tokens` | POST | 估算 Token 数量 |
| `/v1/chat/completions` | POST | OpenAI API 代理 |
| `/api/tokens` | GET | Token 池状态 API |
| `/oauth` | GET | OAuth 授权页面 |
| `/` | GET | 管理 Dashboard |

## 快速开始

### 1. 配置环境变量

创建 `.env` 文件（参考 `.env.example`）：

```bash
# 客户端认证令牌（必填）
KIRO_CLIENT_TOKEN=your-secure-random-password

# 会话池配置
SESSION_POOL_MAX_SIZE=3          # 会话池最大容量
SESSION_POOL_MAX_RETRIES=5       # 会话池最大重试次数
SESSION_POOL_COOLDOWN=60s        # 会话池冷却时间
SESSION_POOL_ENABLED=true        # 会话池功能开关

# OAuth 配置
OAUTH_ENABLED=true                           # 授权功能开关
OAUTH_TOKEN_FILE=/app/data/oauth_tokens.json # 授权令牌文件路径

# 日志配置
LOG_LEVEL=INFO                   # 日志级别: DEBUG, INFO, WARN, ERROR

# 服务端口
PORT=8080
```

### 2. 配置认证

#### 方式一：环境变量 JSON 格式

```bash
# 单凭据
KIRO_AUTH_TOKEN='{"auth":"Social","refreshToken":"your_token"}'

# 多凭据
KIRO_AUTH_TOKEN='[
  {"auth":"Social","refreshToken":"token1"},
  {"auth":"IdC","refreshToken":"token2","clientId":"xxx","clientSecret":"xxx"}
]'
```

#### 方式二：配置文件

```bash
# 指向 JSON 配置文件
KIRO_AUTH_TOKEN=/path/to/auth_config.json
```

配置文件格式（参考 `auth_config.json.example`）：

```json
[
  {
    "auth": "Social",
    "refreshToken": "your_refresh_token"
  },
  {
    "auth": "IdC",
    "refreshToken": "your_refresh_token",
    "clientId": "your_client_id",
    "clientSecret": "your_client_secret"
  }
]
```

### 3. 启动服务

#### 直接运行

```bash
go build -o kiro2api main.go
./kiro2api
```

#### Docker 运行

```bash
docker build -t kiro2api .
docker run -p 8080:8080 --env-file .env kiro2api
```

#### Docker Compose

```bash
docker-compose -f docker-compose.updated.yml up -d
```

### 4. 使用 API

#### Anthropic API 格式

```bash
curl http://127.0.0.1:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: your-client-token" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "Hello, Claude!"}
    ]
  }'
```

#### OpenAI API 格式

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-client-token" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "Hello, Claude!"}
    ]
  }'
```

## 配置说明

### 环境变量

| 变量 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `KIRO_CLIENT_TOKEN` | string | - | 客户端认证令牌（必填） |
| `KIRO_AUTH_TOKEN` | string | - | 认证配置 JSON 或文件路径 |
| `PORT` | string | `8080` | 服务监听端口 |
| `LOG_LEVEL` | string | `INFO` | 日志级别 |
| `GIN_MODE` | string | `release` | Gin 框架模式 |
| `SESSION_POOL_ENABLED` | bool | `true` | 启用会话池 |
| `SESSION_POOL_MAX_SIZE` | int | `3` | 会话池最大容量 |
| `SESSION_POOL_MAX_RETRIES` | int | `5` | 最大重试次数 |
| `SESSION_POOL_COOLDOWN` | duration | `60s` | 冷却时间 |
| `OAUTH_ENABLED` | bool | `false` | 启用 OAuth 授权 |
| `OAUTH_TOKEN_FILE` | string | - | OAuth Token 存储文件 |
| `PROXY_POOL` | string | - | 代理池配置（逗号分隔） |

### 认证配置

| 字段 | 类型 | 描述 |
|------|------|------|
| `auth` | string | 认证方式（`Social` / `IdC`） |
| `refreshToken` | string | OAuth 刷新令牌 |
| `clientId` | string | IdC 登录的客户端 ID（IdC 必填） |
| `clientSecret` | string | IdC 登录的客户端密钥（IdC 必填） |
| `disabled` | bool | 是否禁用该凭据 |

**说明**：
- `Social`: 社交账号登录（Google/GitHub 等）
- `IdC`: AWS IAM Identity Center / Builder ID / IAM 登录

## 模型映射

| 请求模型 | 后端模型 |
|----------|----------|
| `claude-opus-4-5-20251101` | CLAUDE_OPUS_4_5_20251101_V1_0 |
| `claude-sonnet-4-5-20250929` | CLAUDE_SONNET_4_5_20250929_V1_0 |
| `claude-sonnet-4-20250514` | CLAUDE_SONNET_4_20250514_V1_0 |
| `claude-3-7-sonnet-20250219` | CLAUDE_3_7_SONNET_20250219_V1_0 |
| `claude-3-5-haiku-20241022` | auto |
| `claude-haiku-4-5-20251001` | auto |

**Thinking 模式**: 在模型名后添加 `-thinking` 后缀自动启用思考模式，如 `claude-sonnet-4-20250514-thinking`

## 项目结构

```
kiro2api/
├── main.go                     # 程序入口
├── auth/                       # 认证模块
│   ├── auth.go                 # 认证服务
│   ├── config.go               # 配置加载
│   ├── oauth.go                # OAuth 实现
│   ├── token_manager.go        # Token 管理
│   ├── refresh.go              # Token 刷新
│   ├── fingerprint.go          # 设备指纹
│   └── usage_checker.go        # 使用量检查
├── server/                     # HTTP 服务
│   ├── server.go               # 主服务器
│   ├── handlers.go             # 请求处理器
│   ├── middleware.go           # 中间件
│   ├── openai_handlers.go      # OpenAI 兼容处理
│   └── stream_processor.go     # 流式响应处理
├── converter/                  # 协议转换
│   ├── openai.go               # OpenAI 格式转换
│   ├── codewhisperer.go        # CodeWhisperer 格式转换
│   └── tools.go                # 工具转换
├── parser/                     # 解析器
│   ├── event_stream_types.go   # AWS EventStream 类型
│   ├── header_parser.go        # 头部解析
│   └── thinking_detector.go    # Thinking 模式检测
├── types/                      # 类型定义
│   ├── anthropic.go            # Anthropic 类型
│   ├── openai.go               # OpenAI 类型
│   ├── codewhisperer.go        # CodeWhisperer 类型
│   └── usage_limits.go         # 使用限制类型
├── config/                     # 配置
│   ├── config.go               # 模型映射
│   ├── constants.go            # 常量定义
│   └── tuning.go               # 调优参数
├── utils/                      # 工具函数
│   ├── client.go               # HTTP 客户端
│   ├── json.go                 # JSON 处理
│   └── token_counter.go        # Token 计数
├── logger/                     # 日志
│   └── logger.go               # 日志工具
├── static/                     # 静态资源
│   ├── index.html              # Dashboard 页面
│   ├── oauth.html              # OAuth 授权页面
│   ├── css/                    # 样式文件
│   └── js/                     # JavaScript 文件
├── Dockerfile                  # Docker 构建文件
├── docker-compose.updated.yml  # Docker Compose 配置
└── README.md                   # 项目文档
```

## 技术栈

- **Web 框架**: [Gin](https://github.com/gin-gonic/gin)
- **HTTP 客户端**: Go 标准库 `net/http`
- **JSON 处理**: [bytedance/sonic](https://github.com/bytedance/sonic)（高性能）
- **日志**: [Zap](https://github.com/uber-go/zap)
- **环境变量**: [godotenv](https://github.com/joho/godotenv)

## 高级功能

### Thinking 模式

支持 Claude 的 extended thinking 功能：

```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 16000,
  "thinking": {
    "type": "enabled",
    "budget_tokens": 10000
  },
  "messages": [...]
}
```

或使用 `-thinking` 后缀自动启用：

```json
{
  "model": "claude-sonnet-4-20250514-thinking",
  "max_tokens": 16000,
  "messages": [...]
}
```

### 工具调用

完整支持 Anthropic 的 tool use 功能：

```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 1024,
  "tools": [
    {
      "name": "get_weather",
      "description": "获取指定城市的天气",
      "input_schema": {
        "type": "object",
        "properties": {
          "city": {"type": "string"}
        },
        "required": ["city"]
      }
    }
  ],
  "messages": [...]
}
```

### 流式响应

设置 `stream: true` 启用 SSE 流式响应：

```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 1024,
  "stream": true,
  "messages": [...]
}
```

### OAuth 授权

启用 OAuth 后，访问 `/oauth` 页面进行账号授权：

1. 选择认证方式（Social / IdC）
2. 输入必要的凭据信息
3. 完成授权后自动添加到 Token 池

## 认证方式

支持两种 API Key 认证方式：

1. **x-api-key Header**
   ```
   x-api-key: your-client-token
   ```

2. **Authorization Bearer**
   ```
   Authorization: Bearer your-client-token
   ```

## Docker 部署

### 构建镜像

```bash
# 标准构建
docker build -t kiro2api .

# 多平台构建
docker buildx build --platform linux/amd64,linux/arm64 -t kiro2api .
```

### 运行容器

```bash
docker run -d \
  --name kiro2api \
  -p 8080:8080 \
  -e KIRO_CLIENT_TOKEN=your-token \
  -e KIRO_AUTH_TOKEN='[{"auth":"Social","refreshToken":"xxx"}]' \
  -v /path/to/data:/app/data \
  kiro2api
```

## 注意事项

1. **凭证安全**: 请妥善保管认证配置文件，不要提交到版本控制
2. **Token 刷新**: 服务会自动刷新过期的 Token，无需手动干预
3. **请求体限制**: 支持最大 100MB 的请求体，可处理大图片上传
4. **额度统计**: 自动统计基础额度、免费试用额度和 Bonus 额度

## 更新日志

### 2026.1.23

借鉴 kiro.rs 2026.1.6 版本的改进：

- **增大请求体限制**: 100MB 限制，解决图片上传问题
- **Bonus 额度计入**: 修复额度统计不全的问题
- **完善文档**: 大幅更新 README 文档

## 致谢

本项目的实现离不开以下项目的参考：
- [kiro.rs](https://github.com/hank9999/kiro.rs)
- [proxycast](https://github.com/aiclientproxy/proxycast)

再次由衷感谢！

## License

MIT
