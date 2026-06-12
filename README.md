# Kubewisp

Kubewisp is an early-stage Go CLI/TUI for safely navigating and operating GKE
clusters.

## Current Status

Build Kubewisp and run the guided setup:

```bash
make build
./bin/kubewisp doctor
./bin/kubewisp init
./bin/kubewisp cluster status
```

Install a published version with Go:

```bash
go install github.com/nicopiov/kubewisp/cmd/kubewisp@latest
kubewisp version
kubewisp init
```

After initialization, running Kubewisp without a subcommand opens the TUI
dashboard:

```bash
./bin/kubewisp
./bin/kubewisp tui
```

Dashboard controls:

```text
1 / 2 / 3 / 4 / 5 / 6   dashboard / namespaces / pods / workloads / events / doctor
tab / left/right navigate screens
up/down / j/k   navigate lists
enter           switch selected namespace
r               refresh current screen
q               quit
```

In the TUI pod screen, Enter opens troubleshooting details and `l` opens the
latest 200 log lines. Press `p` from the pod list or details to select a
declared TCP port and start `kubectl port-forward`. Multi-container pods open a
container chooser. Use Up/Down or `j`/`k` to scroll details and logs, and Esc
to return to the pod list. While port forwarding, Ctrl+C stops only the tunnel
and returns to the Kubewisp TUI.

Press `e` from the pod list or details to open `/bin/sh` in a selected
container. Kubewisp shows the full target context before entering the shell.
Production profiles require pressing `y` on the confirmation screen. Exiting
the shell returns to the Pods screen.

The dashboard, pod list, and pod details use text-backed colored health
indicators:

```text
● healthy     running and fully ready; historical restarts remain visible
● warning     pending, partially ready, or restarted within the last 10 minutes
● unhealthy   crash, error, failure, or image-pull states
```

Batch workloads use lifecycle labels instead of service health labels. CronJobs
are shown as `running`, `scheduled`, or `suspended`. Job-owned pods are shown as
`running`, `pending`, `completed`, or unhealthy when they genuinely fail.
Completed pods are counted separately from warnings on the dashboard.

Pod details include owners, conditions, container images and states, requests,
limits, ports, mounts, probes, environment variable names, volumes, labels,
annotation names, and recent events.

The Doctor screen shows local dependency checks and verifies Kubernetes API and
selected namespace access. Press `r` to rerun the checks.

`kubewisp init` uses the active `gcloud` account or launches `gcloud auth
login`, discovers accessible projects and GKE clusters, fetches cluster
credentials, and saves the selected profile automatically.

`kubewisp cluster status` uses client-go to verify the Kubernetes API and the
saved profile namespace are accessible.

Kubewisp never stores Google credentials. It stores only local preferences in
`~/.config/kubewisp/config.yaml`; users normally do not need to create, inspect,
or specify this file. The doctor checks for `gcloud`, `kubectl`, and
`gke-gcloud-auth-plugin`.

Available profile commands:

```bash
kubewisp profile list
kubewisp profile show [name]
kubewisp profile use <name>
```

Namespace commands use the current profile and do not modify kubeconfig:

```bash
kubewisp namespace list
kubewisp namespace switch
kubewisp namespace switch <namespace>
```

Running `namespace switch` without a name opens an interactive selector. Use
the arrow keys or `j`/`k` to navigate, Enter to select, and Esc or `q` to
cancel. Passing a namespace directly remains available for scripts.

Pod commands inspect the namespace selected for the current profile:

```bash
kubewisp pods list
kubewisp pods describe
kubewisp pods describe <pod>
kubewisp pods logs
kubewisp pods logs <pod>
kubewisp pods port-forward
kubewisp pods port-forward <pod>
kubewisp pods exec
kubewisp pods exec <pod>
kubewisp pods delete <pod>
kubewisp pods restart <pod>
```

Running `pods describe` or `pods logs` without a pod name opens a keyboard
selector showing pod status, readiness, and restart count. Passing a pod name
directly remains available for scripts.

Pod describe is a curated, safe troubleshooting view rather than raw `kubectl
describe` output. It includes conditions, current and previous container states,
exit codes, resources, mounts, probes, pod and host IPs, service account, QoS,
and recent events. It shows environment variable names and annotation names
only, and never queries or renders Secret values.

Pod logs default to the latest 200 lines. Single-container pods are selected
automatically; multi-container pods open the keyboard selector.

```bash
kubewisp pods logs <pod> --tail 500
kubewisp pods logs <pod> -c <container> --follow
kubewisp pods logs <pod> --previous --timestamps
```

Application logs may contain sensitive values because Kubewisp prints the log
stream exactly as the workload produced it.

Port forwarding selects a pod and declared TCP container port interactively.
The local port defaults to the remote port, and Kubewisp temporarily hands the
terminal to `kubectl`. In the TUI, Ctrl+C stops the tunnel and resumes Kubewisp.
Direct CLI ports are also supported:

```bash
kubewisp pods port-forward <pod> --port 8080
kubewisp pods port-forward <pod> --port 8080 --local-port 18080
```

Pod exec selects the pod and container interactively, shows the complete target
context, and requires confirmation for production profiles:

```bash
kubewisp pods exec <pod>
kubewisp pods exec <pod> --container app
kubewisp pods exec <pod> --container app --shell /bin/bash
```

Pod delete and restart are guarded destructive actions. Both always show the
full target context and require confirmation. Production profiles require
typing the exact pod name. Restart is implemented as pod deletion and is
blocked unless Kubernetes reports a controller owner that can recreate it.

In the TUI, press `d` to delete or uppercase `R` to restart the selected pod.

The Workloads screen combines Deployments, StatefulSets, DaemonSets, and
CronJobs. Replica workloads show ready, desired, updated, and available counts.
CronJobs show their schedule, active runs, suspended state, and latest run.
Press Enter on a CronJob to inspect its settings and recent Jobs. Press `s`
from the CronJob row or details to review and toggle between active and
suspended scheduling.
Press uppercase `R` on a Deployment, StatefulSet, or DaemonSet to review and
trigger a rollout restart. This restarts every pod managed by the selected
workload, always requires confirmation, and requires typing the exact
`Kind/name` for production profiles. CronJobs do not support rollout restart.

Suspend and resume always require confirmation. Production profiles require
typing the exact `CronJob/name`.

Workload CLI commands:

```bash
kubewisp workloads list
kubewisp workloads restart
kubewisp workloads restart Deployment/api
kubewisp workloads cronjob describe
kubewisp workloads cronjob describe cleanup
kubewisp workloads cronjob suspend cleanup
kubewisp workloads cronjob resume cleanup
```

The Events screen groups repeated Warning events across the selected namespace
and sorts them by most recently seen. Press Enter on a pod event to open its
troubleshooting details, or on a Deployment, StatefulSet, DaemonSet, or CronJob
event to jump to that workload. The same feed is available from the CLI:

```bash
kubewisp events
```

For development and tests, override the config location with `--config <path>`
or `KUBEWISP_CONFIG`. A `config.example.yaml` fixture is included for manual
command testing.

See [ROADMAP.md](ROADMAP.md) for the proposed architecture, milestones, testing
strategy, and current decisions.

## Development

Requires Go 1.26.

```bash
make verify
```
