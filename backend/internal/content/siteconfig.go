package content

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// i18nMap is the zh-CN/en-US string map the theme requires for profile
// roles and quotes.
type i18nMap struct {
	ZhCN string `json:"zh-CN" yaml:"zh-CN"`
	EnUS string `json:"en-US" yaml:"en-US"`
}

// SiteConfigDoc mirrors the theme's site.yaml exactly (schema.ts). Only the
// amap security code is treated as a secret: reads never return it and
// writes preserve the stored value when the request omits it.
type SiteConfigDoc struct {
	SchemaVersion int `json:"schemaVersion" yaml:"schemaVersion"`
	Site          struct {
		Name          string `json:"name" yaml:"name"`
		URL           string `json:"url" yaml:"url"`
		StartDate     string `json:"startDate" yaml:"startDate"`
		StartYear     int    `json:"startYear" yaml:"startYear"`
		DefaultLocale string `json:"defaultLocale" yaml:"defaultLocale"`
	} `json:"site" yaml:"site"`
	Profile struct {
		Name      string  `json:"name" yaml:"name"`
		BirthDate string  `json:"birthDate" yaml:"birthDate"`
		AvatarURL string  `json:"avatarUrl" yaml:"avatarUrl"`
		Roles     i18nMap `json:"roles" yaml:"roles"`
		Quotes    i18nMap `json:"quotes" yaml:"quotes"`
	} `json:"profile" yaml:"profile"`
	Repository struct {
		Name  string `json:"name" yaml:"name"`
		URL   string `json:"url" yaml:"url"`
		Owner string `json:"owner" yaml:"owner"`
	} `json:"repository" yaml:"repository"`
	SEO struct {
		Author      string   `json:"author" yaml:"author"`
		Description string   `json:"description" yaml:"description"`
		Keywords    []string `json:"keywords" yaml:"keywords"`
		TwitterID   string   `json:"twitterId,omitempty" yaml:"twitterId,omitempty"`
	} `json:"seo" yaml:"seo"`
	Services struct {
		Wallpaper struct {
			DefaultURL     string `json:"defaultUrl" yaml:"defaultUrl"`
			APIURL         string `json:"apiUrl,omitempty" yaml:"apiUrl,omitempty"`
			MetadataAPIURL string `json:"metadataApiUrl,omitempty" yaml:"metadataApiUrl,omitempty"`
		} `json:"wallpaper" yaml:"wallpaper"`
		Amap struct {
			BrowserKey   string `json:"browserKey,omitempty" yaml:"browserKey,omitempty"`
			SecurityCode string `json:"-" yaml:"securityCode,omitempty"` // secret: write-only through the API
			ServiceHost  string `json:"serviceHost,omitempty" yaml:"serviceHost,omitempty"`
		} `json:"amap" yaml:"amap"`
		GitHubProxyURL          string `json:"githubProxyUrl,omitempty" yaml:"githubProxyUrl,omitempty"`
		AnalyticsProxyURL       string `json:"analyticsProxyUrl,omitempty" yaml:"analyticsProxyUrl,omitempty"`
		CodingActivityProxyURL  string `json:"codingActivityProxyUrl,omitempty" yaml:"codingActivityProxyUrl,omitempty"`
		CodingLanguagesProxyURL string `json:"codingLanguagesProxyUrl,omitempty" yaml:"codingLanguagesProxyUrl,omitempty"`
	} `json:"services" yaml:"services"`
}

// amapSecurityCodeRequest is the write-only channel for the secret.
type amapSecurityCodeRequest struct {
	SecurityCode *string `json:"securityCode"`
}

// GetSiteConfig reads config/site.yaml, redacting the amap security code.
// The ETag covers the raw file bytes, so a save that only rewrites the
// secret still conflicts correctly against a stale reader.
func (s *Store) GetSiteConfig(siteID uuid.UUID) (doc SiteConfigDoc, securityCodeSet bool, digest string, err error) {
	data, digest, err := s.ReadFile(siteID, RootConfig, "site.yaml")
	if err != nil {
		return SiteConfigDoc{}, false, "", err
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return SiteConfigDoc{}, false, "", fmt.Errorf("site.yaml is not valid YAML: %w", err)
	}
	return doc, doc.Services.Amap.SecurityCode != "", digest, nil
}

