package content

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// Friend mirrors the theme's friends.yaml entry exactly (name, url,
// description, avatar), grouped by online/offline status.
type Friend struct {
	Name        string `json:"name" yaml:"name"`
	URL         string `json:"url" yaml:"url"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Avatar      string `json:"avatar,omitempty" yaml:"avatar,omitempty"`
}

// FriendsDoc is the whole friends.yaml document.
type FriendsDoc struct {
	Online  []Friend `json:"online" yaml:"online"`
	Offline []Friend `json:"offline" yaml:"offline"`
}

// Validate enforces required fields and http(s) URLs on every entry.
func (d *FriendsDoc) Validate() error {
	for group, entries := range map[string][]Friend{"online": d.Online, "offline": d.Offline} {
		for i, f := range entries {
			if strings.TrimSpace(f.Name) == "" {
				return fieldError("friends", fmt.Sprintf("%s[%d]: name must not be empty", group, i))
			}
			if err := validHTTPURL(f.URL); err != nil {
				return fieldError("friends", fmt.Sprintf("%s[%d]: %s", group, i, err.Error()))
			}
			if f.Avatar != "" {
				if err := validHTTPURL(f.Avatar); err != nil {
					return fieldError("friends", fmt.Sprintf("%s[%d]: avatar %s", group, i, err.Error()))
				}
			}
			if len([]rune(f.Description)) > 200 {
				return fieldError("friends", fmt.Sprintf("%s[%d]: description must be at most 200 characters", group, i))
			}
		}
	}
	return nil
}

func validHTTPURL(raw string) error {
	if raw == "" {
		return errors.New("url must not be empty")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("url must be an absolute http(s) URL")
	}
	return nil
}

// GetFriends reads and validates friends.yaml (an absent file yields an
// empty document — the provisioning scaffold writes one, but reads stay
// resilient).
func (s *Store) GetFriends(siteID uuid.UUID) (FriendsDoc, string, error) {
	data, digest, err := s.ReadFile(siteID, RootContent, "data/friends.yaml")
	if errors.Is(err, ErrNotFound) {
		return FriendsDoc{Online: []Friend{}, Offline: []Friend{}}, Digest(nil), nil
	}
	if err != nil {
		return FriendsDoc{}, "", err
	}
	var doc FriendsDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return FriendsDoc{}, "", fmt.Errorf("friends.yaml is not valid YAML: %w", err)
	}
	if doc.Online == nil {
		doc.Online = []Friend{}
	}
	if doc.Offline == nil {
		doc.Offline = []Friend{}
	}
	return doc, digest, nil
}

// SaveFriends validates and atomically writes friends.yaml under If-Match
// semantics.
func (s *Store) SaveFriends(siteID uuid.UUID, doc FriendsDoc, expectedDigest string) (FriendsDoc, string, error) {
	if err := doc.Validate(); err != nil {
		return FriendsDoc{}, "", err
	}
	if expectedDigest != "" {
		_, current, err := s.GetFriends(siteID)
		if err != nil {
			return FriendsDoc{}, "", err
		}
		if current != expectedDigest {
			return FriendsDoc{}, "", ErrConflict
		}
	}
	var buf strings.Builder
	buf.WriteString("# 友链数据，按状态分组\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(map[string]any{"online": doc.Online, "offline": doc.Offline}); err != nil {
		return FriendsDoc{}, "", err
	}
	if err := enc.Close(); err != nil {
		return FriendsDoc{}, "", err
	}
	digest, err := s.WriteFile(siteID, RootContent, "data/friends.yaml", []byte(buf.String()))
	if err != nil {
		return FriendsDoc{}, "", err
	}
	saved, _, err := s.GetFriends(siteID)
	return saved, digest, err
}

// SocialLink mirrors social-links.yaml entries (name, href, icon).
type SocialLink struct {
	Name string `json:"name" yaml:"name"`
	Href string `json:"href" yaml:"href"`
	Icon string `json:"icon,omitempty" yaml:"icon,omitempty"`
}

// ValidateSocialLinks enforces required fields; mailto and http(s) links are
// both valid here (the theme links social entries verbatim).
func ValidateSocialLinks(links []SocialLink) error {
	for i, l := range links {
		if strings.TrimSpace(l.Name) == "" {
			return fieldError("socialLinks", fmt.Sprintf("entry %d: name must not be empty", i))
		}
		if l.Href == "" {
			return fieldError("socialLinks", fmt.Sprintf("entry %d: href must not be empty", i))
		}
		u, err := url.Parse(l.Href)
		if err != nil || u.Scheme == "" || (u.Scheme != "mailto" && u.Scheme != "http" && u.Scheme != "https") || (u.Scheme != "mailto" && u.Host == "") {
			return fieldError("socialLinks", fmt.Sprintf("entry %d: href must be an http(s) or mailto URL", i))
		}
	}
	return nil
}

// GetSocialLinks reads data/social-links.yaml.
func (s *Store) GetSocialLinks(siteID uuid.UUID) ([]SocialLink, string, error) {
	data, digest, err := s.ReadFile(siteID, RootContent, "data/social-links.yaml")
	if errors.Is(err, ErrNotFound) {
		return []SocialLink{}, Digest(nil), nil
	}
	if err != nil {
		return nil, "", err
	}
	var links []SocialLink
	if err := yaml.Unmarshal(data, &links); err != nil {
		return nil, "", fmt.Errorf("social-links.yaml is not valid YAML: %w", err)
	}
	if links == nil {
		links = []SocialLink{}
	}
	return links, digest, nil
}

// SaveSocialLinks validates and atomically writes social-links.yaml.
func (s *Store) SaveSocialLinks(siteID uuid.UUID, links []SocialLink, expectedDigest string) ([]SocialLink, string, error) {
	if err := ValidateSocialLinks(links); err != nil {
		return nil, "", err
	}
	if expectedDigest != "" {
		_, current, err := s.GetSocialLinks(siteID)
		if err != nil {
			return nil, "", err
		}
		if current != expectedDigest {
			return nil, "", ErrConflict
		}
	}
	out, err := yaml.Marshal(links)
	if err != nil {
		return nil, "", err
	}
	digest, err := s.WriteFile(siteID, RootContent, "data/social-links.yaml", out)
	if err != nil {
		return nil, "", err
	}
	saved, _, err := s.GetSocialLinks(siteID)
	return saved, digest, err
}
