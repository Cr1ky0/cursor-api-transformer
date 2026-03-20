package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultAnthropicEndpoint = "https://api.anthropic.com"
	anthropicVersion         = "2023-06-01"
	defaultAnthropicModel    = "claude-sonnet-4-5"
)

var (
	anthropicAPIKey   string
	anthropicEndpoint string
)

// modelNameMap maps client-provided model names to Anthropic API model IDs
var modelNameMap = map[string]string{
	"claude-sonnet-4.6": "claude-sonnet-4-6",
	"claude-opus-4.6": "claude-sonnet-4-6",
	"claude-sonnet-4.5": "claude-sonnet-4-5-20250929",
}

func init() {
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found: %v", err)
	}
	anthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	if anthropicAPIKey == "" {
		log.Printf("Warning: ANTHROPIC_API_KEY not set, user must provide key in request")
	}
	anthropicEndpoint = strings.TrimRight(os.Getenv("ANTHROPIC_ENDPOINT"), "/")
	if anthropicEndpoint == "" {
		anthropicEndpoint = defaultAnthropicEndpoint
	}
	log.Printf("Initialized Anthropic proxy, endpoint: %s", anthropicEndpoint)
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)

	// Command-line flags override env vars
	flagEndpoint := flag.String("endpoint", "", "Anthropic API endpoint (overrides ANTHROPIC_ENDPOINT)")
	flagPort := flag.String("port", "", "Listen port (overrides PORT env var)")
	flagKey := flag.String("key", "", "Anthropic API key (overrides ANTHROPIC_API_KEY)")
	flag.Parse()

	if *flagEndpoint != "" {
		anthropicEndpoint = strings.TrimRight(*flagEndpoint, "/")
	}
	if *flagKey != "" {
		anthropicAPIKey = *flagKey
	}

	port := *flagPort
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "9000"
	}

	log.Printf("Using endpoint: %s", anthropicEndpoint)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: http.HandlerFunc(proxyHandler),
	}

	log.Printf("Starting Anthropic proxy on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func enableCors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, anthropic-version, x-api-key")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received: %s %s", r.Method, r.URL.Path)

	if r.Method == "OPTIONS" {
		enableCors(w)
		return
	}
	enableCors(w)

	// Extract API key
	authHeader := r.Header.Get("Authorization")
	apiKey := strings.TrimPrefix(authHeader, "Bearer ")
	if apiKey == "" || apiKey == authHeader {
		apiKey = anthropicAPIKey
	}
	if apiKey == "" {
		http.Error(w, "No API key provided", http.StatusUnauthorized)
		return
	}

	if r.URL.Path == "/v1/models" && r.Method == "GET" {
		handleModelsRequest(w)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse as generic map for field manipulation
	var reqMap map[string]interface{}
	if err := json.Unmarshal(body, &reqMap); err != nil {
		log.Printf("Error parsing request JSON: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Apply model name mapping
	originalModel, _ := reqMap["model"].(string)
	if originalModel == "" {
		originalModel = defaultAnthropicModel
		reqMap["model"] = originalModel
	}
	if mapped, ok := modelNameMap[originalModel]; ok {
		reqMap["model"] = mapped
	}

	// Remove stream_options (OpenAI extension, not supported by Anthropic)
	delete(reqMap, "stream_options")

	isStream, _ := reqMap["stream"].(bool)

	modifiedBody, err := json.Marshal(reqMap)
	if err != nil {
		http.Error(w, "Error serializing request", http.StatusInternalServerError)
		return
	}

	// Forward to Anthropic API
	proxyReq, err := http.NewRequest("POST", anthropicEndpoint+"/v1/messages", bytes.NewReader(modifiedBody))
	if err != nil {
		http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
		return
	}
	proxyReq.Header.Set("x-api-key", apiKey)
	proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
	proxyReq.Header.Set("anthropic-version", anthropicVersion)
	proxyReq.Header.Set("content-type", "application/json")
	proxyReq.Header.Set("user-agent", "claude-cli/2.1.79 (external, cli)")
	proxyReq.Header.Set("anthropic-beta", "claude-code-20250219,interleaved-thinking-2025-05-14,prompt-caching-scope-2026-01-05,effort-2025-11-24")
	proxyReq.Header.Set("x-app", "cli")
	proxyReq.Header.Set("x-stainless-lang", "js")
	proxyReq.Header.Set("x-stainless-package-version", "0.74.0")
	proxyReq.Header.Set("x-stainless-runtime", "node")
	proxyReq.Header.Set("x-stainless-runtime-version", "v24.3.0")
	proxyReq.Header.Set("x-stainless-os", "MacOS")
	proxyReq.Header.Set("x-stainless-arch", "arm64")
	proxyReq.Header.Set("x-stainless-retry-count", "0")
	if isStream {
		proxyReq.Header.Set("accept", "text/event-stream")
	} else {
		proxyReq.Header.Set("accept", "application/json")
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("Error forwarding: %v", err)
		http.Error(w, "Error forwarding request", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	log.Printf("Anthropic response status: %d", resp.StatusCode)

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("Anthropic error: %s", string(respBody))
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	if isStream {
		handleStreamingResponse(w, resp, originalModel)
		return
	}
	handleRegularResponse(w, resp, originalModel)
}

// ---- OpenAI response structures ----

type OAIResponse struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []OAIChoice `json:"choices"`
	Usage   OAIUsage    `json:"usage"`
}

type OAIChoice struct {
	Index        int        `json:"index"`
	Message      OAIMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

type OAIMessage struct {
	Role      string        `json:"role"`
	Content   string        `json:"content,omitempty"`
	ToolCalls []OAIToolCall `json:"tool_calls,omitempty"`
}

type OAIToolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type OAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OAIStreamChunk struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []OAIStreamChoice `json:"choices"`
}

type OAIStreamChoice struct {
	Index        int      `json:"index"`
	Delta        OAIDelta `json:"delta"`
	FinishReason *string  `json:"finish_reason"`
}

type OAIDelta struct {
	Role      string        `json:"role,omitempty"`
	Content   string        `json:"content,omitempty"`
	ToolCalls []OAIToolCall `json:"tool_calls,omitempty"`
}

// handleRegularResponse converts Anthropic response to OpenAI format
func handleRegularResponse(w http.ResponseWriter, resp *http.Response, originalModel string) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Error reading response", http.StatusInternalServerError)
		return
	}
	var aResp map[string]interface{}
	if err := json.Unmarshal(body, &aResp); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	msgID, _ := aResp["id"].(string)
	stopReason, _ := aResp["stop_reason"].(string)

	var textParts []string
	var toolCalls []OAIToolCall
	if content, ok := aResp["content"].([]interface{}); ok {
		for _, c := range content {
			block, _ := c.(map[string]interface{})
			if block == nil {
				continue
			}
			switch block["type"] {
			case "text":
				if t, ok := block["text"].(string); ok {
					textParts = append(textParts, t)
				}
			case "tool_use":
				inputJSON, _ := json.Marshal(block["input"])
				toolCalls = append(toolCalls, OAIToolCall{
					ID:   getString(block, "id"),
					Type: "function",
					Function: struct {
						Name      string `json:"name,omitempty"`
						Arguments string `json:"arguments,omitempty"`
					}{
						Name:      getString(block, "name"),
						Arguments: string(inputJSON),
					},
				})
			}
		}
	}

	msg := OAIMessage{Role: "assistant", Content: strings.Join(textParts, "\n")}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}

	var promptTokens, completionTokens int
	if usage, ok := aResp["usage"].(map[string]interface{}); ok {
		promptTokens = int(getFloat(usage, "input_tokens"))
		completionTokens = int(getFloat(usage, "output_tokens"))
	}

	oaiResp := OAIResponse{
		ID:      msgID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   originalModel,
		Choices: []OAIChoice{{
			Index:        0,
			Message:      msg,
			FinishReason: convertStopReason(stopReason),
		}},
		Usage: OAIUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}

	out, _ := json.Marshal(oaiResp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(out)
}

// handleStreamingResponse converts Anthropic SSE → OpenAI SSE
func handleStreamingResponse(w http.ResponseWriter, resp *http.Response, originalModel string) {

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)
	created := time.Now().Unix()

	var msgID string

	// Track tool_use blocks by content index
	type toolBlock struct {
		id       string
		name     string
		oaiIndex int
	}
	toolBlocks := map[int]*toolBlock{}
	toolCallCount := 0

	sendChunk := func(chunk OAIStreamChunk) {
		data, err := json.Marshal(chunk)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	reader := bufio.NewReader(resp.Body)
	var eventType string

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("Stream read error: %v", err)
			break
		}
		line = strings.TrimRight(line, "\r\n")

		if line == "" {
			eventType = ""
			continue
		}
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch eventType {
		case "message_start":
			if msg, ok := event["message"].(map[string]interface{}); ok {
				msgID, _ = msg["id"].(string)
			}
			// Send initial role chunk
			sendChunk(OAIStreamChunk{
				ID: msgID, Object: "chat.completion.chunk", Created: created, Model: originalModel,
				Choices: []OAIStreamChoice{{Index: 0, Delta: OAIDelta{Role: "assistant"}}},
			})

		case "content_block_start":
			idx := int(getFloat(event, "index"))
			cb, _ := event["content_block"].(map[string]interface{})
			if cb == nil {
				continue
			}
			if getString(cb, "type") == "tool_use" {
				oaiIdx := toolCallCount
				toolCallCount++
				toolBlocks[idx] = &toolBlock{
					id:       getString(cb, "id"),
					name:     getString(cb, "name"),
					oaiIndex: oaiIdx,
				}
				sendChunk(OAIStreamChunk{
					ID: msgID, Object: "chat.completion.chunk", Created: created, Model: originalModel,
					Choices: []OAIStreamChoice{{Index: 0, Delta: OAIDelta{
						ToolCalls: []OAIToolCall{{
							Index: &oaiIdx, ID: toolBlocks[idx].id, Type: "function",
							Function: struct {
								Name      string `json:"name,omitempty"`
								Arguments string `json:"arguments,omitempty"`
							}{Name: toolBlocks[idx].name, Arguments: ""},
						}},
					}}},
				})
			}

		case "content_block_delta":
			idx := int(getFloat(event, "index"))
			delta, _ := event["delta"].(map[string]interface{})
			if delta == nil {
				continue
			}
			switch getString(delta, "type") {
			case "text_delta":
				text, _ := delta["text"].(string)
				sendChunk(OAIStreamChunk{
					ID: msgID, Object: "chat.completion.chunk", Created: created, Model: originalModel,
					Choices: []OAIStreamChoice{{Index: 0, Delta: OAIDelta{Content: text}}},
				})
			case "input_json_delta":
				partial, _ := delta["partial_json"].(string)
				if tb, ok := toolBlocks[idx]; ok {
					oaiIdx := tb.oaiIndex
					sendChunk(OAIStreamChunk{
						ID: msgID, Object: "chat.completion.chunk", Created: created, Model: originalModel,
						Choices: []OAIStreamChoice{{Index: 0, Delta: OAIDelta{
							ToolCalls: []OAIToolCall{{
								Index: &oaiIdx,
								Function: struct {
									Name      string `json:"name,omitempty"`
									Arguments string `json:"arguments,omitempty"`
								}{Arguments: partial},
							}},
						}}},
					})
				}
			}

		case "message_delta":
			delta, _ := event["delta"].(map[string]interface{})
			if delta == nil {
				continue
			}
			stopReason, _ := delta["stop_reason"].(string)
			finishReason := convertStopReason(stopReason)
			sendChunk(OAIStreamChunk{
				ID: msgID, Object: "chat.completion.chunk", Created: created, Model: originalModel,
				Choices: []OAIStreamChoice{{Index: 0, Delta: OAIDelta{}, FinishReason: &finishReason}},
			})

		case "message_stop":
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func convertStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	default:
		return "stop"
	}
}

func getFloat(m map[string]interface{}, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func handleModelsRequest(w http.ResponseWriter) {
	response := map[string]interface{}{
		"object": "list",
		"data": []map[string]interface{}{
			{"id": "claude-opus-4-6", "object": "model", "created": time.Now().Unix(), "owned_by": "anthropic"},
			{"id": "claude-sonnet-4-6", "object": "model", "created": time.Now().Unix(), "owned_by": "anthropic"},
			{"id": "claude-sonnet-4-5", "object": "model", "created": time.Now().Unix(), "owned_by": "anthropic"},
			{"id": "claude-haiku-4-5-20251001", "object": "model", "created": time.Now().Unix(), "owned_by": "anthropic"},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
