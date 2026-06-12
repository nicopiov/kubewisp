package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/nicopiov/kubewisp/internal/kube"
)

type fakeEvents struct {
	items     []kube.NamespaceEventSummary
	namespace string
}

func (f *fakeEvents) ListWarnings(_ context.Context, namespace string) ([]kube.NamespaceEventSummary, error) {
	f.namespace = namespace
	return f.items, nil
}

func TestEventsCommandListsSelectedNamespaceWarnings(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	events := &fakeEvents{items: []kube.NamespaceEventSummary{{
		ObjectKind: "Pod",
		ObjectName: "api-123",
		Reason:     "BackOff",
		Message:    "restarting failed container",
		Count:      4,
	}}}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, Events: events})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", path, "events"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if events.namespace != "api" {
		t.Fatalf("namespace = %q, want api", events.namespace)
	}
	for _, want := range []string{"Pod/api-123", "BackOff", "restarting failed container"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, output.String())
		}
	}
}
