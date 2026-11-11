package content

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// FootprintName is the zh/en display name pair.
type FootprintName struct {
	Zh string `json:"zh" yaml:"zh"`
	En string `json:"en" yaml:"en"`
}

// FootprintCoordinates is the stored position. The theme's AMap map renders
// these directly, so they are GCJ-02; original photo EXIF (WGS-84) is a
// different field elsewhere and never lands here.
type FootprintCoordinates struct {
	Lng float64 `json:"lng" yaml:"lng"`
	Lat float64 `json:"lat" yaml:"lat"`
}

// FootprintPlace is one spot inside a city.
type FootprintPlace struct {
	ID          string               `json:"id" yaml:"id"`
	Name        FootprintName        `json:"name" yaml:"name"`
	Coordinates FootprintCoordinates `json:"coordinates" yaml:"coordinates"`
	Type        string               `json:"type" yaml:"type"`
	VisitDate   string               `json:"visitDate" yaml:"visitDate"`
	Description string               `json:"description,omitempty" yaml:"description,omitempty"`
}

// FootprintCity is one visited city with its places.
type FootprintCity struct {
	ID          string               `json:"id" yaml:"id"`
	Name        FootprintName        `json:"name" yaml:"name"`
	Coordinates FootprintCoordinates `json:"coordinates" yaml:"coordinates"`
	Type        string               `json:"type" yaml:"type"`
	Adcode      string               `json:"adcode,omitempty" yaml:"adcode,omitempty"`
	VisitDate   string               `json:"visitDate" yaml:"visitDate"`
	Description string               `json:"description,omitempty" yaml:"description,omitempty"`
	Places      []FootprintPlace     `json:"places,omitempty" yaml:"places,omitempty"`
}

// FootprintsDoc is the whole footprints.yaml document.
type FootprintsDoc struct {
	Cities []FootprintCity `json:"cities" yaml:"cities"`
}

var (
	visitDatePattern = regexp.MustCompile(`^\d{8}$`)
	footprintIDLine  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// Validate enforces the schema the theme consumes: required names (both
// locales), GCJ-02 coordinate bounds (China mainland), city/spot types,
// YYYYMMDD visit dates, unique ids, and places nested inside their city.
func (d *FootprintsDoc) Validate() error {
	seen := map[string]bool{}
	for i, city := range d.Cities {
		if err := validateEntry("cities", i, city.ID, city.Name, city.Coordinates, city.Type, city.VisitDate); err != nil {
			return err
		}
		if seen[city.ID] {
			return fieldError("footprints", fmt.Sprintf("cities[%d]: duplicate id %q", i, city.ID))
		}
		seen[city.ID] = true
		for j, place := range city.Places {
			if err := validateEntry(fmt.Sprintf("cities[%d].places", i), j, place.ID, place.Name, place.Coordinates, place.Type, place.VisitDate); err != nil {
				return err
			}
			if place.Type != "spot" {
				return fieldError("footprints", fmt.Sprintf("cities[%d].places[%d]: type must be spot", i, j))
			}
			if seen[place.ID] {
				return fieldError("footprints", fmt.Sprintf("cities[%d].places[%d]: duplicate id %q", i, j, place.ID))
			}
			seen[place.ID] = true
		}
	}
	return nil
}

func validateEntry(scope string, index int, id string, name FootprintName, coord FootprintCoordinates, entryType, visitDate string) error {
	if !footprintIDLine.MatchString(id) {
		return fieldError("footprints", fmt.Sprintf("%s[%d]: id must be 1-64 letters, digits, _ or -", scope, index))
	}
	if strings.TrimSpace(name.Zh) == "" || strings.TrimSpace(name.En) == "" {
		return fieldError("footprints", fmt.Sprintf("%s[%d]: name.zh and name.en are both required", scope, index))
	}
	if coord.Lng < 73 || coord.Lng > 136 || coord.Lat < 18 || coord.Lat > 54 {
		return fieldError("footprints", fmt.Sprintf("%s[%d]: coordinates must be GCJ-02 within mainland China bounds", scope, index))
	}
	if entryType != "city" && entryType != "spot" {
		return fieldError("footprints", fmt.Sprintf("%s[%d]: type must be city or spot", scope, index))
	}
	if !visitDatePattern.MatchString(visitDate) {
		return fieldError("footprints", fmt.Sprintf("%s[%d]: visitDate must be YYYYMMDD", scope, index))
	}
	return nil
}

// GetFootprints reads data/footprints.yaml (absent yields an empty doc).
func (s *Store) GetFootprints(siteID uuid.UUID) (FootprintsDoc, string, error) {
	data, digest, err := s.ReadFile(siteID, RootContent, "data/footprints.yaml")
	if errors.Is(err, ErrNotFound) {
		return FootprintsDoc{Cities: []FootprintCity{}}, Digest(nil), nil
	}
	if err != nil {
		return FootprintsDoc{}, "", err
	}
	var doc FootprintsDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return FootprintsDoc{}, "", fmt.Errorf("footprints.yaml is not valid YAML: %w", err)
	}
	if doc.Cities == nil {
		doc.Cities = []FootprintCity{}
	}
	return doc, digest, nil
}

// SaveFootprints validates and writes data/footprints.yaml under If-Match
// semantics.
func (s *Store) SaveFootprints(siteID uuid.UUID, doc FootprintsDoc, expectedDigest string) (FootprintsDoc, string, error) {
	if err := doc.Validate(); err != nil {
		return FootprintsDoc{}, "", err
	}
	if expectedDigest != "" {
		_, current, err := s.GetFootprints(siteID)
		if err != nil {
			return FootprintsDoc{}, "", err
		}
		if current != expectedDigest {
			return FootprintsDoc{}, "", ErrConflict
		}
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return FootprintsDoc{}, "", err
	}
	digest, err := s.WriteFile(siteID, RootContent, "data/footprints.yaml", out)
	if err != nil {
		return FootprintsDoc{}, "", err
	}
	saved, _, err := s.GetFootprints(siteID)
	return saved, digest, err
}
