package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/nicopiov/kubewisp/internal/kube"
)

func TestClusterStatus(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	command := NewRootCommand(Dependencies{
		Runner: fakeRunner{},
		Connectivity: initConnectivity{
			report: kube.ConnectivityReport{
				ServerVersion: "v1.32.1",
				Namespace:     "api",
			},
		},
	})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", path, "cluster", "status"})

	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, expected := range []string{
		"Profile: staging",
		"Project: company-staging",
		"Cluster: staging-main",
		"Namespace: api",
		"Kubernetes: v1.32.1",
		"Status: connected",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, output.String())
		}
	}
}
