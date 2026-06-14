package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/doctor"
	"github.com/nicopiov/kubewisp/internal/kube"
	"github.com/nicopiov/kubewisp/internal/kubectl"
)

type screen int

const (
	dashboardScreen screen = iota
	namespaceScreen
	podScreen
	workloadScreen
	networkScreen
	eventScreen
	doctorScreen
	podDetailsScreen
	podLogsScreen
	containerScreen
	portForwardScreen
	execContainerScreen
	execConfirmScreen
	podActionConfirmScreen
	workloadRestartConfirmScreen
	workloadDetailsScreen
	workloadPodsScreen
	networkDetailsScreen
	cronJobDetailsScreen
	cronJobStateConfirmScreen
	profileScreen
	profileRenameScreen
	profileDeleteConfirmScreen
)

type Dependencies struct {
	ConfigPath   string
	ProfileName  string
	Profile      config.Profile
	Connectivity kube.ConnectivityChecker
	Namespaces   kube.NamespaceService
	Pods         kube.PodService
	Workloads    kube.WorkloadService
	Network      kube.NetworkService
	Events       kube.EventService
	Doctor       doctor.Reporter
	PortForward  kubectl.PortForwarder
	Exec         kubectl.Executor
	Profiles     ProfileConnector
}

type Model struct {
	dependencies Dependencies
	screen       screen
	cursor       int
	scroll       int
	width        int
	height       int
	loading      bool
	status       string
	err          error
	loadedAt     map[screen]time.Time
	cacheTTL     time.Duration
	now          func() time.Time

	connectivity      kube.ConnectivityReport
	namespaces        []string
	pods              []kube.PodSummary
	podDetails        kube.PodDetails
	logs              string
	containers        []string
	selectedPod       string
	selectedContainer string
	doctorReport      doctor.Report
	doctorConnection  kube.ConnectivityReport
	doctorConnectErr  error
	ports             []kube.PodPort
	workloads         []kube.WorkloadSummary
	workloadPods      []kube.PodSummary
	networkResources  []kube.NetworkSummary
	selectedNetwork   kube.NetworkSummary
	networkDetails    kube.NetworkDetails
	events            []kube.NamespaceEventSummary
	selectedWorkload  kube.WorkloadSummary
	workloadDetails   kube.WorkloadDetails
	cronJobDetails    kube.CronJobDetails
	pendingWorkload   kube.NamespaceEventSummary
	podAction         string
	podActionInfo     kube.PodActionInfo
	confirmationInput string
	cronJobSuspended  bool
	podBackScreen     screen
	profileNames      []string
	profileInput      string
	selectedProfile   string
}

type connectivityMsg struct {
	report kube.ConnectivityReport
	err    error
}

type namespacesMsg struct {
	names []string
	err   error
}

type podsMsg struct {
	pods []kube.PodSummary
	err  error
}

type namespaceSwitchedMsg struct {
	namespace string
	err       error
}

type podDetailsMsg struct {
	details kube.PodDetails
	err     error
}

type containersMsg struct {
	names []string
	err   error
}

type podLogsMsg struct {
	logs string
	err  error
}

type doctorMsg struct {
	report        doctor.Report
	connection    kube.ConnectivityReport
	connectionErr error
}

type dashboardDataMsg struct {
	pods   []kube.PodSummary
	report doctor.Report
	err    error
}

type portsMsg struct {
	ports []kube.PodPort
	err   error
}

type portForwardFinishedMsg struct {
	err error
}

type execFinishedMsg struct {
	err error
}

type podActionInfoMsg struct {
	info kube.PodActionInfo
	err  error
}

type podDeletedMsg struct {
	action string
	err    error
}

type workloadsMsg struct {
	workloads []kube.WorkloadSummary
	err       error
}

type workloadPodsMsg struct {
	pods []kube.PodSummary
	err  error
}

type networkMsg struct {
	resources []kube.NetworkSummary
	err       error
}

type networkDetailsMsg struct {
	details kube.NetworkDetails
	err     error
}

type eventsMsg struct {
	events []kube.NamespaceEventSummary
	err    error
}

type workloadRestartedMsg struct {
	err error
}

type cronJobDetailsMsg struct {
	details kube.CronJobDetails
	err     error
}

type workloadDetailsMsg struct {
	details kube.WorkloadDetails
	err     error
}

type cronJobStateMsg struct {
	suspended bool
	err       error
}

type profilesMsg struct {
	names []string
	err   error
}

type profileSwitchedMsg struct {
	name    string
	profile config.Profile
	err     error
}

type profileRenamedMsg struct {
	oldName string
	newName string
	err     error
}

type profileDeletedMsg struct {
	name string
	err  error
}

var (
	titleStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	activeTabStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	warningStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	healthyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	mutedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

func NewModel(dependencies Dependencies) Model {
	return Model{
		dependencies: dependencies, loading: true, podBackScreen: podScreen,
		loadedAt: make(map[screen]time.Time), cacheTTL: 15 * time.Second, now: time.Now,
	}
}

func (m Model) Init() tea.Cmd {
	return m.loadCurrent()
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
	case connectivityMsg:
		m.err = message.err
		m.connectivity = message.report
		if message.err == nil {
			m.loading = true
			return m, m.loadDashboardData()
		}
		m.loading = false
	case namespacesMsg:
		m.loading = false
		m.err = message.err
		m.namespaces = message.names
		m.markLoaded(namespaceScreen)
		m.clampCursor(len(m.namespaces))
	case podsMsg:
		m.loading = false
		m.err = message.err
		m.pods = message.pods
		m.markLoaded(podScreen)
		m.clampCursor(len(m.pods))
	case workloadsMsg:
		m.loading = false
		m.err = message.err
		m.workloads = message.workloads
		if m.pendingWorkload.ObjectName != "" {
			for index, workload := range m.workloads {
				if strings.EqualFold(workload.Kind, m.pendingWorkload.ObjectKind) &&
					workload.Name == m.pendingWorkload.ObjectName {
					m.cursor = index
					m.status = "Selected " + workloadReference(workload)
					break
				}
			}
			m.pendingWorkload = kube.NamespaceEventSummary{}
		}
		m.clampCursor(len(m.workloads))
		m.markLoaded(workloadScreen)
	case workloadPodsMsg:
		m.loading = false
		m.err = message.err
		m.workloadPods = message.pods
		m.clampCursor(len(m.workloadPods))
	case networkMsg:
		m.loading = false
		m.err = message.err
		m.networkResources = message.resources
		m.markLoaded(networkScreen)
		m.clampCursor(len(m.networkResources))
	case networkDetailsMsg:
		m.loading = false
		m.err = message.err
		m.networkDetails = message.details
	case eventsMsg:
		m.loading = false
		m.err = message.err
		m.events = message.events
		m.markLoaded(eventScreen)
		m.clampCursor(len(m.events))
	case namespaceSwitchedMsg:
		m.loading = false
		m.err = message.err
		if message.err == nil {
			m.dependencies.Profile.CurrentNamespace = message.namespace
			m.loadedAt = make(map[screen]time.Time)
			m.status = fmt.Sprintf("Namespace switched to %s", message.namespace)
		}
	case podDetailsMsg:
		m.loading = false
		m.err = message.err
		m.podDetails = message.details
	case containersMsg:
		m.loading = false
		m.err = message.err
		m.containers = message.names
		m.clampCursor(len(m.containers))
		if message.err == nil && len(message.names) == 1 && m.screen == containerScreen {
			m.selectedContainer = message.names[0]
			m.screen = podLogsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadLogs(message.names[0])
		}
		if message.err == nil && len(message.names) == 1 && m.screen == execContainerScreen {
			m.selectedContainer = message.names[0]
			m.screen = execConfirmScreen
		}
	case podLogsMsg:
		m.loading = false
		m.err = message.err
		m.logs = message.logs
	case doctorMsg:
		m.loading = false
		m.err = nil
		m.doctorReport = message.report
		m.doctorConnection = message.connection
		m.doctorConnectErr = message.connectionErr
		m.markLoaded(doctorScreen)
	case dashboardDataMsg:
		m.loading = false
		m.err = message.err
		m.pods = message.pods
		m.doctorReport = message.report
		m.markLoaded(dashboardScreen)
		m.markLoaded(podScreen)
	case portsMsg:
		m.loading = false
		m.err = message.err
		m.ports = message.ports
		m.clampCursor(len(m.ports))
	case portForwardFinishedMsg:
		m.screen = podScreen
		m.loading = false
		m.err = nil
		if message.err != nil && !isInterrupted(message.err) {
			m.err = message.err
			m.status = ""
		} else {
			m.status = "Port-forward stopped"
		}
	case execFinishedMsg:
		m.screen = podScreen
		m.loading = false
		m.err = message.err
		m.status = ""
		if message.err == nil || isInterrupted(message.err) {
			m.err = nil
			m.status = "Exec session ended"
		}
	case podActionInfoMsg:
		m.loading = false
		m.err = message.err
		m.podActionInfo = message.info
		if message.err == nil {
			if m.podAction == "restart" && message.info.ControllerOwner == "" {
				m.err = errors.New("restart is blocked because this pod has no controller owner")
				m.screen = podScreen
			} else {
				m.screen = podActionConfirmScreen
			}
		}
	case podDeletedMsg:
		m.loading = false
		m.err = message.err
		m.screen = podScreen
		if message.err == nil {
			m.status = strings.Title(message.action) + " requested for " + m.selectedPod
			return m, m.loadPods()
		}
	case workloadRestartedMsg:
		m.loading = false
		m.err = message.err
		m.screen = workloadScreen
		if message.err == nil {
			m.status = "Rollout restart requested for " + workloadReference(m.selectedWorkload)
			return m, m.loadWorkloads()
		}
	case cronJobDetailsMsg:
		m.loading = false
		m.err = message.err
		m.cronJobDetails = message.details
	case workloadDetailsMsg:
		m.loading = false
		m.err = message.err
		m.workloadDetails = message.details
	case cronJobStateMsg:
		m.loading = false
		m.err = message.err
		m.screen = cronJobDetailsScreen
		if message.err == nil {
			m.selectedWorkload.Suspended = message.suspended
			m.status = "CronJob/" + m.selectedWorkload.Name + " is now " + cronJobStateWord(message.suspended)
			return m, m.loadCronJobDetails()
		}
	case profilesMsg:
		m.loading = false
		m.err = message.err
		m.profileNames = message.names
		m.clampCursor(len(m.profileNames))
	case profileSwitchedMsg:
		m.loading = false
		m.err = message.err
		if message.err == nil {
			m.dependencies.ProfileName = message.name
			m.dependencies.Profile = message.profile
			m.resetClusterData()
			m.screen = dashboardScreen
			m.status = "Switched to profile " + message.name
			m.loading = true
			return m, m.loadCurrent()
		}
	case profileRenamedMsg:
		m.loading = false
		m.err = message.err
		m.screen = profileScreen
		if message.err == nil {
			if m.dependencies.ProfileName == message.oldName {
				m.dependencies.ProfileName = message.newName
			}
			m.status = fmt.Sprintf("Profile %s renamed to %s", message.oldName, message.newName)
			return m, m.loadProfiles()
		}
	case profileDeletedMsg:
		m.loading = false
		m.err = message.err
		m.screen = profileScreen
		if message.err == nil {
			m.status = "Profile " + message.name + " deleted"
			return m, m.loadProfiles()
		}
	case tea.KeyMsg:
		return m.handleKey(message)
	}
	return m, nil
}

