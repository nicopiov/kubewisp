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
	"github.com/charmbracelet/lipgloss"
	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/doctor"
	"github.com/nicopiov/kubewisp/internal/kube"
	"github.com/nicopiov/kubewisp/internal/kubectl"
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

type fakeWorkloads struct {
	items           []kube.WorkloadSummary
	restarted       *string
	details         kube.CronJobDetails
	workloadDetails kube.WorkloadDetails
	managedPods     []kube.PodSummary
	suspended       *bool
}

type fakeEvents struct {
	items []kube.NamespaceEventSummary
}

type fakeNetwork struct {
	items   []kube.NetworkSummary
	details kube.NetworkDetails
}

func (f fakeNetwork) List(context.Context, string) ([]kube.NetworkSummary, error) {
	return f.items, nil
}

func (f fakeNetwork) Describe(context.Context, string, string, string) (kube.NetworkDetails, error) {
	return f.details, nil
}

func (f fakeEvents) ListWarnings(context.Context, string) ([]kube.NamespaceEventSummary, error) {
	return f.items, nil
}

func (f fakeWorkloads) List(context.Context, string) ([]kube.WorkloadSummary, error) {
	return f.items, nil
}

func (f fakeWorkloads) RolloutRestart(_ context.Context, _, kind, name string) error {
	if f.restarted != nil {
		*f.restarted = kind + "/" + name
	}
	return nil
}

func (f fakeWorkloads) DescribeCronJob(context.Context, string, string) (kube.CronJobDetails, error) {
	return f.details, nil
}

func (f fakeWorkloads) Describe(context.Context, string, string, string) (kube.WorkloadDetails, error) {
	return f.workloadDetails, nil
}

func (f fakeWorkloads) Pods(context.Context, string, string, string) ([]kube.PodSummary, error) {
	return f.managedPods, nil
}

func (f fakeWorkloads) SetCronJobSuspended(_ context.Context, _, _ string, suspended bool) error {
	if f.suspended != nil {
		*f.suspended = suspended
	}
	return nil
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
	actionInfo kube.PodActionInfo
	deleted    *bool
}

type fakePortForwarder struct {
	options kubectl.PortForwardOptions
	err     error
}

type fakeExecutor struct {
	options kubectl.ExecOptions
	err     error
}

type fakeProfileConnector struct {
	profile config.Profile
}

func (f *fakeProfileConnector) Connect(_ context.Context, profile config.Profile) error {
	f.profile = profile
	return nil
}

func (f *fakeExecutor) Exec(
	_ context.Context,
	_ io.Reader,
	_, _ io.Writer,
	options kubectl.ExecOptions,
) error {
	f.options = options
	return f.err
}

