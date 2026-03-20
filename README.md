## 说明

本项目为二次开发，[源项目地址](https://github.com/danilofalcao/cursor-deepseek)，主要改动：

- 去除 HTTP/2 支持（实际使用兼容性不佳）
- 添加 DeepSeek 推理模型代理
- 添加 Cursor 对接 POE 的 Claude 相关模型（如 Claude-Sonnet-4.5、Claude-Opus-4.5 等）
- 添加直连 Anthropic API 的代理（`proxy-o2a`、`proxy-o2a-max`）
- 仅支持 Cursor，其他插件和平台可自行测试

## 代理变体说明

| 变体 | 源文件 | 说明 |
|------|--------|------|
| `deepseek`（默认） | `proxy.go` | 对接 DeepSeek API，需要 `DEEPSEEK_API_KEY` |
| `poe` | `proxy-poe.go` | 对接 POE API，支持 Claude 系列模型，需要 `POE_API_KEY` |
| `o2a` | `proxy-o2a.go` | 直连 Anthropic API（OpenAI 格式转 Anthropic 格式），可以把一些第三方中转站的API对接进来，需要 `ANTHROPIC_API_KEY` |
| `o2a-max` | `proxy-o2a-max.go` | 同上，伪装为 Claude CLI 客户端请求头，可以使用第三方中转站API中的MAX接口（部分不行），需要 `ANTHROPIC_API_KEY` |

## 环境变量配置

复制 `.env.example` 为 `.env` 并按需填写：

```bash
cp .env.example .env
```

各变体所需环境变量：

```env
# deepseek 变体
DEEPSEEK_API_KEY=YOUR_DEEPSEEK_API_KEY

# poe 变体
POE_API_KEY=YOUR_POE_API_KEY

# o2a / o2a-max 变体
ANTHROPIC_API_KEY=YOUR_ANTHROPIC_API_KEY
# 可选：自定义 Anthropic 端点（默认 https://api.anthropic.com）
ANTHROPIC_ENDPOINT=https://api.anthropic.com

# 可选：自定义监听端口（默认 9000）
PORT=9000
```

## 本地运行

```bash
# deepseek 变体
go run proxy.go

# poe 变体
go run proxy-poe.go

# o2a 变体（直连 Anthropic）
go run proxy-o2a.go

# o2a-max 变体（伪装 Claude CLI）
go run proxy-o2a-max.go
```

支持通过命令行参数覆盖环境变量：

```bash
go run proxy-o2a-max.go -key YOUR_KEY -port 8080 -endpoint https://api.anthropic.com
```

## Docker 部署

构建时通过 `PROXY_VARIANT` 参数选择变体，可选值：`deepseek`（默认）、`poe`、`o2a`、`o2a-max`。

```bash
# 构建 o2a-max 变体
docker build --build-arg PROXY_VARIANT=o2a-max -t cursor-proxy:o2a-max .

# 构建 o2a 变体
docker build --build-arg PROXY_VARIANT=o2a -t cursor-proxy:o2a .

# 构建 poe 变体
docker build --build-arg PROXY_VARIANT=poe -t cursor-proxy:poe .

# 构建 deepseek 变体（默认）
docker build -t cursor-proxy:deepseek .
```

运行容器（以 o2a-max 为例）：

```bash
docker run -d \
  -p 9000:9000 \
  -e ANTHROPIC_API_KEY=YOUR_ANTHROPIC_API_KEY \
  -e ANTHROPIC_ENDPOINT=https://your-endpoint.com \
  --name cursor-proxy \
  cursor-proxy:o2a-max
```

也可以在构建阶段将配置烤入镜像（适合固定部署环境，注意 API Key 会留在镜像层中，不建议用于密钥）：

```bash
docker build \
  --build-arg PROXY_VARIANT=o2a-max \
  --build-arg ANTHROPIC_ENDPOINT=https://your-endpoint.com \
  -t cursor-proxy:o2a-max .
```

> `ANTHROPIC_ENDPOINT` 默认为 `https://api.anthropic.com`，如需对接第三方中转站可按上述方式配置。`ANTHROPIC_API_KEY` 建议通过 `-e` 在运行时传入，避免密钥被固化进镜像。

## 在 Cursor 中配置

1. 打开 Cursor 设置 → `Models` → `OpenAI API Key`
2. 填入任意非空字符串（代理会使用服务端配置的 API Key）
3. 将 `Override OpenAI Base URL` 设置为代理地址：
   ```
   http://localhost:9000/v1
   ```
   若使用公网地址（见下方「公网暴露」章节）则替换为对应 URL。

## 公网暴露

Cursor 强制需要外网链接，可使用 ngrok 或类似服务将本地服务暴露到互联网，或直接在服务器上部署。

### 使用 ngrok

1. 从 [ngrok.com/download](https://ngrok.com/download) 安装 ngrok

2. 本地启动代理服务（默认端口 9000）

3. 新终端运行 ngrok：
   ```bash
   ngrok http 9000
   ```

4. ngrok 提供公共 URL（如 `https://your-unique-id.ngrok.io`）

5. 在 Cursor 设置中填入：
   ```
   https://your-unique-id.ngrok.io/v1
   ```

## 许可证

本项目采用 GNU 通用公共许可证 v2.0（GPLv2）。详见 [LICENSE](LICENSE) 文件。
