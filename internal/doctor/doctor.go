package doctor

import (
	"context"
	"fmt"
	"io"

	"github.com/nicopiov/kubewisp/internal/runner"
)

type Dependency struct {
	Name        string
	Description string
	InstallURL  string
}

type Check struct {
	Dependency Dependency
	Path       string
	Err        error
}

func (c Check) Passed() bool {
	return c.Err == nil
}

type Report struct {
	Checks []Check
}

type Reporter interface {
	Run(ctx context.Context) Report
}

func (r Report) Healthy() bool {
	for _, check := range r.Checks {
		if !check.Passed() {
			return false
		}
	}
	return true
}

type Service struct {
	runner       runner.CommandRunner
	dependencies []Dependency
}

func NewService(commandRunner runner.CommandRunner) *Service {
	return &Service{
		runner: commandRunner,
		dependencies: []Dependency{
			{
				Name:        "gcloud",
				Description: "Google Cloud CLI",
				InstallURL:  "https://cloud.google.com/sdk/docs/install",
			},
			{
				Name:        "kubectl",
				Description: "Kubernetes command-line tool",
				InstallURL:  "https://kubernetes.io/docs/tasks/tools/",
			},
			{
				Name:        "gke-gcloud-auth-plugin",
				Description: "GKE authentication plugin",
				InstallURL:  "https://cloud.google.com/kubernetes-engine/docs/how-to/cluster-access-for-kubectl#install_plugin",
			},
		},
	}
}

func (s *Service) Run(_ context.Context) Report {
	report := Report{
		Checks: make([]Check, 0, len(s.dependencies)),
	}

	for _, dependency := range s.dependencies {
		path, err := s.runner.LookPath(dependency.Name)
		report.Checks = append(report.Checks, Check{
			Dependency: dependency,
			Path:       path,
			Err:        err,
		})
	}

	return report
}

func WriteReport(w io.Writer, report Report) {
	for _, check := range report.Checks {
		if check.Passed() {
			fmt.Fprintf(w, "PASS  %-24s %s\n", check.Dependency.Name, check.Path)
			continue
		}

		fmt.Fprintf(w, "FAIL  %-24s not found\n", check.Dependency.Name)
		fmt.Fprintf(w, "      Install %s: %s\n", check.Dependency.Description, check.Dependency.InstallURL)
	}

	if report.Healthy() {
		fmt.Fprintln(w, "\nAll required dependencies are available.")
		return
	}

	fmt.Fprintln(w, "\nOne or more required dependencies are missing.")
}
