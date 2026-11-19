package web

import "github.com/daknoblo/waim/internal/version"

func (l Layout) TabTitle() string {
	title := l.T.T("app.title") + " — " + l.T.T("app.tagline")
	if (version.Info{Version: l.Version}).IsDevelopment() {
		return "[DEV] " + title
	}
	return title
}
