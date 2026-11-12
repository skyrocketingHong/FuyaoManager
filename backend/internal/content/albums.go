package content

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AlbumIndex mirrors the theme's albums/metadata/index.json. Years are the
// generator's numeric years (2026, not "2026").
type AlbumIndex struct {
	SchemaVersion int    `json:"schemaVersion"`
	GeneratedAt   string `json:"generatedAt"`
	TotalPhotos   int    `json:"totalPhotos"`
	Years         []int  `json:"years"`
	Devices       []any  `json:"devices"`
}

// AlbumPhoto is one entry of a year metadata file. Only title, description,
// alt, and ordering are editable through the API; the importer owns the
// identity and variant fields.
type AlbumPhoto struct {
	Filename    string          `json:"filename"`
	Path        string          `json:"path"`
	Date        string          `json:"date,omitempty"`
	GPS         json.RawMessage `json:"gps,omitempty"`
	CoordType   string          `json:"coordType,omitempty"`
	Make        string          `json:"make,omitempty"`
	Model       string          `json:"model,omitempty"`
	Width       int             `json:"width,omitempty"`
	Height      int             `json:"height,omitempty"`
	Variants    []AlbumVariant  `json:"variants,omitempty"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	Alt         string          `json:"alt,omitempty"`
	Extra       json.RawMessage `json:"-"`
	raw         map[string]any
}

// AlbumVariant is one rendered thumbnail.
type AlbumVariant struct {
	Path   string `json:"path"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Type   string `json:"type"`
}

// AlbumYear is one year metadata document.
type AlbumYear struct {
	Year   int          `json:"year"`
	Photos []AlbumPhoto `json:"photos"`
	raw    []byte
}

var (
	yearPattern     = regexp.MustCompile(`^\d{4}$`)
	photoPathRegexp = regexp.MustCompile(`^\d{4}/\d{2}/\d{2}/[A-Za-z0-9_-]+\.(jpeg|jpg|png|webp)$`)
	allowedImageExt = map[string]bool{".jpeg": true, ".jpg": true, ".png": true, ".webp": true}
)

// maxUploadBytes bounds one uploaded original.
const maxUploadBytes = 40 << 20 // 40 MiB

// MaxUploadBytes exposes the upload limit to the API layer.
func MaxUploadBytes() int64 { return maxUploadBytes }

// ErrTooLarge marks oversized uploads.
var ErrTooLarge = errors.New("upload exceeds the size limit")

// GetAlbumIndex reads metadata/index.json (an absent file yields an empty
// index — sites start without photos).
func (s *Store) GetAlbumIndex(siteID uuid.UUID) (AlbumIndex, string, error) {
	data, digest, err := s.ReadFile(siteID, RootContent, "albums/metadata/index.json")
	if errors.Is(err, ErrNotFound) {
		return AlbumIndex{SchemaVersion: 1, Years: []int{}, Devices: []any{}}, Digest(nil), nil
	}
	if err != nil {
		return AlbumIndex{}, "", err
	}
	var index AlbumIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return AlbumIndex{}, "", fmt.Errorf("index.json is not valid JSON: %w", err)
	}
	return index, digest, nil
}

// GetAlbumYear reads one year document plus its digest.
func (s *Store) GetAlbumYear(siteID uuid.UUID, year string) (AlbumYear, string, error) {
	if !yearPattern.MatchString(year) {
		return AlbumYear{}, "", ErrInvalidPath
	}
	data, digest, err := s.ReadFile(siteID, RootContent, "albums/metadata/"+year+".json")
	if err != nil {
		return AlbumYear{}, "", err
	}
	var doc AlbumYear
	if err := json.Unmarshal(data, &doc); err != nil {
		return AlbumYear{}, "", fmt.Errorf("year metadata is not valid JSON: %w", err)
	}
	doc.raw = data
	return doc, digest, nil
}

// SaveAlbumYear applies an edit to one year document under If-Match
// semantics. Only title/description/alt and the photo order change; the
// caller supplies an edit callback receiving the parsed document. The
// compare-edit-write sequence runs inside the site's album-metadata lock,
// serializing against the Node indexer (which the media runner wraps in the
// same lock) and against concurrent editors.
func (s *Store) SaveAlbumYear(siteID uuid.UUID, year string, expectedDigest string, edit func(*AlbumYear) error) (AlbumYear, string, error) {
	if !yearPattern.MatchString(year) {
		return AlbumYear{}, "", ErrInvalidPath
	}
	var saved AlbumYear
	var newDigest string
	err := s.WithAlbumMetaLock(context.Background(), siteID, func() error {
		doc, digest2, err := s.saveAlbumYearLocked(siteID, year, expectedDigest, edit)
		saved, newDigest = doc, digest2
		return err
	})
	return saved, newDigest, err
}