func (m Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.screen == profileRenameScreen {
		switch key.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.screen = profileScreen
			m.profileInput = ""
			return m, nil
		case "backspace":
			if len(m.profileInput) > 0 {
				m.profileInput = m.profileInput[:len(m.profileInput)-1]
			}
			return m, nil
		case "enter":
			if strings.TrimSpace(m.profileInput) == "" {
				return m, nil
			}
			m.loading = true
			return m, m.renameProfile(m.selectedProfile, strings.TrimSpace(m.profileInput))
		default:
			if key.Type == tea.KeyRunes {
				m.profileInput += string(key.Runes)
			}
			return m, nil
		}
	}
	if m.screen == profileDeleteConfirmScreen {
		switch key.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc", "n":
			m.screen = profileScreen
			return m, nil
		case "y":
			m.loading = true
			return m, m.deleteProfile(m.selectedProfile)
		default:
			return m, nil
		}
	}
	if (m.screen == podActionConfirmScreen || m.screen == workloadRestartConfirmScreen ||
		m.screen == cronJobStateConfirmScreen) &&
		m.dependencies.Profile.Production {
		switch key.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.screen == workloadRestartConfirmScreen || m.screen == cronJobStateConfirmScreen {
				if m.screen == cronJobStateConfirmScreen {
					m.screen = cronJobDetailsScreen
				} else {
					m.screen = workloadScreen
				}
			} else {
				m.screen = podScreen
			}
			m.confirmationInput = ""
			return m, nil
		case "backspace":
			if len(m.confirmationInput) > 0 {
				m.confirmationInput = m.confirmationInput[:len(m.confirmationInput)-1]
			}
			return m, nil
		case "enter":
			expected := m.selectedPod
			if m.screen == workloadRestartConfirmScreen || m.screen == cronJobStateConfirmScreen {
				expected = workloadReference(m.selectedWorkload)
			}
			if m.confirmationInput == expected {
				if m.screen == workloadRestartConfirmScreen {
					return m.executeWorkloadRestart()
				}
				if m.screen == cronJobStateConfirmScreen {
					return m.executeCronJobState()
				}
				return m.executePodAction()
			}
			return m, nil
		default:
			if key.Type == tea.KeyRunes {
				m.confirmationInput += string(key.Runes)
			}
			return m, nil
		}
	}

	switch key.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		if m.isNestedScreen() {
			if m.screen == workloadRestartConfirmScreen {
				m.screen = workloadScreen
			} else if m.screen == profileScreen || m.screen == profileRenameScreen ||
				m.screen == profileDeleteConfirmScreen {
				m.screen = dashboardScreen
			} else if m.screen == networkDetailsScreen {
				m.screen = networkScreen
			} else if m.screen == workloadDetailsScreen || m.screen == cronJobDetailsScreen ||
				m.screen == cronJobStateConfirmScreen {
				m.screen = workloadScreen
			} else if m.screen == workloadPodsScreen {
				m.screen = workloadDetailsScreen
			} else if m.screen == podDetailsScreen || m.screen == podLogsScreen || m.screen == containerScreen {
				m.screen = m.podBackScreen
			} else {
				m.screen = podScreen
			}
			m.cursor = 0
			m.scroll = 0
			m.loading = false
			m.err = nil
			m.status = ""
			return m, nil
		}
	case "1":
		return m.changeScreen(dashboardScreen)
	case "2":
		return m.changeScreen(namespaceScreen)
	case "3":
		return m.changeScreen(podScreen)
	case "4":
		return m.changeScreen(workloadScreen)
	case "5":
		return m.changeScreen(networkScreen)
	case "6":
		return m.changeScreen(eventScreen)
	case "7":
		return m.changeScreen(doctorScreen)
	case "P":
		if !m.isNestedScreen() {
			m.screen = profileScreen
			m.cursor = 0
			m.loading = true
			m.status = ""
			m.err = nil
			return m, m.loadProfiles()
		}
	case "tab", "right":
		if !m.isNestedScreen() {
			return m.changeScreen((m.screen + 1) % 7)
		}
	case "shift+tab", "left":
		if !m.isNestedScreen() {
			return m.changeScreen((m.screen + 6) % 7)
		}
	case "up", "k":
		if m.screen == podDetailsScreen || m.screen == podLogsScreen ||
			m.screen == workloadDetailsScreen || m.screen == networkDetailsScreen || m.screen == cronJobDetailsScreen {
			if m.scroll > 0 {
				m.scroll--
			}
			return m, nil
		}
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.screen == podDetailsScreen || m.screen == podLogsScreen ||
			m.screen == workloadDetailsScreen || m.screen == networkDetailsScreen || m.screen == cronJobDetailsScreen {
			m.scroll++
			return m, nil
		}
		if m.cursor < m.itemCount()-1 {
			m.cursor++
		}
	case "l":
		if (m.screen == podScreen && len(m.pods) > 0) ||
			(m.screen == workloadPodsScreen && len(m.workloadPods) > 0) ||
			m.screen == podDetailsScreen {
			if m.screen == podScreen {
				m.selectedPod = m.pods[m.cursor].Name
				m.podBackScreen = podScreen
			}
			if m.screen == workloadPodsScreen {
				m.selectedPod = m.workloadPods[m.cursor].Name
				m.podBackScreen = workloadPodsScreen
			}
			m.screen = containerScreen
			m.cursor = 0
			m.scroll = 0
			m.loading = true
			return m, m.loadContainers()
		}
	case "p":
		if m.screen == workloadDetailsScreen {
			m.screen = workloadPodsScreen
			m.cursor = 0
			m.scroll = 0
			m.loading = true
			return m, m.loadWorkloadPods()
		}
		if (m.screen == podScreen && len(m.pods) > 0) || m.screen == podDetailsScreen {
			if m.screen == podScreen {
				m.selectedPod = m.pods[m.cursor].Name
			}
			m.screen = portForwardScreen
			m.cursor = 0
			m.scroll = 0
			m.loading = true
			return m, m.loadPorts()
		}
	case "e":
		if (m.screen == podScreen && len(m.pods) > 0) || m.screen == podDetailsScreen {
			if m.screen == podScreen {
				m.selectedPod = m.pods[m.cursor].Name
			}
			m.screen = execContainerScreen
			m.cursor = 0
			m.scroll = 0
			m.loading = true
			return m, m.loadContainers()
		}
	case "d":
		if m.screen == profileScreen && len(m.profileNames) > 0 {
			m.selectedProfile = m.profileNames[m.cursor]
			if m.selectedProfile == m.dependencies.ProfileName {
				m.status = "Switch away from the active profile before deleting it"
				return m, nil
			}
			m.screen = profileDeleteConfirmScreen
			return m, nil
		}
		if (m.screen == podScreen && len(m.pods) > 0) || m.screen == podDetailsScreen {
			return m.beginPodAction("delete")
		}
	case "r":
		if m.screen == profileScreen && len(m.profileNames) > 0 {
			m.selectedProfile = m.profileNames[m.cursor]
			m.profileInput = ""
			m.screen = profileRenameScreen
			m.loading = false
			return m, nil
		}
		delete(m.loadedAt, m.screen)
		m.loading = true
		m.status = ""
		m.err = nil
		return m, m.loadCurrent()
	case "R":
		if (m.screen == workloadScreen && len(m.workloads) > 0) || m.screen == workloadDetailsScreen {
			if m.screen == workloadScreen {
				m.selectedWorkload = m.workloads[m.cursor]
			}
			if !kube.SupportsRolloutRestart(m.selectedWorkload.Kind) {
				m.status = fmt.Sprintf("%s does not support rollout restart", m.selectedWorkload.Kind)
				return m, nil
			}
			m.confirmationInput = ""
			m.status = ""
			m.err = nil
			m.screen = workloadRestartConfirmScreen
			m.loading = false
			return m, nil
		}
		if (m.screen == podScreen && len(m.pods) > 0) || m.screen == podDetailsScreen {
			return m.beginPodAction("restart")
		}
	case "s":
		if m.screen == workloadScreen && len(m.workloads) > 0 {
			m.selectedWorkload = m.workloads[m.cursor]
			if !strings.EqualFold(m.selectedWorkload.Kind, "CronJob") {
				m.status = "Suspend/resume is available only for CronJobs"
				return m, nil
			}
			return m.beginCronJobState()
		}
		if m.screen == cronJobDetailsScreen {
			return m.beginCronJobState()
		}
	case "y":
		if m.screen == execConfirmScreen && m.dependencies.Profile.Production {
			return m.startExec()
		}
		if m.screen == podActionConfirmScreen && !m.dependencies.Profile.Production {
			return m.executePodAction()
		}
		if m.screen == workloadRestartConfirmScreen && !m.dependencies.Profile.Production {
			return m.executeWorkloadRestart()
		}
		if m.screen == cronJobStateConfirmScreen && !m.dependencies.Profile.Production {
			return m.executeCronJobState()
		}
	case "enter":
		if m.screen == profileScreen && len(m.profileNames) > 0 {
			name := m.profileNames[m.cursor]
			if name == m.dependencies.ProfileName {
				m.status = "Profile " + name + " is already active"
				return m, nil
			}
			m.loading = true
			m.status = "Connecting profile " + name
			return m, m.switchProfile(name)
		}
		if m.screen == namespaceScreen && len(m.namespaces) > 0 {
			m.loading = true
			return m, m.switchNamespace(m.namespaces[m.cursor])
		}
		if m.screen == podScreen && len(m.pods) > 0 {
			m.selectedPod = m.pods[m.cursor].Name
			m.podBackScreen = podScreen
			m.screen = podDetailsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadPodDetails()
		}
		if m.screen == workloadPodsScreen && len(m.workloadPods) > 0 {
			m.selectedPod = m.workloadPods[m.cursor].Name
			m.podBackScreen = workloadPodsScreen
			m.screen = podDetailsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadPodDetails()
		}
		if m.screen == workloadScreen && len(m.workloads) > 0 {
			m.selectedWorkload = m.workloads[m.cursor]
			if strings.EqualFold(m.selectedWorkload.Kind, "CronJob") {
				m.screen = cronJobDetailsScreen
				m.scroll = 0
				m.loading = true
				return m, m.loadCronJobDetails()
			}
			m.screen = workloadDetailsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadWorkloadDetails()
		}
		if m.screen == networkScreen && len(m.networkResources) > 0 {
			m.selectedNetwork = m.networkResources[m.cursor]
			m.screen = networkDetailsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadNetworkDetails()
		}
		if m.screen == eventScreen && len(m.events) > 0 {
			event := m.events[m.cursor]
			switch strings.ToLower(event.ObjectKind) {
			case "pod":
				m.selectedPod = event.ObjectName
				m.podBackScreen = podScreen
				m.screen = podDetailsScreen
				m.scroll = 0
				m.loading = true
				return m, m.loadPodDetails()
			case "deployment", "statefulset", "daemonset", "cronjob":
				m.pendingWorkload = event
				return m.changeScreen(workloadScreen)
			default:
				m.status = fmt.Sprintf("No drill-down available for %s/%s", event.ObjectKind, event.ObjectName)
				return m, nil
			}
		}
		if m.screen == containerScreen && len(m.containers) > 0 {
			container := m.containers[m.cursor]
			m.selectedContainer = container
			m.screen = podLogsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadLogs(container)
		}
		if m.screen == execContainerScreen && len(m.containers) > 0 {
			m.selectedContainer = m.containers[m.cursor]
			m.screen = execConfirmScreen
			m.loading = false
			return m, nil
		}
		if m.screen == execConfirmScreen && !m.dependencies.Profile.Production {
			return m.startExec()
		}
		if m.screen == portForwardScreen && len(m.ports) > 0 {
			port := m.ports[m.cursor]
			options := kubectl.PortForwardOptions{
				Namespace:  selectedNamespace(m.dependencies.Profile),
				Pod:        m.selectedPod,
				LocalPort:  port.Port,
				RemotePort: port.Port,
			}
			if m.dependencies.PortForward == nil {
				m.err = errors.New("kubectl port-forward service is not configured")
				return m, nil
			}
			m.status = ""
			return m, tea.Exec(
				&portForwardCommand{
					forwarder: m.dependencies.PortForward,
					options:   options,
				},
				func(err error) tea.Msg {
					return portForwardFinishedMsg{err: err}
				},
			)
		}
	}
	return m, nil
}

