package kubectl

import (
	"bytes"
	"context"
	"reflect"
	"testing"
)

func TestExecBuildsKubectlCommand(t *testing.T) {
	t.Parallel()

	commandRunner := &fakeRunner{}
	service := NewService(commandRunner)
	err := service.Exec(
		context.Background(),
		nil,
		&bytes.Buffer{},
		&bytes.Buffer{},
		ExecOptions{Namespace: "api", Pod: "api-abc", Container: "app", Command: "/bin/sh"},
	)
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}

	want := []string{
		"--namespace", "api",
		"exec", "-it", "pod/api-abc",
		"--container", "app",
		"--", "/bin/sh",
	}
	if commandRunner.name != "kubectl" || !reflect.DeepEqual(commandRunner.args, want) {
		t.Fatalf("command = %s %#v, want kubectl %#v", commandRunner.name, commandRunner.args, want)
	}
}
