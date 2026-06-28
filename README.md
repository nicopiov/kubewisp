# Kubewisp

Kubewisp is a keyboard-driven Go CLI and TUI for navigating GKE clusters,
troubleshooting Kubernetes resources, and performing common operations with
production-aware safeguards.

It uses `gcloud` for Google authentication and `client-go` for Kubernetes API
access. Kubewisp never stores Google credentials or Secret values.

## Features

- Guided GKE setup and profile management
- Automatic expired-session reauthentication
- Live profile and namespace switching
- Responsive dashboard with cluster, pod-health, and dependency cards
- Pod details, YAML preview, logs, exec, port-forward, guarded delete, and restart
- Deployment, StatefulSet, DaemonSet, and CronJob visibility
- Guarded rollout restart and CronJob suspend/resume
- Workload-to-pod drill-down
- Service, Ingress, endpoint, and route inspection
- Grouped namespace Warning events with resource drill-down

## Requirements

- Go 1.26 or newer when building from source
- `gcloud`
- `kubectl`
- `gke-gcloud-auth-plugin`

Check local dependencies with:

```bash
kubewisp doctor
```

## Installation

Install with Go:

```bash
go install github.com/nicopiov/kubewisp/cmd/kubewisp@latest
kubewisp version
```

Or build the repository:

```bash
make build
./bin/kubewisp version
```

## First Run

Run the guided setup:

```bash
kubewisp init
```

Kubewisp will:

1. Check required local tools.
2. Authenticate through `gcloud` when needed.
3. Discover accessible Google Cloud projects and GKE clusters.
4. Fetch cluster credentials.
5. Verify Kubernetes API and namespace access.
6. Save a local Kubewisp profile.

OAuth login happens in the browser. Kubewisp runs `gcloud auth login --quiet`,
so after browser authentication no terminal input is required. The process may
take a few seconds to finish and print informational project suggestions before
returning to Kubewisp.

## Opening Kubewisp

```bash
kubewisp
# or
kubewisp tui
```

Before opening the dashboard, Kubewisp prints each startup step while it loads
profiles, activates the Google Cloud project, refreshes GKE credentials, and
verifies Kubernetes access.

If the gcloud session expired, Kubewisp offers to launch browser OAuth login
and retries the connection automatically.

When multiple profiles exist, startup opens an arrow-key profile selector and
remembers the selected profile.

## TUI Navigation

| Key | Action |
| --- | --- |
| `1`-`6` | Open Dashboard, Namespaces, Pods, Workloads, Network, or Events |
| `Tab`, `Left`, `Right` | Move between top-level screens |
| `Up`, `Down`, `j`, `k` | Navigate lists or scroll details |
| `Enter` | Open or select the highlighted resource |
| `/` | Filter resource lists or search inside scrollable detail views |
| `c` | Copy the selected resource, useful address/host, or focused text line |
| `r` | Refresh the current screen |
| `P` | Manage and switch profiles |
| `Esc` | Return to the previous screen |
| `q` | Quit |

The contextual help bar groups movement and opening resources under
`Navigate`, contextual operations such as filtering under `Actions`, and
application-wide controls such as tab switching under `General`. It changes
with the current screen and highlighted resource, then wraps each group for
smaller terminals. Shortcut keys are highlighted separately from their
descriptions, and the helper remains anchored at the bottom of the terminal.
While entering text or confirming an operation, `Esc` or `Ctrl+C` cancels the
current interaction without quitting Kubewisp. Screen content wraps to the
terminal width, and wrapped detail views remain vertically scrollable.

On Namespaces, Pods, Workloads, Network, Events, and relationship lists, press
`/` and type to filter immediately. `Enter` keeps the filter while navigating;
`Esc` clears it. Filtering matches useful metadata such as resource kind,
status, owner, host, reason, and message in addition to names.

On scrollable detail views such as YAML, logs, diagnostics, rollout status, and
resource details, press `/` to search within the current text. `Enter` applies
the search, `n` jumps to the next match, `N` jumps to the previous match, and
`Esc` clears it. Matching text is highlighted in the current view.
The active match uses a stronger highlight and the search status shows the
current match index, such as `2/5`.

On resource lists and details, press `c` to copy the most useful value for the
current context. Lists copy the selected resource name or `Kind/name`, Service
details copy the address when available, Ingress details copy the first host
when available, and scrollable YAML/log/diagnostic views copy the focused
search match line or current top line. Wrapped display lines copy the full
original line to the clipboard, while the status message shows a shortened
preview.

### Dashboard

The Dashboard shows responsive cards for:

- Active project, cluster, region or zone, namespace, and Kubernetes version
- Healthy, completed, warning, and unhealthy pod counts
- Availability and resolved executable paths for `gcloud`, `kubectl`, and
  `gke-gcloud-auth-plugin`

Cards sit side by side in wide terminals and stack in narrow terminals.
Run `kubewisp doctor` when a missing dependency needs installation guidance.

### Profiles

Press `P` from a top-level screen:

- `Enter`: connect and switch without restarting Kubewisp
- `r`: rename a profile
- `d`: delete a non-active profile after confirmation

