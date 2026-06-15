// Package i18n provides a tiny message-catalog based translation layer with
// embedded English and German locales. English is the fallback language.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
)

//go:embed locales/*.json
var localesFS embed.FS

// DefaultLocale is used when a requested locale or key is unavailable.
const DefaultLocale = "en"

// Catalog holds all loaded locale message maps.
type Catalog struct {
	messages map[string]map[string]string
}

// Load reads all embedded locale files into a Catalog.
func Load() (*Catalog, error) {
	entries, err := localesFS.ReadDir("locales")
	if err != nil {
		return nil, fmt.Errorf("i18n: read locales: %w", err)
	}
	c := &Catalog{messages: map[string]map[string]string{}}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		locale := name[:len(name)-len(".json")]
		data, rerr := localesFS.ReadFile("locales/" + name)
		if rerr != nil {
			return nil, fmt.Errorf("i18n: read %s: %w", name, rerr)
		}
		var m map[string]string
		if jerr := json.Unmarshal(data, &m); jerr != nil {
			return nil, fmt.Errorf("i18n: parse %s: %w", name, jerr)
		}
		c.messages[locale] = m
	}
	if _, ok := c.messages[DefaultLocale]; !ok {
		return nil, fmt.Errorf("i18n: default locale %q missing", DefaultLocale)
	}
	return c, nil
}

// Languages returns the available locale codes, sorted.
func (c *Catalog) Languages() []string {
	out := make([]string, 0, len(c.messages))
	for k := range c.messages {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Has reports whether a locale is available.
func (c *Catalog) Has(locale string) bool {
	_, ok := c.messages[locale]
	return ok
}

// Translator resolves messages for a specific locale.
type Translator struct {
	catalog *Catalog
	locale  string
}

// For returns a Translator for the given locale, falling back to the default.
func (c *Catalog) For(locale string) *Translator {
	if !c.Has(locale) {
		locale = DefaultLocale
	}
	return &Translator{catalog: c, locale: locale}
}

// Locale returns the active locale code.
func (t *Translator) Locale() string { return t.locale }

// T returns the localized message for key, formatting it with args (fmt.Sprintf
// semantics). Missing keys fall back to the default locale and finally to the
// key itself.
func (t *Translator) T(key string, args ...any) string {
	msg, ok := t.catalog.messages[t.locale][key]
	if !ok {
		msg, ok = t.catalog.messages[DefaultLocale][key]
	}
	if !ok {
		return key
	}
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}
