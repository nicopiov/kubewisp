package kube

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type ConnectivityReport struct {
	ServerVersion string
	Namespace     string
}

type ConnectivityChecker interface {
	Check(ctx context.Context, namespace string) (ConnectivityReport, error)
}

type ClientFactory interface {
	Client() (kubernetes.Interface, error)
}

type KubeconfigClientFactory struct{}

func (KubeconfigClientFactory) Client() (kubernetes.Interface, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules,
		&clientcmd.ConfigOverrides{},
	)
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	return client, nil
}

type Checker struct {
	factory ClientFactory
}

func NewConnectivityChecker() *Checker {
	return &Checker{factory: KubeconfigClientFactory{}}
}

func NewConnectivityCheckerWithFactory(factory ClientFactory) *Checker {
	return &Checker{factory: factory}
}

func (c *Checker) Check(ctx context.Context, namespace string) (ConnectivityReport, error) {
	client, err := c.factory.Client()
	if err != nil {
		return ConnectivityReport{}, err
	}

	serverVersion, err := client.Discovery().ServerVersion()
	if err != nil {
		return ConnectivityReport{}, diagnose("reach Kubernetes API", err)
	}
	if _, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); err != nil {
		return ConnectivityReport{}, diagnose(fmt.Sprintf("access namespace %q", namespace), err)
	}

	return ConnectivityReport{
		ServerVersion: versionString(serverVersion),
		Namespace:     namespace,
	}, nil
}

func versionString(info *version.Info) string {
	if info == nil {
		return "unknown"
	}
	if info.GitVersion != "" {
		return info.GitVersion
	}
	return strings.TrimSpace(info.Major + "." + info.Minor)
}

func diagnose(action string, err error) error {
	message := strings.ToLower(err.Error())

	switch {
	case strings.Contains(message, "gke-gcloud-auth-plugin not found"),
		strings.Contains(message, "executable gke-gcloud-auth-plugin"):
		return fmt.Errorf(
			"%s: gke-gcloud-auth-plugin is required; install it from https://cloud.google.com/kubernetes-engine/docs/how-to/cluster-access-for-kubectl#install_plugin: %w",
			action,
			err,
		)
	case strings.Contains(message, "forbidden"):
		return fmt.Errorf("%s: Kubernetes RBAC denied access: %w", action, err)
	case strings.Contains(message, "unauthorized"):
		return fmt.Errorf("%s: Kubernetes credentials are unauthorized; refresh GKE credentials: %w", action, err)
	case strings.Contains(message, "connection refused"),
		strings.Contains(message, "no route to host"),
		strings.Contains(message, "i/o timeout"),
		strings.Contains(message, "context deadline exceeded"):
		return fmt.Errorf("%s: Kubernetes API is unreachable; check VPN, tunnel, or network access: %w", action, err)
	default:
		return fmt.Errorf("%s: %w", action, err)
	}
}