func (f *fakePortForwarder) PortForward(
	_ context.Context,
	_ io.Reader,
	_, _ io.Writer,
	options kubectl.PortForwardOptions,
) error {
	f.options = options
	return f.err
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

func (f fakePods) ActionInfo(context.Context, string, string) (kube.PodActionInfo, error) {
	return f.actionInfo, nil
}

func (f fakePods) Delete(context.Context, string, string) error {
	if f.deleted != nil {
		*f.deleted = true
	}
	return nil
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
	cfg.Profiles["production"] = config.Profile{
		Provider: config.ProviderGKE, ProjectID: "company-production", ClusterName: "production-main",
		LocationType: config.LocationRegion, Location: "europe-west1", DefaultNamespace: "default", Production: true,
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
		Workloads: fakeWorkloads{items: []kube.WorkloadSummary{{
			Kind: "Deployment", Name: "api", Ready: 2, Desired: 3, Updated: 3, Available: 2,
		}, {
			Kind: "StatefulSet", Name: "db", Ready: 3, Desired: 3, Updated: 3, Available: 3,
		}, {
			Kind: "CronJob", Name: "cleanup", Schedule: "0 * * * *", Active: 1, Suspended: true,
		}}, details: kube.CronJobDetails{
			WorkloadSummary: kube.WorkloadSummary{
				Kind: "CronJob", Name: "cleanup", Schedule: "0 * * * *", Active: 1, Suspended: true,
			},
			ConcurrencyPolicy: "Forbid",
			Jobs: []kube.JobSummary{{
				Name: "cleanup-123", Status: "Completed", Succeeded: 1,
			}},
		}, workloadDetails: kube.WorkloadDetails{
			WorkloadSummary: kube.WorkloadSummary{
				Kind: "Deployment", Name: "api", Ready: 2, Desired: 3, Updated: 3, Available: 2,
			},
			Strategy:       "RollingUpdate",
			Selector:       "app=api",
			ServiceAccount: "api",
			Containers:     []string{"app | image=example/api:v1"},
			Conditions: []kube.WorkloadCondition{{
				Type: "Available", Status: "True", Reason: "MinimumReplicasAvailable",
			}},
		}, managedPods: []kube.PodSummary{{
			Name: "api-abc", Ready: "1/1", Status: "Running",
		}}},
		Network: fakeNetwork{items: []kube.NetworkSummary{{
			Kind: "Service", Name: "api", Type: "ClusterIP", Address: "10.0.0.1",
			Ports: []string{"http:80/TCP -> 8080"},
		}, {
			Kind: "Ingress", Name: "api", Type: "gce", Hosts: []string{"api.example.com"},
		}}, details: kube.NetworkDetails{
			NetworkSummary: kube.NetworkSummary{
				Kind: "Service", Name: "api", Type: "ClusterIP", Address: "10.0.0.1",
				Ports: []string{"http:80/TCP -> 8080"},
			},
			Selector:  []string{"app=api"},
			Endpoints: []string{"10.2.0.5"},
		}},
		Events: fakeEvents{items: []kube.NamespaceEventSummary{{
			ObjectKind: "Pod",
			ObjectName: "worker-abc",
			Reason:     "BackOff",
			Message:    "restarting failed container",
			Count:      4,
			LastSeen:   time.Now().Add(-time.Minute),
		}, {
			ObjectKind: "Deployment",
			ObjectName: "api",
			Reason:     "FailedCreate",
			Message:    "quota exceeded",
			Count:      2,
			LastSeen:   time.Now().Add(-time.Hour),
		}}},
		Doctor: fakeDoctor{report: doctor.Report{Checks: []doctor.Check{{
			Dependency: doctor.Dependency{Name: "gcloud", Description: "Google Cloud CLI"},
			Path:       "/usr/local/bin/gcloud",
		}}}},
		PortForward: &fakePortForwarder{},
		Exec:        &fakeExecutor{},
		Profiles:    &fakeProfileConnector{},
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

func TestProfileScreenRenamesDeletesAndSwitches(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	model := NewModel(dependencies)
	model.loading = false

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	updated, _ = updated.(Model).Update(command())
	profiles := updated.(Model)
	if profiles.screen != profileScreen || !strings.Contains(profiles.View(), "staging (active)") {
		t.Fatalf("unexpected profile screen:\n%s", profiles.View())
	}

	profiles.cursor = 0
	updated, _ = profiles.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	rename := updated.(Model)
	updated, _ = rename.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("prod")})
	updated, command = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.(Model).Update(command())
	renamed := updated.(Model)
	if !strings.Contains(renamed.status, "renamed to prod") {
		t.Fatalf("rename status = %q", renamed.status)
	}

	renamed.cursor = 0
	updated, _ = renamed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	updated, command = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	updated, _ = updated.(Model).Update(command())
	deleted := updated.(Model)
	if strings.Contains(deleted.View(), "\n> prod") || strings.Contains(deleted.View(), "\n  prod") {
		t.Fatalf("deleted profile remains:\n%s", deleted.View())
	}

}

func TestProfileScreenSwitchesLiveAndClearsClusterData(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.loading = false
	model.pods = []kube.PodSummary{{Name: "old-cluster-pod"}}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	updated, _ = updated.(Model).Update(command())
	profiles := updated.(Model)
	profiles.cursor = 0

	updated, command = profiles.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, command = updated.(Model).Update(command())
	updated, command = updated.(Model).Update(command())
	updated, _ = updated.(Model).Update(command())
	switched := updated.(Model)
	if switched.dependencies.ProfileName != "production" || switched.screen != dashboardScreen ||
		len(switched.pods) != 2 {
		t.Fatalf("switched model = %#v", switched)
	}
	connector := dependenciesProfileConnector(switched.dependencies)
	if connector.profile.ClusterName != "production-main" {
		t.Fatalf("connected profile = %#v", connector.profile)
	}
}

func dependenciesProfileConnector(dependencies Dependencies) *fakeProfileConnector {
	return dependencies.Profiles.(*fakeProfileConnector)
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
	forwarding := updated.(Model)
	if command == nil {
		t.Fatal("port-forward selection did not start an external command")
	}

	updated, _ = forwarding.Update(portForwardFinishedMsg{err: errors.New("signal: interrupt")})
	final := updated.(Model)
	if final.screen != podScreen || final.status != "Port-forward stopped" || final.err != nil {
		t.Fatalf("final model = %#v", final)
	}
}

func TestPodPortForwardFailureReturnsToPodsWithError(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = portForwardScreen

	updated, _ := model.Update(portForwardFinishedMsg{err: errors.New("address already in use")})
	final := updated.(Model)

	if final.screen != podScreen || final.err == nil || !strings.Contains(final.View(), "address already in use") {
		t.Fatalf("unexpected model after port-forward failure:\n%s", final.View())
	}
}

func TestPodExecShowsContextAndReturnsToPods(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.pods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	message := command()
	updated, _ = updated.(Model).Update(message)
	confirm := updated.(Model)

	if confirm.screen != execConfirmScreen {
		t.Fatalf("screen = %d, want execConfirmScreen", confirm.screen)
	}
	for _, expected := range []string{"Exec target:", "Project: company-staging", "Pod: api-abc", "Container: app"} {
		if !strings.Contains(confirm.View(), expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, confirm.View())
		}
	}

	updated, command = confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("Enter did not start non-production exec")
	}
	updated, _ = updated.(Model).Update(execFinishedMsg{})
	final := updated.(Model)
	if final.screen != podScreen || final.status != "Exec session ended" {
		t.Fatalf("final model = %#v", final)
	}
}

