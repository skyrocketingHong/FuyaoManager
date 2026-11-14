// Package content is the platform's single authority for per-site editable
// content: markdown posts, data YAML files, and the site configuration that
// the frozen-snapshot theme builds consume. All paths are derived from the
// provisioning root and jailed inside the site's own tree; nothing here
// trusts browser-supplied paths.
package content

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// ErrNotFound marks missing files; ErrInvalidPath marks rejected path shapes.
var (
	ErrNotFound    = errors.New("content not found")
	ErrInvalidPath = errors.New("invalid content path")
	ErrEmptyFile   = errors.New("content file is empty")
	ErrShared      = errors.New("edit shared content in the platform library")
)

// Store owns the per-site content and config trees (siteprovision layout).
type Store struct {
	// SitesRoot is the provisioning root: <root>/<siteID>/{content,config}.
	SitesRoot string
}

// New assembles the store around the provisioning root.
func New(sitesRoot string) (*Store, error) {
	abs, err := filepath.Abs(sitesRoot)
	if err != nil {
		return nil, err
	}
	return &Store{SitesRoot: abs}, nil
}

func (s *Store) siteRoot(siteID uuid.UUID) string {
	return filepath.Join(s.SitesRoot, siteID.String())
}

// ContentRoot returns the site's content directory.
func (s *Store) ContentRoot(siteID uuid.UUID) string {
	return filepath.Join(s.siteRoot(siteID), "content")
}

// ConfigRoot returns the site's config directory.
func (s *Store) ConfigRoot(siteID uuid.UUID) string {
	return filepath.Join(s.siteRoot(siteID), "config")
}

// FaviconRoot returns the site's favicon directory (inside the config tree).
func (s *Store) FaviconRoot(siteID uuid.UUID) string {
	return filepath.Join(s.siteRoot(siteID), "config", "favicon")
}

// resolve jails a site-relative slash path inside the given root, rejecting
// traversal, absolute paths, and symlink escapes. The jail check anchors at
// the fully resolved root (e.g. /private/var on macOS), so platform-level
// symlinks in the prefix never false-positive; an existing target that
// resolves outside the root is rejected.
func resolve(root, rel string) (string, error) {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") {
		return "", ErrInvalidPath
	}
	clean := filepath.Clean("/" + rel)
	if clean == "/" || strings.Contains(clean, "..") {
		return "", ErrInvalidPath
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", ErrNotFound
	}
	joined := filepath.Join(resolvedRoot, filepath.FromSlash(clean))
	if _, err := os.Lstat(joined); err == nil {
		resolved, err := filepath.EvalSymlinks(joined)
		if err != nil {
			return "", ErrNotFound
		}
		if resolved != resolvedRoot && !strings.HasPrefix(resolved, resolvedRoot+string(os.PathSeparator)) {
			return "", ErrInvalidPath
		}
	}
	return joined, nil
}

// ReadFile returns the bytes and SHA-256 digest of one jailed file.
func (s *Store) ReadFile(siteID uuid.UUID, root Root, rel string) ([]byte, string, error) {
	base := s.ContentRoot(siteID)
	if root == RootConfig {
		base = s.ConfigRoot(siteID)
	}
	path, err := resolve(base, rel)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return data, Digest(data), nil
}