// saveAlbumYearLocked implements SaveAlbumYear without taking the lock; the
// caller must already hold the album-metadata lock.
func (s *Store) saveAlbumYearLocked(siteID uuid.UUID, year string, expectedDigest string, edit func(*AlbumYear) error) (AlbumYear, string, error) {
	doc, digest, err := s.GetAlbumYear(siteID, year)
	if err != nil {
		return AlbumYear{}, "", err
	}
	if expectedDigest != "" && digest != expectedDigest {
		return AlbumYear{}, "", ErrConflict
	}
	if err := edit(&doc); err != nil {
		return AlbumYear{}, "", err
	}
	// Re-marshal through the raw entries so importer-owned fields survive
	// verbatim; entries pair by filename (never by index) so reordering and
	// deletion are both safe.
	var raw struct {
		Year   int              `json:"year"`
		Photos []map[string]any `json:"photos"`
	}
	if err := json.Unmarshal(doc.raw, &raw); err != nil {
		return AlbumYear{}, "", err
	}
	byFilename := make(map[string]map[string]any, len(raw.Photos))
	for _, entry := range raw.Photos {
		if name, ok := entry["filename"].(string); ok {
			byFilename[name] = entry
		}
	}
	emitted := make([]map[string]any, 0, len(doc.Photos))
	for _, photo := range doc.Photos {
		entry, ok := byFilename[photo.Filename]
		if !ok {
			return AlbumYear{}, "", fmt.Errorf("photo %q is not in the stored metadata", photo.Filename)
		}
		setStr(entry, "title", photo.Title)
		setStr(entry, "description", photo.Description)
		setStr(entry, "alt", photo.Alt)
		emitted = append(emitted, entry)
	}
	raw.Photos = emitted
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return AlbumYear{}, "", err
	}
	out = append(out, '\n')
	newDigest, err := s.WriteFile(siteID, RootContent, "albums/metadata/"+year+".json", out)
	if err != nil {
		return AlbumYear{}, "", err
	}
	// Parse the bytes this call wrote (inside the lock) so the response
	// describes this save, never a concurrent writer's content.
	saved, _, err := s.GetAlbumYear(siteID, year)
	if err != nil {
		return AlbumYear{}, "", err
	}
	return saved, newDigest, nil
}

func setStr(entry map[string]any, key, value string) {
	if value == "" {
		delete(entry, key)
		return
	}
	entry[key] = value
}

// StoreUpload validates and stores one uploaded original into the site's
// photos tree (server-controlled path yyyy/mm/dd/<id><ext>) and returns the
// relative path. The content type is sniffed from the bytes, never trusted
// from the client.
func (s *Store) StoreUpload(siteID uuid.UUID, reader io.Reader, sizeHint int64) (string, error) {
	if sizeHint > maxUploadBytes {
		return "", ErrTooLarge
	}
	limited := io.LimitReader(reader, maxUploadBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}
	if len(data) > maxUploadBytes {
		return "", ErrTooLarge
	}
	if len(data) == 0 {
		return "", errors.New("empty upload")
	}
	contentType := http.DetectContentType(data[:min(512, len(data))])
	var ext string
	switch {
	case strings.HasPrefix(contentType, "image/jpeg"):
		ext = ".jpeg"
	case strings.HasPrefix(contentType, "image/png"):
		ext = ".png"
	case strings.HasPrefix(contentType, "image/webp"):
		ext = ".webp"
	case strings.HasPrefix(contentType, "image/gif"):
		ext = ".gif"
	default:
		return "", fmt.Errorf("unsupported content type %q (jpeg, png, webp allowed)", contentType)
	}
	if ext == ".gif" && !allowedImageExt[ext] {
		return "", fmt.Errorf("unsupported content type %q", contentType)
	}
	now := time.Now().UTC()
	name := fmt.Sprintf("%s%s", randomHexName(10), ext)
	rel := fmt.Sprintf("%04d/%02d/%02d/%s", now.Year(), int(now.Month()), now.Day(), name)
	if _, err := s.WriteFile(siteID, RootContent, "albums/photos/"+rel, data); err != nil {
		return "", err
	}
	return rel, nil
}

func randomHexName(n int) string {
	const hexDigits = "0123456789abcdefghijklmnopqrstuvwxyz"
	buf := randomBytes(n)
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = hexDigits[int(b)%len(hexDigits)]
	}
	return string(out)
}

