// Command demo renders the waim UI as a static site with sample data, so the
// GitHub Pages demo shows the real templates without a Jellyfin or TMDB server.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/a-h/templ"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/web"
)

const repoURL = "https://github.com/daknoblo/waim"

func main() {
	out := flag.String("out", "dist", "output directory for the static demo site")
	locale := flag.String("locale", "en", "locale to render the demo in")
	flag.Parse()

	if err := run(*out, *locale); err != nil {
		log.Fatalf("demo: %v", err)
	}
}

func run(out, locale string) error {
	catalog, err := i18n.Load()
	if err != nil {
		return err
	}
	t := catalog.For(locale)

	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	if err := copyStatic(filepath.Join(out, "static")); err != nil {
		return err
	}

	run := demoRun()
	findings := demoFindings()
	pages := map[string]templ.Component{
		"index.html":       web.Dashboard(demoDashboard(t, run, findings)),
		"stats.html":       web.Stats(demoStats(t, run, findings)),
		"suggestions.html": web.Suggestions(demoSuggestions(t)),
		"logs.html":        web.Logs(demoLogs(t)),
		"settings.html":    web.Settings(demoSettings(t)),
		"about.html":       web.About(demoAbout(t)),
	}
	for name, comp := range pages {
		var sb strings.Builder
		if err := comp.Render(context.Background(), &sb); err != nil {
			return fmt.Errorf("render %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(out, name), []byte(staticHTML(sb.String(), t)), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	// Pages would otherwise run the output through Jekyll.
	if err := os.WriteFile(filepath.Join(out, ".nojekyll"), nil, 0o644); err != nil {
		return err
	}
	fmt.Printf("demo: wrote %d pages to %s\n", len(pages), out)
	return nil
}

// hxAttr matches the htmx attributes of the live UI; the static demo has no
// backend to talk to, so they are stripped along with the htmx script.
var hxAttr = regexp.MustCompile(`\s(hx-[a-z-]+)="[^"]*"`)

// scriptTag matches the htmx bundle include.
var scriptTag = regexp.MustCompile(`<script src="[^"]*htmx[^"]*"[^>]*></script>`)

// absAction matches any remaining server route used as a form target.
var absAction = regexp.MustCompile(`(action|formaction)="/[^"]*"`)

// absHref matches any remaining absolute route in a link.
var absHref = regexp.MustCompile(`href="/[^"]*"`)

// staticHTML turns a server-rendered page into a self-contained file: absolute
// routes become file names, dynamic behaviour is removed and a demo banner is
// added.
func staticHTML(html string, t *i18n.Translator) string {
	html = hxAttr.ReplaceAllString(html, "")
	html = scriptTag.ReplaceAllString(html, "")
	html = strings.NewReplacer(
		`href="/static/`, `href="static/`,
		`src="/static/`, `src="static/`,
		`href="/"`, `href="index.html"`,
		`href="/stats"`, `href="stats.html"`,
		`href="/suggestions"`, `href="suggestions.html"`,
		`href="/logs"`, `href="logs.html"`,
		`href="/settings"`, `href="settings.html"`,
		`href="/about"`, `href="about.html"`,
		`href="/export/settings"`, `href="`+repoURL+`"`,
		`href="/export/sync"`, `href="`+repoURL+`"`,
		`action="/settings"`, `action="#"`,
		`action="/locale"`, `action="#"`,
	).Replace(html)
	// Anything still pointing at a server route would 404 on a static host.
	html = absAction.ReplaceAllString(html, `$1="#"`)
	html = absHref.ReplaceAllString(html, `href="`+repoURL+`"`)

	banner := `<div class="mx-auto w-full px-4 pt-4 sm:px-6 lg:w-2/3 lg:px-4"><div class="rounded-lg border border-indigo-500/40 bg-indigo-500/10 px-4 py-3 text-sm text-indigo-100">` +
		templ.EscapeString(t.T("demo.banner")) +
		` <a class="font-medium underline hover:text-white" href="` + repoURL + `">` + templ.EscapeString(t.T("demo.repo")) + `</a></div></div>`
	if i := strings.Index(html, "<body"); i >= 0 {
		if j := strings.Index(html[i:], ">"); j >= 0 {
			cut := i + j + 1
			html = html[:cut] + banner + html[cut:]
		}
	}
	return html
}

// copyStatic writes the embedded CSS/JS assets next to the generated pages.
func copyStatic(dir string) error {
	sub, err := fs.Sub(web.StaticFS, "assets/static")
	if err != nil {
		return err
	}
	return fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rerr := fs.ReadFile(sub, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dir, path)
		if merr := os.MkdirAll(filepath.Dir(target), 0o755); merr != nil {
			return merr
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func demoLayout(t *i18n.Translator, active string) web.Layout {
	return web.Layout{
		T:            t,
		Active:       active,
		Version:      "demo",
		AssetVersion: "demo",
		Repo:         repoURL,
		Languages:    web.LanguageOptions(t.Locale()),
	}
}
