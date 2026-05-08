// Package proxy provides a minimal HTTP proxy that strips OpenAI tool definitions
// from requests before forwarding to an upstream LLM (e.g. Ollama).
//
// This forces the model to respond with plain text rather than tool-call JSON,
// which is necessary when using the copilot CLI in BYOK mode: copilot injects
// its own tool definitions that cause small models to output tool calls instead
// of direct answers.
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// Server is an HTTP proxy that strips tool definitions and forwards to upstream.
type Server struct {
	upstream string
	listener net.Listener
	srv      *http.Server
}

// New creates a Server proxying to the given upstream base URL (e.g. "http://localhost:11434").
// Call Start() to begin listening on a random port.
func New(upstream string) *Server {
	s := &Server{upstream: strings.TrimRight(upstream, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/", s.proxyThrough)
	s.srv = &http.Server{Handler: mux}
	return s
}

// Start begins listening on a random available port and returns the assigned port.
func (s *Server) Start() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("proxy listen: %w", err)
	}
	s.listener = ln
	go s.srv.Serve(ln) //nolint:errcheck
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// Close shuts down the proxy server.
func (s *Server) Close() {
	if s.srv != nil {
		s.srv.Close()
	}
}

// handleChatCompletions strips tool definitions and copilot's system message,
// then forwards to upstream so the model responds with plain text.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err == nil {
		// Strip tool-related fields to prevent the model from outputting tool calls.
		delete(req, "tools")
		delete(req, "tool_choice")

		// Remove copilot's injected system message so it doesn't override our prompt.
		// The user message already contains our system prompt + the diff.
		if msgs, ok := req["messages"].([]interface{}); ok {
			var filtered []interface{}
			for _, m := range msgs {
				msg, ok := m.(map[string]interface{})
				if !ok {
					filtered = append(filtered, m)
					continue
				}
				if role, _ := msg["role"].(string); role == "system" {
					continue // drop copilot's system message
				}
				filtered = append(filtered, m)
			}
			req["messages"] = filtered
		}

		if body, err = json.Marshal(req); err != nil {
			http.Error(w, "re-marshal: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	s.forward(w, r, body)
}

// proxyThrough forwards the request as-is to the upstream.
func (s *Server) proxyThrough(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.forward(w, r, body)
}

func (s *Server) forward(w http.ResponseWriter, r *http.Request, body []byte) {
	url := s.upstream + r.URL.RequestURI()
	req, err := http.NewRequestWithContext(r.Context(), r.Method, url, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "build request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header = r.Header.Clone()
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
}
