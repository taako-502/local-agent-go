package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// TestDocumentWorkflow は挨拶APIの誤選択を拒否し、資料を読んだ結果から回答することを確認します。
func TestDocumentWorkflow(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "custom-manual.txt"), []byte("API port 7171. Health check: GET /ready."), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	calls := 0
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct{ Messages []Message }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if req.Messages[0].Content == answerPrompt {
			if !strings.Contains(req.Messages[1].Content, "API port 7171") {
				t.Error("missing evidence in answer request")
			}
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "7171 /ready [custom-manual.txt]"}, "finish_reason": "stop"}}})
			return
		}
		catalog := ""
		for _, message := range req.Messages {
			if message.Role == "system" && strings.Contains(message.Content, "say_hello") {
				t.Error("greeting tool advertised for document question")
			}
			if strings.HasPrefix(message.Content, "Original user request:") {
				catalog = message.Content
			}
		}
		if !strings.Contains(catalog, "Document root entries") || !strings.Contains(catalog, "custom-manual.txt") {
			t.Errorf("document catalog missing: %s", catalog)
		}
		d := Decision{Action: "read_file", Path: "custom-manual.txt"}
		if calls == 1 {
			d = Decision{Action: "say_hello"}
		}
		if calls == 2 && !strings.Contains(req.Messages[len(req.Messages)-1].Content, "tool not allowed") {
			t.Error("missing tool rejection feedback")
		}
		if calls > 2 {
			observed := false
			for _, message := range req.Messages {
				if strings.HasPrefix(message.Content, "Tool observation") && strings.Contains(message.Content, "API port 7171") {
					observed = true
				}
			}
			if !observed {
				t.Error("actual document content missing")
			}
			d = Decision{Action: "finish"}
		}
		raw, _ := json.Marshal(d)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: string(raw)}, "finish_reason": "stop"}}})
	}))
	defer llm.Close()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.OnGetVersion("common-tools", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("document request must not call greeting API")
		w.Write([]byte(`{"message":"Hello, World!"}`))
	}))
	defer api.Close()
	env.RegisterActivity(&Activities{Root: root, Endpoint: llm.URL, APIBase: api.URL, Model: "test"})
	env.ExecuteWorkflow(Investigate, Input{Question: "資料に書かれた社内資料管理APIの起動手順とヘルスチェック方法を教えて", Steps: 3})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var answer string
	if err := env.GetWorkflowResult(&answer); err != nil || answer != "7171 /ready [custom-manual.txt]" {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
	if calls != 4 {
		t.Fatalf("expected four LLM calls including rejected tool and prose answer, got %d", calls)
	}
}
