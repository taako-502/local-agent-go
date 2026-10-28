package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"
)

// TestPlanTools は質問のキーワードに関係なく同じツール定義を渡し、不正な操作を修正することを確認します。
func TestPlanTools(t *testing.T) {
	for _, question := range []string{"資料の起動手順は？", "挨拶APIを呼んで", "挨拶APIと資料を合わせて説明して"} {
		t.Run(question, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var request struct {
					Messages  []Message
					MaxTokens int            `json:"max_tokens"`
					Format    map[string]any `json:"response_format"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				if request.Messages[0].Content != planningPrompt || !strings.Contains(request.Messages[1].Content, question) || request.MaxTokens != 128 || request.Format["type"] != "json_schema" {
					t.Error("invalid planning request")
				}
				content := `{"action":"shell"}`
				if calls == 2 {
					content = `{"action":"read_file","path":"setup.md"}`
				}
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{"assistant", content}, "finish_reason": "stop"}}})
			}))
			defer server.Close()
			a := &Activities{Endpoint: server.URL}
			decision, err := a.Plan(context.Background(), AnswerInput{Question: question})
			if err != nil || decision.Path != "setup.md" || calls != 2 {
				t.Fatalf("%+v %v calls=%d", decision, err, calls)
			}
		})
	}
}

// TestCommonToolsWorkflow はAPIと資料の取得順が異なっても、両方の結果から文章を生成することを確認します。
func TestCommonToolsWorkflow(t *testing.T) {
	for _, apiFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "APIから", false: "資料から"}[apiFirst], func(t *testing.T) {
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			a := &Activities{}
			choices := []Decision{{Action: "say_hello"}, {Action: "read_file", Path: "setup.md"}}
			if !apiFirst {
				choices[0], choices[1] = choices[1], choices[0]
			}
			for _, d := range choices {
				env.OnActivity(a.Plan, mock.Anything, mock.Anything).Return(d, nil).Once()
			}
			env.OnActivity(a.Plan, mock.Anything, mock.Anything).Return(Decision{Action: "finish"}, nil).Once()
			env.OnActivity(a.SayHello, mock.Anything).Return(`{"message":"Hello, World!"}`, nil).Once()
			env.OnActivity(a.Read, mock.Anything, Decision{Action: "read_file", Path: "setup.md"}).Return("port 9090", nil).Once()
			env.OnActivity(a.Answer, mock.Anything, mock.Anything).Return(func(_ context.Context, in AnswerInput) (string, error) {
				if len(in.Evidence) != 2 {
					t.Fatalf("missing evidence: %+v", in)
				}
				data, _ := json.Marshal(in)
				if !strings.Contains(string(data), "Hello, World!") || !strings.Contains(string(data), "9090") {
					t.Error("both results required")
				}
				return "挨拶はHello, World!、資料のAPIは9090です。", nil
			}).Once()
			env.ExecuteWorkflow(Investigate, Input{Question: "あの挨拶サービスと文書を調べて", Steps: 5})
			var result string
			if err := env.GetWorkflowResult(&result); err != nil || !strings.Contains(result, "参照した資料・ツール") {
				t.Fatalf("%q %v", result, err)
			}
			env.AssertExpectations(t)
		})
	}
}

// TestCommonToolsRepeat は新規経路でも重複操作を再実行せず、生成失敗をWorkflowの失敗として返すことを確認します。
func TestCommonToolsRepeat(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "重複停止", true: "生成失敗"}[fail], func(t *testing.T) {
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			a := &Activities{}
			env.OnActivity(a.Plan, mock.Anything, mock.Anything).Return(Decision{Action: "read_file", Path: "setup.md"}, nil).Twice()
			env.OnActivity(a.Read, mock.Anything, mock.Anything).Return("port 9090", nil).Once()
			var generationError error
			if fail {
				generationError = fmt.Errorf("モデル接続失敗")
			}
			env.OnActivity(a.Answer, mock.Anything, mock.Anything).Return("9090", generationError).Once()
			env.ExecuteWorkflow(Investigate, Input{Question: "資料のポートは？", Steps: 6})
			if (env.GetWorkflowError() != nil) != fail {
				t.Fatal(env.GetWorkflowError())
			}
			env.AssertExpectations(t)
		})
	}
}