// WriteFile atomically places bytes at a jailed path (temp file + rename)
// and returns the new digest. Parent directories are created on demand.
func (s *Store) WriteFile(siteID uuid.UUID, root Root, rel string, data []byte) (string, error) {
	base := s.ContentRoot(siteID)
	if root == RootConfig {
		base = s.ConfigRoot(siteID)
	}
	path, err := resolve(base, rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	tmp := path + ".tmp-" + hex.EncodeToString(randomBytes(4))
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return Digest(data), nil
}

// Remove deletes one jailed file (directories are never removed here).
func (s *Store) Remove(siteID uuid.UUID, root Root, rel string) error {
	base := s.ContentRoot(siteID)
	if root == RootConfig {
		base = s.ConfigRoot(siteID)
	}
	path, err := resolve(base, rel)
	if err != nil {
		return err
	}
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return nil
}

// Digest is the digest form used for content ETags: sha256:<hex>.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Root selects the content or config tree.
type Root int

const (
	RootContent Root = iota
	RootConfig
)

// randomBytes is crypto-random material for temp names.
func randomBytes(n int) []byte {
	buf := make([]byte, n)
	if _, err := readRandom(buf); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return buf
}

// PostPathPattern constrains post locations: category/<slug>.md with one
// nesting level, lowercase slugs, no traversal.
var PostPathPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}/[a-z0-9][a-z0-9-]{0,120}\.md$`)

// ValidPostPath reports whether a site-relative post path is well-formed.
func ValidPostPath(rel string) bool {
	return PostPathPattern.MatchString(rel)
}

// errUnterminatedFrontmatter marks a recognized start delimiter whose
// closing line never arrives; it is a hard error, never a "no frontmatter".
var errUnterminatedFrontmatter = errors.New("unterminated frontmatter")

// utf8BOM is stripped only when it sits directly before a start delimiter.
const utf8BOM = "\xef\xbb\xbf"

// SplitFrontmatter separates YAML frontmatter from the markdown body and
// returns the raw frontmatter bytes (nil when absent, verbatim input — BOM
// included — as the body). The start delimiter must be the first line,
// optionally after a UTF-8 BOM, exactly "---" with LF or CRLF; a bare "---"
// at EOF is a recognized but unterminated header. The end delimiter must be
// a whole line exactly "---" (a trailing CR is allowed for CRLF; suffix
// text, extra dashes, indentation, and trailing spaces keep it from
// counting), and only that line's own LF or CRLF is consumed — leading
// blank lines, mixed newlines, trailing newlines, and mid-body "---" lines
// stay part of the body untouched. Newlines are never normalized.
func SplitFrontmatter(data []byte) (frontmatter []byte, body []byte, err error) {
	rest := data
	if bytes.HasPrefix(rest, []byte(utf8BOM)) {
		rest = rest[3:]
	}
	if !bytes.HasPrefix(rest, []byte("---")) {
		return nil, data, nil
	}
	switch {
	case len(rest) == 3:
		// A bare start delimiter at EOF is an unterminated header.
		return nil, nil, errUnterminatedFrontmatter
	case rest[3] == '\n':
		rest = rest[4:]
	case rest[3] == '\r' && len(rest) >= 5 && rest[4] == '\n':
		rest = rest[5:]
	default:
		// The first line is not exactly "---": no frontmatter at all, and the
		// input is the body verbatim.
		return nil, data, nil
	}
	offset := 0
	for {
		idx := bytes.IndexByte(rest[offset:], '\n')
		if idx < 0 {
			// The final line runs to EOF. Without an actual LF there is no
			// CRLF: only a line that reads exactly "---" closes the header,
			// so "---\r" at EOF is an unterminated header, never a CRLF end.
			if string(rest[offset:]) == "---" {
				return rest[:offset], rest[len(rest):], nil
			}
			return nil, nil, errUnterminatedFrontmatter
		}
		line := rest[offset : offset+idx]
		if isFrontmatterEndLine(line) {
			// Consume only the end line's own line terminator; everything
			// after it is the body, byte for byte.
			return rest[:offset], rest[offset+idx+1:], nil
		}
		offset += idx + 1
	}
}

// isFrontmatterEndLine reports whether a whole line is exactly the closing
// delimiter, tolerating one trailing CR for CRLF endings and nothing else.
func isFrontmatterEndLine(line []byte) bool {
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return string(line) == "---"
}

// RenderFrontmatter marshals ordered frontmatter fields as YAML with the
// conventional --- fences.
func RenderFrontmatter(fields map[string]any) ([]byte, error) {
	var buf strings.Builder
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fields); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	buf.WriteString("---\n")
	return []byte(buf.String()), nil
}

func readRandom(buf []byte) (int, error) {
	return cryptoRead(buf)
}
