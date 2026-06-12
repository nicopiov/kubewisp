package gcloud

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/runner"
)

var zonePattern = regexp.MustCompile(`-[a-z]$`)

type Cluster struct {
	Name         string `json:"name"`
	Location     string `json:"location"`
	LocationType string `json:"-"`
}

type Client struct {
	runner runner.CommandRunner
}

func NewClient(commandRunner runner.CommandRunner) *Client {
	return &Client{runner: commandRunner}
}

func (c *Client) ActiveAccount(ctx context.Context) (string, error) {
	result := c.runner.Run(
		ctx,
		"gcloud",
		"auth",
		"list",
		"--filter=status:ACTIVE",
		"--format=value(account)",
	)
	if err := resultError("get active Google account", result); err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (c *Client) Login(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := c.runner.RunInteractive(ctx, stdin, stdout, stderr, "gcloud", "auth", "login"); err != nil {
		return fmt.Errorf("run gcloud auth login: %w", err)
	}
	return nil
}

func (c *Client) ListProjects(ctx context.Context) ([]string, error) {
	result := c.runner.Run(ctx, "gcloud", "projects", "list", "--format=value(projectId)")
	if err := resultError("list Google Cloud projects", result); err != nil {
		return nil, err
	}

	projects := nonEmptyLines(result.Stdout)
	sort.Strings(projects)
	return projects, nil
}

func (c *Client) ListClusters(ctx context.Context, projectID string) ([]Cluster, error) {
	result := c.runner.Run(
		ctx,
		"gcloud",
		"container",
		"clusters",
		"list",
		"--project",
		projectID,
		"--format=json(name,location)",
	)
	if err := resultError("list GKE clusters", result); err != nil {
		return nil, err
	}

	var clusters []Cluster
	if err := json.Unmarshal([]byte(result.Stdout), &clusters); err != nil {
		return nil, fmt.Errorf("decode GKE cluster list: %w", err)
	}
	for index := range clusters {
		if zonePattern.MatchString(clusters[index].Location) {
			clusters[index].LocationType = config.LocationZone
		} else {
			clusters[index].LocationType = config.LocationRegion
		}
	}
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].Name == clusters[j].Name {
			return clusters[i].Location < clusters[j].Location
		}
		return clusters[i].Name < clusters[j].Name
	})
	return clusters, nil
}

func (c *Client) SetProject(ctx context.Context, projectID string) error {
	result := c.runner.Run(ctx, "gcloud", "config", "set", "project", projectID)
	return resultError("set active Google Cloud project", result)
}

func (c *Client) GetCredentials(ctx context.Context, projectID string, cluster Cluster) error {
	locationFlag := "--region"
	if cluster.LocationType == config.LocationZone {
		locationFlag = "--zone"
	}

	result := c.runner.Run(
		ctx,
		"gcloud",
		"container",
		"clusters",
		"get-credentials",
		cluster.Name,
		locationFlag,
		cluster.Location,
		"--project",
		projectID,
	)
	return resultError("get GKE cluster credentials", result)
}

func resultError(action string, result runner.CommandResult) error {
	if result.Err == nil {
		return nil
	}

	message := strings.TrimSpace(result.Stderr)
	if message == "" {
		message = result.Err.Error()
	}
	return fmt.Errorf("%s: %s", action, message)
}

func nonEmptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
