package i18n

import (
	"reflect"
	"testing"
)

func TestEmbeddedCatalogsHaveMatchingNonemptyKeys(t *testing.T) {
	catalog, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.Languages(); !reflect.DeepEqual(got, []string{"de", "en"}) {
		t.Fatalf("unexpected languages: %v", got)
	}
	for locale, messages := range catalog.messages {
		for key, text := range messages {
			if text == "" {
				t.Errorf("%s has empty text for %s", locale, key)
			}
			if _, exists := catalog.messages[DefaultLocale][key]; !exists {
				t.Errorf("%s key %s has no default translation", locale, key)
			}
		}
		for key := range catalog.messages[DefaultLocale] {
			if _, exists := messages[key]; !exists {
				t.Errorf("%s is missing key %s", locale, key)
			}
		}
	}
}

func TestTranslatorFallbackAndFormatting(t *testing.T) {
	catalog := &Catalog{messages: map[string]map[string]string{
		"en": {"greeting": "Hello %s", "fallback": "%d results"},
		"de": {"greeting": "Hallo %s"},
	}}
	de := catalog.For("de")
	if de.Locale() != "de" || de.T("greeting", "Ada") != "Hallo Ada" {
		t.Fatal("localized formatting failed")
	}
	if de.T("fallback", 3) != "3 results" || de.T("unknown", 3) != "unknown" {
		t.Fatal("missing-key fallback failed")
	}
	if fallback := catalog.For("unsupported"); fallback.Locale() != DefaultLocale || fallback.T("greeting", "Ada") != "Hello Ada" {
		t.Fatal("unsupported-locale fallback failed")
	}
	if de.T("greeting") != "Hallo %s" {
		t.Fatal("literal format changed without arguments")
	}
}