func (m Model) changeScreen(next screen) (tea.Model, tea.Cmd) {
	m.screen = next
	m.cursor = 0
	m.scroll = 0
	m.loading = !m.cacheFresh(next)
	m.status = ""
	m.err = nil
	if !m.loading {
		return m, nil
	}
	return m, m.loadCurrent()
}

func (m Model) View() string {
	var view strings.Builder
	view.WriteString(m.header())
	view.WriteString("\n\n")
	view.WriteString(m.tabs())
	view.WriteString("\n\n")

	if m.loading {
		view.WriteString("Loading...\n")
	} else if m.err != nil {
		view.WriteString(errorStyle.Render(m.err.Error()))
		view.WriteString("\n")
	} else {
		switch m.screen {
		case dashboardScreen:
			view.WriteString(m.dashboardView())
		case namespaceScreen:
			view.WriteString(m.namespaceView())
		case podScreen:
			view.WriteString(m.podView())
		case workloadPodsScreen:
			view.WriteString(m.workloadPodsView())
		case workloadScreen:
			view.WriteString(m.workloadView())
		case networkScreen:
			view.WriteString(m.networkView())
		case eventScreen:
			view.WriteString(m.eventView())
		case doctorScreen:
			view.WriteString(m.doctorView())
		case podDetailsScreen:
			view.WriteString(m.scrollView(m.podDetailsView()))
		case podLogsScreen:
			view.WriteString(m.scrollView(m.podLogsView()))
		case containerScreen:
			view.WriteString(m.containerView())
		case portForwardScreen:
			view.WriteString(m.portForwardView())
		case execContainerScreen:
			view.WriteString(m.execContainerView())
		case execConfirmScreen:
			view.WriteString(m.execConfirmView())
		case podActionConfirmScreen:
			view.WriteString(m.podActionConfirmView())
		case workloadRestartConfirmScreen:
			view.WriteString(m.workloadRestartConfirmView())
		case workloadDetailsScreen:
			view.WriteString(m.scrollView(m.workloadDetailsView()))
		case networkDetailsScreen:
			view.WriteString(m.scrollView(m.networkDetailsView()))
		case cronJobDetailsScreen:
			view.WriteString(m.scrollView(m.cronJobDetailsView()))
		case cronJobStateConfirmScreen:
			view.WriteString(m.cronJobStateConfirmView())
		case profileScreen:
			view.WriteString(m.profileView())
		case profileRenameScreen:
			view.WriteString(m.profileRenameView())
		case profileDeleteConfirmScreen:
			view.WriteString(m.profileDeleteConfirmView())
		}
	}
	if m.status != "" {
		view.WriteString("\n")
		view.WriteString(activeTabStyle.Render(m.status))
		view.WriteString("\n")
	}
	view.WriteString("\n")
	view.WriteString(mutedStyle.Render(m.responsiveHelpView()))
	return view.String()
}

func (m Model) header() string {
	profile := m.dependencies.Profile
	namespace := selectedNamespace(profile)
	production := ""
	if profile.Production {
		production = " | PRODUCTION"
	}
	return titleStyle.Render("kubewisp") + "\n" +
		fmt.Sprintf(
			"Profile: %s%s | Project: %s | Cluster: %s | Namespace: %s",
			m.dependencies.ProfileName,
			production,
			profile.ProjectID,
			profile.ClusterName,
			namespace,
		)
}

func (m Model) tabs() string {
	labels := []string{"Dashboard", "Namespaces", "Pods", "Workloads", "Network", "Events", "Doctor"}
	for index := range labels {
		if screen(index) == m.screen {
			labels[index] = activeTabStyle.Render("[" + labels[index] + "]")
		}
	}
	return strings.Join(labels, "   ")
}