// DeleteAlbumPhoto removes one photo's original, its thumbnails, and its
// metadata entry. The caller passes the set of release metadata documents
// still referenced (live or rollback-able); a photo listed there is refused
// so frozen and rollback targets keep working media.
func (s *Store) DeleteAlbumPhoto(siteID uuid.UUID, year, filename string, releaseJSONs [][]byte, expectedDigest string) error {
	if !yearPattern.MatchString(year) || filename == "" || strings.Contains(filename, "/") {
		return ErrInvalidPath
	}
	return s.WithAlbumMetaLock(context.Background(), siteID, func() error {
		doc, digest, err := s.GetAlbumYear(siteID, year)
		if err != nil {
			return err
		}
		if expectedDigest != "" && digest != expectedDigest {
			return ErrConflict
		}
		var target *AlbumPhoto
		for i := range doc.Photos {
			if doc.Photos[i].Filename == filename {
				target = &doc.Photos[i]
				break
			}
		}
		if target == nil {
			return ErrNotFound
		}
		// Reference check: any live/superseded release metadata still listing
		// this photo's path blocks a hard delete.
		for _, raw := range releaseJSONs {
			if strings.Contains(string(raw), target.Path) {
				return &PhotoInUseError{Path: target.Path}
			}
		}
		// Remove files: original plus every variant thumbnail.
		_ = s.Remove(siteID, RootContent, "albums/photos/"+target.Path)
		for _, variant := range target.Variants {
			_ = s.Remove(siteID, RootContent, "albums/thumbnails/"+variant.Path)
		}
		// Remove the metadata entry through the locked raw edit.
		_, _, err = s.saveAlbumYearLocked(siteID, year, digest, func(edit *AlbumYear) error {
			kept := edit.Photos[:0]
			for _, photo := range edit.Photos {
				if photo.Filename != filename {
					kept = append(kept, photo)
				}
			}
			edit.Photos = kept
			return nil
		})
		return err
	})
}

// PhotoInUseError marks a photo still referenced by frozen releases.
type PhotoInUseError struct{ Path string }

func (e *PhotoInUseError) Error() string {
	return "photo " + e.Path + " is still referenced by a live or rollback-able release"
}

// ReleaseMetadataForSite collects every album metadata document of one
// site's releases (live, superseded, or rollback-able) so photo deletion can
// prove nothing still references the file. The real frozen layout stores
// year documents at <release>/albums/<year>.json (the build snapshot's
// public tree); <release>/albums/metadata is also scanned for layouts that
// keep the source-side structure. Any unreadable release directory fails
// the scan: a deletion decision may never treat unknown references as "no
// references".
func ReleaseMetadataForSite(releasesRoot string, siteID uuid.UUID) ([][]byte, error) {
	siteDir := filepath.Join(releasesRoot, siteID.String())
	entries, err := os.ReadDir(siteDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan releases of site %s: %w", siteID, err)
	}
	var out [][]byte
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		releaseAlbums := filepath.Join(siteDir, entry.Name(), "albums")
		metaDirs := []string{releaseAlbums, filepath.Join(releaseAlbums, "metadata")}
		for _, metaDir := range metaDirs {
			files, err := os.ReadDir(metaDir)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("scan release %s albums metadata: %w", entry.Name(), err)
			}
			for _, mf := range files {
				if mf.IsDir() || !strings.HasSuffix(mf.Name(), ".json") {
					continue
				}
				data, err := os.ReadFile(filepath.Join(metaDir, mf.Name()))
				if err != nil {
					return nil, fmt.Errorf("read release %s %s: %w", entry.Name(), mf.Name(), err)
				}
				out = append(out, data)
			}
		}
	}
	return out, nil
}

// DetachAlbumPhoto removes a photo's metadata entry only: the next build
// excludes it from public output, while the original and thumbnail files
// stay on disk for releases that still reference them. This is the
// "remove from future content" path; the hard DELETE additionally removes
// files and is blocked while any release still references them.
func (s *Store) DetachAlbumPhoto(siteID uuid.UUID, year, filename string, expectedDigest string) error {
	if !yearPattern.MatchString(year) || filename == "" || strings.Contains(filename, "/") {
		return ErrInvalidPath
	}
	return s.WithAlbumMetaLock(context.Background(), siteID, func() error {
		doc, digest, err := s.GetAlbumYear(siteID, year)
		if err != nil {
			return err
		}
		if expectedDigest != "" && digest != expectedDigest {
			return ErrConflict
		}
		found := false
		for _, photo := range doc.Photos {
			if photo.Filename == filename {
				found = true
				break
			}
		}
		if !found {
			return ErrNotFound
		}
		_, _, err = s.saveAlbumYearLocked(siteID, year, digest, func(edit *AlbumYear) error {
			kept := edit.Photos[:0]
			for _, photo := range edit.Photos {
				if photo.Filename != filename {
					kept = append(kept, photo)
				}
			}
			edit.Photos = kept
			return nil
		})
		return err
	})
}
