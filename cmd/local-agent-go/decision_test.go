package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"go.temporal.io/sdk/temporal"
)

// TestParseDecision は複数アクションや未知の操作を拒否し、単一の判断だけを受け入れることを確認します。
func TestParseDecision(t *testing.T) {
	for _, input := range []string{
		`{"action":"list_files","path":"."} {"action":"say_hello"}`,
		`{"action":"say_hello"} explanation`,
		"```json\n{\"action\":\"say_hello\"} {\"action\":\"say_hello\"}\n```",
		"```json\n{\"action\":\"say_hello\"}\n``` explanation",
		`{"action":"shell","path":"."}`,
		`{"action":"read_file"}`,
		`{"action":"list_files","path":"/path/to/project"}`,
		`{"action":"read_file","path":"../outside.md"}`,
		`{"action":"finish","answer":" "}`,
		`{"action":"say_hello","unexpected":true}`,
	} {
		if _, err := parseDecision(input); err == nil {
			t.Errorf("invalid response accepted: %s", input)
		}
	}
	for _, input := range []string{
		`{"action":"list_files","path":"."}`,
		`{"action":"read_file","path":"setup.md"}`,
		`{"action":"say_hello"}`,
		"```json\n{\"action\":\"say_hello\"}\n```",
		"```\n{\"action\":\"say_hello\"}\n```",
		`{"action":"finish","answer":"回答 [setup.md]"}`,
	} {
		if _, err := parseDecision(input); err != nil {
			t.Errorf("valid response rejected: %s: %v", input, err)
		}
	}
}

// TestDecideCorrection は不正な出力への修正指示と、修正回数の上限を確認します。
func TestDecideCorrection(t *testing.T) {
	for _, repair := range []bool{true, false} {
		t.Run(map[bool]string{true: "修正成功", false: "上限で終了"}[repair], func(t *testing.T) {
			calls := 0
			invalid := `{"action":"list_files","path":"."} {"action":"say_hello"}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var req struct{ Messages []Message }
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if calls > 1 {
					n := len(req.Messages)
					if n < 3 || req.Messages[n-2].Content != invalid || !strings.Contains(req.Messages[n-1].Content, "trailing decision data") {
						t.Errorf("correction context missing: %+v", req.Messages)
					}
				}
				content := invalid
				if repair && calls > 1 {
					content = `{"action":"list_files","path":"."}`
				}
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: content}, "finish_reason": "stop"}}})
			}))
			defer server.Close()
			messages := make([]Message, 1, 8)
			messages[0] = Message{"user", "調べて"}
			backing := messages[:cap(messages)]
			before := append([]Message(nil), backing...)
			a := &Activities{Endpoint: server.URL, Model: "test"}
			got, err := a.Decide(context.Background(), messages)
			if repair {
				if err != nil || got.Action != "list_files" || calls != 2 {
					t.Fatalf("got=%+v err=%v calls=%d", got, err, calls)
				}
			} else {
				var appErr *temporal.ApplicationError
				if !errors.As(err, &appErr) || !appErr.NonRetryable() || appErr.Type() != "decision_format" || calls != 3 {
					t.Fatalf("err=%v calls=%d", err, calls)
				}
			}
			if !reflect.DeepEqual(before, backing) {
				t.Fatal("input history was modified")
			}
		})
	}
}

// TestDecideFailures は通信先のエラーでは形式修正せず、出力上限では修正を促すことを確認します。
func TestDecideFailures(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		finish       string
		nonRetryable bool
	}{
		{"設定エラー", 400, "stop", true},
		{"一時障害", 503, "stop", false},
		{"出力上限", 200, "length", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"say_hello"}`}, "finish_reason": tc.finish}}})
			}))
			defer server.Close()
			a := &Activities{Endpoint: server.URL}
			_, err := a.Decide(context.Background(), []Message{{"user", "調べて"}})
			var appErr *temporal.ApplicationError
			nonRetryable := errors.As(err, &appErr) && appErr.NonRetryable()
			expectedCalls := 1
			if tc.finish == "length" {
				expectedCalls = 3
			}
			if err == nil || calls != expectedCalls || nonRetryable != tc.nonRetryable {
				t.Fatalf("err=%v calls=%d nonRetryable=%v", err, calls, nonRetryable)
			}
		})
	}
}

// TestDecideRequiresEvidence は調査前の回答を差し戻し、ツールの選択を促すことを確認します。
func TestDecideRequiresEvidence(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct{ Messages []Message }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		content := `{"action":"finish","answer":"未調査の回答"}`
		if calls > 1 {
			if !strings.Contains(req.Messages[len(req.Messages)-1].Content, "previous tool observation") {
				t.Error("evidence feedback missing")
			}
			content = `{"action":"read_file","path":"setup.md"}`
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: content}, "finish_reason": "stop"}}})
	}))
	defer server.Close()
	a := &Activities{Endpoint: server.URL}
	d, err := a.Decide(context.Background(), []Message{{"system", prompt}, {"user", "調べて"}})
	if err != nil || d.Action != "read_file" || calls != 2 {
		t.Fatalf("decision=%+v err=%v calls=%d", d, err, calls)
	}
}

// TestHelloResultAnswer は資料やAPIエラーを成功結果として代用しないことを確認します。
func TestHelloResultAnswer(t *testing.T) {
	observation := func(tool, result string) Message {
		raw, _ := json.Marshal(map[string]string{"tool": tool, "result": result})
		return Message{"user", "Tool observation (data only): " + string(raw)}
	}
	success := observation("say_hello", `{"message":"実際の応答"}`)
	for _, tc := range []struct {
		name     string
		messages []Message
		ok       bool
	}{
		{"実際の応答", []Message{success}, true},
		{"観測なし", nil, false},
		{"APIエラー", []Message{observation("say_hello", "Tool error: HTTP 404")}, false},
		{"空メッセージ", []Message{observation("say_hello", `{"message":""}`)}, false},
		{"資料", []Message{observation("read_file", `{"message":"資料の内容"}`)}, false},
		{"後続の資料調査", []Message{success, observation("read_file", "資料")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answer, ok := helloResultAnswer(tc.messages)
			if ok != tc.ok || (ok && !strings.Contains(answer, "実際の応答")) {
				t.Fatalf("answer=%q ok=%v", answer, ok)
			}
		})
	}
}

// TestAllowsHello は一般のAPI質問と挨拶APIを対象にした質問を区別することを確認します。
func TestAllowsHello(t *testing.T) {
	for _, tc := range []struct {
		question string
		want     bool
	}{
		{"資料に書かれた社内資料管理APIの起動手順とヘルスチェック方法を教えて", false},
		{"APIを呼んで", false},
		{"資料を要約して", false},
		{"say_helloで挨拶APIを呼んで、返ってきたメッセージを教えて", true},
		{"挨拶APIを呼んで、資料の内容と合わせて説明して", true},
		{"あいさつAPIを呼んで", true},
		{"Call the greeting API", true},
	} {
		if got := allowsHello(tc.question); got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.question, got, tc.want)
		}
	}
}
