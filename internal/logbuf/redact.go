package logbuf

import (
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

var logAddress = regexp.MustCompile(`(?i)(?:https?://|/)[^\s<>"']+`)
var logCredential = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|secret|authorization|cookie|x-emby-token)["']?\s*[:=]\s*("(?:\\.|[^"])*"|'[^']*'|[^\s,;]+)`)
var logBearer = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[a-z0-9+/_.=-]+`)

func safeText(text string) string {
	text = logAddress.ReplaceAllStringFunc(text, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return "[address omitted]"
		}
		u.User, u.RawQuery, u.Fragment, u.RawFragment, u.ForceQuery = nil, "", "", "", false
		return u.String()
	})
	text = logBearer.ReplaceAllString(text, "$1 [redacted]")
	text = logCredential.ReplaceAllString(text, "$1=[redacted]")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
}

func sensitiveField(key string) bool {
	key = strings.ToLower(strings.Map(func(r rune) rune {
		if r == '_' || r == '-' || r == '.' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, key))
	for _, suffix := range []string{"apikey", "apikeyenc", "password", "token", "secret", "privatekey", "masterkey", "encryptionkey", "body", "payload", "headers", "cookie"} {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	switch key {
	case "key", "apikey", "apikeyenc", "token", "accesstoken", "refreshtoken", "password",
		"secret", "authorization", "proxyauthorization", "cookie", "setcookie", "xembytoken",
		"body", "payload", "headers", "request", "response", "credentials", "secrets":
		return true
	default:
		return false
	}
}

// Keep operational scalar fields, but never dump upstream bodies or arbitrary
// objects (which may contain credentials) into either logging destination.
func safeAttr(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	structuredRequest := a.Value.Kind() == slog.KindGroup && (strings.EqualFold(a.Key, "request") || strings.EqualFold(a.Key, "response"))
	if sensitiveField(a.Key) && !structuredRequest {
		return slog.String(a.Key, "[redacted]")
	}
	a.Key = safeText(a.Key)
	switch a.Value.Kind() {
	case slog.KindString:
		a.Value = slog.StringValue(safeText(a.Value.String()))
	case slog.KindGroup:
		children := a.Value.Group()
		cleaned := make([]slog.Attr, 0, len(children))
		for _, child := range children {
			cleaned = append(cleaned, safeAttr(child))
		}
		a.Value = slog.GroupValue(cleaned...)
	case slog.KindAny:
		switch v := a.Value.Any().(type) {
		case error:
			a.Value = slog.StringValue(safeText(v.Error()))
		case fmt.Stringer:
			a.Value = slog.StringValue(safeText(v.String()))
		case nil:
			// Preserve the normal nil representation.
		default:
			a.Value = slog.StringValue("[object omitted]")
		}
	}
	return a
}