func TestProductionPodExecRequiresY(t *testing.T) {
	t.Parallel()

	dependencies := testDependencies(t)
	dependencies.Profile.Production = true
	model := NewModel(dependencies)
	model.screen = execConfirmScreen
	model.selectedPod = "api-abc"
	model.selectedContainer = "app"

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || updated.(Model).screen != execConfirmScreen {
		t.Fatal("Enter started production exec")
	}
	updated, command = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if command == nil {
		t.Fatal("y did not start production exec")
	}
}

func TestPodRestartRequiresControllerAndDeletesAfterConfirmation(t *testing.T) {
	t.Parallel()

	deleted := false
	dependencies := testDependencies(t)
	pods := dependencies.Pods.(fakePods)
	pods.actionInfo = kube.PodActionInfo{ControllerOwner: "ReplicaSet/api-123"}
	pods.deleted = &deleted
	dependencies.Pods = pods
	model := NewModel(dependencies)
	model.screen = podScreen
	model.pods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	updated, _ = updated.(Model).Update(command())
	confirm := updated.(Model)
	if confirm.screen != podActionConfirmScreen || !strings.Contains(confirm.View(), "controller recreates it") {
		t.Fatalf("unexpected restart confirmation:\n%s", confirm.View())
	}
	updated, command = confirm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	updated, _ = updated.(Model).Update(command())
	if !deleted {
		t.Fatal("confirmed restart did not delete pod")
	}
}

func TestPodRestartBlocksUnmanagedPodInTUI(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.pods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	updated, _ = updated.(Model).Update(command())
	final := updated.(Model)
	if final.screen != podScreen || final.err == nil || !strings.Contains(final.View(), "no controller owner") {
		t.Fatalf("unexpected unmanaged restart result:\n%s", final.View())
	}
}

