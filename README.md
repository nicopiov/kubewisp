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
- Pod details, logs, exec, port-forward, guarded delete, and restart
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
| `1`-`7` | Open Dashboard, Namespaces, Pods, Workloads, Network, Events, or Doctor |
| `Tab`, `Left`, `Right` | Move between top-level screens |
| `Up`, `Down`, `j`, `k` | Navigate lists or scroll details |
| `Enter` | Open or select the highlighted resource |
| `r` | Refresh the current screen |
| `P` | Manage and switch profiles |
| `Esc` | Return to the previous screen |
| `q` | Quit |

The contextual help bar changes with the selected resource, compacts on smaller
terminals, and wraps at action boundaries.

### Dashboard

The Dashboard shows responsive cards for:

- Active project, cluster, region or zone, namespace, and Kubernetes version
- Healthy, completed, warning, and unhealthy pod counts
- Availability of `gcloud`, `kubectl`, and `gke-gcloud-auth-plugin`

Cards sit side by side in wide terminals and stack in narrow terminals.

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
| `l` | View the latest 200 log lines |
| `p` | Start `kubectl port-forward` |
| `e` | Open a guarded `kubectl exec` shell |
| `d` | Delete after confirmation |
| `R` | Restart a controller-managed pod after confirmation |

Pod details include owners, conditions, container states, images, resources,
ports, mounts, probes, network identity, service account, QoS, labels,
annotation names, and recent events. Environment and annotation values are not
shown. Application logs are printed exactly as produced and may contain
sensitive values.

### Workloads

The Workloads screen combines Deployments, StatefulSets, DaemonSets, and
CronJobs.

- Enter a replica workload for rollout details, conditions, images, and its
  managed pods.
- Press `R` for a guarded rollout restart.
- Enter a CronJob for recent Jobs and scheduling details.
- Press `s` to confirm suspend or resume.

Production profiles require typing the exact resource reference for destructive
or disruptive actions.

### Network And Events

The Network screen combines Services and Ingresses. Details include Service
selectors, ready EndpointSlice addresses, Ingress routes, and Service backends.

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
