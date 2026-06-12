package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/kube"
)

type fakeWorkloads struct {
	items     []kube.WorkloadSummary
	namespace string
	kind      string
	name      string
	details   kube.CronJobDetails
	suspended bool
}

func setProfileProduction(t *testing.T, path string) {
	t.Helper()
	cfg, err := (config.Store{Path: path}).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	profile := cfg.Profiles["staging"]
	profile.Production = true
	cfg.Profiles["staging"] = profile
	if err := (config.Store{Path: path}).Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
}

func (f *fakeWorkloads) List(_ context.Context, namespace string) ([]kube.WorkloadSummary, error) {
	f.namespace = namespace
	return f.items, nil
}

func (f *fakeWorkloads) RolloutRestart(_ context.Context, namespace, kind, name string) error {
	f.namespace = namespace
	f.kind = kind
	f.name = name
	return nil
}

func (f *fakeWorkloads) DescribeCronJob(_ context.Context, namespace, name string) (kube.CronJobDetails, error) {
	f.namespace = namespace
	f.name = name
	return f.details, nil
}

func (f *fakeWorkloads) Describe(_ context.Context, namespace, kind, name string) (kube.WorkloadDetails, error) {
	f.namespace = namespace
	f.kind = kind
	f.name = name
	return kube.WorkloadDetails{}, nil
}

func (f *fakeWorkloads) Pods(_ context.Context, namespace, kind, name string) ([]kube.PodSummary, error) {
	f.namespace = namespace
	f.kind = kind
	f.name = name
	return nil, nil
}

func (f *fakeWorkloads) SetCronJobSuspended(_ context.Context, namespace, name string, suspended bool) error {
	f.namespace = namespace
	f.name = name
	f.suspended = suspended
	return nil
}

func TestWorkloadsList(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	workloads := &fakeWorkloads{items: []kube.WorkloadSummary{{
		Kind: "Deployment", Name: "api", Ready: 2, Desired: 3, Updated: 3, Available: 2,
	}, {
		Kind: "CronJob", Name: "cleanup", Schedule: "0 * * * *", Active: 1, Suspended: true,
	}}}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, Workloads: workloads})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", path, "workloads", "list"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{"Deployment", "ready 2/3", "CronJob", "0 * * * *", "active 1, suspended"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, output.String())
		}
	}
}

func TestWorkloadsRestartRejectsCronJob(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	workloads := &fakeWorkloads{items: []kube.WorkloadSummary{{Kind: "CronJob", Name: "cleanup"}}}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, Workloads: workloads})
	command.SetArgs([]string{"--config", path, "workloads", "restart", "CronJob/cleanup"})

	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "does not support rollout restart") {
		t.Fatalf("Execute() error = %v, want unsupported rollout restart", err)
	}
}

func TestWorkloadsRestartProductionRequiresExactReference(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	setProfileProduction(t, path)
	workloads := &fakeWorkloads{items: []kube.WorkloadSummary{{Kind: "Deployment", Name: "api", Ready: 3, Desired: 3}}}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, Workloads: workloads})
	command.SetIn(strings.NewReader("Deployment/api\n"))
	command.SetArgs([]string{"--config", path, "workloads", "restart", "Deployment/api"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if workloads.kind != "Deployment" || workloads.name != "api" {
		t.Fatalf("restart = %s/%s", workloads.kind, workloads.name)
	}
}

func TestCronJobDescribeShowsRecentJobs(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	workloads := &fakeWorkloads{
		items: []kube.WorkloadSummary{{Kind: "CronJob", Name: "cleanup", Schedule: "0 * * * *"}},
		details: kube.CronJobDetails{
			WorkloadSummary:   kube.WorkloadSummary{Kind: "CronJob", Name: "cleanup", Schedule: "0 * * * *"},
			ConcurrencyPolicy: "Forbid",
			Jobs:              []kube.JobSummary{{Name: "cleanup-123", Status: "Completed", Succeeded: 1}},
		},
	}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, Workloads: workloads})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", path, "workloads", "cronjob", "describe", "cleanup"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{"CronJob: cleanup", "Concurrency Policy: Forbid", "cleanup-123", "Completed"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, output.String())
		}
	}
}

func TestCronJobSuspendProductionRequiresExactReference(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	setProfileProduction(t, path)
	workloads := &fakeWorkloads{
		items: []kube.WorkloadSummary{{Kind: "CronJob", Name: "cleanup", Schedule: "0 * * * *"}},
	}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, Workloads: workloads})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetIn(strings.NewReader("CronJob/cleanup\n"))
	command.SetArgs([]string{"--config", path, "workloads", "cronjob", "suspend", "cleanup"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if workloads.name != "cleanup" || !workloads.suspended || !strings.Contains(output.String(), "is now suspended") {
		t.Fatalf("unexpected suspend result name=%q suspended=%t output:\n%s", workloads.name, workloads.suspended, output.String())
	}
}

func TestCronJobResumeChangesSuspendedState(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	workloads := &fakeWorkloads{
		items: []kube.WorkloadSummary{{Kind: "CronJob", Name: "cleanup", Suspended: true}},
	}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, Workloads: workloads})
	command.SetIn(strings.NewReader("y\n"))
	command.SetArgs([]string{"--config", path, "workloads", "cronjob", "resume", "cleanup"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if workloads.suspended {
		t.Fatal("resume kept CronJob suspended")
	}
}
