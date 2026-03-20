package main

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/http"
	"strings"
)

//go:embed templates
var templateFS embed.FS

type Templates struct {
	t *template.Template
}

func NewTemplates() *Templates {
	funcs := template.FuncMap{
		"humanBytes": func(b int64) string {
			if b == 0 {
				return "0 B"
			}
			units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
			i := int(math.Log(float64(b)) / math.Log(1024))
			if i >= len(units) {
				i = len(units) - 1
			}
			v := float64(b) / math.Pow(1024, float64(i))
			if v == math.Floor(v) {
				return fmt.Sprintf("%.0f %s", v, units[i])
			}
			return fmt.Sprintf("%.1f %s", v, units[i])
		},
		"badgeClass": func(status string) string {
			switch strings.ToLower(status) {
			case "running":
				return "badge-running"
			case "stopped":
				return "badge-stopped"
			default:
				return "badge-other"
			}
		},
		"lower": strings.ToLower,
	}

	t := template.Must(
		template.New("").Funcs(funcs).ParseFS(templateFS,
			"templates/*.html",
			"templates/partials/*.html",
		),
	)
	return &Templates{t: t}
}

func (t *Templates) Render(w io.Writer, name string, data any) error {
	return t.t.ExecuteTemplate(w, name, data)
}

func (t *Templates) RenderHTML(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Render(w, name, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}
