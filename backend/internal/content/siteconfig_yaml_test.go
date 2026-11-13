package content

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSiteConfigPreservesUnknownFieldsAndClearsKnownOptionalFields(t *testing.T) {
	raw := []byte("schemaVersion: 1\nextension:\n  future: keep\nservices:\n  codingLanguagesProxyUrl: https://site.example/api/languages\n  newThemeService:\n    enabled: true\n  amap:\n    browserKey: old-key\n    futureMapOption: keep\n")
	var doc SiteConfigDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Services.CodingLanguagesProxyURL != "https://site.example/api/languages" {
		t.Fatal("languages proxy was lost")
	}
	doc.Services.Amap.BrowserKey = ""
	saved, err := marshalSiteConfig(&doc, raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"future: keep", "newThemeService:", "futureMapOption: keep", "codingLanguagesProxyUrl: https://site.example/api/languages"} {
		if !strings.Contains(string(saved), value) {
			t.Fatalf("extension was lost: %s", value)
		}
	}
	if strings.Contains(string(saved), "old-key") {
		t.Fatal("cleared known optional field was restored as an extension")
	}
}
