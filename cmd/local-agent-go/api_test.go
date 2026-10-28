package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go.temporal.io/sdk/testsuite"
)

// TestAPIWorkflow は形式修正を経て、HTTP APIの結果が最終回答まで渡ることを確認します。
func TestAPIWorkflow(t *testing.T) {
	api := httptest.NewServer(sampleAPI())
	defer api.Close()
	var calls atomic.Int32
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Messages []Message }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		call := calls.Add(1)
		if call == 1 {
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"say_hello"} {"action":"finish","answer":"未実行の回答"}`}, "finish_reason": "stop"}}})
			return
		}
		d := Decision{Action: "say_hello"}
		if call > 2 {
			last := req.Messages[len(req.Messages)-1].Content
			if !strings.Contains(last, "Hello, World!") {
				t.Errorf("missing real API observation: %s", last)
			}
			d = Decision{Action: "finish", Answer: "Hello, World!"}
		}
		raw, _ := json.Marshal(d)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: string(raw)}, "finish_reason": "stop"}}})
	}))
	defer llm.Close()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivity(&Activities{Endpoint: llm.URL, APIBase: api.URL, Model: "test"})
	env.ExecuteWorkflow(Investigate, Input{Question: "挨拶APIを呼んで", Steps: 3})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var result string
	if err := env.GetWorkflowResult(&result); err != nil || !strings.Contains(result, "Hello, World!") {
		t.Fatalf("%q %v", result, err)
	}
	if calls.Load() != 3 {
		t.Fatal("expected three LLM calls including format correction")
	}
}

// TestAPIResponses はHTTPステータスに応じた結果の扱いと外部接続先の拒否を確認します。
func TestAPIResponses(t *testing.T) {
	for _, status := range []int{200, 404, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/hello" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
				w.WriteHeader(status)
				w.Write([]byte(`{"status":"Hello, World!"}`))
			}))
			defer server.Close()
			a := &Activities{APIBase: server.URL}
			result, err := a.SayHello(context.Background())
			if status == 429 || status >= 500 {
				if err == nil {
					t.Fatal("expected retryable error")
				}
			} else if err != nil {
				t.Fatal(err)
			} else if status == 404 && !strings.HasPrefix(result, "Tool error:") {
				t.Fatal(result)
			}
		})
	}
	a := &Activities{APIBase: "http://example.com"}
	result, _ := a.SayHello(context.Background())
	if !strings.HasPrefix(result, "Tool error:") {
		t.Fatal("external origin accepted")
	}
}
