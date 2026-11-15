package content

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/google/uuid"
)

const categoryAliasesPath = ".post-category-aliases.json"

type categoryAliases struct {
	Version int               `json:"version"`
	Aliases map[string]string `json:"aliases"`
}

func (s *Store) getCategoryAliases(id uuid.UUID) (map[string]string, error) {
	data, _, err := s.ReadFile(id, RootContent, categoryAliasesPath)
	if errors.Is(err, ErrNotFound) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc categoryAliases
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil || doc.Version != 1 || doc.Aliases == nil {
		return nil, fieldError("categoryAliases", "invalid category alias metadata")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, fieldError("categoryAliases", "invalid category alias metadata")
	}
	for alias, slug := range doc.Aliases {
		if strings.TrimSpace(alias) == "" || !categoryPattern.MatchString(slug) {
			return nil, fieldError("categoryAliases", "invalid category alias")
		}
	}
	return doc.Aliases, nil
}

// The alias map is manager metadata and remains outside published snapshots.
// Adding aliases before changing an index is safe if the later write fails.
func (s *Store) rememberCategoryAliases(id uuid.UUID, slug string, names []string) error {
	return s.withFileLock(id, "data", categoryAliasesPath, func() error {
		aliases, err := s.getCategoryAliases(id)
		if err != nil {
			return err
		}
		changed := false
		for _, name := range names {
			if name == slug || strings.TrimSpace(name) == "" {
				continue
			}
			if existing, ok := aliases[name]; ok && existing != slug {
				return ErrConflict
			}
			if aliases[name] != slug {
				aliases[name] = slug
				changed = true
			}
		}
		if !changed {
			return nil
		}
		data, err := json.MarshalIndent(categoryAliases{Version: 1, Aliases: aliases}, "", "  ")
		if err != nil {
			return err
		}
		_, err = s.WriteFile(id, RootContent, categoryAliasesPath, append(data, '\n'))
		return err
	})
}
