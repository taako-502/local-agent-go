package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
)

// TestLiveAgent は明示指定時だけ実際のWorkerとモデルで3種類の依頼を順番に検証します。
func TestLiveAgent(t *testing.T) {
	if os.Getenv("LOCAL_AGENT_LIVE") != "1" {
		t.Skip("LOCAL_AGENT_LIVE=1で実モデルを検証")
	}
	queue := os.Getenv("LOCAL_AGENT_QUEUE")
	if queue == "" {
		queue = "local-docs"
	}
	c, err := client.Dial(client.Options{HostPort: "127.0.0.1:7233"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, tc := range []struct {
		name, question string
		want           []string
		read, hello    bool
	}{
		{"資料", "資料に書かれた社内資料管理APIの起動手順とヘルスチェック方法を教えて", []string{"docker compose", "cmd/migrate", "cmd/api", "9090", "healthz", "setup.md"}, true, false},
		{"API", "say_helloで挨拶APIを呼んで、返ってきたメッセージを教えて", []string{"Hello, World!", "say_hello"}, false, true},
		{"複合", "挨拶APIを呼んで、資料の内容と合わせて説明して", []string{"Hello, World!", "9090", "healthz"}, true, true},
	} {
		if !t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			id := fmt.Sprintf("live-agent-%d", time.Now().UnixNano())
			started := time.Now()
			run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: id, TaskQueue: queue, WorkflowExecutionTimeout: 3 * time.Minute}, Investigate, Input{Question: tc.question, Steps: 6})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				info, err := c.DescribeWorkflowExecution(cleanup, id, run.GetRunID())
				if err == nil && info.WorkflowExecutionInfo.Status == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
					c.TerminateWorkflow(cleanup, id, run.GetRunID(), "実モデル検証終了")
				}
			}()
			var answer string
			if err := run.Get(ctx, &answer); err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(answer, want) {
					t.Errorf("answer missing %q: %s", want, answer)
				}
			}
			counts := map[string]int{}
			history := c.GetWorkflowHistory(ctx, id, run.GetRunID(), false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
			for history.HasNext() {
				event, err := history.Next()
				if err != nil {
					t.Fatal(err)
				}
				if a := event.GetActivityTaskScheduledEventAttributes(); a != nil {
					counts[a.ActivityType.Name]++
				}
			}
			if (counts["Read"] > 0) != tc.read || (counts["SayHello"] > 0) != tc.hello || counts["Answer"] != 1 {
				t.Errorf("unexpected activities: %v", counts)
			}
			t.Logf("workflow=%s elapsed=%s activities=%v\n%s", id, time.Since(started).Round(time.Millisecond), counts, answer)
		}) {
			break
		}
	}
}
