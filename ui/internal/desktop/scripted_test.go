package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptedSaveAnswersUnderSaved(t *testing.T) {
	s := &scripted{dir: t.TempDir()}
	got, err := s.pickFile(PickRequest{Save: true, Name: "Sol Ring.png", Dir: "/somewhere/else"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(s.dir, "saved", "Sol Ring.png"); got != want {
		t.Errorf("save path = %q, want %q", got, want)
	}
	if st, err := os.Stat(filepath.Dir(got)); err != nil || !st.IsDir() {
		t.Errorf("the saved folder was not made: %v", err)
	}
}

func TestScriptedSaveKeepsToItsFolderWhateverTheName(t *testing.T) {
	s := &scripted{dir: t.TempDir()}
	got, _ := s.pickFile(PickRequest{Save: true, Name: "../../escape.png"})
	if filepath.Dir(got) != filepath.Join(s.dir, "saved") {
		t.Errorf("a name with separators escaped: %q", got)
	}
	got, _ = s.pickFile(PickRequest{Save: true})
	if filepath.Base(got) != "untitled" {
		t.Errorf("a save with no name = %q, want untitled", got)
	}
}

func TestScriptedOpenAnswersWithTheFirstFileOrCancels(t *testing.T) {
	s := &scripted{dir: t.TempDir()}
	if got, err := s.pickFile(PickRequest{}); err != nil || got != "" {
		t.Fatalf("with nothing to open = %q, %v, want a cancel", got, err)
	}
	open := filepath.Join(s.dir, "open")
	for _, n := range []string{"b.txt", "a.txt"} {
		os.WriteFile(filepath.Join(open, n), []byte("x"), 0o644)
	}
	os.Mkdir(filepath.Join(open, "0-dir"), 0o755)
	got, _ := s.pickFile(PickRequest{})
	if filepath.Base(got) != "a.txt" {
		t.Errorf("open = %q, want a.txt", got)
	}
}

func TestScriptedFolderAndConfirm(t *testing.T) {
	s := &scripted{dir: t.TempDir()}
	dir, err := s.pickFolder(PickRequest{})
	if err != nil || filepath.Base(dir) != "folder" {
		t.Fatalf("folder = %q, %v", dir, err)
	}
	if !s.confirm(ConfirmRequest{Title: "Quit?"}) {
		t.Error("a question should agree by default")
	}
	os.WriteFile(filepath.Join(s.dir, "confirm-no"), nil, 0o644)
	if s.confirm(ConfirmRequest{Title: "Quit?"}) {
		t.Error("confirm-no should make a question decline")
	}
}

func TestScriptedLinksAreRecordedAndNeverOpened(t *testing.T) {
	s := &scripted{dir: t.TempDir()}
	s.openURL("https://scryfall.com/")
	s.openPath("/tmp/out")
	s.pickFile(PickRequest{Save: true, Name: "x.png"})
	raw, err := os.ReadFile(filepath.Join(s.dir, "events.log"))
	if err != nil {
		t.Fatal(err)
	}
	log := string(raw)
	for _, want := range []string{"url\thttps://scryfall.com/", "path\t/tmp/out", "save\t"} {
		if !strings.Contains(log, want) {
			t.Errorf("events.log lacks %q:\n%s", want, log)
		}
	}
}

func TestScriptedOpenOfSeveralAnswersWithEveryFile(t *testing.T) {
	s := &scripted{dir: t.TempDir()}
	if got, err := s.pickFiles(PickRequest{}); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("with nothing to open = %q, %v, want an empty list", got, err)
	}
	open := filepath.Join(s.dir, "open")
	for _, n := range []string{"tf-front.psd", "tf-back.psd"} {
		os.WriteFile(filepath.Join(open, n), []byte("x"), 0o644)
	}
	os.Mkdir(filepath.Join(open, "0-dir"), 0o755)
	got, _ := s.pickFiles(PickRequest{})
	if len(got) != 2 || filepath.Base(got[0]) != "tf-back.psd" || filepath.Base(got[1]) != "tf-front.psd" {
		t.Errorf("open = %q, want both PSDs", got)
	}
}
