package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// TestInvestigationStops は再読と回数上限で回答へ移り、生成失敗でも調査へ戻らないことを確認します。
func TestInvestigationStops(t *testing.T) {
	for _, tc := range []struct {
		name    string
		steps   int
		failure error
	}{
		{"重複読み取り", 10, nil}, {"調査上限", 1, nil},
		{"回答生成失敗", 10, errors.New("生成エラー")},
		{"回答期限", 10, temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_START_TO_CLOSE, nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			env.OnGetVersion("common-tools", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
			a := &Activities{}
			decision := Decision{Action: "read_file", Path: "setup.md"}
			calls := 2
			if tc.steps == 1 {
				calls = 1
			}
			env.OnActivity(a.Decide, mock.Anything, mock.Anything).Return(decision, nil).Times(calls)
			env.OnActivity(a.Read, mock.Anything, decision).Return("port 9090", nil).Once()
			env.OnActivity(a.Answer, mock.Anything, AnswerInput{Question: "port?", Evidence: []Evidence{{Tool: "read_file", Path: "setup.md", Result: "port 9090"}}}).Return(func(context.Context, AnswerInput) (string, error) {
				value, err := env.QueryWorkflow("progress")
				var phase string
				if err != nil || value.Get(&phase) != nil || !strings.Contains(phase, "回答生成中") {
					t.Errorf("progress=%q err=%v", phase, err)
				}
				return "9090 [setup.md]", tc.failure
			}).Once()
			env.ExecuteWorkflow(Investigate, Input{Question: "port?", Steps: tc.steps})
			if err := env.GetWorkflowError(); err != nil {
				t.Fatal(err)
			}
			var result string
			if err := env.GetWorkflowResult(&result); err != nil {
				t.Fatal(err)
			}
			if tc.failure == nil && result != "9090 [setup.md]" {
				t.Fatal(result)
			}
			if tc.failure != nil && (!strings.Contains(result, "回答を生成できませんでした") || !strings.Contains(result, "setup.md")) {
				t.Fatal(result)
			}
			env.AssertExpectations(t)
		})
	}
}

// TestFailedReadNotCached は失敗した読み取りを成功済みとして扱わず、回数上限で終了することを確認します。
func TestFailedReadNotCached(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.OnGetVersion("common-tools", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	a := &Activities{}
	env.OnActivity(a.Decide, mock.Anything, mock.Anything).Return(Decision{Action: "read_file", Path: "missing.md"}, nil).Twice()
	env.OnActivity(a.Read, mock.Anything, mock.Anything).Return("Tool error: path not found", nil).Twice()
	env.ExecuteWorkflow(Investigate, Input{Question: "資料は？", Steps: 2})
	if env.GetWorkflowError() == nil {
		t.Fatal("expected no evidence error")
	}
	env.AssertExpectations(t)
}

// TestAnswer は文章生成専用の入力、空回答、操作JSON、通信失敗とキャンセルを確認します。
func TestAnswer(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		status        int
		fail          bool
	}{
		{"文章", "9090です [setup.md]", 200, false}, {"空回答", " ", 200, true},
		{"操作JSON", `{"action":"read_file","path":"setup.md"}`, 200, true},
		{"通信失敗", "", 503, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var req struct{ Messages []Message }
				json.NewDecoder(r.Body).Decode(&req)
				if len(req.Messages) != 2 || req.Messages[0].Content != answerPrompt || !strings.Contains(req.Messages[1].Content, "port 9090") {
					t.Error("missing answer-only evidence")
				}
				w.WriteHeader(tc.status)
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{"assistant", tc.content}, "finish_reason": "stop"}}})
			}))
			defer server.Close()
			a := &Activities{Endpoint: server.URL}
			_, err := a.Answer(context.Background(), AnswerInput{Question: "port?", Evidence: []Evidence{{Tool: "read_file", Path: "setup.md", Result: "port 9090"}}})
			if (err != nil) != tc.fail || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := a.Answer(ctx, AnswerInput{}); err == nil {
				t.Error("cancellation ignored")
			}
		})
	}
}

// TestAnswerCancellation は回答生成中の取消しを成功結果に変換しないことを確認します。
func TestAnswerCancellation(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.OnGetVersion("common-tools", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	a := &Activities{}
	env.OnActivity(a.Decide, mock.Anything, mock.Anything).Return(Decision{Action: "read_file", Path: "setup.md"}, nil).Once()
	env.OnActivity(a.Read, mock.Anything, mock.Anything).Return("port 9090", nil).Once()
	env.OnActivity(a.Answer, mock.Anything, mock.Anything).After(time.Hour).Return("unused", nil)
	env.RegisterDelayedCallback(func() { env.CancelWorkflow() }, time.Second)
	env.ExecuteWorkflow(Investigate, Input{Question: "port?", Steps: 1})
	if !temporal.IsCanceledError(env.GetWorkflowError()) {
		t.Fatalf("expected canceled: %v", env.GetWorkflowError())
	}
}

// TestProgressDuringRead は取得中の資料名がQueryから確認できることを確認します。
func TestProgressDuringRead(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.OnGetVersion("common-tools", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	a := &Activities{}
	env.OnActivity(a.Decide, mock.Anything, mock.Anything).Return(Decision{Action: "read_file", Path: "setup.md"}, nil).Once()
	env.OnActivity(a.Read, mock.Anything, mock.Anything).After(time.Minute).Return("port 9090", nil).Once()
	env.OnActivity(a.Answer, mock.Anything, mock.Anything).Return("9090", nil).Once()
	env.RegisterDelayedCallback(func() {
		value, err := env.QueryWorkflow("progress")
		var status string
		if err != nil || value.Get(&status) != nil || status != "資料取得中：read_file:setup.md" {
			t.Errorf("status=%q err=%v", status, err)
		}
	}, time.Second)
	env.ExecuteWorkflow(Investigate, Input{Question: "port?", Steps: 1})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	env.AssertExpectations(t)
}

// TestLegacyVersion は更新前の履歴が従来の回答経路を使用することを確認します。
func TestLegacyVersion(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.OnGetVersion("separate-answer", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	a := &Activities{}
	env.OnActivity(a.Decide, mock.Anything, mock.Anything).Return(Decision{Action: "read_file", Path: "setup.md"}, nil).Once()
	env.OnActivity(a.Read, mock.Anything, mock.Anything).Return("9090", nil).Once()
	env.OnActivity(a.Decide, mock.Anything, mock.Anything).Return(Decision{Action: "finish", Answer: "legacy"}, nil).Once()
	env.ExecuteWorkflow(Investigate, Input{Question: "port?", Steps: 2})
	var result string
	if err := env.GetWorkflowResult(&result); err != nil || result != "legacy" {
		t.Fatalf("%q %v", result, err)
	}
	env.AssertExpectations(t)
}
