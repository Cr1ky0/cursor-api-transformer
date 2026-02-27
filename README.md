## 说明
- 本项目为二次开发，源项目地址:https://github.com/danilofalcao/cursor-deepseek
- 去除HTTP/2支持，实际使用兼容性不佳
- 添加Deepseek推理模型代理
- 添加Cursor对接POE的Claude相关模型（如Claude-Sonnet-4.5、Claude-Opus-4.5等等）
- 支持 Cursor，还支持Continue、Cline等VSCode插件（Cline 只支持 Deepseek）。

## 支持的代理模式
1. **DeepSeek 代理**：OpenAI 格式 → DeepSeek 格式
2. **POE 的 Claude 系列模型代理**：Anthropic Claude API 格式 → OpenAI 格式

## 支持的客户端
1. Cursor 通过 OpenAI BaseUrl 和 OpenAI API Key 可实现对接
2. VSCode Continue插件可通过配置对接
3. VSCode Cline插件可通过配置对接

## 功能特性
- 完整的 CORS 支持
- 流式响应
- 支持函数调用/工具
- 自动消息格式转换
- 压缩支持（Brotli、Gzip、Deflate）
- 兼容 OpenAI API 客户端库
- API 密钥验证以确保安全访问
- Docker 容器支持，具有多变体构建

## 前置要求
- Cursor Pro 订阅
- DeepSeek 或 POE API 密钥
- Go 1.19 或更高版本
- 公网部署

## 安装

1. 克隆仓库
2. 安装依赖：
```bash
go mod download
```

### Docker 安装

根据你的需求选择合适的构建命令：

1. 构建 Docker 镜像：
   - 对于 DeepSeek（默认）：
   ```bash
   docker build -t cursor-deepseek .
   ```
   - 对于 POE：
   ```bash
   docker build -t cursor-poe --build-arg PROXY_VARIANT=poe .
   ```

2. 配置环境变量：
   - 复制示例配置：
   ```bash
   cp .env.example .env
   ```
   - 编辑 `.env` 并添加你的 API 密钥（DeepSeek 或 POE）
   - 也可以不配置，直接使用客户端 API Key

3. 运行容器：
```bash
docker run -p 9000:9000 --env-file .env cursor-deepseek
# 或者对于 POE
docker run -p 9000:9000 --env-file .env cursor-poe

# 自定义端口（例如使用 8080）：
docker run -p 8080:8080 -e PORT=8080 --env-file .env cursor-deepseek
# 或者对于 POE
docker run -p 8080:8080 -e PORT=8080 --env-file .env cursor-poe
```

## 配置

仓库包含一个 `.env.example` 文件，显示所需的环境变量。配置步骤：

1. 复制示例配置：
```bash
cp .env.example .env
```

2. 编辑 `.env` 并添加你的 API 密钥：
```bash
# 对于 DeepSeek
DEEPSEEK_API_KEY=your_deepseek_api_key_here

# 或者对于 POE
POE_API_KEY=your_poe_api_key_here

# 可选：自定义监听端口（默认为 9000）
PORT=8080
```

注意：根据你使用的变体，只配置其中一个 API 密钥。

## 使用方法

1. 启动代理服务器：

### 对于 DeepSeek（OpenAI → DeepSeek）
```bash
go run -tags deepseek proxy.go
```

### 对于 Claude 到 POE 代理（Cursor Claude 格式 → POE）
```bash
go run -tags claude proxy-poe.go
```

### 自定义监听端口

服务器默认将在端口 9000 上启动。如需使用其他端口，可以通过以下方式：

**方式 1：在 .env 文件中配置**
```bash
PORT=8080
```

**方式 2：运行时指定环境变量**
```bash
PORT=8080 go run -tags deepseek proxy.go
# 或
PORT=8080 go run -tags claude proxy-poe.go
```

2. 通过将基础 URL 设置为 `http://your-public-endpoint:9000/v1`（或你自定义的端口）来使用代理与你的 OpenAI API 客户端


## 公网暴露

Cursor强制需要外网链接，你可以使用 ngrok 或类似服务将本地代理服务器暴露到互联网，或者直接在服务器上部署该服务。

### 使用 ngrok

1. 从 https://ngrok.com/download 安装 ngrok

2. 在本地启动代理服务器（默认在端口 9000 上运行，也可通过 PORT 环境变量自定义）

3. 在新终端中运行 ngrok（使用你的端口号）：
```bash
ngrok http 9000
# 如果使用自定义端口（例如 8080）：
ngrok http 8080
```

4. ngrok 将为你提供一个公共 URL（例如：https://your-unique-id.ngrok.io）

5. 在 Cursor 的设置中使用此 URL 作为你的 OpenAI API 基础 URL：
```
https://your-unique-id.ngrok.io/v1
```

### 支持的端点

- `/v1/chat/completions` - 聊天完成端点
- `/v1/models` - 模型列表端点

### 模型映射

- `deepseek-chat` 用于 DeepSeek 的原生聊天模型
- `deepseek-reasoner` DeepSeek推理模型
- `deepseek-coder` DeepSeek的 Code 模型，目前处于 beta 测试中
- `Poe的Claude系列模型` Sonnet-4.5/4.6 和 Opus-4.5/4.6 模型都经过测试，其他的自测

## 依赖项

- `github.com/andybalholm/brotli` - Brotli 压缩支持
- `github.com/joho/godotenv` - 环境变量管理

## Continue 插件配置
```yaml
name: Local Config
version: 1.0.0
schema: v1
models:
  - name: Sonnet-4.6
    provider: openai
    apiKey: "your api key"
    apiBase: http://your-base-url:port
    model: claude-sonnet-4.6

  - name: Deepseek-Chat
    provider: openai
    apiKey: "your api key"
    apiBase: http://your-base-url:port
    model: deepseek-chat

  - name: Deepseek-Thinking
    provider: openai
    apiKey: "your api key"
    apiBase: http://your-base-url:port
    model: deepseek-reasoner
```

## Cline 插件配置
- API Provider选择```OpenAI Compatible```
- 配置 BaseUrl 和 API Key 为自己的服务

## 安全性
- 代理包含用于跨域请求的 CORS 头
- 需要 API 密钥并根据环境变量进行验证
- 安全处理请求/响应数据
- 对所有请求进行严格的 API 密钥验证
- 环境变量永远不会提交到仓库

## 许可证
本项目采用 GNU 通用公共许可证 v2.0（GPLv2）。详见 [LICENSE.md](LICENSE.md) 文件。