func (m Model) helpView() string {
	if m.isNestedScreen() {
		if m.screen == containerScreen {
			return "up/down navigate | enter select | esc back | q quit"
		}
		if m.screen == portForwardScreen {
			return "up/down navigate | enter start port-forward | esc back | q quit"
		}
		if m.screen == execContainerScreen {
			return "up/down navigate | enter select | esc back | q quit"
		}
		if m.screen == execConfirmScreen {
			if m.dependencies.Profile.Production {
				return "y confirm production exec | esc cancel | q quit"
			}
			return "enter start shell | esc cancel | q quit"
		}
		if m.screen == podActionConfirmScreen {
			if m.dependencies.Profile.Production {
				return "type exact pod name | enter confirm | esc cancel | ctrl+c quit"
			}
			return "y confirm | esc cancel | q quit"
		}
		if m.screen == workloadRestartConfirmScreen {
			if m.dependencies.Profile.Production {
				return "type exact Kind/name | enter confirm | esc cancel | ctrl+c quit"
			}
			return "y confirm rollout restart | esc cancel | q quit"
		}
		if m.screen == workloadDetailsScreen {
			return "p managed pods | R rollout restart | r refresh | esc back | q quit"
		}
		if m.screen == workloadPodsScreen {
			return "enter details | l logs | up/down navigate | r refresh | esc back | q quit"
		}
		if m.screen == networkDetailsScreen {
			return "r refresh | esc back | q quit"
		}
		if m.screen == cronJobDetailsScreen {
			return "s suspend/resume | r refresh | esc back | q quit"
		}
		if m.screen == cronJobStateConfirmScreen {
			if m.dependencies.Profile.Production {
				return "type exact CronJob/name | enter confirm | esc cancel | ctrl+c quit"
			}
			return "y confirm state change | esc cancel | q quit"
		}
		if m.screen == podDetailsScreen {
			return "l logs | p port-forward | e exec | d delete | R restart | r refresh | esc back | q quit"
		}
		if m.screen == profileScreen {
			return "enter switch | r rename | d delete | up/down navigate | esc back | q quit"
		}
		if m.screen == profileRenameScreen {
			return "type new profile name | enter rename | esc cancel | ctrl+c quit"
		}
		if m.screen == profileDeleteConfirmScreen {
			return "y confirm delete | n/esc cancel | ctrl+c quit"
		}
		return "r refresh | esc back | q quit"
	}
	if m.screen == podScreen {
		return "enter details | l logs | p port-forward | e exec | d delete | R restart | up/down navigate | r refresh | q quit"
	}
	if m.screen == workloadScreen {
		return m.workloadHelpView()
	}
	if m.screen == eventScreen {
		return "enter inspect affected object | up/down navigate | r refresh | tab/left/right screens | q quit"
	}
	if m.screen == networkScreen {
		return "enter details | up/down navigate | r refresh | tab/left/right screens | q quit"
	}
	return "1 dashboard | 2 namespaces | 3 pods | 4 workloads | 5 network | 6 events | 7 doctor | P profiles | tab/left/right screens | r refresh | q quit"
}

func (m Model) responsiveHelpView() string {
	help := m.helpView()
	width := m.width
	if width <= 0 {
		return help
	}
	if width < 100 {
		replacements := []struct {
			old string
			new string
		}{
			{"up/down navigate", "up/down"},
			{"tab/left/right screens", "tab screens"},
			{"enter inspect affected object", "enter inspect"},
			{"enter start port-forward", "enter start"},
			{"type exact pod name", "type pod name"},
			{"type exact Kind/name", "type Kind/name"},
			{"type exact CronJob/name", "type CronJob/name"},
		}
		for _, replacement := range replacements {
			help = strings.ReplaceAll(help, replacement.old, replacement.new)
		}
	}
	return wrapHelp(help, width)
}

func wrapHelp(help string, width int) string {
	if width <= 0 || lipgloss.Width(help) <= width {
		return help
	}
	segments := strings.Split(help, " | ")
	var lines []string
	current := ""
	for _, segment := range segments {
		next := segment
		if current != "" {
			next = current + " | " + segment
		}
		if current != "" && lipgloss.Width(next) > width {
			lines = append(lines, current)
			current = segment
			continue
		}
		current = next
	}
	if current != "" {
		lines = append(lines, current)
	}
	return strings.Join(lines, "\n")
}

func (m Model) profileView() string {
	var view strings.Builder
	fmt.Fprintln(&view, "Profiles")
	fmt.Fprintln(&view)
	for index, name := range m.profileNames {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		active := ""
		if name == m.dependencies.ProfileName {
			active = " (active)"
		}
		fmt.Fprintf(&view, "%s%s%s\n", cursor, name, active)
	}
	return view.String()
}

func (m Model) profileRenameView() string {
	return fmt.Sprintf("Rename profile %s\n\nNew name: %s", m.selectedProfile, m.profileInput)
}

func (m Model) profileDeleteConfirmView() string {
	return fmt.Sprintf(
		"Delete profile %s?\n\nThis removes only Kubewisp's saved profile. It does not delete the GKE cluster.",
		m.selectedProfile,
	)
}

func (m Model) dashboardView() string {
	status := "connected"
	if m.connectivity.ServerVersion == "" {
		status = "unknown"
	}
	healthy, completed, warnings, unhealthy := podHealthCounts(m.pods)
	profile := m.dependencies.Profile
	clusterCard := dashboardCard("Cluster", []string{
		"Connection: " + status,
		"Kubernetes: " + valueOrDash(m.connectivity.ServerVersion),
		"Project: " + valueOrDash(profile.ProjectID),
		"Cluster: " + valueOrDash(profile.ClusterName),
		"Location: " + locationLabel(profile),
		"Namespace: " + selectedNamespace(profile),
	})
	podCard := dashboardCard("Pod Health", []string{
		healthyStyle.Render(fmt.Sprintf("● healthy %d", healthy)),
		mutedStyle.Render(fmt.Sprintf("● completed %d", completed)),
		warningStyle.Render(fmt.Sprintf("● warning %d", warnings)),
		errorStyle.Render(fmt.Sprintf("● unhealthy %d", unhealthy)),
		fmt.Sprintf("Total: %d", len(m.pods)),
	})
	dependencyCard := dashboardCard("Local Dependencies", dependencyStatusLines(m.doctorReport))
	if m.width >= 105 {
		return lipgloss.JoinHorizontal(lipgloss.Top, clusterCard, " ", podCard, " ", dependencyCard)
	}
	return lipgloss.JoinVertical(lipgloss.Left, clusterCard, podCard, dependencyCard)
}

func dashboardCard(title string, lines []string) string {
	content := titleStyle.Render(title) + "\n\n" + strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Width(30).
		Render(content)
}

func dependencyStatusLines(report doctor.Report) []string {
	if len(report.Checks) == 0 {
		return []string{mutedStyle.Render("Checking...")}
	}
	lines := make([]string, 0, len(report.Checks)+1)
	for _, check := range report.Checks {
		if check.Passed() {
			lines = append(lines, healthyStyle.Render("● pass")+" "+check.Dependency.Name)
			continue
		}
		lines = append(lines, errorStyle.Render("● fail")+" "+check.Dependency.Name)
	}
	if report.Healthy() {
		lines = append(lines, "", healthyStyle.Render("All available"))
	} else {
		lines = append(lines, "", warningStyle.Render("Open Doctor for guidance"))
	}
	return lines
}

func locationLabel(profile config.Profile) string {
	if profile.Location == "" {
		return "-"
	}
	if profile.LocationType == "" {
		return profile.Location
	}
	return fmt.Sprintf("%s (%s)", profile.Location, profile.LocationType)
}

func (m Model) namespaceView() string {
	if len(m.namespaces) == 0 {
		return "No accessible namespaces."
	}
	var view strings.Builder
	current := selectedNamespace(m.dependencies.Profile)
	for index, namespace := range m.namespaces {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		active := ""
		if namespace == current {
			active = " *"
		}
		fmt.Fprintf(&view, "%s%s%s\n", cursor, namespace, active)
	}
	view.WriteString("\nEnter switches namespace for this profile.")
	return view.String()
}

func (m Model) doctorView() string {
	var view strings.Builder
	fmt.Fprintln(&view, "Local Dependencies")
	for _, check := range m.doctorReport.Checks {
		if check.Passed() {
			fmt.Fprintf(&view, "%s  %-24s %s\n", healthyStyle.Render("● pass"), check.Dependency.Name, check.Path)
			continue
		}
		fmt.Fprintf(&view, "%s  %-24s not found\n", errorStyle.Render("● fail"), check.Dependency.Name)
		fmt.Fprintf(&view, "        Install %s: %s\n", check.Dependency.Description, check.Dependency.InstallURL)
	}

	fmt.Fprintln(&view, "\nCluster Connectivity")
	if m.doctorConnectErr != nil {
		fmt.Fprintf(&view, "%s  Kubernetes API and namespace: %s\n", errorStyle.Render("● fail"), m.doctorConnectErr)
		return view.String()
	}
	fmt.Fprintf(
		&view,
		"%s  Kubernetes API %s | namespace %s\n",
		healthyStyle.Render("● pass"),
		valueOrDash(m.doctorConnection.ServerVersion),
		valueOrDash(m.doctorConnection.Namespace),
	)
	return view.String()
}

func (m Model) podView() string {
	if len(m.pods) == 0 {
		return fmt.Sprintf("No pods found in namespace %s.", selectedNamespace(m.dependencies.Profile))
	}
	return podListView(m.pods, m.cursor)
}

