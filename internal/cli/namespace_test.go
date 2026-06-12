package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/selector"
)

type fakeNamespaces struct {
	names       []string
	listErr     error
	existsErr   error
	existsCalls []string
}

func (f *fakeNamespaces) List(context.Context) ([]string, error) {
	return f.names, f.listErr
}

func (f *fakeNamespaces) Exists(_ context.Context, name string) error {
	f.existsCalls = append(f.existsCalls, name)
	return f.existsErr
}

type fakeSelector struct {
	selected string
	err      error
	options  []string
	initial  string
}

func (f *fakeSelector) Select(
	_ context.Context,
	_ io.Reader,
	_ io.Writer,
	_ string,
	options []string,
	initial string,
) (string, error) {
	f.options = options
	f.initial = initial
	return f.selected, f.err
}

func TestNamespaceListMarksCurrentNamespace(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	namespaces := &fakeNamespaces{names: []string{"api", "default"}}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, Namespaces: namespaces})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", path, "namespace", "list"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "* api") {
		t.Fatalf("unexpected output:\n%s", output.String())
	}
}

func TestNamespaceSwitchPersistsSelection(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	namespaces := &fakeNamespaces{}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, Namespaces: namespaces})
	command.SetArgs([]string{"--config", path, "namespace", "switch", "workers"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !reflect.DeepEqual(namespaces.existsCalls, []string{"workers"}) {
		t.Fatalf("Exists calls = %#v, want workers", namespaces.existsCalls)
	}
	assertCurrentNamespace(t, path, "workers")
}

func TestNamespaceSwitchInteractivePersistsSelection(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	namespaces := &fakeNamespaces{names: []string{"api", "workers"}}
	terminalSelector := &fakeSelector{selected: "workers"}
	command := NewRootCommand(Dependencies{
		Runner:     fakeRunner{},
		Namespaces: namespaces,
		Selector:   terminalSelector,
	})
	command.SetArgs([]string{"--config", path, "namespace", "switch"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if terminalSelector.initial != "api" {
		t.Fatalf("initial selection = %q, want api", terminalSelector.initial)
	}
	if !reflect.DeepEqual(terminalSelector.options, []string{"api", "workers"}) {
		t.Fatalf("selector options = %#v", terminalSelector.options)
	}
	assertCurrentNamespace(t, path, "workers")
}

func TestNamespaceSwitchInteractiveCancellationDoesNotPersist(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	namespaces := &fakeNamespaces{names: []string{"api", "workers"}}
	command := NewRootCommand(Dependencies{
		Runner:     fakeRunner{},
		Namespaces: namespaces,
		Selector:   &fakeSelector{err: selector.ErrCancelled},
	})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", path, "namespace", "switch"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "Namespace selection cancelled.") {
		t.Fatalf("unexpected output:\n%s", output.String())
	}
	if len(namespaces.existsCalls) != 0 {
		t.Fatalf("Exists calls = %#v, want none", namespaces.existsCalls)
	}
	assertCurrentNamespace(t, path, "")
}

func TestNamespaceSwitchDoesNotPersistInaccessibleNamespace(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	namespaces := &fakeNamespaces{existsErr: errors.New("forbidden")}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, Namespaces: namespaces})
	command.SetArgs([]string{"--config", path, "namespace", "switch", "secret"})

	if err := command.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
	assertCurrentNamespace(t, path, "")
}

func assertCurrentNamespace(t *testing.T, path, want string) {
	t.Helper()

	cfg, err := (config.Store{Path: path}).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Profiles["staging"].CurrentNamespace; got != want {
		t.Fatalf("CurrentNamespace = %q, want %q", got, want)
	}
}