func TestProductionPodDeleteRequiresExactNameInTUI(t *testing.T) {
	t.Parallel()

	deleted := false
	dependencies := testDependencies(t)
	dependencies.Profile.Production = true
	pods := dependencies.Pods.(fakePods)
	pods.deleted = &deleted
	dependencies.Pods = pods
	model := NewModel(dependencies)
	model.screen = podScreen
	model.pods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	updated, _ = updated.(Model).Update(command())
	confirm := updated.(Model)
	updated, command = confirm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("wrong")})
	if command != nil || deleted {
		t.Fatal("wrong production confirmation deleted pod")
	}
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	confirm = updated.(Model)
	confirm.confirmationInput = ""
	updated, _ = confirm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("api-abc")})
	updated, command = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("exact production confirmation did not create delete command")
	}
	updated, _ = updated.(Model).Update(command())
	if !deleted {
		t.Fatal("exact production confirmation did not delete pod")
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
		"Project: company-staging",
		"Location: europe-west1",
		"Local Dependencies",
		"pass gcloud",
		"healthy 1",
		"unhealthy 1",
	} {
		if !strings.Contains(final.View(), expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, final.View())
		}
	}
}

func TestDashboardCardsRespondToTerminalWidth(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.loading = false
	model.connectivity = kube.ConnectivityReport{ServerVersion: "v1.32.1"}
	model.pods = []kube.PodSummary{{Ready: "1/1", Status: "Running"}}
	model.doctorReport = testDependencies(t).Doctor.(fakeDoctor).report

	model.width = 120
	wide := model.dashboardView()
	if strings.Count(wide, "╭") != 3 || strings.Count(strings.Split(wide, "\n")[0], "╭") != 3 {
		t.Fatalf("wide dashboard cards are not side by side:\n%s", wide)
	}

	model.width = 80
	narrow := model.dashboardView()
	if strings.Count(strings.Split(narrow, "\n")[0], "╭") != 1 {
		t.Fatalf("narrow dashboard cards are not stacked:\n%s", narrow)
	}
}

func TestContextHelpCompactsAndWrapsAtTerminalWidth(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.loading = false
	model.width = 55

	help := model.responsiveHelpView()
	if !strings.Contains(help, "\n") || strings.Contains(help, "up/down navigate") {
		t.Fatalf("help was not compacted and wrapped:\n%s", help)
	}
	for _, line := range strings.Split(help, "\n") {
		if lipgloss.Width(line) > model.width {
			t.Fatalf("help line width = %d, want <= %d: %q", lipgloss.Width(line), model.width, line)
		}
	}
}

func TestPodListShowsHealthMarkers(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.loading = false
	model.pods = []kube.PodSummary{
		{Name: "api", Ready: "1/1", Status: "Running", Restarts: 8},
		{Name: "recent", Ready: "1/1", Status: "Running", Restarts: 1, LastRestartAt: time.Now().Add(-time.Minute)},
		{Name: "worker", Ready: "0/1", Status: "CrashLoopBackOff", Restarts: 4},
		{Name: "cleanup-running", Ready: "1/1", Status: "Running", OwnerKind: "Job"},
		{Name: "cleanup-pending", Ready: "0/1", Status: "Pending", OwnerKind: "Job"},
		{Name: "cleanup-complete", Ready: "0/1", Status: "Completed", OwnerKind: "Job"},
	}

	view := model.View()
	for _, expected := range []string{
		"● healthy", "● warning", "● unhealthy", "● running", "● pending", "● completed",
		"api", "recent", "worker", "cleanup-running", "cleanup-pending", "cleanup-complete",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, view)
		}
	}
}

func TestDashboardCountsCompletedPodsSeparately(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.loading = false
	model.pods = []kube.PodSummary{
		{Ready: "1/1", Status: "Running"},
		{Ready: "0/1", Status: "Completed", OwnerKind: "Job"},
	}

	view := model.View()
	for _, expected := range []string{"healthy 1", "completed 1", "warning 0", "unhealthy 0"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("dashboard missing %q:\n%s", expected, view)
		}
	}
}

