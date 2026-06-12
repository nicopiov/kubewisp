package tui

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/doctor"
	"github.com/nicopiov/kubewisp/internal/kube"
)

type fakeConnectivity struct {
	report kube.ConnectivityReport
	err    error
}

func (f fakeConnectivity) Check(context.Context, string) (kube.ConnectivityReport, error) {
	return f.report, f.err
}

type fakeNamespaces struct {
	names []string
}

type fakeDoctor struct {
	report doctor.Report
}

func (f fakeDoctor) Run(context.Context) doctor.Report {
	return f.report
}

func (f fakeNamespaces) List(context.Context) ([]string, error) {
	return f.names, nil
}

func (f fakeNamespaces) Exists(context.Context, string) error {
	return nil
}

type fakePods struct {
	pods       []kube.PodSummary
	details    kube.PodDetails
	containers []string
	ports      []kube.PodPort
	logs       string
}

func (f fakePods) List(context.Context, string) ([]kube.PodSummary, error) {
	return f.pods, nil
}

func (f fakePods) Describe(context.Context, string, string) (kube.PodDetails, error) {
	return f.details, nil
}

func (f fakePods) Containers(context.Context, string, string) ([]string, error) {
	return f.containers, nil
}

func (f fakePods) Ports(context.Context, string, string) ([]kube.PodPort, error) {
	return f.ports, nil
}

func (f fakePods) Logs(context.Context, kube.PodLogsOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f.logs)), nil
}

func testDependencies(t *testing.T) Dependencies {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.CurrentProfile = "staging"
	cfg.Profiles["staging"] = config.Profile{
		Provider:         config.ProviderGKE,
		ProjectID:        "company-staging",
		ClusterName:      "staging-main",
		LocationType:     config.LocationRegion,
		Location:         "europe-west1",
		DefaultNamespace: "api",
	}
	if err := (config.Store{Path: path}).Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	return Dependencies{
		ConfigPath:  path,
		ProfileName: "staging",
		Profile:     cfg.Profiles["staging"],
		Connectivity: fakeConnectivity{report: kube.ConnectivityReport{
			ServerVersion: "v1.32.1",
			Namespace:     "api",
		}},
		Namespaces: fakeNamespaces{names: []string{"api", "workers"}},
		Doctor: fakeDoctor{report: doctor.Report{Checks: []doctor.Check{{
			Dependency: doctor.Dependency{Name: "gcloud", Description: "Google Cloud CLI"},
			Path:       "/usr/local/bin/gcloud",
		}}}},
		Pods: fakePods{pods: []kube.PodSummary{{
			Name:      "api-abc",
			Ready:     "1/1",
			Status:    "Running",
			CreatedAt: time.Now().Add(-time.Hour),
		}, {
			Name:     "worker-abc",
			Ready:    "0/1",
			Status:   "CrashLoopBackOff",
			Restarts: 4,
		}}, details: kube.PodDetails{
			PodSummary: kube.PodSummary{Name: "api-abc", Ready: "1/1", Status: "Running"},
			Namespace:  "api",
			Owners:     []string{"ReplicaSet/api-123"},
			Conditions: []kube.ConditionSummary{{Type: "Ready", Status: "True"}},
			Volumes:    []string{"config (configMap)"},
			Labels:     []string{"app=api"},
			Containers: []kube.ContainerSummary{{
				Name:             "app",
				Image:            "example/api:v1",
				Ready:            true,
				State:            "running",
				Requests:         []string{"cpu=100m"},
				EnvironmentNames: []string{"API_TOKEN"},
			}},
		}, containers: []string{"app"}, ports: []kube.PodPort{{
			Container: "app",
			Name:      "http",
			Port:      8080,
			Protocol:  "TCP",
		}}, logs: "hello from app\n"},
	}
}

func TestPodEnterLoadsDetails(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.pods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	message := command()
	updated, _ = updated.(Model).Update(message)
	final := updated.(Model)

	if final.screen != podDetailsScreen {
		t.Fatalf("screen = %d, want podDetailsScreen", final.screen)
	}
	if !strings.Contains(final.View(), "Pod Details: api-abc") {
		t.Fatalf("view does not contain pod details:\n%s", final.View())
	}
}

func TestPodLogsLoadsSingleContainer(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.pods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	message := command()
	updated, command = updated.(Model).Update(message)
	message = command()
	updated, _ = updated.(Model).Update(message)
	final := updated.(Model)

	if final.screen != podLogsScreen {
		t.Fatalf("screen = %d, want podLogsScreen", final.screen)
	}
	if !strings.Contains(final.View(), "hello from app") {
		t.Fatalf("view does not contain logs:\n%s", final.View())
	}
}

func TestPodPortForwardSelectsDeclaredPort(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.pods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	message := command()
	updated, _ = updated.(Model).Update(message)
	chooser := updated.(Model)

	if chooser.screen != portForwardScreen || !strings.Contains(chooser.View(), "http | 8080/TCP") {
		t.Fatalf("unexpected port-forward chooser:\n%s", chooser.View())
	}
	updated, command = chooser.Update(tea.KeyMsg{Type: tea.KeyEnter})
	final := updated.(Model)
	if command == nil || final.portForward == nil {
		t.Fatal("port-forward selection did not request quit and handoff")
	}
	if final.portForward.Pod != "api-abc" || final.portForward.RemotePort != 8080 {
		t.Fatalf("port-forward = %#v", final.portForward)
	}
}

