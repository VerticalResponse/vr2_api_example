package main

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"
)

//go:embed templates/*.html static/*.css
var assets embed.FS

// page is the part of every view the layout needs: the environment banner and
// the navigation state.
type page struct {
	Title     string
	Config    config
	Connected bool
	Revoked   bool
	Expired   bool
	Notice    string
	Problem   string
}

type indexPage struct {
	page
	ClientID string
	Card     consoleCard
}

type consoleCard struct {
	card
	Result cardResult
	Ran    bool
}

type consolePage struct {
	page
	Token tokenSet
	Cards []consoleCard
}

type problemPage struct {
	page
	Code     string
	Meaning  string
	Body     string
	Status   int
	Failures map[string][]string
}

var funcs = template.FuncMap{
	"millis": func(d time.Duration) string {
		return fmt.Sprintf("%.0f ms", float64(d)/float64(time.Millisecond))
	},
	"fingerprint": fingerprint,
	"stamp":       func(t time.Time) string { return t.Format("2006-01-02 15:04:05 MST") },
}

// Each page is the layout parsed together with one content file, so the layout
// can call {{template "content"}} and get the right one.
var views = parseViews("index", "console", "revoke", "problem")

func parseViews(names ...string) map[string]*template.Template {
	out := make(map[string]*template.Template, len(names))
	for _, name := range names {
		out[name] = template.Must(
			template.New("layout.html").Funcs(funcs).ParseFS(assets,
				"templates/layout.html", "templates/"+name+".html"),
		)
	}
	return out
}

func render(w http.ResponseWriter, name string, status int, data any) {
	// Buffer first: a template that fails halfway through would otherwise
	// leave a half-written 200 on the wire.
	var buf bytes.Buffer
	if err := views[name].Execute(&buf, data); err != nil {
		log.Printf("rendering %s: %v", name, err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		log.Printf("writing %s: %v", name, err)
	}
}