func TestPodHealthIgnoresHistoricalRestartsButWarnsOnRecentRestart(t *testing.T) {
	t.Parallel()

	historical := kube.PodSummary{
		Ready:         "1/1",
		Status:        "Running",
		Restarts:      8,
		LastRestartAt: time.Now().Add(-time.Hour),
	}
	recent := historical
	recent.LastRestartAt = time.Now().Add(-time.Minute)

	if got := podHealthLevel(historical); got != podHealthy {
		t.Fatalf("historical restart health = %d, want healthy", got)
	}
	if got := podHealthLevel(recent); got != podWarning {
		t.Fatalf("recent restart health = %d, want warning", got)
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
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'7'}})
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

func TestNetworkScreenShowsResourcesAndOpensDetails(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	updated, _ = updated.(Model).Update(command())
	network := updated.(Model)
	for _, expected := range []string{"[Network]", "Service", "10.0.0.1", "Ingress", "api.example.com"} {
		if !strings.Contains(network.View(), expected) {
			t.Fatalf("network view does not contain %q:\n%s", expected, network.View())
		}
	}
	network.cursor = 1
	updated, command = network.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.(Model).Update(command())
	details := updated.(Model)
	for _, expected := range []string{"Service Details: api", "app=api", "10.2.0.5", "http:80/TCP"} {
		if !strings.Contains(details.View(), expected) {
			t.Fatalf("network details do not contain %q:\n%s", expected, details.View())
		}
	}
	updated, _ = details.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).screen != networkScreen {
		t.Fatalf("details escape screen = %d, want networkScreen", updated.(Model).screen)
	}
}

func TestFreshTabCacheAvoidsReloadUntilRefresh(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = podScreen
	model.loading = false
	model.loadedAt[networkScreen] = model.now()

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	cached := updated.(Model)
	if command != nil || cached.loading {
		t.Fatal("fresh Network tab triggered a reload")
	}
	updated, command = cached.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if command == nil || !updated.(Model).loading {
		t.Fatal("explicit refresh did not reload cached Network tab")
	}
}

func TestEventsScreenShowsWarningsAndDrillsIntoPod(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'6'}})
	updated, _ = updated.(Model).Update(command())
	events := updated.(Model)

	for _, expected := range []string{"[Events]", "Pod/worker-abc", "BackOff", "restarting failed container"} {
		if !strings.Contains(events.View(), expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, events.View())
		}
	}
	updated, command = events.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("pod event did not load pod details")
	}
	if updated.(Model).screen != podDetailsScreen || updated.(Model).selectedPod != "worker-abc" {
		t.Fatalf("event drill-down model = %#v", updated.(Model))
	}
}

func TestWorkloadEventSelectsAffectedWorkload(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = eventScreen
	model.loading = false
	model.events = testDependencies(t).Events.(fakeEvents).items
	model.cursor = 1

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.(Model).Update(command())
	final := updated.(Model)

	if final.screen != workloadScreen || final.cursor != 0 || final.status != "Selected Deployment/api" {
		t.Fatalf("workload event drill-down model = %#v", final)
	}
}

func TestWorkloadsScreenShowsReplicaHealth(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	updated, _ = updated.(Model).Update(command())
	final := updated.(Model)

	if final.screen != workloadScreen {
		t.Fatalf("screen = %d, want workloadScreen", final.screen)
	}
	for _, expected := range []string{
		"[Workloads]", "Deployment", "api", "ready 2/3", "CronJob", "cleanup",
		"0 * * * *", "active 1, suspended", "● suspended", "● warning", "● healthy",
	} {
		if !strings.Contains(final.View(), expected) {
			t.Fatalf("view does not contain %q:\n%s", expected, final.View())
		}
	}
}

