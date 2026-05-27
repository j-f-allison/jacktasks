package syncserver

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/j-f-allison/jacktasks/internal/store"
)

// webSessionLimit caps how many sessions the read-only web view renders.
const webSessionLimit = 500

// dayGroup is one calendar day's worth of sessions for the web view.
type dayGroup struct {
	Date     string // e.g. "Monday, 26 May 2026"
	Sessions []webSession
}

// webSession is a single row in the rendered list, pre-formatted for display.
type webSession struct {
	Time      string // start time, e.g. "14:05"
	Project   string // "—" when no project
	Category  string
	Duration  string // e.g. "25m" or "1h 05m"
	Status    string // raw status value (drives the CSS class)
	Completed bool
	Notes     string
}

// handleSessions renders the read-only, day-grouped list of logged sessions.
// It is intentionally unauthenticated — the server binds only to the Tailscale
// interface, so network reachability is the access control (same posture as
// /healthz). Times are rendered in the server's local timezone.
func handleSessions(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		views, err := st.ListSessionViews(r.Context(), webSessionLimit)
		if err != nil {
			log.Printf("web sessions: %v", err)
			http.Error(w, "failed to load sessions", http.StatusInternalServerError)
			return
		}

		groups := groupByDay(views)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := sessionsTmpl.Execute(w, struct {
			Groups []dayGroup
			Total  int
		}{Groups: groups, Total: len(views)}); err != nil {
			log.Printf("web sessions render: %v", err)
		}
	}
}

// groupByDay turns newest-first session views into day-labelled groups,
// preserving the newest-first order both across and within days.
func groupByDay(views []store.SessionView) []dayGroup {
	var groups []dayGroup
	var curKey string
	for _, v := range views {
		local := v.StartedAt.Local()
		key := local.Format("2006-01-02")
		ws := webSession{
			Time:      local.Format("15:04"),
			Project:   v.ProjectName,
			Category:  v.CategoryName,
			Duration:  formatDuration(v.ActualDurationSec),
			Status:    string(v.Status),
			Completed: v.Status == store.SessionCompleted,
			Notes:     v.EndNotes,
		}
		if ws.Project == "" {
			ws.Project = "—"
		}
		if ws.Category == "" {
			ws.Category = "—"
		}
		if key != curKey {
			groups = append(groups, dayGroup{Date: local.Format("Monday, 2 January 2006")})
			curKey = key
		}
		last := &groups[len(groups)-1]
		last.Sessions = append(last.Sessions, ws)
	}
	return groups
}

// formatDuration renders a duration in seconds as "Mm" or "Hh MMm".
func formatDuration(sec int) string {
	d := time.Duration(sec) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// sessionsTmpl is the single page rendered by handleSessions. Tokyo Night
// palette to match the TUI.
var sessionsTmpl = template.Must(template.New("sessions").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>jacktasks — sessions</title>
<style>
  :root {
    --bg: #1a1b26; --panel: #24283b; --fg: #c0caf5; --muted: #565f89;
    --purple: #bb9af7; --blue: #7aa2f7; --cyan: #7dcfff; --green: #9ece6a; --orange: #e0af68;
  }
  * { box-sizing: border-box; }
  body { margin: 0; background: var(--bg); color: var(--fg);
    font: 15px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
  .wrap { max-width: 820px; margin: 0 auto; padding: 2rem 1.25rem 4rem; }
  h1 { font-size: 1.4rem; margin: 0 0 .25rem;
    background: linear-gradient(90deg, var(--purple), var(--blue), var(--cyan));
    -webkit-background-clip: text; background-clip: text; color: transparent; }
  .sub { color: var(--muted); margin: 0 0 2rem; font-size: .9rem; }
  h2 { font-size: .95rem; font-weight: 600; color: var(--blue);
    margin: 1.75rem 0 .5rem; padding-bottom: .35rem; border-bottom: 1px solid var(--panel); }
  .row { display: flex; gap: .85rem; align-items: baseline; padding: .55rem .25rem;
    border-bottom: 1px solid rgba(86,95,137,.18); }
  .time { color: var(--muted); font-variant-numeric: tabular-nums; min-width: 3.2em; }
  .what { flex: 1; min-width: 0; }
  .what .pc { font-weight: 600; }
  .what .pc .cat { color: var(--cyan); font-weight: 500; }
  .what .notes { color: var(--muted); font-size: .88rem; margin-top: .15rem;
    white-space: pre-wrap; word-break: break-word; }
  .dur { color: var(--orange); font-variant-numeric: tabular-nums; min-width: 4.5em; text-align: right; }
  .badge { font-size: .7rem; text-transform: uppercase; letter-spacing: .04em;
    padding: .1rem .4rem; border-radius: 4px; min-width: 5.2em; text-align: center; }
  .badge.completed { background: rgba(158,206,106,.15); color: var(--green); }
  .badge.ended_early { background: rgba(224,175,104,.15); color: var(--orange); }
  .empty { color: var(--muted); margin-top: 2rem; }
</style>
</head>
<body>
<div class="wrap">
  <h1>jacktasks</h1>
  <p class="sub">{{.Total}} logged session{{if ne .Total 1}}s{{end}}</p>
  {{if not .Groups}}
    <p class="empty">No sessions logged yet.</p>
  {{end}}
  {{range .Groups}}
    <h2>{{.Date}}</h2>
    {{range .Sessions}}
      <div class="row">
        <span class="time">{{.Time}}</span>
        <span class="what">
          <span class="pc">{{.Project}} <span class="cat">/ {{.Category}}</span></span>
          {{if .Notes}}<div class="notes">{{.Notes}}</div>{{end}}
        </span>
        <span class="dur">{{.Duration}}</span>
        <span class="badge {{.Status}}">{{if .Completed}}done{{else}}early{{end}}</span>
      </div>
    {{end}}
  {{end}}
</div>
</body>
</html>
`))
