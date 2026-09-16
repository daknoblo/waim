package server

import (
	"net/http"
	"time"

	"github.com/daknoblo/waim/internal/web"
)

func (s *Server) activityViews() []web.ActivityView {
	return web.BuildActivities(s.activities.Snapshot(), s.cfg.Get().TMDB.APIKey != "", time.Now())
}

func (s *Server) handlePartialActivity(w http.ResponseWriter, r *http.Request) {
	s.renderPartialPlain(w, r, web.ActivityPanel(s.translator(r), s.activityViews()))
}
