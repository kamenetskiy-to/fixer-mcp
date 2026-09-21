package console

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewConsoleIsWorkFirstAndLaunchCardIsEditable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	model, err := New(Options{StatePath: t.TempDir() + "/console.json", InitialPath: t.TempDir(), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	view := model.View()
	for _, want := range []string{"Работа", "Ресурсы", "Активная работа", "Новая работа"} {
		if !strings.Contains(view, want) {
			t.Fatalf("initial view does not contain %q:\n%s", want, view)
		}
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model = updated.(Model)
	if model.form == nil {
		t.Fatal("n should open the launch card")
	}
	if !strings.Contains(model.View(), "одна редактируемая карточка запуска") {
		t.Fatal("launch card view missing")
	}
}

func TestSearchMovesToResources(t *testing.T) {
	model, err := New(Options{StatePath: t.TempDir() + "/console.json", InitialPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(Model)
	for _, r := range "quota" {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(Model)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.tab != resourcesTab || model.search != nil {
		t.Fatalf("search did not select resources: tab=%d search=%v", model.tab, model.search)
	}
}
