package desktop

import (
	"fmt"
	"log"
	"net/url"

	"github.com/odevine/mimic/ui/internal/apierr"
	"github.com/odevine/mimic/ui/internal/buildinfo"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// System is the bound service for what only the window can do: native dialogs,
// opening links, and the app's version
type System struct {
	app    *application.App
	window application.Window
	smoke  *smoke
	// scripted stands in for the native dialogs and links in a test build
	scripted *scripted
}

// VersionInfo names the builds the app is made of
type VersionInfo struct {
	UI     string `json:"ui"`
	Commit string `json:"commit,omitempty"`
	Engine string `json:"engine"`
	// TestBuild is set in a build made for tests, which never ships
	TestBuild bool `json:"testBuild,omitempty"`
}

// Version reports the ui and engine versions, which are dev for a build that
// was not stamped
func (s *System) Version() VersionInfo {
	return VersionInfo{UI: buildinfo.Version, Commit: buildinfo.Commit, Engine: buildinfo.Engine(), TestBuild: buildinfo.TestBuild}
}

// Filter is one entry in a file dialog's type menu. Pattern is a
// semicolon-separated list of globs such as "*.txt;*.csv"
type Filter struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
}

// PickRequest describes a file or folder dialog. Dir is the folder it opens in
// and Name the filename a save dialog offers
type PickRequest struct {
	Title   string   `json:"title"`
	Filters []Filter `json:"filters,omitempty"`
	Dir     string   `json:"dir,omitempty"`
	Name    string   `json:"name,omitempty"`
	// Save makes a save dialog, which can name a file that does not exist yet
	Save bool `json:"save,omitempty"`
}

// PickFile shows an open or save dialog attached to the window and returns the
// chosen path, or an empty string when the user cancelled
func (s *System) PickFile(req PickRequest) (string, error) {
	if s.scripted != nil {
		return s.scripted.pickFile(req)
	}
	if req.Save {
		d := s.app.Dialog.SaveFile().AttachToWindow(s.window).SetMessage(req.Title).SetFilename(req.Name).CanCreateDirectories(true)
		if req.Dir != "" {
			d.SetDirectory(req.Dir)
		}
		for _, f := range req.Filters {
			d.AddFilter(f.Name, f.Pattern)
		}
		return cancelled(d.PromptForSingleSelection())
	}
	d := s.app.Dialog.OpenFile().AttachToWindow(s.window).SetTitle(req.Title).CanChooseFiles(true).CanChooseDirectories(false)
	if req.Dir != "" {
		d.SetDirectory(req.Dir)
	}
	for _, f := range req.Filters {
		d.AddFilter(f.Name, f.Pattern)
	}
	return cancelled(d.PromptForSingleSelection())
}

// PickFiles shows an open dialog that can choose several files and returns
// their paths, or none when the user cancelled
func (s *System) PickFiles(req PickRequest) ([]string, error) {
	if s.scripted != nil {
		return s.scripted.pickFiles(req)
	}
	d := s.app.Dialog.OpenFile().AttachToWindow(s.window).SetTitle(req.Title).CanChooseFiles(true).CanChooseDirectories(false)
	if req.Dir != "" {
		d.SetDirectory(req.Dir)
	}
	for _, f := range req.Filters {
		d.AddFilter(f.Name, f.Pattern)
	}
	paths, err := d.PromptForMultipleSelection()
	if err != nil {
		return nil, apierr.Wrap(apierr.Internal, "showing the dialog", err)
	}
	out := []string{}
	for _, p := range paths {
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

// PickFolder shows a folder dialog attached to the window and returns the chosen
// path, or an empty string when the user cancelled
func (s *System) PickFolder(req PickRequest) (string, error) {
	if s.scripted != nil {
		return s.scripted.pickFolder(req)
	}
	d := s.app.Dialog.OpenFile().AttachToWindow(s.window).SetTitle(req.Title).CanChooseFiles(false).CanChooseDirectories(true).CanCreateDirectories(true)
	if req.Dir != "" {
		d.SetDirectory(req.Dir)
	}
	return cancelled(d.PromptForSingleSelection())
}

// cancelled turns a dialog's answer into a path. A dismissed dialog answers
// with an empty path and no error
func cancelled(path string, err error) (string, error) {
	if err != nil {
		return "", apierr.Wrap(apierr.Internal, "showing the dialog", err)
	}
	return path, nil
}

// ConfirmRequest describes a yes or no question. Accept labels the button that
// agrees, and the other button is always Cancel
type ConfirmRequest struct {
	Title   string `json:"title"`
	Message string `json:"message"`
	Accept  string `json:"accept"`
}

// Confirm asks a yes or no question in a native dialog attached to the window
// and reports whether the user agreed. The webview has no confirm() of its own
func (s *System) Confirm(req ConfirmRequest) bool {
	if s.scripted != nil {
		return s.scripted.confirm(req)
	}
	answer := make(chan bool, 1)
	d := s.app.Dialog.Question().AttachToWindow(s.window).SetTitle(req.Title).SetMessage(req.Message)
	yes := d.AddButton(req.Accept).OnClick(func() { answer <- true })
	no := d.AddButton("Cancel").OnClick(func() { answer <- false })
	d.SetDefaultButton(no).SetCancelButton(no)
	_ = yes
	d.Show()
	return <-answer
}

// OpenURL opens an http or https link in the user's browser. It refuses any
// other scheme, since the page may be showing text from a card or a list
func (s *System) OpenURL(link string) error {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return apierr.Newf(apierr.BadRequest, "%q is not a web link", link)
	}
	if s.scripted != nil {
		return s.scripted.openURL(link)
	}
	return s.app.Browser.OpenURL(link)
}

// smoke is the launch check's verdict and the deadline it has to arrive by
type smoke struct{ done chan error }

// SmokeResult is how the launch check page reports in. It is a no-op outside
// that check
func (s *System) SmokeResult(ok bool, detail string) {
	if s.smoke == nil {
		return
	}
	var err error
	if !ok {
		err = fmt.Errorf("launch check failed: %s", detail)
	}
	if err == nil {
		log.Printf("mimic: launch check passed: %s", detail)
	}
	select {
	case s.smoke.done <- err:
	default:
	}
}