func TestEscapeReturnsFromPodDetails(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podDetailsScreen

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	final := updated.(Model)

	if final.screen != podScreen {
		t.Fatalf("screen = %d, want podScreen", final.screen)
	}
}

func TestPodLogsScrolls(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podLogsScreen
	model.height = 12
	model.loading = false
	model.logs = "one\ntwo\nthree\nfour\nfive\nsix"

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	final := updated.(Model)

	if final.scroll != 1 {
		t.Fatalf("scroll = %d, want 1", final.scroll)
	}
	if !strings.Contains(final.View(), "lines 2-") {
		t.Fatalf("view does not contain scroll position:\n%s", final.View())
	}
}

func TestDashboardLoadsConnectivity(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	message := model.Init()()
	updated, command := model.Update(message)
	message = command()
	updated, _ = updated.(Model).Update(message)
	final := updated.(Model)

	if final.connectivity.ServerVersion != "v1.32.1" {
		t.Fatalf("ServerVersion = %q, want v1.32.1", final.connectivity.ServerVersion)
	}
	for _, expected := range []string{
		"Profile: staging",
		"Cluster: staging-main",
		"Connection: connected",
		"healthy 1",
		"unhealthy 1",
	} {
		if !strings.Contains(final.View(), expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, final.View())
		}
	}
}

func TestPodListShowsHealthMarkers(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.loading = false
	model.pods = []kube.PodSummary{
		{Name: "api", Ready: "1/1", Status: "Running"},
		{Name: "worker", Ready: "0/1", Status: "CrashLoopBackOff", Restarts: 4},
	}

	view := model.View()
	for _, expected := range []string{"● healthy", "● unhealthy", "api", "worker"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, view)
		}
	}
}

func TestPodDetailsIncludesTroubleshootingSections(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podDetailsScreen
	model.loading = false
	model.podDetails = testDependencies(t).Pods.(fakePods).details

	view := model.View()
	for _, expected := range []string{
		"● healthy",
		"Owners:",
		"ReplicaSet/api-123",
		"Conditions:",
		"Image: example/api:v1",
		"Requests: cpu=100m",
		"Environment names: API_TOKEN",
		"Volumes:",
		"Labels:",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, view)
		}
	}
}

func TestNavigateToPodsAndLoadList(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	message := command()
	updated, _ = updated.(Model).Update(message)
	final := updated.(Model)

	if final.screen != podScreen {
		t.Fatalf("screen = %d, want podScreen", final.screen)
	}
	if !strings.Contains(final.View(), "api-abc") {
		t.Fatalf("view does not contain pod:\n%s", final.View())
	}
}

func TestNavigateToDoctorShowsHealthyReport(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	message := command()
	updated, _ = updated.(Model).Update(message)
	final := updated.(Model)

	if final.screen != doctorScreen {
		t.Fatalf("screen = %d, want doctorScreen", final.screen)
	}
	for _, expected := range []string{
		"[Doctor]",
		"● pass",
		"gcloud",
		"/usr/local/bin/gcloud",
		"Kubernetes API v1.32.1",
		"namespace api",
	} {
		if !strings.Contains(final.View(), expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, final.View())
		}
	}
}

func TestDoctorShowsDependencyAndConnectivityFailures(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	dependencies.Doctor = fakeDoctor{report: doctor.Report{Checks: []doctor.Check{{
		Dependency: doctor.Dependency{
			Name:        "kubectl",
			Description: "Kubernetes command-line tool",
			InstallURL:  "https://kubernetes.io/docs/tasks/tools/",
		},
		Err: errors.New("not found"),
	}}}}
	dependencies.Connectivity = fakeConnectivity{err: errors.New("namespace forbidden")}

	model := NewModel(dependencies)
	model.screen = doctorScreen
	message := model.Init()()
	updated, _ := model.Update(message)
	final := updated.(Model)

	for _, expected := range []string{
		"● fail",
		"kubectl",
		"https://kubernetes.io/docs/tasks/tools/",
		"Kubernetes API and namespace: namespace forbidden",
	} {
		if !strings.Contains(final.View(), expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, final.View())
		}
	}
}

func TestNamespaceEnterPersistsSelection(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	model := NewModel(dependencies)
	model.screen = namespaceScreen
	model.namespaces = []string{"api", "workers"}
	model.cursor = 1

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	message := command()
	updated, _ = updated.(Model).Update(message)
	final := updated.(Model)

	if final.dependencies.Profile.CurrentNamespace != "workers" {
		t.Fatalf("CurrentNamespace = %q, want workers", final.dependencies.Profile.CurrentNamespace)
	}
	cfg, err := (config.Store{Path: dependencies.ConfigPath}).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Profiles["staging"].CurrentNamespace != "workers" {
		t.Fatalf("persisted namespace = %q, want workers", cfg.Profiles["staging"].CurrentNamespace)
	}
}
