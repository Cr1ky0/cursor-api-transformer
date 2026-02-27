package main

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/joho/godotenv"
)

const (
	openAIEndpoint      = "https://api.poe.com" // POE的OpenAI兼容endpoint
	claudeSonnetModel   = "claude-sonnet-4.5"
	defaultOpenAIModel  = "claude-sonnet-4.5" // 默认使用POE的Claude模型
)

var poeAPIKey string

func init() {
	// 加载 .env 文件
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found or error loading it: %v", err)
	}

	// 获取 POE API key
	poeAPIKey = os.Getenv("POE_API_KEY")
	if poeAPIKey == "" {
		log.Printf("Warning: POE_API_KEY environment variable is not set, user must provide API key in request")
	}

	log.Printf("Initialized Claude to POE proxy with endpoint: %s", openAIEndpoint)
}

// Claude 请求结构
type ClaudeRequest struct {
	Model         string                 `json:"model"`
	Messages      []ClaudeMessage        `json:"messages"`
	MaxTokens     *int                   `json:"max_tokens,omitempty"`
	System        []ClaudeSystemMessage  `json:"system,omitempty"`
	Tools         []ClaudeTool           `json:"tools,omitempty"`
	ToolChoice    interface{}            `json:"tool_choice,omitempty"`
	Temperature   *float64               `json:"temperature,omitempty"`
	Stream        bool                   `json:"stream"`
	StreamOptions map[string]interface{} `json:"stream_options,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

type ClaudeMessage struct {
	Role       string             `json:"role"`
	Content    json.RawMessage    `json:"content,omitempty"`
	ToolCalls  []ClaudeToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	Name       string             `json:"name,omitempty"`
}

type ClaudeContentPart struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text,omitempty"`
	CacheControl map[string]interface{} `json:"cache_control,omitempty"`
	// tool_use 相关字段
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
}

type ClaudeToolResultContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ClaudeSystemMessage struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl map[string]interface{} `json:"cache_control,omitempty"`
}

type ClaudeTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type ClaudeToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// OpenAI 请求结构
type OpenAIRequest struct {
	Model       string                 `json:"model"`
	Messages    []OpenAIMessage        `json:"messages"`
	MaxTokens   *int                   `json:"max_tokens,omitempty"`
	Temperature *float64               `json:"temperature,omitempty"`
	Stream      bool                   `json:"stream"`
	Tools       []OpenAITool           `json:"tools,omitempty"`
	ToolChoice  interface{}            `json:"tool_choice,omitempty"`
}

type OpenAIMessage struct {
	Role       string             `json:"role"`
	Content    interface{}        `json:"content,omitempty"`
	ToolCalls  []OpenAIToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	Name       string             `json:"name,omitempty"`
}

type OpenAITool struct {
	Type     string       `json:"type"`
	Function OpenAIFunc   `json:"function"`
}

type OpenAIFunc struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type OpenAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// OpenAI 响应结构
type OpenAIResponse struct {
	ID      string          `json:"id"`
	Object  string          `json:"object"`
	Created int64           `json:"created"`
	Model   string          `json:"model"`
	Choices []OpenAIChoice  `json:"choices"`
	Usage   OpenAIUsage     `json:"usage"`
}

