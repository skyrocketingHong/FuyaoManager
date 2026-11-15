package content

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

type PostDirectoryDocument struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Digest string `json:"digest"`
}

func (s *Store) DeletePostCategory(scopeID uuid.UUID, slug, expectedDigest string) error {
	if s.Posts == nil {
		return ErrNotFound
	}
	return s.Posts.DeletePostCategory(scopeID, slug, expectedDigest)
}

func (s *Store) postDirectoryIndex(scopeID uuid.UUID, slug string) ([]byte, string, error) {
	if !categoryPattern.MatchString(slug) {
		return nil, "", fieldError("slug", "must be a valid category directory")
	}
	root, err := resolve(s.ContentRoot(scopeID), "posts")
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, "", err
	}
	if err == nil {
		info, statErr := os.Lstat(filepath.Join(root, slug))
		if statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
			return nil, "", ErrInvalidPath
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return nil, "", statErr
		}
		index, indexErr := os.Lstat(filepath.Join(root, slug, "_index.md"))
		if indexErr == nil && index.Mode()&os.ModeSymlink != 0 {
			return nil, "", ErrInvalidPath
		}
		if indexErr != nil && !errors.Is(indexErr, os.ErrNotExist) {
			return nil, "", indexErr
		}
	}
	data, digest, err := s.ReadFile(scopeID, RootContent, "posts/"+slug+"/_index.md")
	if errors.Is(err, ErrNotFound) {
		return nil, Digest(nil), nil
	}
	return data, digest, err
}

func directoryTitle(raw []byte, fallback string) (string, error) {
	fm, _, err := SplitFrontmatter(raw)
	if err != nil {
		return "", err
	}
	if len(bytes.TrimSpace(fm)) == 0 {
		return fallback, nil
	}
	var doc struct {
		Title string `yaml:"title"`
	}
	if err := yaml.Unmarshal(fm, &doc); err != nil {
		return "", fmt.Errorf("invalid directory index: %w", err)
	}
	if strings.TrimSpace(doc.Title) == "" {
		return fallback, nil
	}
	return doc.Title, nil
}

// PostDirectoryTitle reads legacy category metadata during database import.
func PostDirectoryTitle(raw []byte, fallback string) (string, error) {
	return directoryTitle(raw, fallback)
}

func (s *Store) GetPostDirectory(scopeID uuid.UUID, slug string) (PostDirectoryDocument, string, error) {
	if s.Posts != nil {
		return s.Posts.GetPostDirectory(scopeID, slug)
	}
	raw, digest, err := s.postDirectoryIndex(scopeID, slug)
	if err != nil {
		return PostDirectoryDocument{}, "", err
	}
	title, err := directoryTitle(raw, slug)
	if err != nil {
		return PostDirectoryDocument{}, "", err
	}
	return PostDirectoryDocument{Slug: slug, Title: title, Digest: digest}, digest, nil
}

func (s *Store) SavePostDirectory(scopeID uuid.UUID, slug, title, expectedDigest string) (PostDirectoryDocument, string, error) {
	if s.Posts != nil {
		return s.Posts.SavePostDirectory(scopeID, slug, title, expectedDigest)
	}
	if !categoryPattern.MatchString(slug) {
		return PostDirectoryDocument{}, "", fieldError("slug", "must be a valid category directory")
	}
	if strings.TrimSpace(title) == "" {
		return PostDirectoryDocument{}, "", fieldError("title", "must not be empty")
	}
	var saved PostDirectoryDocument
	var digest string
	err := s.withFileLock(scopeID, "posts", "posts/"+slug+"/_index.md", func() error {
		raw, current, err := s.postDirectoryIndex(scopeID, slug)
		if err != nil {
			return err
		}
		if expectedDigest != "" && current != expectedDigest {
			return ErrConflict
		}
		oldTitle, err := directoryTitle(raw, slug)
		if err != nil {
			return err
		}
		if raw != nil && oldTitle != title {
			taxonomy, err := s.GetPostTaxonomy(scopeID)
			if err != nil {
				return err
			}
			names := []string{}
			matches := 0
			otherSlug := false
			for _, entry := range taxonomy.Directories {
				if entry.Title == oldTitle {
					matches++
				}
				if entry.Slug == oldTitle && entry.Slug != slug {
					otherSlug = true
				}
			}
			for _, group := range taxonomy.CategoryGroups {
				if group.Directory == slug {
					names = append(names, group.Aliases...)
				}
			}
			if matches == 1 && !otherSlug {
				names = append(names, oldTitle)
			}
			if err := s.rememberCategoryAliases(scopeID, slug, names); err != nil {
				return err
			}
		}
		data, err := RenderPostDirectory(raw, title)
		if err != nil {
			return err
		}
		digest, err = s.WriteFile(scopeID, RootContent, "posts/"+slug+"/_index.md", data)
		if err != nil {
			return err
		}
		saved = PostDirectoryDocument{Slug: slug, Title: title, Digest: digest}
		return nil
	})
	return saved, digest, err
}

// RenderPostDirectory preserves an imported category index while changing its title.
func RenderPostDirectory(raw []byte, title string) ([]byte, error) {
	fm, body, err := SplitFrontmatter(raw)
	if err != nil {
		return nil, err
	}
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if len(bytes.TrimSpace(fm)) != 0 {
		var doc yaml.Node
		if err := yaml.Unmarshal(fm, &doc); err != nil {
			return nil, err
		}
		if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
			return nil, fieldError("frontmatter", "must be a YAML mapping")
		}
		mapping = doc.Content[0]
	}
	found := false
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != "title" {
			continue
		}
		old := mapping.Content[i+1]
		next := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: title, Anchor: old.Anchor, HeadComment: old.HeadComment, LineComment: old.LineComment, FootComment: old.FootComment}
		mapping.Content[i+1] = next
		found = true
		break
	}
	if !found {
		mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "title"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: title})
	}
	var buffer bytes.Buffer
	buffer.WriteString("---\n")
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(mapping); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	buffer.WriteString("---\n")
	buffer.Write(body)
	data := buffer.Bytes()
	if _, err := directoryTitle(data, ""); err != nil {
		return nil, err
	}
	return data, nil
}
