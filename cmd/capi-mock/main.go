package main

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed assets/deno.png assets/demo.mp4
var fixtures embed.FS

type server struct {
	key        string
	videoPolls int
	videoSeq   atomic.Uint64
	mu         sync.Mutex
	videos     map[string]int
}

func main() {
	s := &server{key: env("CAPI_MOCK_API_KEY", "mock-key"), videoPolls: intEnv("CAPI_MOCK_VIDEO_POLLS", 2), videos: make(map[string]int)}
	addr := env("CAPI_MOCK_ADDR", "127.0.0.1:3221")
	log.Printf("capi-mock listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, s))
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
		writeJSON(w, 200, map[string]string{"status": "ok"})
		return
	}
	if !s.authorized(r) {
		writeError(w, 401, "invalid_api_key", "missing or invalid bearer token")
		return
	}
	if status, err := strconv.Atoi(r.Header.Get("X-Mock-Status")); err == nil && status >= 400 && status <= 599 {
		writeError(w, status, "mock_error", "injected by X-Mock-Status")
		return
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/v1/models":
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{
			model("mock-chat"), model("mock-reasoning"), model("mock-image"), model("mock-video"), model("typesafe-ai/jev"),
		}})
	case r.Method == "GET" && r.URL.Path == "/assets/deno.png":
		s.fixture(w, "assets/deno.png", "image/png")
	case r.Method == "GET" && r.URL.Path == "/assets/demo.mp4":
		s.fixture(w, "assets/demo.mp4", "video/mp4")
	case r.Method == "POST" && r.URL.Path == "/v1/chat/completions":
		s.chat(w, r)
	case r.Method == "POST" && r.URL.Path == "/v1/responses":
		s.responses(w, r)
	case r.Method == "POST" && r.URL.Path == "/v1/messages":
		s.anthropic(w, r)
	case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/v1beta/models/") && strings.HasSuffix(r.URL.Path, ":generateContent"):
		s.gemini(w, r)
	case r.Method == "POST" && (r.URL.Path == "/v1/images/generations" || r.URL.Path == "/v1/images/edits"):
		s.image(w, r)
	case r.Method == "POST" && r.URL.Path == "/v1/systemone":
		s.systemOne(w, r)
	case r.Method == "POST" && r.URL.Path == "/v1/videos":
		s.submitVideo(w, r)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/tasks/"):
		s.pollVideo(w, strings.TrimPrefix(r.URL.Path, "/v1/tasks/"))
	default:
		writeError(w, 404, "not_found", "unsupported mock endpoint")
	}
}

func (s *server) authorized(r *http.Request) bool {
	return strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")) == s.key
}

func (s *server) chat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Content any `json:"content"`
		} `json:"messages"`
	}
	if !decode(w, r, &in) {
		return
	}
	text := env("CAPI_MOCK_CHAT_TEXT", "This is a deterministic response from capi-mock.")
	if in.Stream {
		s.streamChat(w, in.Model, text)
		return
	}
	writeJSON(w, 200, map[string]any{"id": "chatcmpl-mock", "object": "chat.completion", "created": time.Now().Unix(), "model": in.Model, "choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": text}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 8, "completion_tokens": 8, "total_tokens": 16}})
}

func (s *server) streamChat(w http.ResponseWriter, model, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	f, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "stream_unsupported", "streaming unsupported")
		return
	}
	id := "chatcmpl-mock"
	for _, part := range strings.Fields(text) {
		chunk := map[string]any{"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "delta": map[string]string{"content": part + " "}, "finish_reason": nil}}}
		b, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", b)
		f.Flush()
	}
	b, _ := json.Marshal(map[string]any{"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}})
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
	f.Flush()
}

func (s *server) responses(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Model string `json:"model"`
	}
	if !decode(w, r, &in) {
		return
	}
	text := env("CAPI_MOCK_CHAT_TEXT", "This is a deterministic response from capi-mock.")
	writeJSON(w, 200, map[string]any{"id": "resp_mock", "object": "response", "created_at": time.Now().Unix(), "status": "completed", "model": in.Model, "output": []any{map[string]any{"id": "msg_mock", "type": "message", "role": "assistant", "content": []any{map[string]string{"type": "output_text", "text": text}}, "status": "completed"}}, "output_text": text, "usage": map[string]any{"input_tokens": 8, "output_tokens": 8, "total_tokens": 16}})
}

func (s *server) anthropic(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Stream    bool   `json:"stream"`
	}
	if !decode(w, r, &in) {
		return
	}
	text := env("CAPI_MOCK_CHAT_TEXT", "This is a deterministic response from capi-mock.")
	if in.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", `{"type":"message_start","message":{"id":"msg_mock","type":"message","role":"assistant","model":"`+in.Model+`","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":8,"output_tokens":0}}}`)
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		b, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": text}})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", b)
		return
	}
	writeJSON(w, 200, map[string]any{"id": "msg_mock", "type": "message", "role": "assistant", "model": in.Model, "content": []any{map[string]string{"type": "text", "text": text}}, "stop_reason": "end_turn", "stop_sequence": nil, "usage": map[string]int{"input_tokens": 8, "output_tokens": 8}})
}

