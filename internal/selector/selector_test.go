package selector

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModelStartsAtInitialOption(t *testing.T) {
	t.Parallel()

	selectorModel := newModel("Choose", []string{"api", "default", "workers"}, "default")

	if selectorModel.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", selectorModel.cursor)
	}
}

func TestModelNavigatesAndSelects(t *testing.T) {
	t.Parallel()

	selectorModel := newModel("Choose", []string{"api", "default", "workers"}, "api")
	updated, _ := selectorModel.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, command := updated.(model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	final := updated.(model)

	if final.cursor != 1 || !final.selected {
		t.Fatalf("model = %#v, want second option selected", final)
	}
	if command == nil {
		t.Fatal("enter command = nil, want quit command")
	}
}

func TestModelCancels(t *testing.T) {
	t.Parallel()

	selectorModel := newModel("Choose", []string{"api"}, "api")
	updated, command := selectorModel.Update(tea.KeyMsg{Type: tea.KeyEsc})
	final := updated.(model)

	if !final.cancelled {
		t.Fatal("cancelled = false, want true")
	}
	if command == nil {
		t.Fatal("escape command = nil, want quit command")
	}
}
