package gcloud

import (
	"context"
	"fmt"

	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/kube"
)

type ClientResetter interface {
	Reset()
}

type ProfileConnector struct {
	client       *Client
	connectivity kube.ConnectivityChecker
	resetter     ClientResetter
}

func NewProfileConnector(client *Client, connectivity kube.ConnectivityChecker, resetter ClientResetter) *ProfileConnector {
	return &ProfileConnector{client: client, connectivity: connectivity, resetter: resetter}
}

func (c *ProfileConnector) Connect(ctx context.Context, profile config.Profile) error {
	if err := c.client.SetProject(ctx, profile.ProjectID); err != nil {
		return err
	}
	if err := c.client.GetCredentials(ctx, profile.ProjectID, ClusterFromProfile(profile)); err != nil {
		return err
	}
	c.resetter.Reset()
	namespace := profile.CurrentNamespace
	if namespace == "" {
		namespace = profile.DefaultNamespace
	}
	if _, err := c.connectivity.Check(ctx, namespace); err != nil {
		return fmt.Errorf("verify switched profile connectivity: %w", err)
	}
	return nil
}