Live switching refreshes GKE credentials, resets the Kubernetes client, clears
cached cluster data, and reloads the dashboard.

### Pods

| Key | Action |
| --- | --- |
| `Enter` | Open curated troubleshooting details |
| `y` | View read-only resource YAML from details |
| `l` | View the latest 200 log lines |
| `p` | Start `kubectl port-forward` |
| `e` | Open a guarded `kubectl exec` shell |
| `d` | Delete after confirmation |
| `R` | Restart a controller-managed pod after confirmation |

The Pods list includes the total Warning event count attached to each pod.
Pod details include owners, conditions, container states, images, resources,
ports, mounts, probes, network identity, service account, QoS, labels,
annotation names, and recent events. Environment and annotation values are not
shown. Application logs are printed exactly as produced and may contain
sensitive values.

Press `v` from Pod details for resource-aware diagnostics. Kubewisp combines
container states, restart history, failed conditions, and Warning events into a
short likely-cause summary without exposing environment or Secret values.
Press `y` from Pod, Workload, CronJob, Service, or Ingress details to inspect
the read-only Kubernetes YAML for the selected resource.
Detail screens also include compact related Warning events when Kubernetes
allows event access; if event listing is blocked, the main details still render.

### Workloads

The Workloads screen combines Deployments, StatefulSets, DaemonSets, and
CronJobs. Warning counts attached directly to each workload are shown in the
list.

- Enter a replica workload for rollout details, conditions, images, and its
  managed pods.
- Press `w` from workload details to monitor rollout generation, revision,
  replica replacement, conditions, and pods. The monitor refreshes every two
  seconds until the rollout completes or stalls.
- Press `v` from replica-workload details to diagnose the workload together
  with warnings from its selected pods.
- Press `y` from workload or CronJob details to inspect the read-only YAML.
- From Pod details, press `o` to open its owning workload. Kubewisp follows
  ReplicaSet-to-Deployment and Job-to-CronJob ownership chains.
- Press `R` for a guarded rollout restart.
  After confirmation, Kubewisp opens the live rollout monitor automatically.
- Enter a CronJob for recent Jobs and scheduling details.
- Press `s` to confirm suspend or resume.

Production profiles require typing the exact resource reference for destructive
or disruptive actions.

### Network And Events

The Network screen combines Services and Ingresses. Details include Service
selectors, ready EndpointSlice addresses, Ingress routes, and Service backends.
From Service details, press `p` to inspect selected pods. From Ingress details,
press `s` to inspect and open backend Services. Press `y` from Service or
Ingress details to inspect the read-only YAML.

The Events screen groups repeated namespace Warning events and drills into
affected pods and supported workloads.

## CLI Commands

Use `kubewisp --help` or `kubewisp <command> --help` for complete command help
and flags.

```bash
# Profiles and setup
kubewisp init
kubewisp profile add
kubewisp profile list
kubewisp profile show [name]
kubewisp profile use <name>
kubewisp profile rename <old-name> <new-name>
kubewisp profile delete <name>

# Cluster and namespace
kubewisp cluster status
kubewisp namespace list
kubewisp namespace switch [namespace]

# Pods
kubewisp pods list
kubewisp pods describe [pod]
kubewisp pods logs [pod] --tail 500 --follow
kubewisp pods exec [pod] --container app --shell /bin/bash
kubewisp pods port-forward [pod] --port 8080 --local-port 18080
kubewisp pods delete <pod>
kubewisp pods restart <pod>

# Workloads and events
kubewisp workloads list
kubewisp workloads restart [kind/name]
kubewisp workloads cronjob describe [name]
kubewisp workloads cronjob suspend [name]
kubewisp workloads cronjob resume [name]
kubewisp events
```

Commands with optional resource names open a keyboard selector when the name is
omitted.

## Safety

- Kubewisp stores preferences only in `~/.config/kubewisp/config.yaml`.
- Profile deletion removes only local Kubewisp configuration, never a cluster.
- Namespace switching does not modify the kubeconfig namespace.
- Pod deletion, pod restart, rollout restart, and CronJob state changes require
  confirmation.
- Production profiles require stronger exact-name confirmation.
- Pod restart is blocked when Kubernetes reports no controller that can
  recreate the pod.

## Performance

Kubewisp reuses one Kubernetes client, fetches independent resource kinds
concurrently, and caches recently visited top-level tabs for 15 seconds.
Press `r` to force a fresh API read.

## Troubleshooting

Run:

```bash
kubewisp doctor
kubewisp cluster status
```

If authentication expired, launch Kubewisp again and accept its reauthentication
prompt, or run:

```bash
gcloud auth login
```

If OAuth succeeds in the browser but the terminal still shows gcloud output,
wait for the command to finish. Kubewisp uses quiet login and does not require
pressing Enter.

## Development

```bash
make fmt
make test
make verify
make test-race
```

Tests use injected command runners and fake Kubernetes clients; the default
suite does not require Google credentials, a cluster, or network access.

Override the config location during development with `--config <path>` or
`KUBEWISP_CONFIG`.
