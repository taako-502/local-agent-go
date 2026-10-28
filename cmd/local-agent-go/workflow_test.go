package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// TestAgentLoop は資料の読み取り結果を経てWorkflowが最終回答を返すことを確認します。
func TestAgentLoop(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.OnGetVersion("separate-answer", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	a := &Activities{}
	env.OnActivity(a.Decide, mock.Anything, mock.Anything).Return(func(context.Context, []Message) (Decision, error) {
		return Decision{Action: "read_file", Path: "setup.md"}, nil
	}).Once()
	env.OnActivity(a.Read, mock.Anything, Decision{Action: "read_file", Path: "setup.md"}).Return("port 9090", nil).Once()
	env.OnActivity(a.Decide, mock.Anything, mock.Anything).Return(Decision{Action: "finish", Answer: "9090 [setup.md]"}, nil).Once()
	env.ExecuteWorkflow(Investigate, Input{Question: "port?", Steps: 3})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var result string
	if err := env.GetWorkflowResult(&result); err != nil || result != "9090 [setup.md]" {
		t.Fatalf("%q %v", result, err)
	}
	env.AssertExpectations(t)
}

// TestStepLimit は判断回数の上限に達するとWorkflowがエラーになることを確認します。
func TestStepLimit(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.OnGetVersion("separate-answer", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	a := &Activities{}
	env.OnActivity(a.Decide, mock.Anything, mock.Anything).Return(Decision{Action: "list_files", Path: "."}, nil)
	env.OnActivity(a.Read, mock.Anything, mock.Anything).Return("[]", nil)
	env.ExecuteWorkflow(Investigate, Input{Question: "question", Steps: 2})
	if env.GetWorkflowError() == nil {
		t.Fatal("expected step limit error")
	}
}