func (m Model) workloadPodsView() string {
	if len(m.workloadPods) == 0 {
		return fmt.Sprintf("No pods found for %s.", workloadReference(m.selectedWorkload))
	}
	var view strings.Builder
	fmt.Fprintf(&view, "Pods managed by %s\n\n", workloadReference(m.selectedWorkload))
	view.WriteString(podListView(m.workloadPods, m.cursor))
	return view.String()
}

func podListView(pods []kube.PodSummary, cursorIndex int) string {
	var view strings.Builder
	fmt.Fprintln(&view, "  HEALTH | NAME | READY | STATUS | RESTARTS | AGE")
	for index, pod := range pods {
		cursor := "  "
		if index == cursorIndex {
			cursor = "> "
		}
		fmt.Fprintf(
			&view,
			"%s%s | %s | %s | %s | %d | %s\n",
			cursor,
			podStatusMarker(pod),
			pod.Name,
			pod.Ready,
			pod.Status,
			pod.Restarts,
			formatAge(time.Now(), pod.CreatedAt),
		)
	}
	return view.String()
}

func (m Model) workloadView() string {
	if len(m.workloads) == 0 {
		return fmt.Sprintf("No workloads found in namespace %s.", selectedNamespace(m.dependencies.Profile))
	}
	var view strings.Builder
	fmt.Fprintln(&view, "  HEALTH | KIND | NAME | STATUS | SCHEDULE | LAST RUN | AGE")
	for index, workload := range m.workloads {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		fmt.Fprintf(
			&view,
			"%s%s | %s | %s | %s | %s | %s | %s\n",
			cursor,
			workloadStatusMarker(workload),
			workload.Kind,
			workload.Name,
			workloadStatusText(workload),
			valueOrDash(workload.Schedule),
			formatAge(time.Now(), workload.LastScheduleTime),
			formatAge(time.Now(), workload.CreatedAt),
		)
	}
	return view.String()
}

func (m Model) workloadHelpView() string {
	if len(m.workloads) == 0 || m.cursor >= len(m.workloads) {
		return "r refresh | tab/left/right screens | q quit"
	}
	if strings.EqualFold(m.workloads[m.cursor].Kind, "CronJob") {
		return "enter details | s suspend/resume | up/down navigate | r refresh | q quit"
	}
	return "enter details | R rollout restart | up/down navigate | r refresh | q quit"
}

func (m Model) networkView() string {
	if len(m.networkResources) == 0 {
		return fmt.Sprintf("No Services or Ingresses found in namespace %s.", selectedNamespace(m.dependencies.Profile))
	}
	var view strings.Builder
	fmt.Fprintln(&view, "  KIND | NAME | TYPE/CLASS | ADDRESS | PORTS/HOSTS | AGE")
	for index, resource := range m.networkResources {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		exposure := strings.Join(resource.Ports, ", ")
		if resource.Kind == "Ingress" {
			exposure = strings.Join(resource.Hosts, ", ")
		}
		fmt.Fprintf(&view, "%s%s | %s | %s | %s | %s | %s\n",
			cursor, resource.Kind, resource.Name, resource.Type, valueOrDash(resource.Address),
			valueOrDash(exposure), formatAge(time.Now(), resource.CreatedAt))
	}
	return view.String()
}

func (m Model) eventView() string {
	if len(m.events) == 0 {
		return fmt.Sprintf("No warning events found in namespace %s.", selectedNamespace(m.dependencies.Profile))
	}
	var view strings.Builder
	fmt.Fprintln(&view, "  LAST SEEN | COUNT | OBJECT | REASON | MESSAGE")
	for index, event := range m.events {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		fmt.Fprintf(
			&view,
			"%s%s | %d | %s/%s | %s | %s\n",
			cursor,
			warningStyle.Render(formatAge(time.Now(), event.LastSeen)),
			event.Count,
			event.ObjectKind,
			event.ObjectName,
			event.Reason,
			event.Message,
		)
	}
	return view.String()
}

func (m Model) podDetailsView() string {
	details := m.podDetails
	var view strings.Builder
	fmt.Fprintf(&view, "Pod Details: %s\n\n", details.Name)
	fmt.Fprintf(&view, "Health: %s\n", podStatusMarker(details.PodSummary))
	fmt.Fprintf(&view, "Status: %s | Ready: %s | Restarts: %d\n", details.Status, details.Ready, details.Restarts)
	fmt.Fprintf(&view, "Node: %s | Pod IP: %s | Host IP: %s\n", valueOrDash(details.Node), valueOrDash(details.PodIP), valueOrDash(details.HostIP))
	fmt.Fprintf(&view, "Service Account: %s | QoS: %s\n", valueOrDash(details.ServiceAccount), valueOrDash(details.QoSClass))
	writeDetailSection(&view, "Owners", details.Owners)
	fmt.Fprintln(&view, "\nConditions:")
	if len(details.Conditions) == 0 {
		fmt.Fprintln(&view, "  -")
	}
	for _, condition := range details.Conditions {
		fmt.Fprintf(
			&view,
			"  %s=%s | reason=%s | %s\n",
			condition.Type,
			condition.Status,
			valueOrDash(condition.Reason),
			valueOrDash(condition.Message),
		)
	}
	fmt.Fprintln(&view, "\nContainers:")
	for _, container := range details.Containers {
		fmt.Fprintf(
			&view,
			"  %s | ready=%t | restarts=%d | %s | last=%s\n",
			container.Name,
			container.Ready,
			container.Restarts,
			container.State,
			container.LastState,
		)
		writeDetailValues(&view, "Image", []string{container.Image})
		writeDetailValues(&view, "Ports", container.Ports)
		writeDetailValues(&view, "Requests", container.Requests)
		writeDetailValues(&view, "Limits", container.Limits)
		writeDetailValues(&view, "Mounts", container.Mounts)
		writeDetailValues(&view, "Probes", container.Probes)
		writeDetailValues(&view, "Environment names", container.EnvironmentNames)
	}
	writeDetailSection(&view, "Volumes", details.Volumes)
	writeDetailSection(&view, "Labels", details.Labels)
	writeDetailSection(&view, "Annotation names", details.Annotations)
	fmt.Fprintln(&view, "\nRecent Events:")
	if details.EventsWarning != "" {
		fmt.Fprintf(&view, "  Warning: %s\n", details.EventsWarning)
	}
	for _, event := range details.Events {
		fmt.Fprintf(&view, "  %s %s | %s\n", event.Type, event.Reason, event.Message)
	}
	if len(details.Events) == 0 {
		fmt.Fprintln(&view, "  -")
	}
	return view.String()
}

func writeDetailSection(view *strings.Builder, title string, values []string) {
	fmt.Fprintf(view, "\n%s:\n", title)
	if len(values) == 0 {
		fmt.Fprintln(view, "  -")
		return
	}
	for _, value := range values {
		fmt.Fprintf(view, "  %s\n", value)
	}
}

func writeDetailValues(view *strings.Builder, title string, values []string) {
	if len(values) > 0 {
		fmt.Fprintf(view, "    %s: %s\n", title, strings.Join(values, ", "))
	}
}

func (m Model) podLogsView() string {
	return fmt.Sprintf("Logs: %s (latest 200 lines)\n\n%s", m.selectedPod, m.logs)
}

func (m Model) scrollView(content string) string {
	lines := strings.Split(content, "\n")
	visible := m.height - 9
	if visible <= 0 || len(lines) <= visible {
		return content
	}
	scroll := min(m.scroll, len(lines)-visible)
	return strings.Join(lines[scroll:scroll+visible], "\n") +
		"\n\n" + mutedStyle.Render(fmt.Sprintf("lines %d-%d of %d", scroll+1, scroll+visible, len(lines)))
}

func (m Model) containerView() string {
	var view strings.Builder
	fmt.Fprintf(&view, "Select container for %s\n\n", m.selectedPod)
	for index, container := range m.containers {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		fmt.Fprintf(&view, "%s%s\n", cursor, container)
	}
	return view.String()
}

func (m Model) portForwardView() string {
	var view strings.Builder
	fmt.Fprintf(&view, "Select TCP port to forward for %s\n\n", m.selectedPod)
	if len(m.ports) == 0 {
		view.WriteString("No declared TCP container ports. Use the CLI with --port for an undeclared port.")
		return view.String()
	}
	for index, port := range m.ports {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		fmt.Fprintf(&view, "%s%s\n", cursor, podPortLabel(port))
	}
	view.WriteString("\nThe local port will match the selected pod port.")
	view.WriteString("\nPress Ctrl+C while forwarding to stop and return to Kubewisp.")
	return view.String()
}

func (m Model) execContainerView() string {
	var view strings.Builder
	fmt.Fprintf(&view, "Select container for exec in %s\n\n", m.selectedPod)
	if len(m.containers) == 0 {
		view.WriteString("Pod has no containers.")
		return view.String()
	}
	for index, container := range m.containers {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		fmt.Fprintf(&view, "%s%s\n", cursor, container)
	}
	return view.String()
}

