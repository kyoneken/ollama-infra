package proxy_test

import (
"bytes"
"encoding/json"
"fmt"
"io"
"net/http"
"net/http/httptest"
"testing"

"github.com/kyoneken/ollama-infra/internal/proxy"
)

func TestProxyStripsToolsAndSystemMessage(t *testing.T) {
var received []byte
upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
received, _ = io.ReadAll(r.Body)
fmt.Fprintln(w, `{"choices":[{"message":{"content":"ok"}}]}`)
}))
defer upstream.Close()

prx := proxy.New(upstream.URL)
port, err := prx.Start()
if err != nil {
t.Fatal(err)
}
defer prx.Close()

reqBody := map[string]interface{}{
"model": "test",
"messages": []map[string]string{
{"role": "system", "content": "copilot system prompt"},
{"role": "user", "content": "review this diff"},
},
"tools":       []map[string]string{{"name": "review_diff"}},
"tool_choice": "auto",
}
b, _ := json.Marshal(reqBody)
resp, err := http.Post(fmt.Sprintf("http://localhost:%d/v1/chat/completions", port), "application/json", bytes.NewReader(b))
if err != nil {
t.Fatal(err)
}
defer resp.Body.Close()
io.ReadAll(resp.Body)

var req map[string]interface{}
json.Unmarshal(received, &req)

if _, has := req["tools"]; has {
t.Error("tools field not stripped")
}
if _, has := req["tool_choice"]; has {
t.Error("tool_choice field not stripped")
}

msgs, _ := req["messages"].([]interface{})
for _, m := range msgs {
msg, _ := m.(map[string]interface{})
if role, _ := msg["role"].(string); role == "system" {
t.Error("system message not stripped")
}
}
if len(msgs) != 1 {
t.Errorf("expected 1 message (user only), got %d", len(msgs))
}
t.Logf("PASS: upstream received %d messages, keys: %v", len(msgs), func() []string {
var ks []string
for k := range req {
ks = append(ks, k)
}
return ks
}())
}
