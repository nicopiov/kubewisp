package gcloud

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/nicopiov/kubewisp/internal/runner"
)

type call struct {
	name string
	args []string
}

type fakeRunner struct {
	results          []runner.CommandResult
	calls            []call
	interactiveCalls []call
	interactiveInput io.Reader
}

func (f *fakeRunner) LookPath(string) (string, error) {
	return "/bin/gcloud", nil
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) runner.CommandResult {
	f.calls = append(f.calls, call{name: name, args: args})
	result := f.results[0]
	f.results = f.results[1:]
	return result
}

func (f *fakeRunner) RunInteractive(_ context.Context, input io.Reader, _ io.Writer, _ io.Writer, name string, args ...string) error {
	f.interactiveCalls = append(f.interactiveCalls, call{name: name, args: args})
	f.interactiveInput = input
	return nil
}

func TestLoginUsesBrowserOAuthWithoutTerminalPrompts(t *testing.T) {
	t.Parallel()

	commandRunner := &fakeRunner{}
	if err := NewClient(commandRunner).Login(context.Background(), strings.NewReader("unused"), io.Discard, io.Discard); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if commandRunner.interactiveInput != nil {
		t.Fatal("Login() passed terminal input to gcloud")
	}
	want := []string{"auth", "login", "--quiet"}
	if !reflect.DeepEqual(commandRunner.interactiveCalls[0].args, want) {
		t.Fatalf("login args = %#v, want %#v", commandRunner.interactiveCalls[0].args, want)
	}
}

func TestListClustersDetectsRegionalAndZonalLocations(t *testing.T) {
	t.Parallel()

	commandRunner := &fakeRunner{
		results: []runner.CommandResult{{
			Stdout: `[{"name":"zonal","location":"europe-west1-b"},{"name":"regional","location":"europe-west1"}]`,
		}},
	}

	clusters, err := NewClient(commandRunner).ListClusters(context.Background(), "demo")

	if err != nil {
		t.Fatalf("ListClusters() error = %v", err)
	}
	if got := clusters[0].LocationType; got != "region" {
		t.Fatalf("regional LocationType = %q, want region", got)
	}
	if got := clusters[1].LocationType; got != "zone" {
		t.Fatalf("zonal LocationType = %q, want zone", got)
	}
	wantArgs := []string{"container", "clusters", "list", "--project", "demo", "--format=json(name,location)"}
	if !reflect.DeepEqual(commandRunner.calls[0].args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", commandRunner.calls[0].args, wantArgs)
	}
}

func TestGetCredentialsUsesLocationFlag(t *testing.T) {
	t.Parallel()

	commandRunner := &fakeRunner{
		results: []runner.CommandResult{{}, {}},
	}
	client := NewClient(commandRunner)

	if err := client.GetCredentials(context.Background(), "demo", Cluster{
		Name:         "regional",
		Location:     "europe-west1",
		LocationType: "region",
	}); err != nil {
		t.Fatalf("GetCredentials(region) error = %v", err)
	}
	if err := client.GetCredentials(context.Background(), "demo", Cluster{
		Name:         "zonal",
		Location:     "europe-west1-b",
		LocationType: "zone",
	}); err != nil {
		t.Fatalf("GetCredentials(zone) error = %v", err)
	}

	if got := commandRunner.calls[0].args[4]; got != "--region" {
		t.Fatalf("regional location flag = %q, want --region", got)
	}
	if got := commandRunner.calls[1].args[4]; got != "--zone" {
		t.Fatalf("zonal location flag = %q, want --zone", got)
	}
}

func TestIsReauthenticationError(t *testing.T) {
	t.Parallel()

	for _, message := range []string{
		"There was a problem refreshing your current auth tokens: reauthentication failed",
		"invalid_grant: Token has been expired or revoked",
		"Please run: gcloud auth login",
	} {
		if !IsReauthenticationError(errors.New(message)) {
			t.Errorf("IsReauthenticationError(%q) = false", message)
		}
	}
	if IsReauthenticationError(errors.New("permission denied")) {
		t.Fatal("IsReauthenticationError(permission denied) = true")
	}
}
