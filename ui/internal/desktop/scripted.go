package desktop

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// scripted answers the dialogs and links a test build cannot show, from a
// folder a test controls. A save dialog answers with a path under saved/, an
// open dialog with the first file in open/, or every one when it can choose
// several, a folder dialog with folder/, and a question agrees unless
// confirm-no exists. Every call is appended to events.log, so a test can tell
// that a link or a folder was opened without anything opening
type scripted struct {
	dir string
	mu  sync.Mutex
}

func (s *scripted) sub(name string) (string, error) {
	p := filepath.Join(s.dir, name)
	return p, os.MkdirAll(p, 0o755)
}

func (s *scripted) record(kind, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(s.dir, "events.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(kind + "\t" + detail + "\n")
}

func (s *scripted) pickFile(req PickRequest) (string, error) {
	if req.Save {
		dir, err := s.sub("saved")
		if err != nil {
			return "", err
		}
		name := filepath.Base(req.Name)
		if req.Name == "" || name == "." || name == string(filepath.Separator) {
			name = "untitled"
		}
		path := filepath.Join(dir, name)
		s.record("save", path)
		return path, nil
	}
	paths, err := s.offered()
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		s.record("open", "")
		return "", nil
	}
	s.record("open", paths[0])
	return paths[0], nil
}

// pickFiles answers a dialog that can choose several files with every file in
// open/, or with none
func (s *scripted) pickFiles(PickRequest) ([]string, error) {
	paths, err := s.offered()
	if err != nil {
		return nil, err
	}
	s.record("open", strings.Join(paths, "\t"))
	return paths, nil
}

// offered lists the files in open/ by name
func (s *scripted) offered() ([]string, error) {
	dir, err := s.sub("open")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, e := range entries {
		if e.Type().IsRegular() {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func (s *scripted) pickFolder(PickRequest) (string, error) {
	dir, err := s.sub("folder")
	if err != nil {
		return "", err
	}
	s.record("folder", dir)
	return dir, nil
}

func (s *scripted) confirm(req ConfirmRequest) bool {
	_, err := os.Stat(filepath.Join(s.dir, "confirm-no"))
	yes := err != nil
	answer := "no"
	if yes {
		answer = "yes"
	}
	s.record("confirm", req.Title+"\t"+answer)
	return yes
}

func (s *scripted) openURL(link string) error {
	s.record("url", link)
	return nil
}

func (s *scripted) openPath(path string) error {
	s.record("path", path)
	return nil
}
