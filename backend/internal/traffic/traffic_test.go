package traffic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const goodPayload = `{"days":[{"date":"2026-11-11","pageViews":99,"requests":199,"uniques":88},{"date":"2026-11-13","pageViews":7,"requests":11,"uniques":3},{"date":"2026-11-12","pageViews":12,"requests":20,"uniques":9}],"totals":{"pageViews":999,"requests":1999,"uniques":777}}`

func TestFetchLatestDayPreservesStaleAndIgnoresPeriodTotals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != statsPath || r.Method != "GET" {
			t.Error("metrics fetch changed its fixed route")
		}
		if r.Header.Get("Authorization") != "" || len(r.Cookies()) > 0 {
			t.Error("metrics fetch sent credentials")
		}
		w.Header().Set("X-Data-Cache", "stale")
		_, _ = w.Write([]byte(goodPayload))
	}))
	defer server.Close()
	result := Fetch(t.Context(), server.URL+statsPath)
	if !result.Configured || !result.Available || result.Provider != "cloudflare" || result.Cache != "stale" || result.Latest == nil {
		t.Fatalf("unexpected snapshot: %+v", result)
	}
	if result.Latest.Date != "2026-11-13" || result.Latest.Uniques != 3 || result.Latest.PageViews != 7 || result.Latest.Requests != 11 {
		t.Fatal("period totals or array order replaced the latest daily counters")
	}
	for _, cache := range []string{"hit", "miss", "fresh", "unknown"} {
		t.Run(cache, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Data-Cache", cache)
				_, _ = w.Write([]byte(goodPayload))
			}))
			defer upstream.Close()
			snapshot := Fetch(t.Context(), upstream.URL+statsPath)
			if !snapshot.Available {
				t.Fatal("valid source unavailable")
			}
			if cache != "unknown" && snapshot.Cache != "fresh" {
				t.Fatal("fresh projection was mislabeled")
			}
		})
	}
}

func TestFetchRejectsNonLoopbackWrongPathAndRedirects(t *testing.T) {
	for _, endpoint := range []string{"https://example.com" + statsPath, "http://127.0.0.1.example.com" + statsPath, "http://user:secret@127.0.0.1" + statsPath, "http://127.0.0.1/other", "http://127.0.0.1" + statsPath + "?token=x", "http://127.0.0.1" + statsPath + "?", "http://127.0.0.1" + statsPath + "#fragment", "file:///api/stats/cloudflare", "http://127.0.0.1/api/%73tats/cloudflare"} {
		result := Fetch(t.Context(), endpoint)
		if !result.Configured || result.Available || result.Latest != nil {
			t.Fatal("invalid metrics source was used")
		}
	}
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { redirected.Add(1); _, _ = w.Write([]byte(goodPayload)) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+statsPath, http.StatusFound)
	}))
	defer server.Close()
	if snapshot := Fetch(t.Context(), server.URL+statsPath); snapshot.Available || snapshot.Latest != nil || redirected.Load() != 0 {
		t.Fatal("loopback metrics redirect was followed")
	}
}

func TestFetchValidatesDatesSafeIntegersAndMissingData(t *testing.T) {
	for name, payload := range map[string]string{
		"empty":            `{"days":[],"totals":{"uniques":999}}`,
		"missing-requests": `{"days":[{"date":"2026-11-13","pageViews":0,"uniques":0}]}`,
		"negative":         `{"days":[{"date":"2026-11-13","pageViews":-1,"requests":1,"uniques":1}]}`,
		"fraction":         `{"days":[{"date":"2026-11-13","pageViews":1.5,"requests":1,"uniques":1}]}`,
		"unsafe":           `{"days":[{"date":"2026-11-13","pageViews":9007199254740992,"requests":1,"uniques":1}]}`,
		"string-counter":   `{"days":[{"date":"2026-11-13","pageViews":"1","requests":1,"uniques":1}]}`,
		"invalid-date":     `{"days":[{"date":"2026-02-29","pageViews":1,"requests":1,"uniques":1}]}`,
		"duplicate-date":   `{"days":[{"date":"2026-11-13","pageViews":1,"requests":1,"uniques":1},{"date":"2026-11-13","pageViews":2,"requests":2,"uniques":2}]}`,
		"malformed":        "not json",
		"oversized":        strings.Repeat(" ", maxBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(payload)) }))
			defer server.Close()
			result := Fetch(t.Context(), server.URL+statsPath)
			if result.Available || result.Latest != nil {
				t.Fatal("invalid source was converted into real-looking counters")
			}
			raw, _ := json.Marshal(result)
			if !strings.Contains(string(raw), `"latest":null`) {
				t.Fatal("unavailable snapshot manufactured zero traffic")
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"days":[{"date":"2026-11-13","pageViews":0,"requests":0,"uniques":0}]}`))
	}))
	defer server.Close()
	if snapshot := Fetch(t.Context(), server.URL+statsPath); !snapshot.Available || snapshot.Latest == nil || snapshot.Latest.PageViews != 0 {
		t.Fatal("explicit real zero counters were rejected")
	}
}

func TestFetchRespectsCancellationAndUnavailableSources(t *testing.T) {
	if snapshot := Fetch(t.Context(), ""); snapshot.Configured || snapshot.Available || snapshot.Latest != nil {
		t.Fatal("missing config reported available data")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if snapshot := Fetch(ctx, server.URL+statsPath); snapshot.Available || snapshot.Latest != nil {
		t.Fatal("cancelled fetch returned traffic")
	}
	failure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer failure.Close()
	if snapshot := Fetch(t.Context(), failure.URL+statsPath); snapshot.Available || snapshot.Latest != nil {
		t.Fatal("unavailable provider fabricated counters")
	}
}
