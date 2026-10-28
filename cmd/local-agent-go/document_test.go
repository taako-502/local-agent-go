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
)

// TestDocumentWorkflow は一覧にある資料を選択し、実際に読んだ内容から回答する流れを確認します。
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
		catalog := req.Messages[len(req.Messages)-1].Content
		if !strings.Contains(catalog, "Document root entries") || !strings.Contains(catalog, "custom-manual.txt") {
			t.Errorf("document catalog missing: %s", catalog)
		}
		d := Decision{Action: "read_file", Path: "custom-manual.txt"}
		if calls > 1 {
			observed := false
			for _, message := range req.Messages {
				if strings.HasPrefix(message.Content, "Tool observation") && strings.Contains(message.Content, "API port 7171") {
					observed = true
				}
			}
			if !observed {
				t.Error("actual document content missing")
			}
			d = Decision{Action: "finish", Answer: "7171 /ready [custom-manual.txt]"}
		}
		raw, _ := json.Marshal(d)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: string(raw)}, "finish_reason": "stop"}}})
	}))
	defer llm.Close()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivity(&Activities{Root: root, Endpoint: llm.URL, Model: "test"})
	env.ExecuteWorkflow(Investigate, Input{Question: "APIのポートとヘルスチェックは？", Steps: 3})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var answer string
	if err := env.GetWorkflowResult(&answer); err != nil || answer != "7171 /ready [custom-manual.txt]" {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
	if calls != 2 {
		t.Fatalf("expected two LLM calls, got %d", calls)
	}
}