type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)

	// 从环境变量读取端口，如果没有设置则使用默认端口9000
	port := os.Getenv("PORT")
	if port == "" {
		port = "9000"
	}

	server := &http.Server{
		Addr:    ":" + port,
		Handler: http.HandlerFunc(proxyHandler),
	}

	log.Printf("Starting Claude to OpenAI proxy server on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func enableCors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received request: %s %s", r.Method, r.URL.Path)

	if r.Method == "OPTIONS" {
		enableCors(w)
		return
	}

	enableCors(w)

	// 验证 API key
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		log.Printf("Missing or invalid Authorization header")
		http.Error(w, "Missing or invalid Authorization header", http.StatusUnauthorized)
		return
	}

	// 优先使用用户传来的 API Key，若为空则回退到环境变量中的 poeAPIKey
	userAPIKey := strings.TrimPrefix(authHeader, "Bearer ")
	activeAPIKey := userAPIKey
	if activeAPIKey == "" {
		activeAPIKey = poeAPIKey
	}
	if activeAPIKey == "" {
		log.Printf("No API key available: neither user-provided nor POE_API_KEY env var is set")
		http.Error(w, "No API key available", http.StatusUnauthorized)
		return
	}
	log.Printf("Using API key source: %s", map[bool]string{true: "user-provided", false: "env POE_API_KEY"}[userAPIKey != ""])

	// 处理 /v1/models 端点
	if r.URL.Path == "/v1/models" && r.Method == "GET" {
		log.Printf("Handling /v1/models request")
		handleModelsRequest(w)
		return
	}

	// 读取请求体
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "Error reading request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Printf("========== Request from Cursor (Claude format) ==========")
	log.Printf("Request body: %s", string(body))
	log.Printf("==========================================================")

	// 解析 Claude 格式请求
	var claudeReq ClaudeRequest
	if err := json.Unmarshal(body, &claudeReq); err != nil {
		log.Printf("Error parsing Claude request JSON: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("Parsed Claude request - Model: %s, Stream: %v", claudeReq.Model, claudeReq.Stream)

	// 转换为 OpenAI 格式
	openAIReq, err := convertClaudeToOpenAI(claudeReq)
	if err != nil {
		log.Printf("Error converting request: %v", err)
		http.Error(w, "Error converting request", http.StatusInternalServerError)
		return
	}

	log.Printf("Using model: %s (from client request)", openAIReq.Model)

	// 创建新的请求体
	modifiedBody, err := json.Marshal(openAIReq)
	if err != nil {
		log.Printf("Error creating modified request body: %v", err)
		http.Error(w, "Error creating modified request", http.StatusInternalServerError)
		return
	}

	log.Printf("========== Request to OpenAI ==========")
	log.Printf("Modified request body: %s", string(modifiedBody))
	log.Printf("========================================")

	// 创建代理请求到 OpenAI
	targetURL := openAIEndpoint + "/v1/chat/completions"
	proxyReq, err := http.NewRequest("POST", targetURL, bytes.NewReader(modifiedBody))
	if err != nil {
		log.Printf("Error creating proxy request: %v", err)
		http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
		return
	}

	// 设置请求头
	proxyReq.Header.Set("Authorization", "Bearer "+activeAPIKey)
	proxyReq.Header.Set("Content-Type", "application/json")
	if claudeReq.Stream {
		proxyReq.Header.Set("Accept", "text/event-stream")
	}

	// 创建客户端并发送请求
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}

	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("Error forwarding request: %v", err)
		http.Error(w, "Error forwarding request", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	log.Printf("========== OpenAI Response ==========")
	log.Printf("Status: %d", resp.StatusCode)
	log.Printf("=====================================")

	// 处理错误响应
	if resp.StatusCode >= 400 {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading error response: %v", err)
			http.Error(w, "Error reading response", http.StatusInternalServerError)
			return
		}
		log.Printf("OpenAI ERROR Response: %s", string(respBody))

		// 转发错误响应
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	// 处理流式响应
	if claudeReq.Stream {
		handleStreamingResponse(w, resp, claudeReq.Model)
		return
	}

	// 处理普通响应
	handleRegularResponse(w, resp, claudeReq.Model)
}

// 转换 Claude 请求为 OpenAI 请求
func convertClaudeToOpenAI(claudeReq ClaudeRequest) (*OpenAIRequest, error) {
	// 使用客户端请求的模型名称，如果为空则使用默认模型
	modelName := claudeReq.Model
	if modelName == "" {
		modelName = defaultOpenAIModel
	}

	openAIReq := &OpenAIRequest{
		Model:       modelName,
		Stream:      claudeReq.Stream,
		Temperature: claudeReq.Temperature,
		MaxTokens:   claudeReq.MaxTokens,
	}

	// 转换消息
	var messages []OpenAIMessage

	// 如果有 system 消息，添加到消息列表开头
	if len(claudeReq.System) > 0 {
		var systemTexts []string
		for _, sys := range claudeReq.System {
			if sys.Text != "" {
				systemTexts = append(systemTexts, sys.Text)
			}
		}
		if len(systemTexts) > 0 {
			messages = append(messages, OpenAIMessage{
				Role:    "system",
				Content: strings.Join(systemTexts, "\n\n"),
			})
		}
	}

	// 转换用户和助手消息
	for _, msg := range claudeReq.Messages {
		// 检查是否是包含 tool_result 的 user 消息（需要特殊处理）
		if msg.Role == "user" && msg.Content != nil {
			var parts []map[string]interface{}
			if err := json.Unmarshal(msg.Content, &parts); err == nil {
				hasToolResult := false
				for _, part := range parts {
					if partType, ok := part["type"].(string); ok && partType == "tool_result" {
						hasToolResult = true
						// 这是一个 tool result，需要转换为 tool 角色消息
						toolMsg := OpenAIMessage{
							Role: "tool",
						}

						// 获取 tool_use_id
						if toolUseID, ok := part["tool_use_id"].(string); ok {
							toolMsg.ToolCallID = toolUseID
						}

						// 提取 content
						if content, ok := part["content"].([]interface{}); ok {
							var resultTexts []string
							for _, c := range content {
								if contentMap, ok := c.(map[string]interface{}); ok {
									if text, ok := contentMap["text"].(string); ok {
										resultTexts = append(resultTexts, text)
									}
								}
							}
							if len(resultTexts) > 0 {
								toolMsg.Content = strings.Join(resultTexts, "\n")
							}
						} else if contentStr, ok := part["content"].(string); ok {
							toolMsg.Content = contentStr
						}

						messages = append(messages, toolMsg)
					}
				}

				// 如果已经处理了tool_result，跳过后续的常规处理
				if hasToolResult {
					continue
				}
			}
		}

		// 常规消息处理
		openAIMsg := OpenAIMessage{
			Role:       msg.Role,
			ToolCallID: msg.ToolCallID,
			Name:       msg.Name,
		}

		// 转换 content
		if msg.Content != nil {
			// 尝试解析为字符串
			var strContent string
			if err := json.Unmarshal(msg.Content, &strContent); err == nil {
				openAIMsg.Content = strContent
			} else {
				// 尝试解析为数组
				var parts []ClaudeContentPart
				if err := json.Unmarshal(msg.Content, &parts); err == nil {
					// 提取文本部分（忽略 cache_control）和 tool_use 部分
					var textParts []string
					var toolCalls []OpenAIToolCall

					for _, part := range parts {
						if part.Type == "text" && part.Text != "" {
							textParts = append(textParts, part.Text)
						} else if part.Type == "tool_use" {
							// 将 Claude 的 tool_use 转换为 OpenAI 的 tool_calls
							inputJSON, _ := json.Marshal(part.Input)
							toolCalls = append(toolCalls, OpenAIToolCall{
								ID:   part.ID,
								Type: "function",
								Function: struct {
									Name      string `json:"name"`
									Arguments string `json:"arguments"`
								}{
									Name:      part.Name,
									Arguments: string(inputJSON),
								},
							})
						}
					}

					if len(textParts) > 0 {
						openAIMsg.Content = strings.Join(textParts, "\n")
					}

					if len(toolCalls) > 0 {
						openAIMsg.ToolCalls = toolCalls
					}
				}
			}
		}

		// 转换 tool_calls（如果直接存在）
		if len(msg.ToolCalls) > 0 {
			openAIMsg.ToolCalls = make([]OpenAIToolCall, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				openAIMsg.ToolCalls[i] = OpenAIToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
		}

		// 添加消息
		// 对于assistant角色，如果有tool_calls但没有content，可以设置content为null
		contentStr, _ := openAIMsg.Content.(string)
		hasContent := openAIMsg.Content != nil && contentStr != ""
		hasToolCalls := len(openAIMsg.ToolCalls) > 0

		// 只添加有效的消息（content 或 tool_calls 不为空）
		if hasContent || hasToolCalls {
			// 如果是assistant且只有tool_calls没有content，content设为nil
			if openAIMsg.Role == "assistant" && hasToolCalls && !hasContent {
				openAIMsg.Content = nil
			}
			messages = append(messages, openAIMsg)
		}
	}

	openAIReq.Messages = messages

	// 转换 tools
	if len(claudeReq.Tools) > 0 {
		openAIReq.Tools = make([]OpenAITool, len(claudeReq.Tools))
		for i, tool := range claudeReq.Tools {
			openAIReq.Tools[i] = OpenAITool{
				Type: "function",
				Function: OpenAIFunc{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			}
		}
	}

	// 转换 tool_choice
	if claudeReq.ToolChoice != nil {
		openAIReq.ToolChoice = claudeReq.ToolChoice
	}

	return openAIReq, nil
}

// 处理流式响应
func handleStreamingResponse(w http.ResponseWriter, resp *http.Response, originalModel string) {
	log.Printf("Starting streaming response handling")

	// 设置流式响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)

	reader := bufio.NewReader(resp.Body)
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("Warning: ResponseWriter does not support Flush")
	}

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("Error reading stream: %v", err)
			return
		}

		lineStr := string(line)

		// 转发空行
		if len(strings.TrimSpace(lineStr)) == 0 {
			w.Write(line)
			if flusher != nil {
				flusher.Flush()
			}
			continue
		}

		// 处理 SSE data 行
		if strings.HasPrefix(lineStr, "data: ") {
			data := strings.TrimPrefix(lineStr, "data: ")
			data = strings.TrimSpace(data)

			// 处理 [DONE] 标记
			if data == "[DONE]" {
				w.Write([]byte("data: [DONE]\n\n"))
				if flusher != nil {
					flusher.Flush()
				}
				break
			}

			// 解析并修改 JSON，替换模型名称为原始请求的模型
			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunk); err == nil {
				chunk["model"] = originalModel

				modifiedData, err := json.Marshal(chunk)
				if err == nil {
					w.Write([]byte("data: "))
					w.Write(modifiedData)
					w.Write([]byte("\n\n"))
				} else {
					w.Write(line)
				}
			} else {
				w.Write(line)
			}
		} else {
			// 转发其他行
			w.Write(line)
		}

		if flusher != nil {
			flusher.Flush()
		}
	}

	log.Printf("Streaming response completed")
}

