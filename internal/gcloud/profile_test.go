package gcloud

import (
	"context"
	"testing"

	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/kube"
	"github.com/nicopiov/kubewisp/internal/runner"
)

type profileConnectivity struct {
	namespace string
}

func (c *profileConnectivity) Check(_ context.Context, namespace string) (kube.ConnectivityReport, error) {
	c.namespace = namespace
	return kube.ConnectivityReport{}, nil
}

type profileResetter struct {
	calls int
}

func (r *profileResetter) Reset() {
	r.calls++
}

func TestProfileConnectorActivatesCredentialsAndResetsClient(t *testing.T) {
	t.Parallel()

	commandRunner := &fakeRunner{results: []runner.CommandResult{{}, {}}}
	connectivity := &profileConnectivity{}
	resetter := &profileResetter{}
	connector := NewProfileConnector(NewClient(commandRunner), connectivity, resetter)
	profile := config.Profile{
		ProjectID: "company-production", ClusterName: "production-main",
		LocationType: config.LocationRegion, Location: "europe-west1",
		DefaultNamespace: "default", CurrentNamespace: "api",
	}

	if err := connector.Connect(context.Background(), profile); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if resetter.calls != 1 || connectivity.namespace != "api" {
		t.Fatalf("reset calls = %d, namespace = %q", resetter.calls, connectivity.namespace)
	}
}
