package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// TestAPIWorkflow は資料フォルダがあっても元のAPI呼び出し依頼と実行結果がLLMに渡ることを確認します。
func TestAPIWorkflow(t *testing.T) {
	for _, emptyAnswer := range []bool{false, true} {
		t.Run(map[bool]string{false: "API結果を直接返す", true: "旧履歴の空回答から結果を復元"}[emptyAnswer], func(t *testing.T) {
			root, err := os.OpenRoot("../../examples/docs")
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			var apiCalls atomic.Int32
			handler := sampleAPI()
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				apiCalls.Add(1)
				handler.ServeHTTP(w, r)
			}))
			defer api.Close()
			var calls atomic.Int32
			llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct{ Messages []Message }
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				requestPresent := false
				for _, message := range req.Messages {
					if strings.HasPrefix(message.Content, "Original user request:") &&
						strings.Contains(message.Content, "挨拶APIを呼んで") &&
						strings.Contains(message.Content, "Document root entries") {
						requestPresent = true
					}
				}
				if !requestPresent {
					t.Error("original API request missing from document context")
				}
				call := calls.Add(1)
				if call == 1 {
					json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"say_hello"} {"action":"finish","answer":"未実行の回答"}`}, "finish_reason": "stop"}}})
					return
				}
				d := Decision{Action: "say_hello"}
				if call > 2 {
					observed := false
					for _, message := range req.Messages {
						if strings.HasPrefix(message.Content, "Tool observation") && strings.Contains(message.Content, "Hello, World!") {
							observed = true
						}
					}
					if !observed {
						t.Error("missing real API observation")
					}
					d = Decision{Action: "finish", Answer: "Hello, World!"}
					if emptyAnswer {
						d.Answer = ""
					}
				}
				raw, _ := json.Marshal(d)
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: string(raw)}, "finish_reason": "stop"}}})
			}))
			defer llm.Close()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			env.OnGetVersion("common-tools", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
			if emptyAnswer {
				env.OnGetVersion("separate-answer", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
				env.OnGetVersion("hello-direct-result", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
			}
			env.RegisterActivity(&Activities{Root: root, Endpoint: llm.URL, APIBase: api.URL, Model: "test"})
			env.ExecuteWorkflow(Investigate, Input{Question: "挨拶APIを呼んで", Steps: 3})
			if err := env.GetWorkflowError(); err != nil {
				t.Fatal(err)
			}
			var result string
			if err := env.GetWorkflowResult(&result); err != nil || !strings.Contains(result, "Hello, World!") {
				t.Fatalf("%q %v", result, err)
			}
			expectedCalls := int32(2)
			if !emptyAnswer && result != "Hello, World!" {
				t.Fatalf("unexpected direct result: %q", result)
			}
			if emptyAnswer {
				expectedCalls = 5
				if !strings.Contains(result, "回答の生成に失敗") {
					t.Fatal("fallback must be explicit")
				}
			}
			if calls.Load() != expectedCalls {
				t.Fatalf("LLM calls=%d, want %d", calls.Load(), expectedCalls)
			}
			if apiCalls.Load() != 1 {
				t.Fatalf("expected one actual API call, got %d", apiCalls.Load())
			}
		})
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