// 处理普通响应
func handleRegularResponse(w http.ResponseWriter, resp *http.Response, originalModel string) {
	log.Printf("Handling regular (non-streaming) response")

	// 读取响应体
	body, err := readResponse(resp)
	if err != nil {
		log.Printf("Error reading response: %v", err)
		http.Error(w, "Error reading response from upstream", http.StatusInternalServerError)
		return
	}

	log.Printf("OpenAI response body: %s", string(body))

	// 解析 OpenAI 响应
	var openAIResp OpenAIResponse
	if err := json.Unmarshal(body, &openAIResp); err != nil {
		log.Printf("Error parsing OpenAI response: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// 替换模型名称为原始请求的模型
	openAIResp.Model = originalModel

	// 转换回 JSON
	modifiedBody, err := json.Marshal(openAIResp)
	if err != nil {
		log.Printf("Error creating modified response: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	log.Printf("Modified response body: %s", string(modifiedBody))

	// 设置响应头并发送
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(modifiedBody)
	log.Printf("Modified response sent successfully")
}

// 处理模型列表请求
func handleModelsRequest(w http.ResponseWriter) {
	log.Printf("Handling models request")

	response := map[string]interface{}{
		"object": "list",
		"data": []map[string]interface{}{
			{
				"id":       claudeSonnetModel,
				"object":   "model",
				"created":  time.Now().Unix(),
				"owned_by": "anthropic",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
	log.Printf("Models response sent successfully")
}

// 读取响应（处理压缩）
func readResponse(resp *http.Response) ([]byte, error) {
	var reader io.Reader = resp.Body

	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error creating gzip reader: %v", err)
		}
		defer gzReader.Close()
		reader = gzReader
	case "br":
		reader = brotli.NewReader(resp.Body)
	case "deflate":
		reader = flate.NewReader(resp.Body)
	}

	return io.ReadAll(reader)
}