// SaveSiteConfig validates and writes config/site.yaml under If-Match
// semantics. The digest comparison, secret preservation, and the write run
// inside one exclusive lock; the returned document and digest describe
// exactly the bytes this call wrote. The amap security code keeps its
// stored value unless the request supplies a non-nil replacement through
// securityCode (empty string clears it).
func (s *Store) SaveSiteConfig(siteID uuid.UUID, doc SiteConfigDoc, securityCode *string, expectedDigest string) (SiteConfigDoc, string, error) {
	if err := validateSiteConfig(&doc); err != nil {
		return SiteConfigDoc{}, "", err
	}
	var saved SiteConfigDoc
	var newDigest string
	err := s.withFileLock(siteID, "config", "site.yaml", func() error {
		existing, _, currentDigest, err := s.GetSiteConfig(siteID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if expectedDigest != "" {
			if err != nil {
				return err
			}
			if currentDigest != expectedDigest {
				return ErrConflict
			}
			// Preserve the stored secret unless explicitly replaced.
			doc.Services.Amap.SecurityCode = existing.Services.Amap.SecurityCode
		}
		if securityCode != nil {
			doc.Services.Amap.SecurityCode = *securityCode
		}
		existingRaw, _, readErr := s.ReadFile(siteID, RootConfig, "site.yaml")
		if readErr != nil && !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		out, err := marshalSiteConfig(&doc, existingRaw)
		if err != nil {
			return err
		}
		newDigest, err = s.WriteFile(siteID, RootConfig, "site.yaml", out)
		if err != nil {
			return err
		}
		saved, _, _, err = s.GetSiteConfig(siteID)
		return err
	})
	return saved, newDigest, err
}

// validateSiteConfig enforces the fields the theme's production-mode schema
// requires as non-empty (placeholder patterns like example.com would fail
// the theme build; surface them here with precise errors).
func validateSiteConfig(doc *SiteConfigDoc) error {
	if strings.TrimSpace(doc.Site.Name) == "" {
		return fieldError("site.name", "must not be empty")
	}
	if err := validHTTPURL(doc.Site.URL); err != nil {
		return fieldError("site.url", err.Error())
	}
	if strings.TrimSpace(doc.Profile.Name) == "" {
		return fieldError("profile.name", "must not be empty")
	}
	if err := validHTTPURL(doc.Profile.AvatarURL); err != nil {
		return fieldError("profile.avatarUrl", err.Error())
	}
	if strings.TrimSpace(doc.Repository.Name) == "" {
		return fieldError("repository.name", "must not be empty")
	}
	if err := validHTTPURL(doc.Repository.URL); err != nil {
		return fieldError("repository.url", err.Error())
	}
	if strings.TrimSpace(doc.Repository.Owner) == "" {
		return fieldError("repository.owner", "must not be empty")
	}
	if strings.TrimSpace(doc.SEO.Author) == "" {
		return fieldError("seo.author", "must not be empty")
	}
	if strings.TrimSpace(doc.SEO.Description) == "" {
		return fieldError("seo.description", "must not be empty")
	}
	if err := validHTTPURL(doc.Services.Wallpaper.DefaultURL); err != nil {
		return fieldError("services.wallpaper.defaultUrl", err.Error())
	}
	if doc.Site.DefaultLocale != "zh-CN" && doc.Site.DefaultLocale != "en-US" {
		return fieldError("site.defaultLocale", "must be zh-CN or en-US")
	}
	return nil
}

// UpdateSiteBaseURL rewrites only site.yaml's site.url (used when the
// managed entry changes, e.g. a visibility switch) under the config lock.
// The rewrite is skipped when the file already carries the target URL.
func (s *Store) UpdateSiteBaseURL(siteID uuid.UUID, url string) error {
	return s.withFileLock(siteID, "config", "site.yaml", func() error {
		data, _, err := s.ReadFile(siteID, RootConfig, "site.yaml")
		if err != nil {
			return err
		}
		var doc SiteConfigDoc
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("site.yaml is not valid YAML: %w", err)
		}
		if doc.Site.URL == url {
			return nil
		}
		doc.Site.URL = url
		out, err := marshalSiteConfig(&doc, data)
		if err != nil {
			return err
		}
		_, err = s.WriteFile(siteID, RootConfig, "site.yaml", out)
		return err
	})
}