func TestWorkloadHelpChangesWithSelectedResource(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = workloadScreen
	model.loading = false
	model.workloads = []kube.WorkloadSummary{
		{Kind: "Deployment", Name: "api"},
		{Kind: "CronJob", Name: "cleanup"},
	}

	if view := model.View(); !strings.Contains(view, "R rollout restart") || strings.Contains(view, "s suspend/resume") {
		t.Fatalf("deployment help is not dynamic:\n%s", view)
	}
	model.cursor = 1
	if view := model.View(); !strings.Contains(view, "s suspend/resume") || strings.Contains(view, "R rollout restart") {
		t.Fatalf("CronJob help is not dynamic:\n%s", view)
	}
}

func TestReplicaWorkloadEnterLoadsDetails(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = workloadScreen
	model.loading = false
	model.workloads = []kube.WorkloadSummary{{Kind: "Deployment", Name: "api"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("Enter did not load workload details")
	}
	updated, _ = updated.(Model).Update(command())
	final := updated.(Model)

	if final.screen != workloadDetailsScreen {
		t.Fatalf("screen = %d, want workloadDetailsScreen", final.screen)
	}
	for _, want := range []string{
		"Deployment Details: api", "RollingUpdate", "app=api",
		"app | image=example/api:v1", "MinimumReplicasAvailable",
		"p managed pods", "R rollout restart",
	} {
		if !strings.Contains(final.View(), want) {
			t.Fatalf("details missing %q:\n%s", want, final.View())
		}
	}
}

func TestWorkloadDetailsOpenManagedPodsAndPodDetails(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = workloadDetailsScreen
	model.loading = false
	model.selectedWorkload = kube.WorkloadSummary{Kind: "Deployment", Name: "api"}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if command == nil {
		t.Fatal("p did not load managed pods")
	}
	updated, _ = updated.(Model).Update(command())
	pods := updated.(Model)
	if pods.screen != workloadPodsScreen ||
		!strings.Contains(pods.View(), "Pods managed by Deployment/api") ||
		!strings.Contains(pods.View(), "api-abc") {
		t.Fatalf("unexpected managed pods view:\n%s", pods.View())
	}

	updated, command = pods.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.(Model).Update(command())
	details := updated.(Model)
	if details.screen != podDetailsScreen || details.selectedPod != "api-abc" {
		t.Fatalf("unexpected pod details model: %#v", details)
	}
	updated, _ = details.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).screen != workloadPodsScreen {
		t.Fatalf("details escape screen = %d, want workloadPodsScreen", updated.(Model).screen)
	}
}

