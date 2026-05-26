package main

import (
	"context"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

var tmpl *template.Template

var templateFuncs = template.FuncMap{
	// joinTags renders a tag slice as a comma-separated string for form inputs.
	"joinTags": func(tags []string) string {
		return strings.Join(tags, ", ")
	},
	// fmtDate shows a short human date for list items.
	"fmtDate": func(t time.Time) string {
		return t.Format("Jan 2")
	},
	// fmtDateTime shows full date + time for tooltips/detail views.
	"fmtDateTime": func(t time.Time) string {
		return t.Format("Jan 2, 2006 15:04")
	},
	// truncate clips a string for list previews.
	"truncate": func(s string, n int) string {
		r := []rune(s)
		if len(r) <= n {
			return s
		}
		return string(r[:n]) + "…"
	},
}

// initTemplates parses every template under web/templates/. Call this once
// after fs.Sub(embedWeb, "web") so paths are relative to the web root.
func initTemplates(webFS fs.FS) error {
	t, err := template.New("").Funcs(templateFuncs).ParseFS(webFS,
		"templates/*.html",
		"templates/partials/*.html",
	)
	if err != nil {
		return err
	}
	tmpl = t
	slog.Info("templates parsed")
	return nil
}

// render writes a named template (as defined by {{define "name"}}) to w.
func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("template error", "name", name, "error", err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// requireAuthHTML is like requireAuth but sends browsers to /login and
// responds to htmx requests with HX-Redirect rather than a bare 401.
func (s *Server) requireAuthHTML(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := s.sessionFromRequest(r)
		if err != nil || sess == nil {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusOK)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
