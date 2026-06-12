package tui

import (
	"context"
	"fmt"
	"io"
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
	doctorScreen
	podDetailsScreen
	podLogsScreen
	containerScreen
	portForwardScreen
)

type Dependencies struct {
	ConfigPath   string
	ProfileName  string
	Profile      config.Profile
	Connectivity kube.ConnectivityChecker
	Namespaces   kube.NamespaceService
	Pods         kube.PodService
	Doctor       doctor.Reporter
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
	portForward       *kubectl.PortForwardOptions
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

type portsMsg struct {
	ports []kube.PodPort
	err   error
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
	return Model{dependencies: dependencies, loading: true}
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
		m.loading = false
		m.err = message.err
		m.connectivity = message.report
		if message.err == nil {
			return m, m.loadPods()
		}
	case namespacesMsg:
		m.loading = false
		m.err = message.err
		m.namespaces = message.names
		m.clampCursor(len(m.namespaces))
	case podsMsg:
		m.loading = false
		m.err = message.err
		m.pods = message.pods
		m.clampCursor(len(m.pods))
	case namespaceSwitchedMsg:
		m.loading = false
		m.err = message.err
		if message.err == nil {
			m.dependencies.Profile.CurrentNamespace = message.namespace
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
		if message.err == nil && len(message.names) == 1 {
			m.selectedContainer = message.names[0]
			m.screen = podLogsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadLogs(message.names[0])
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
	case portsMsg:
		m.loading = false
		m.err = message.err
		m.ports = message.ports
		m.clampCursor(len(m.ports))
	case tea.KeyMsg:
		return m.handleKey(message)
	}
	return m, nil
}

func (m Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		if m.isNestedScreen() {
			m.screen = podScreen
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
		return m.changeScreen(doctorScreen)
	case "tab", "right":
		if !m.isNestedScreen() {
			return m.changeScreen((m.screen + 1) % 4)
		}
	case "shift+tab", "left":
		if !m.isNestedScreen() {
			return m.changeScreen((m.screen + 3) % 4)
		}
	case "up", "k":
		if m.screen == podDetailsScreen || m.screen == podLogsScreen {
			if m.scroll > 0 {
				m.scroll--
			}
			return m, nil
		}
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.screen == podDetailsScreen || m.screen == podLogsScreen {
			m.scroll++
			return m, nil
		}
		if m.cursor < m.itemCount()-1 {
			m.cursor++
		}
	case "r":
		m.loading = true
		m.status = ""
		m.err = nil
		return m, m.loadCurrent()
	case "l":
		if (m.screen == podScreen && len(m.pods) > 0) || m.screen == podDetailsScreen {
			if m.screen == podScreen {
				m.selectedPod = m.pods[m.cursor].Name
			}
			m.screen = containerScreen
			m.cursor = 0
			m.scroll = 0
			m.loading = true
			return m, m.loadContainers()
		}
	case "p":
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
	case "enter":
		if m.screen == namespaceScreen && len(m.namespaces) > 0 {
			m.loading = true
			return m, m.switchNamespace(m.namespaces[m.cursor])
		}
		if m.screen == podScreen && len(m.pods) > 0 {
			m.selectedPod = m.pods[m.cursor].Name
			m.screen = podDetailsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadPodDetails()
		}
		if m.screen == containerScreen && len(m.containers) > 0 {
			container := m.containers[m.cursor]
			m.selectedContainer = container
			m.screen = podLogsScreen
			m.scroll = 0
			m.loading = true
			return m, m.loadLogs(container)
		}
		if m.screen == portForwardScreen && len(m.ports) > 0 {
			port := m.ports[m.cursor]
			m.portForward = &kubectl.PortForwardOptions{
				Namespace:  selectedNamespace(m.dependencies.Profile),
				Pod:        m.selectedPod,
				LocalPort:  port.Port,
				RemotePort: port.Port,
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) changeScreen(next screen) (tea.Model, tea.Cmd) {
	m.screen = next
	m.cursor = 0
	m.scroll = 0
	m.loading = true
	m.status = ""
	m.err = nil
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
		}
	}
	if m.status != "" {
		view.WriteString("\n")
		view.WriteString(activeTabStyle.Render(m.status))
		view.WriteString("\n")
	}
	view.WriteString("\n")
	view.WriteString(mutedStyle.Render(m.helpView()))
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
	labels := []string{"Dashboard", "Namespaces", "Pods", "Doctor"}
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
		if m.screen == podDetailsScreen {
			return "l logs | p port-forward | r refresh | esc back | q quit"
		}
		return "r refresh | esc back | q quit"
	}
	if m.screen == podScreen {
		return "enter details | l logs | p port-forward | up/down navigate | r refresh | tab/left/right screens | q quit"
	}
	return "1 dashboard | 2 namespaces | 3 pods | 4 doctor | tab/left/right screens | r refresh | q quit"
}

func (m Model) dashboardView() string {
	status := "connected"
	if m.connectivity.ServerVersion == "" {
		status = "unknown"
	}
	healthy, warnings, unhealthy := podHealthCounts(m.pods)
	return fmt.Sprintf(
		"Connection: %s\nKubernetes: %s\nNamespace: %s\n\nPod Health: %s  %s  %s\nTotal Pods: %d\n\nUse tabs or number keys to inspect namespaces and pods.",
		status,
		valueOrDash(m.connectivity.ServerVersion),
		selectedNamespace(m.dependencies.Profile),
		healthyStyle.Render(fmt.Sprintf("● healthy %d", healthy)),
		warningStyle.Render(fmt.Sprintf("● warning %d", warnings)),
		errorStyle.Render(fmt.Sprintf("● unhealthy %d", unhealthy)),
		len(m.pods),
	)
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
	var view strings.Builder
	fmt.Fprintln(&view, "  HEALTH | NAME | READY | STATUS | RESTARTS | AGE")
	for index, pod := range m.pods {
		cursor := "  "
		if index == m.cursor {
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
	return view.String()
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
	case portForwardScreen:
		return m.loadPorts()
	case podLogsScreen:
		return m.loadLogs(m.selectedContainer)
	default:
		return nil
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
	case containerScreen:
		return len(m.containers)
	case portForwardScreen:
		return len(m.ports)
	default:
		return 0
	}
}

func (m Model) isNestedScreen() bool {
	return m.screen == podDetailsScreen || m.screen == podLogsScreen ||
		m.screen == containerScreen || m.screen == portForwardScreen
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

type podHealth int

const (
	podHealthy podHealth = iota
	podWarning
	podUnhealthy
)

func podHealthLevel(pod kube.PodSummary) podHealth {
	status := strings.ToLower(pod.Status)
	switch {
	case strings.Contains(status, "crash"),
		strings.Contains(status, "error"),
		strings.Contains(status, "failed"),
		strings.Contains(status, "imagepull"),
		strings.Contains(status, "invalid"),
		strings.Contains(status, "unknown"):
		return podUnhealthy
	case status != "running", pod.Restarts > 0, !allReady(pod.Ready):
		return podWarning
	default:
		return podHealthy
	}
}

func podStatusMarker(pod kube.PodSummary) string {
	switch podHealthLevel(pod) {
	case podUnhealthy:
		return errorStyle.Render("● unhealthy")
	case podWarning:
		return warningStyle.Render("● warning")
	default:
		return healthyStyle.Render("● healthy")
	}
}

func podHealthCounts(pods []kube.PodSummary) (healthy, warnings, unhealthy int) {
	for _, pod := range pods {
		switch podHealthLevel(pod) {
		case podUnhealthy:
			unhealthy++
		case podWarning:
			warnings++
		default:
			healthy++
		}
	}
	return healthy, warnings, unhealthy
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