func TestWorkloadManagedPodLogsReturnToManagedPods(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = workloadPodsScreen
	model.loading = false
	model.selectedWorkload = kube.WorkloadSummary{Kind: "Deployment", Name: "api"}
	model.workloadPods = []kube.PodSummary{{Name: "api-abc"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	updated, command = updated.(Model).Update(command())
	updated, _ = updated.(Model).Update(command())
	logs := updated.(Model)
	if logs.screen != podLogsScreen || !strings.Contains(logs.View(), "hello from app") {
		t.Fatalf("unexpected logs view:\n%s", logs.View())
	}
	updated, _ = logs.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).screen != workloadPodsScreen {
		t.Fatalf("logs escape screen = %d, want workloadPodsScreen", updated.(Model).screen)
	}
}

func TestCronJobRolloutRestartIsBlockedInTUI(t *testing.T) {
	t.Parallel()

	model := NewModel(testDependencies(t))
	model.screen = workloadScreen
	model.loading = false
	model.workloads = []kube.WorkloadSummary{{Kind: "CronJob", Name: "cleanup"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	final := updated.(Model)

	if command != nil || final.screen != workloadScreen ||
		!strings.Contains(final.status, "does not support rollout restart") {
		t.Fatalf("unexpected CronJob restart result: %#v", final)
	}
}

func TestCronJobStatusMarkersDescribeLifecycle(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		workload kube.WorkloadSummary
		want     string
	}{
		{workload: kube.WorkloadSummary{Kind: "CronJob", Active: 1}, want: "running"},
		{workload: kube.WorkloadSummary{Kind: "CronJob"}, want: "scheduled"},
		{workload: kube.WorkloadSummary{Kind: "CronJob", Suspended: true}, want: "suspended"},
	} {
		if got := workloadStatusMarker(test.workload); !strings.Contains(got, test.want) {
			t.Fatalf("marker = %q, want %q", got, test.want)
		}
	}
}

func TestCronJobEnterLoadsDetailsAndSuspends(t *testing.T) {
	t.Parallel()

	suspended := false
	dependencies := testDependencies(t)
	workloads := dependencies.Workloads.(fakeWorkloads)
	workloads.suspended = &suspended
	dependencies.Workloads = workloads
	model := NewModel(dependencies)
	model.screen = workloadScreen
	model.loading = false
	model.workloads = []kube.WorkloadSummary{{Kind: "CronJob", Name: "cleanup", Schedule: "0 * * * *"}}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.(Model).Update(command())
	details := updated.(Model)
	for _, want := range []string{"CronJob Details: cleanup", "Concurrency Policy: Forbid", "cleanup-123", "Completed"} {
		if !strings.Contains(details.View(), want) {
			t.Fatalf("details missing %q:\n%s", want, details.View())
		}
	}
	updated, _ = details.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	confirm := updated.(Model)
	if confirm.screen != cronJobStateConfirmScreen ||
		!strings.Contains(confirm.View(), "Current state: active") ||
		!strings.Contains(confirm.View(), "New state: suspended") {
		t.Fatalf("unexpected state confirmation:\n%s", confirm.View())
	}
	updated, command = confirm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	updated, command = updated.(Model).Update(command())
	updated, _ = updated.(Model).Update(command())
	final := updated.(Model)
	if !suspended || !strings.Contains(final.status, "CronJob/cleanup is now suspended") {
		t.Fatalf("final state model = %#v, suspended=%t", final, suspended)
	}
}

func TestProductionCronJobStateRequiresExactReferenceInTUI(t *testing.T) {
	t.Parallel()

	suspended := false
	dependencies := testDependencies(t)
	dependencies.Profile.Production = true
	workloads := dependencies.Workloads.(fakeWorkloads)
	workloads.suspended = &suspended
	dependencies.Workloads = workloads
	model := NewModel(dependencies)
	model.screen = cronJobDetailsScreen
	model.selectedWorkload = kube.WorkloadSummary{Kind: "CronJob", Name: "cleanup"}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	confirm := updated.(Model)
	updated, _ = confirm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("wrong")})
	updated, command := updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || suspended {
		t.Fatal("wrong production confirmation suspended CronJob")
	}
	confirm = updated.(Model)
	confirm.confirmationInput = ""
	updated, _ = confirm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("CronJob/cleanup")})
	updated, command = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("exact CronJob reference did not create state command")
	}
	updated, _ = updated.(Model).Update(command())
	if !suspended {
		t.Fatal("exact confirmation did not suspend CronJob")
	}
}

func TestProductionWorkloadRestartRequiresExactReference(t *testing.T) {
	t.Parallel()

	restarted := ""
	dependencies := testDependencies(t)
	dependencies.Profile.Production = true
	dependencies.Workloads = fakeWorkloads{
		items:     []kube.WorkloadSummary{{Kind: "Deployment", Name: "api", Ready: 3, Desired: 3}},
		restarted: &restarted,
	}
	model := NewModel(dependencies)
	model.screen = workloadScreen
	model.workloads = []kube.WorkloadSummary{{Kind: "Deployment", Name: "api", Ready: 3, Desired: 3}}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	confirm := updated.(Model)
	if confirm.screen != workloadRestartConfirmScreen || !strings.Contains(confirm.View(), "every pod") {
		t.Fatalf("unexpected confirmation:\n%s", confirm.View())
	}
	updated, _ = confirm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Deployment/api")})
	updated, command := updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("exact workload reference did not create restart command")
	}
	updated, _ = updated.(Model).Update(command())
	if restarted != "Deployment/api" {
		t.Fatalf("restarted = %q", restarted)
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