func (m Model) execConfirmView() string {
	profile := m.dependencies.Profile
	var view strings.Builder
	fmt.Fprintln(&view, "Exec target:")
	fmt.Fprintf(&view, "  Profile: %s\n", m.dependencies.ProfileName)
	fmt.Fprintf(&view, "  Project: %s\n", profile.ProjectID)
	fmt.Fprintf(&view, "  Cluster: %s\n", profile.ClusterName)
	fmt.Fprintf(&view, "  Namespace: %s\n", selectedNamespace(profile))
	fmt.Fprintf(&view, "  Pod: %s\n", m.selectedPod)
	fmt.Fprintf(&view, "  Container: %s\n", m.selectedContainer)
	fmt.Fprintln(&view, "  Shell: /bin/sh")
	if profile.Production {
		fmt.Fprintln(&view, "\nPRODUCTION profile. Press y to open this shell.")
	} else {
		fmt.Fprintln(&view, "\nPress Enter to open this shell.")
	}
	return view.String()
}

func (m Model) podActionConfirmView() string {
	profile := m.dependencies.Profile
	var view strings.Builder
	fmt.Fprintf(&view, "%s target:\n", strings.Title(m.podAction))
	fmt.Fprintf(&view, "  Profile: %s\n", m.dependencies.ProfileName)
	fmt.Fprintf(&view, "  Project: %s\n", profile.ProjectID)
	fmt.Fprintf(&view, "  Cluster: %s\n", profile.ClusterName)
	fmt.Fprintf(&view, "  Namespace: %s\n", selectedNamespace(profile))
	fmt.Fprintf(&view, "  Pod: %s\n", m.selectedPod)
	fmt.Fprintf(&view, "  Controller: %s\n", valueOrDash(m.podActionInfo.ControllerOwner))
	if m.podAction == "restart" {
		fmt.Fprintln(&view, "\nRestart deletes this pod so its controller recreates it.")
	}
	if profile.Production {
		fmt.Fprintf(&view, "\nPRODUCTION: type the exact pod name to confirm:\n> %s", m.confirmationInput)
	} else {
		fmt.Fprintf(&view, "\nPress y to confirm %s.", m.podAction)
	}
	return view.String()
}

func (m Model) workloadRestartConfirmView() string {
	profile := m.dependencies.Profile
	workload := m.selectedWorkload
	var view strings.Builder
	fmt.Fprintln(&view, "Rollout restart target:")
	fmt.Fprintf(&view, "  Profile: %s\n", m.dependencies.ProfileName)
	fmt.Fprintf(&view, "  Project: %s\n", profile.ProjectID)
	fmt.Fprintf(&view, "  Cluster: %s\n", profile.ClusterName)
	fmt.Fprintf(&view, "  Namespace: %s\n", selectedNamespace(profile))
	fmt.Fprintf(&view, "  Workload: %s\n", workloadReference(workload))
	fmt.Fprintf(&view, "  Ready: %d/%d\n", workload.Ready, workload.Desired)
	fmt.Fprintln(&view, "\nThis will restart every pod managed by this workload.")
	if profile.Production {
		fmt.Fprintf(&view, "\nPRODUCTION: type the exact Kind/name to confirm:\n> %s", m.confirmationInput)
	} else {
		fmt.Fprintln(&view, "\nPress y to confirm rollout restart.")
	}
	return view.String()
}

func (m Model) workloadDetailsView() string {
	details := m.workloadDetails
	var view strings.Builder
	fmt.Fprintf(&view, "%s Details: %s\n\n", details.Kind, details.Name)
	fmt.Fprintf(&view, "Health: %s\n", workloadStatusMarker(details.WorkloadSummary))
	fmt.Fprintf(
		&view,
		"Ready: %d/%d | Updated: %d | Available: %d | Age: %s\n",
		details.Ready,
		details.Desired,
		details.Updated,
		details.Available,
		formatAge(time.Now(), details.CreatedAt),
	)
	fmt.Fprintf(&view, "Strategy: %s\n", valueOrDash(details.Strategy))
	fmt.Fprintf(&view, "Selector: %s\n", valueOrDash(details.Selector))
	fmt.Fprintf(&view, "Service Account: %s\n", valueOrDash(details.ServiceAccount))
	writeDetailSection(&view, "Pod Template Containers", details.Containers)
	fmt.Fprintln(&view, "\nConditions:")
	if len(details.Conditions) == 0 {
		fmt.Fprintln(&view, "  -")
	}
	for _, condition := range details.Conditions {
		fmt.Fprintf(
			&view,
			"  %s=%s | reason=%s | %s\n",
			condition.Type,
			condition.Status,
			valueOrDash(condition.Reason),
			valueOrDash(condition.Message),
		)
	}
	return view.String()
}

func (m Model) networkDetailsView() string {
	details := m.networkDetails
	var view strings.Builder
	fmt.Fprintf(&view, "%s Details: %s\n\n", details.Kind, details.Name)
	fmt.Fprintf(&view, "Type/Class: %s\n", valueOrDash(details.Type))
	fmt.Fprintf(&view, "Address: %s\n", valueOrDash(details.Address))
	fmt.Fprintf(&view, "Age: %s\n", formatAge(time.Now(), details.CreatedAt))
	writeDetailSection(&view, "Ports", details.Ports)
	writeDetailSection(&view, "Hosts", details.Hosts)
	writeDetailSection(&view, "Selector", details.Selector)
	writeDetailSection(&view, "Ready Endpoints", details.Endpoints)
	writeDetailSection(&view, "Routes", details.Routes)
	return view.String()
}

func (m Model) cronJobDetailsView() string {
	details := m.cronJobDetails
	var view strings.Builder
	fmt.Fprintf(&view, "CronJob Details: %s\n\n", details.Name)
	fmt.Fprintf(&view, "State: %s\n", workloadStatusMarker(details.WorkloadSummary))
	fmt.Fprintf(&view, "Schedule: %s | Suspended: %t | Active Jobs: %d\n", details.Schedule, details.Suspended, details.Active)
	fmt.Fprintf(&view, "Concurrency Policy: %s\n", valueOrDash(details.ConcurrencyPolicy))
	fmt.Fprintf(&view, "Last Scheduled: %s | Last Successful: %s\n", formatAge(time.Now(), details.LastScheduleTime), formatAge(time.Now(), details.LastSuccessfulTime))
	fmt.Fprintln(&view, "\nRecent Jobs:")
	if len(details.Jobs) == 0 {
		fmt.Fprintln(&view, "  -")
	}
	for _, job := range details.Jobs {
		fmt.Fprintf(
			&view,
			"  %s | %s | active=%d succeeded=%d failed=%d | age=%s\n",
			job.Name,
			job.Status,
			job.Active,
			job.Succeeded,
			job.Failed,
			formatAge(time.Now(), job.CreatedAt),
		)
	}
	return view.String()
}

func (m Model) cronJobStateConfirmView() string {
	profile := m.dependencies.Profile
	cronJob := m.selectedWorkload
	var view strings.Builder
	fmt.Fprintf(&view, "CronJob %s target:\n", cronJobStateAction(m.cronJobSuspended))
	fmt.Fprintf(&view, "  Profile: %s\n", m.dependencies.ProfileName)
	fmt.Fprintf(&view, "  Project: %s\n", profile.ProjectID)
	fmt.Fprintf(&view, "  Cluster: %s\n", profile.ClusterName)
	fmt.Fprintf(&view, "  Namespace: %s\n", selectedNamespace(profile))
	fmt.Fprintf(&view, "  CronJob: %s\n", cronJob.Name)
	fmt.Fprintf(&view, "  Schedule: %s\n", cronJob.Schedule)
	fmt.Fprintf(&view, "  Current state: %s\n", cronJobStateWord(cronJob.Suspended))
	fmt.Fprintf(&view, "  New state: %s\n", cronJobStateWord(m.cronJobSuspended))
	if profile.Production {
		fmt.Fprintf(&view, "\nPRODUCTION: type the exact CronJob/name to confirm:\n> %s", m.confirmationInput)
	} else {
		fmt.Fprintf(&view, "\nPress y to confirm %s.\n", cronJobStateAction(m.cronJobSuspended))
	}
	return view.String()
}

func (m Model) beginCronJobState() (tea.Model, tea.Cmd) {
	m.confirmationInput = ""
	m.cronJobSuspended = !m.selectedWorkload.Suspended
	m.status = ""
	m.err = nil
	m.screen = cronJobStateConfirmScreen
	m.loading = false
	return m, nil
}

func (m Model) beginPodAction(action string) (tea.Model, tea.Cmd) {
	if m.screen == podScreen {
		m.selectedPod = m.pods[m.cursor].Name
	}
	m.podAction = action
	m.confirmationInput = ""
	m.loading = true
	m.err = nil
	m.status = ""
	return m, m.loadPodActionInfo()
}

func (m Model) executePodAction() (tea.Model, tea.Cmd) {
	m.loading = true
	m.confirmationInput = ""
	namespace := selectedNamespace(m.dependencies.Profile)
	action := m.podAction
	pod := m.selectedPod
	return m, func() tea.Msg {
		err := m.dependencies.Pods.Delete(context.Background(), namespace, pod)
		return podDeletedMsg{action: action, err: err}
	}
}

func (m Model) executeWorkloadRestart() (tea.Model, tea.Cmd) {
	m.loading = true
	m.confirmationInput = ""
	namespace := selectedNamespace(m.dependencies.Profile)
	workload := m.selectedWorkload
	return m, func() tea.Msg {
		err := m.dependencies.Workloads.RolloutRestart(context.Background(), namespace, workload.Kind, workload.Name)
		return workloadRestartedMsg{err: err}
	}
}

