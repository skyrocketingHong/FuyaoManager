// Package traffic reads an operator-configured loopback metrics projection.
// It does not contact Cloudflare or consume any provider credential.
package traffic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const statsPath = "/api/stats/cloudflare"
const maxBytes = 128 << 10
const maxSafeInteger int64 = 1<<53 - 1

type Daily struct {
	Date      string `json:"date"`
	PageViews int64  `json:"pageViews"`
	Requests  int64  `json:"requests"`
	Uniques   int64  `json:"uniques"`
}
type TrafficSnapshot struct {
	Configured bool   `json:"configured"`
	Available  bool   `json:"available"`
	Provider   string `json:"provider"`
	Cache      string `json:"cache"`
	Latest     *Daily `json:"latest"`
}

func Unconfigured() TrafficSnapshot { return TrafficSnapshot{Provider: "cloudflare", Cache: "unknown"} }

// Fetch reports missing or unavailable data without substituting fabricated
// zero counters. Period totals are deliberately outside the consumed schema.
func Fetch(ctx context.Context, endpoint string) TrafficSnapshot {
	result := Unconfigured()
	if endpoint == "" {
		return result
	}
	result.Configured = true
	parsed, err := validateURL(endpoint)
	if err != nil {
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	dialer := net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 3 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(host, "localhost") {
			host = "127.0.0.1"
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, errors.New("metrics destination must be loopback")
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return result
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return result
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return result
	}
	latest, err := parseDaily(data)
	if err != nil {
		return result
	}
	result.Latest = latest
	result.Available = true
	switch strings.ToLower(strings.TrimSpace(response.Header.Get("X-Data-Cache"))) {
	case "stale":
		result.Cache = "stale"
	case "fresh", "hit", "miss", "revalidated":
		result.Cache = "fresh"
	}
	return result
}

func validateURL(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != statsPath || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(value, "#") {
		return nil, errors.New("invalid metrics endpoint")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("metrics endpoint is not loopback")
	}
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, errors.New("invalid metrics port")
		}
	}
	return u, nil
}

func parseDaily(data []byte) (*Daily, error) {
	var payload struct {
		Days []json.RawMessage `json:"days"`
	}
	if json.Unmarshal(data, &payload) != nil || len(payload.Days) == 0 {
		return nil, errors.New("no daily traffic data")
	}
	seen := map[string]bool{}
	var latest *Daily
	for _, raw := range payload.Days {
		var value struct {
			Date      string          `json:"date"`
			PageViews json.RawMessage `json:"pageViews"`
			Requests  json.RawMessage `json:"requests"`
			Uniques   json.RawMessage `json:"uniques"`
		}
		if json.Unmarshal(raw, &value) != nil {
			return nil, errors.New("invalid daily traffic data")
		}
		date, err := time.Parse("2006-01-02", value.Date)
		if err != nil || date.Year() < 1 || date.Format("2006-01-02") != value.Date || seen[value.Date] {
			return nil, errors.New("invalid traffic date")
		}
		seen[value.Date] = true
		day := Daily{Date: value.Date}
		if day.PageViews, err = parseCounter(value.PageViews); err != nil {
			return nil, err
		}
		if day.Requests, err = parseCounter(value.Requests); err != nil {
			return nil, err
		}
		if day.Uniques, err = parseCounter(value.Uniques); err != nil {
			return nil, err
		}
		if latest == nil || day.Date > latest.Date {
			latest = &day
		}
	}
	return latest, nil
}
func parseCounter(raw []byte) (int64, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return 0, errors.New("traffic counter is missing")
	}
	for _, char := range raw {
		if char < '0' || char > '9' {
			return 0, errors.New("traffic counter must be a non-negative safe integer")
		}
	}
	value, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || value > maxSafeInteger {
		return 0, errors.New("traffic counter exceeds the safe integer range")
	}
	return value, nil
}