func (s *server) gemini(w http.ResponseWriter, r *http.Request) {
	var in struct {
		GenerationConfig struct {
			ResponseMIMEType string `json:"response_mime_type"`
		} `json:"generationConfig"`
	}
	if !decode(w, r, &in) {
		return
	}
	text := env("CAPI_MOCK_CHAT_TEXT", "This is a deterministic response from capi-mock.")
	writeJSON(w, 200, map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]string{"text": text}}}, "finishReason": "STOP", "index": 0}}, "usageMetadata": map[string]int{"promptTokenCount": 8, "candidatesTokenCount": 8, "totalTokenCount": 16}})
}

func (s *server) image(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt string `json:"prompt"`
	}
	if !decode(w, r, &in) {
		return
	}
	img, _ := fixtures.ReadFile("assets/deno.png")
	writeJSON(w, 200, map[string]any{"created": time.Now().Unix(), "data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(img), "revised_prompt": in.Prompt}}})
}

func (s *server) systemOne(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Model     string                    `json:"model"`
		Questions map[string]map[string]any `json:"questions"`
	}
	if !decode(w, r, &in) {
		return
	}
	confidence := floatEnv("CAPI_MOCK_CONFIDENCE", 0.9)
	answers := make(map[string]any, len(in.Questions))
	for id, q := range in.Questions {
		switch q["type"] {
		case "boolean":
			answers[id] = map[string]any{"type": "boolean", "probability": confidence}
		case "noul":
			answers[id] = map[string]any{"type": "noul", "noul": confidence}
		case "string":
			answers[id] = map[string]any{"type": "string", "value": "mock answer"}
		case "number":
			answers[id] = map[string]any{"type": "number", "value": confidence}
		default:
			answers[id] = map[string]any{"type": q["type"], "value": "mock answer"}
		}
	}
	writeJSON(w, 200, map[string]any{"model": in.Model, "answers": answers})
}

func (s *server) submitVideo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Model string `json:"model"`
	}
	if !decode(w, r, &in) {
		return
	}
	id := fmt.Sprintf("mock_video_%d", s.videoSeq.Add(1))
	s.mu.Lock()
	s.videos[id] = 0
	s.mu.Unlock()
	writeJSON(w, 202, map[string]any{"id": id, "task_id": id, "object": "video.task", "model": in.Model, "status": "queued"})
}

func (s *server) pollVideo(w http.ResponseWriter, id string) {
	s.mu.Lock()
	polls, ok := s.videos[id]
	if ok {
		polls++
		s.videos[id] = polls
	}
	s.mu.Unlock()
	if !ok {
		writeError(w, 404, "task_not_found", "unknown mock video task")
		return
	}
	status := "processing"
	if polls > s.videoPolls {
		status = "succeeded"
	}
	result := map[string]any{"id": id, "status": status, "progress": min(polls*100/max(s.videoPolls+1, 1), 100)}
	if status == "succeeded" {
		result["demo_url"] = "/assets/demo.mp4"
	}
	writeJSON(w, 200, result)
}

func (s *server) fixture(w http.ResponseWriter, name, contentType string) {
	data, err := fixtures.ReadFile(name)
	if err != nil {
		writeError(w, 404, "not_found", "demo asset not found")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

func model(id string) map[string]any {
	return map[string]any{"id": id, "object": "model", "created": 0, "owned_by": "capi-mock"}
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(target); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": message, "type": code, "code": code}})
}
func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func intEnv(k string, fallback int) int {
	n, err := strconv.Atoi(os.Getenv(k))
	if err == nil && n >= 0 {
		return n
	}
	return fallback
}
func floatEnv(k string, fallback float64) float64 {
	n, err := strconv.ParseFloat(os.Getenv(k), 64)
	if err == nil && n >= 0 && n <= 1 {
		return n
	}
	return fallback
}