func (m Model) executeCronJobState() (tea.Model, tea.Cmd) {
	m.loading = true
	m.confirmationInput = ""
	namespace := selectedNamespace(m.dependencies.Profile)
	cronJob := m.selectedWorkload
	suspended := m.cronJobSuspended
	return m, func() tea.Msg {
		err := m.dependencies.Workloads.SetCronJobSuspended(context.Background(), namespace, cronJob.Name, suspended)
		return cronJobStateMsg{suspended: suspended, err: err}
	}
}

func (m Model) startExec() (tea.Model, tea.Cmd) {
	if m.dependencies.Exec == nil {
		m.err = errors.New("kubectl exec service is not configured")
		return m, nil
	}
	m.status = ""
	return m, tea.Exec(
		&execCommand{
			executor: m.dependencies.Exec,
			options: kubectl.ExecOptions{
				Namespace: selectedNamespace(m.dependencies.Profile),
				Pod:       m.selectedPod,
				Container: m.selectedContainer,
				Command:   "/bin/sh",
			},
		},
		func(err error) tea.Msg {
			return execFinishedMsg{err: err}
		},
	)
}

func (m Model) loadPodActionInfo() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		info, err := m.dependencies.Pods.ActionInfo(context.Background(), namespace, m.selectedPod)
		return podActionInfoMsg{info: info, err: err}
	}
}

func (m Model) loadCurrent() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	switch m.screen {
	case dashboardScreen:
		return func() tea.Msg {
			report, err := m.dependencies.Connectivity.Check(context.Background(), namespace)
			return connectivityMsg{report: report, err: err}
		}
	case namespaceScreen:
		return func() tea.Msg {
			names, err := m.dependencies.Namespaces.List(context.Background())
			return namespacesMsg{names: names, err: err}
		}
	case podScreen:
		return func() tea.Msg {
			pods, err := m.dependencies.Pods.List(context.Background(), namespace)
			return podsMsg{pods: pods, err: err}
		}
	case workloadScreen:
		return m.loadWorkloads()
	case networkScreen:
		return m.loadNetwork()
	case eventScreen:
		return m.loadEvents()
	case doctorScreen:
		return func() tea.Msg {
			report := doctor.Report{}
			if m.dependencies.Doctor != nil {
				report = m.dependencies.Doctor.Run(context.Background())
			}
			connection, err := m.dependencies.Connectivity.Check(context.Background(), namespace)
			return doctorMsg{report: report, connection: connection, connectionErr: err}
		}
	case podDetailsScreen:
		return m.loadPodDetails()
	case containerScreen:
		return m.loadContainers()
	case execContainerScreen:
		return m.loadContainers()
	case execConfirmScreen:
		return nil
	case podActionConfirmScreen:
		return nil
	case workloadRestartConfirmScreen:
		return nil
	case workloadDetailsScreen:
		return m.loadWorkloadDetails()
	case workloadPodsScreen:
		return m.loadWorkloadPods()
	case networkDetailsScreen:
		return m.loadNetworkDetails()
	case cronJobDetailsScreen:
		return m.loadCronJobDetails()
	case cronJobStateConfirmScreen:
		return nil
	case profileScreen:
		return m.loadProfiles()
	case profileRenameScreen:
		return nil
	case profileDeleteConfirmScreen:
		return nil
	case portForwardScreen:
		return m.loadPorts()
	case podLogsScreen:
		return m.loadLogs(m.selectedContainer)
	default:
		return nil
	}
}

func (m Model) loadDashboardData() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		report := doctor.Report{}
		if m.dependencies.Doctor != nil {
			report = m.dependencies.Doctor.Run(context.Background())
		}
		pods, err := m.dependencies.Pods.List(context.Background(), namespace)
		return dashboardDataMsg{pods: pods, report: report, err: err}
	}
}

func (m Model) loadWorkloadDetails() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Workloads == nil {
			return workloadDetailsMsg{err: errors.New("Kubernetes workload service is not configured")}
		}
		details, err := m.dependencies.Workloads.Describe(
			context.Background(),
			namespace,
			m.selectedWorkload.Kind,
			m.selectedWorkload.Name,
		)
		return workloadDetailsMsg{details: details, err: err}
	}
}

func (m Model) loadCronJobDetails() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Workloads == nil {
			return cronJobDetailsMsg{err: errors.New("Kubernetes workload service is not configured")}
		}
		details, err := m.dependencies.Workloads.DescribeCronJob(context.Background(), namespace, m.selectedWorkload.Name)
		return cronJobDetailsMsg{details: details, err: err}
	}
}

func (m Model) loadWorkloadPods() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Workloads == nil {
			return workloadPodsMsg{err: errors.New("Kubernetes workload service is not configured")}
		}
		pods, err := m.dependencies.Workloads.Pods(
			context.Background(),
			namespace,
			m.selectedWorkload.Kind,
			m.selectedWorkload.Name,
		)
		return workloadPodsMsg{pods: pods, err: err}
	}
}

func (m Model) loadWorkloads() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Workloads == nil {
			return workloadsMsg{err: errors.New("Kubernetes workload service is not configured")}
		}
		workloads, err := m.dependencies.Workloads.List(context.Background(), namespace)
		return workloadsMsg{workloads: workloads, err: err}
	}
}

func (m Model) loadNetwork() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Network == nil {
			return networkMsg{err: errors.New("Kubernetes network service is not configured")}
		}
		resources, err := m.dependencies.Network.List(context.Background(), namespace)
		return networkMsg{resources: resources, err: err}
	}
}

func (m Model) loadNetworkDetails() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Network == nil {
			return networkDetailsMsg{err: errors.New("Kubernetes network service is not configured")}
		}
		details, err := m.dependencies.Network.Describe(
			context.Background(), namespace, m.selectedNetwork.Kind, m.selectedNetwork.Name,
		)
		return networkDetailsMsg{details: details, err: err}
	}
}

func (m Model) loadEvents() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		if m.dependencies.Events == nil {
			return eventsMsg{err: errors.New("Kubernetes event service is not configured")}
		}
		events, err := m.dependencies.Events.ListWarnings(context.Background(), namespace)
		return eventsMsg{events: events, err: err}
	}
}

func (m Model) loadProfiles() tea.Cmd {
	return func() tea.Msg {
		cfg, err := (config.Store{Path: m.dependencies.ConfigPath}).Load()
		if err != nil {
			return profilesMsg{err: err}
		}
		names := make([]string, 0, len(cfg.Profiles))
		for name := range cfg.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		return profilesMsg{names: names}
	}
}

func (m Model) switchProfile(name string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := (config.Store{Path: m.dependencies.ConfigPath}).Load()
		if err != nil {
			return profileSwitchedMsg{name: name, err: err}
		}
		profile, exists := cfg.Profiles[name]
		if !exists {
			return profileSwitchedMsg{name: name, err: fmt.Errorf("profile %q does not exist", name)}
		}
		if m.dependencies.Profiles == nil {
			return profileSwitchedMsg{name: name, err: errors.New("profile connector is not configured")}
		}
		if err := m.dependencies.Profiles.Connect(context.Background(), profile); err != nil {
			return profileSwitchedMsg{name: name, err: err}
		}
		cfg.CurrentProfile = name
		if err := (config.Store{Path: m.dependencies.ConfigPath}).Save(cfg); err != nil {
			return profileSwitchedMsg{name: name, err: err}
		}
		return profileSwitchedMsg{name: name, profile: profile}
	}
}

func (m Model) renameProfile(oldName, newName string) tea.Cmd {
	return func() tea.Msg {
		store := config.Store{Path: m.dependencies.ConfigPath}
		cfg, err := store.Load()
		if err != nil {
			return profileRenamedMsg{oldName: oldName, newName: newName, err: err}
		}
		profile, exists := cfg.Profiles[oldName]
		if !exists {
			return profileRenamedMsg{oldName: oldName, newName: newName, err: fmt.Errorf("profile %q does not exist", oldName)}
		}
		if _, exists := cfg.Profiles[newName]; exists {
			return profileRenamedMsg{oldName: oldName, newName: newName, err: fmt.Errorf("profile %q already exists", newName)}
		}
		delete(cfg.Profiles, oldName)
		cfg.Profiles[newName] = profile
		if cfg.CurrentProfile == oldName {
			cfg.CurrentProfile = newName
		}
		err = store.Save(cfg)
		return profileRenamedMsg{oldName: oldName, newName: newName, err: err}
	}
}

func (m Model) deleteProfile(name string) tea.Cmd {
	return func() tea.Msg {
		store := config.Store{Path: m.dependencies.ConfigPath}
		cfg, err := store.Load()
		if err != nil {
			return profileDeletedMsg{name: name, err: err}
		}
		if name == cfg.CurrentProfile {
			return profileDeletedMsg{name: name, err: errors.New("switch away from the active profile before deleting it")}
		}
		if _, exists := cfg.Profiles[name]; !exists {
			return profileDeletedMsg{name: name, err: fmt.Errorf("profile %q does not exist", name)}
		}
		delete(cfg.Profiles, name)
		err = store.Save(cfg)
		return profileDeletedMsg{name: name, err: err}
	}
}

