package content

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
)

// The hidden file is editing metadata; build snapshots skip hidden entries.
const postVocabularyPath = ".post-vocabulary.json"

type PostVocabulary struct {
	Version    int      `json:"version"`
	Categories []string `json:"categories"`
	Tags       []string `json:"tags"`
}

func emptyPostVocabulary() PostVocabulary {
	return PostVocabulary{Version: 1, Categories: []string{}, Tags: []string{}}
}

func validatePostVocabulary(doc PostVocabulary) error {
	if doc.Version != 1 {
		return fieldError("version", "unsupported vocabulary version")
	}
	if doc.Categories == nil || doc.Tags == nil {
		return fieldError("vocabulary", "categories and tags must be arrays")
	}
	for _, group := range []struct {
		name  string
		terms []string
	}{{"categories", doc.Categories}, {"tags", doc.Tags}} {
		seen := make(map[string]bool, len(group.terms))
		for _, value := range group.terms {
			if strings.TrimSpace(value) == "" {
				return fieldError(group.name, "must not contain empty entries")
			}
			if seen[value] {
				return fieldError(group.name, "must not contain duplicates")
			}
			seen[value] = true
		}
	}
	return nil
}

func (s *Store) GetPostVocabulary(scopeID uuid.UUID) (PostVocabulary, string, error) {
	data, digest, err := s.ReadFile(scopeID, RootContent, postVocabularyPath)
	if errors.Is(err, ErrNotFound) {
		return emptyPostVocabulary(), Digest(nil), nil
	}
	if err != nil {
		return PostVocabulary{}, "", err
	}
	var doc PostVocabulary
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return PostVocabulary{}, "", fmt.Errorf("invalid post vocabulary: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return PostVocabulary{}, "", errors.New("invalid post vocabulary trailing data")
	}
	if err := validatePostVocabulary(doc); err != nil {
		return PostVocabulary{}, "", err
	}
	return doc, digest, nil
}

// SavePostVocabulary compares the exact catalog revision while holding its
// file lock. Article frontmatter remains the authority for existing usages.
func (s *Store) SavePostVocabulary(scopeID uuid.UUID, doc PostVocabulary, expectedDigest string) (PostVocabulary, string, error) {
	if err := validatePostVocabulary(doc); err != nil {
		return PostVocabulary{}, "", err
	}
	var digest string
	err := s.withFileLock(scopeID, "data", postVocabularyPath, func() error {
		_, current, err := s.GetPostVocabulary(scopeID)
		if err != nil {
			return err
		}
		if expectedDigest != "" && current != expectedDigest {
			return ErrConflict
		}
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		digest, err = s.WriteFile(scopeID, RootContent, postVocabularyPath, data)
		return err
	})
	return doc, digest, err
}