func (m Model) loadPorts() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		ports, err := m.dependencies.Pods.Ports(context.Background(), namespace, m.selectedPod)
		if err != nil {
			return portsMsg{err: err}
		}
		return portsMsg{ports: tcpPorts(ports)}
	}
}

func (m Model) loadPods() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		pods, err := m.dependencies.Pods.List(context.Background(), namespace)
		return podsMsg{pods: pods, err: err}
	}
}

func (m Model) loadPodDetails() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		details, err := m.dependencies.Pods.Describe(context.Background(), namespace, m.selectedPod)
		return podDetailsMsg{details: details, err: err}
	}
}

func (m Model) loadContainers() tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		names, err := m.dependencies.Pods.Containers(context.Background(), namespace, m.selectedPod)
		return containersMsg{names: names, err: err}
	}
}

func (m Model) loadLogs(container string) tea.Cmd {
	namespace := selectedNamespace(m.dependencies.Profile)
	return func() tea.Msg {
		stream, err := m.dependencies.Pods.Logs(context.Background(), kube.PodLogsOptions{
			Namespace: namespace,
			Pod:       m.selectedPod,
			Container: container,
			TailLines: 200,
		})
		if err != nil {
			return podLogsMsg{err: err}
		}
		defer stream.Close()
		logs, err := io.ReadAll(stream)
		return podLogsMsg{logs: string(logs), err: err}
	}
}

func (m Model) switchNamespace(namespace string) tea.Cmd {
	return func() tea.Msg {
		if err := m.dependencies.Namespaces.Exists(context.Background(), namespace); err != nil {
			return namespaceSwitchedMsg{err: err}
		}
		store := config.Store{Path: m.dependencies.ConfigPath}
		cfg, err := store.Load()
		if err != nil {
			return namespaceSwitchedMsg{err: err}
		}
		profile := cfg.Profiles[m.dependencies.ProfileName]
		profile.CurrentNamespace = namespace
		cfg.Profiles[m.dependencies.ProfileName] = profile
		if err := store.Save(cfg); err != nil {
			return namespaceSwitchedMsg{err: err}
		}
		return namespaceSwitchedMsg{namespace: namespace}
	}
}

func (m *Model) clampCursor(length int) {
	if length == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= length {
		m.cursor = length - 1
	}
}

func (m Model) itemCount() int {
	switch m.screen {
	case namespaceScreen:
		return len(m.namespaces)
	case podScreen:
		return len(m.pods)
	case workloadPodsScreen:
		return len(m.workloadPods)
	case workloadScreen:
		return len(m.workloads)
	case networkScreen:
		return len(m.networkResources)
	case eventScreen:
		return len(m.events)
	case profileScreen:
		return len(m.profileNames)
	case containerScreen:
		return len(m.containers)
	case execContainerScreen:
		return len(m.containers)
	case portForwardScreen:
		return len(m.ports)
	default:
		return 0
	}
}

func (m Model) isNestedScreen() bool {
	return m.screen == podDetailsScreen || m.screen == podLogsScreen ||
		m.screen == containerScreen || m.screen == portForwardScreen ||
		m.screen == execContainerScreen || m.screen == execConfirmScreen ||
		m.screen == podActionConfirmScreen || m.screen == workloadRestartConfirmScreen ||
		m.screen == workloadDetailsScreen || m.screen == workloadPodsScreen || m.screen == networkDetailsScreen ||
		m.screen == cronJobDetailsScreen ||
		m.screen == cronJobStateConfirmScreen || m.screen == profileScreen ||
		m.screen == profileRenameScreen || m.screen == profileDeleteConfirmScreen
}

func (m *Model) markLoaded(value screen) {
	if m.err == nil {
		m.loadedAt[value] = m.now()
	}
}

func (m Model) cacheFresh(value screen) bool {
	loaded, ok := m.loadedAt[value]
	return ok && m.now().Sub(loaded) < m.cacheTTL
}

func (m *Model) resetClusterData() {
	m.connectivity = kube.ConnectivityReport{}
	m.namespaces = nil
	m.pods = nil
	m.podDetails = kube.PodDetails{}
	m.logs = ""
	m.containers = nil
	m.workloads = nil
	m.workloadPods = nil
	m.networkResources = nil
	m.networkDetails = kube.NetworkDetails{}
	m.events = nil
	m.loadedAt = make(map[screen]time.Time)
	m.cursor = 0
	m.scroll = 0
	m.err = nil
}

func cronJobStateAction(suspended bool) string {
	if suspended {
		return "suspend"
	}
	return "resume"
}

func cronJobStateWord(suspended bool) string {
	if suspended {
		return "suspended"
	}
	return "active"
}

func selectedNamespace(profile config.Profile) string {
	if profile.CurrentNamespace != "" {
		return profile.CurrentNamespace
	}
	return profile.DefaultNamespace
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func podPortLabel(port kube.PodPort) string {
	name := ""
	if port.Name != "" {
		name = port.Name + " | "
	}
	return fmt.Sprintf("%s%d/%s | container %s", name, port.Port, port.Protocol, port.Container)
}

func tcpPorts(ports []kube.PodPort) []kube.PodPort {
	var values []kube.PodPort
	for _, port := range ports {
		if strings.EqualFold(port.Protocol, "TCP") || port.Protocol == "" {
			values = append(values, port)
		}
	}
	return values
}

func isInterrupted(err error) bool {
	return errors.Is(err, context.Canceled) ||
		strings.Contains(strings.ToLower(err.Error()), "signal: interrupt")
}

type podHealth int

const (
	podHealthy podHealth = iota
	podCompleted
	podWarning
	podUnhealthy
)

func podHealthLevel(pod kube.PodSummary) podHealth {
	status := strings.ToLower(pod.Status)
	switch {
	case status == "completed", status == "succeeded":
		return podCompleted
	case strings.Contains(status, "crash"),
		strings.Contains(status, "error"),
		strings.Contains(status, "failed"),
		strings.Contains(status, "imagepull"),
		strings.Contains(status, "invalid"),
		strings.Contains(status, "unknown"):
		return podUnhealthy
	case status != "running", !allReady(pod.Ready), recentlyRestarted(pod, time.Now()):
		return podWarning
	default:
		return podHealthy
	}
}

func recentlyRestarted(pod kube.PodSummary, now time.Time) bool {
	return !pod.LastRestartAt.IsZero() && now.Sub(pod.LastRestartAt) <= 10*time.Minute
}

func workloadReference(workload kube.WorkloadSummary) string {
	return workload.Kind + "/" + workload.Name
}

func workloadStatusMarker(workload kube.WorkloadSummary) string {
	if strings.EqualFold(workload.Kind, "CronJob") {
		if workload.Suspended {
			return mutedStyle.Render("● suspended")
		}
		if workload.Active > 0 {
			return healthyStyle.Render("● running")
		}
		return healthyStyle.Render("● scheduled")
	}
	switch {
	case workload.Desired > 0 && workload.Ready == 0:
		return errorStyle.Render("● unhealthy")
	case workload.Ready != workload.Desired,
		workload.Updated != workload.Desired,
		workload.Available != workload.Desired:
		return warningStyle.Render("● warning")
	default:
		return healthyStyle.Render("● healthy")
	}
}

func workloadStatusText(workload kube.WorkloadSummary) string {
	if strings.EqualFold(workload.Kind, "CronJob") {
		status := fmt.Sprintf("active %d", workload.Active)
		if workload.Suspended {
			status += ", suspended"
		}
		if !workload.LastSuccessfulTime.IsZero() {
			status += ", last success " + formatAge(time.Now(), workload.LastSuccessfulTime)
		}
		return status
	}
	return fmt.Sprintf(
		"ready %d/%d, updated %d, available %d",
		workload.Ready,
		workload.Desired,
		workload.Updated,
		workload.Available,
	)
}

func podStatusMarker(pod kube.PodSummary) string {
	switch podHealthLevel(pod) {
	case podCompleted:
		return mutedStyle.Render("● completed")
	case podUnhealthy:
		return errorStyle.Render("● unhealthy")
	case podWarning:
		if strings.EqualFold(pod.OwnerKind, "Job") && strings.EqualFold(pod.Status, "Pending") {
			return warningStyle.Render("● pending")
		}
		return warningStyle.Render("● warning")
	default:
		if strings.EqualFold(pod.OwnerKind, "Job") {
			return healthyStyle.Render("● running")
		}
		return healthyStyle.Render("● healthy")
	}
}

func podHealthCounts(pods []kube.PodSummary) (healthy, completed, warnings, unhealthy int) {
	for _, pod := range pods {
		switch podHealthLevel(pod) {
		case podCompleted:
			completed++
		case podUnhealthy:
			unhealthy++
		case podWarning:
			warnings++
		default:
			healthy++
		}
	}
	return healthy, completed, warnings, unhealthy
}

func allReady(ready string) bool {
	parts := strings.Split(ready, "/")
	return len(parts) == 2 && parts[0] == parts[1] && parts[0] != "0"
}

func formatAge(now, createdAt time.Time) string {
	if createdAt.IsZero() {
		return "-"
	}
	age := now.Sub(createdAt)
	if age < time.Minute {
		return fmt.Sprintf("%ds", max(0, int(age.Seconds())))
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	if age < 24*time.Hour {
		return fmt.Sprintf("%dh", int(age.Hours()))
	}
	return fmt.Sprintf("%dd", int(age.Hours()/24))
}
