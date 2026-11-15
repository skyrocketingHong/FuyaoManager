package content

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

// PostMedia names the public URL path, never an absolute server path.
type PostMedia struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

var wordpressSizeSuffix = regexp.MustCompile(`(?i)-[0-9]{1,5}x[0-9]{1,5}(\.(?:jpe?g|png|webp|gif))$`)

// WordPressOriginalPath maps a generated size URL to its unsized original.
func WordPressOriginalPath(publicPath string) (string, bool) {
	if !strings.HasPrefix(publicPath, "/wp-content/uploads/") || CheckPostMediaPath(publicPath) != nil {
		return "", false
	}
	original := wordpressSizeSuffix.ReplaceAllString(publicPath, "$1")
	if original == publicPath || CheckPostMediaPath(original) != nil {
		return "", false
	}
	return original, true
}

func postMediaName(value string, upload bool) (string, error) {
	if len(value) > 1024 || !utf8.ValidString(value) || strings.ContainsAny(value, "\\\x00\r\n") {
		return "", ErrInvalidPath
	}
	if !strings.HasPrefix(value, "/wp-content/uploads/") && (upload || !strings.HasPrefix(value, "/images/")) {
		return "", ErrInvalidPath
	}
	rel := strings.TrimPrefix(value, "/")
	if !fs.ValidPath(rel) || path.Clean(rel) != rel {
		return "", ErrInvalidPath
	}
	for _, part := range strings.Split(rel, "/") {
		if len(part) > 255 || strings.HasPrefix(part, ".") || strings.IndexFunc(part, unicode.IsControl) >= 0 {
			return "", ErrInvalidPath
		}
	}
	switch strings.ToLower(path.Ext(rel)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return rel, nil
	default:
		return "", ErrInvalidPath
	}
}

func mediaType(data []byte, rel string) (string, error) {
	if len(data) == 0 {
		return "", fieldError("file", "must not be empty")
	}
	kind := http.DetectContentType(data[:min(len(data), 512)])
	ext := strings.ToLower(path.Ext(rel))
	if (kind == "image/jpeg" && (ext == ".jpg" || ext == ".jpeg")) ||
		(kind == "image/png" && ext == ".png") || (kind == "image/gif" && ext == ".gif") ||
		(kind == "image/webp" && ext == ".webp") {
		return kind, nil
	}
	return "", fieldError("file", "image content and extension must match")
}

// mediaDirectory resolves every component relative to an opened content root.
// ASVS 5.3.2: O_NOFOLLOW prevents user-controlled path components escaping the scope.
func (s *Store) mediaDirectory(id uuid.UUID, rel string, create bool) (*os.File, string, error) {
	root, err := filepath.EvalSymlinks(s.ContentRoot(id))
	if err != nil {
		return nil, "", ErrNotFound
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", ErrInvalidPath
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts[:len(parts)-1] {
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(openErr, unix.ENOENT) && create {
			if mkdirErr := unix.Mkdirat(fd, part, 0750); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				unix.Close(fd)
				return nil, "", mkdirErr
			}
			next, openErr = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		_ = unix.Close(fd)
		if errors.Is(openErr, unix.ENOENT) {
			return nil, "", ErrNotFound
		}
		if openErr != nil {
			return nil, "", ErrInvalidPath
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), root), parts[len(parts)-1], nil
}

func readMediaFile(dir *os.File, name string) ([]byte, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, ErrInvalidPath
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Nlink != 1 {
		return nil, ErrInvalidPath
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxUploadBytes {
		return nil, ErrInvalidPath
	}
	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil || len(data) > int(maxUploadBytes) {
		return nil, ErrInvalidPath
	}
	return data, nil
}

func (s *Store) ReadPostMedia(id uuid.UUID, publicPath string) ([]byte, string, error) {
	rel, err := postMediaName(publicPath, false)
	if err != nil {
		return nil, "", err
	}
	dir, name, err := s.mediaDirectory(id, rel, false)
	if err != nil {
		return nil, "", err
	}
	defer dir.Close()
	data, err := readMediaFile(dir, name)
	if err != nil {
		return nil, "", err
	}
	kind, err := mediaType(data, rel)
	return data, kind, err
}

// PreparePostMedia bounds and sniffs an image before storing it.
func PreparePostMedia(publicPath string, reader io.Reader, sizeHint int64) (PostMedia, []byte, string, error) {
	if sizeHint > maxUploadBytes {
		return PostMedia{}, nil, "", ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxUploadBytes+1))
	if err != nil {
		return PostMedia{}, nil, "", err
	}
	if len(data) > int(maxUploadBytes) {
		return PostMedia{}, nil, "", ErrTooLarge
	}
	if len(data) == 0 {
		return PostMedia{}, nil, "", fieldError("file", "must not be empty")
	}
	if publicPath == "" {
		kind := http.DetectContentType(data[:min(len(data), 512)])
		ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif"}[kind]
		if ext == "" {
			return PostMedia{}, nil, "", fieldError("file", "unsupported image type")
		}
		now := time.Now().UTC()
		publicPath = fmt.Sprintf("/wp-content/uploads/%04d/%02d/%s%s", now.Year(), now.Month(), randomHexName(20), ext)
	}
	rel, err := postMediaName(publicPath, true)
	if err != nil {
		return PostMedia{}, nil, "", err
	}
	kind, err := mediaType(data, rel)
	if err != nil {
		return PostMedia{}, nil, "", err
	}
	return PostMedia{Path: publicPath, Size: int64(len(data))}, data, kind, nil
}

func ValidatePostMedia(publicPath string, data []byte) (string, error) {
	rel, err := postMediaName(publicPath, false)
	if err != nil {
		return "", err
	}
	if len(data) > int(maxUploadBytes) {
		return "", ErrTooLarge
	}
	return mediaType(data, rel)
}

func CheckPostMediaPath(publicPath string) error {
	_, err := postMediaName(publicPath, false)
	return err
}

// StorePostMedia retains the file-backed path for existing content.
func (s *Store) StorePostMedia(id uuid.UUID, publicPath string, reader io.Reader, sizeHint int64) (PostMedia, error) {
	item, data, _, err := PreparePostMedia(publicPath, reader, sizeHint)
	if err != nil {
		return PostMedia{}, err
	}
	rel := strings.TrimPrefix(item.Path, "/")
	if err := s.withFileLock(id, "post-media", rel, func() error { return s.writePostMedia(id, rel, data) }); err != nil {
		return PostMedia{}, err
	}
	return item, nil
}

func (s *Store) writePostMedia(id uuid.UUID, rel string, data []byte) error {
	dir, name, err := s.mediaDirectory(id, rel, true)
	if err != nil {
		return err
	}
	defer dir.Close()
	tmp := ".import-" + randomHexName(20)
	fd, err := unix.Openat(int(dir.Fd()), tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0640)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), tmp)
	defer unix.Unlinkat(int(dir.Fd()), tmp, 0)
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := unix.Linkat(int(dir.Fd()), tmp, int(dir.Fd()), name, 0); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return err
		}
		existing, readErr := readMediaFile(dir, name)
		if readErr != nil || !bytes.Equal(existing, data) {
			return ErrExists
		}
	}
	return nil
}

func (s *Store) ListPostMedia(id uuid.UUID) ([]PostMedia, error) {
	result := []PostMedia{}
	visited := 0
	for _, base := range []string{"wp-content/uploads", "images"} {
		dir, _, err := s.mediaDirectory(id, base+"/__probe__", false)
		if errors.Is(err, ErrNotFound) {
			continue
		} else if err != nil {
			return nil, err
		}
		err = listMediaDirectory(dir, base, 0, &visited, &result)
		_ = dir.Close()
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func listMediaDirectory(dir *os.File, relative string, depth int, visited *int, result *[]PostMedia) error {
	if depth > 32 {
		return ErrInvalidPath
	}
	entries, err := dir.ReadDir(25001)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	for _, entry := range entries {
		*visited++
		if *visited > 25000 {
			return ErrTooLarge
		}
		if strings.HasPrefix(entry.Name(), ".") || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := relative + "/" + entry.Name()
		if entry.IsDir() {
			fd, err := unix.Openat(int(dir.Fd()), entry.Name(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				return ErrInvalidPath
			}
			child := os.NewFile(uintptr(fd), entry.Name())
			err = listMediaDirectory(child, name, depth+1, visited, result)
			_ = child.Close()
			if err != nil {
				return err
			}
			continue
		}
		publicPath := "/" + name
		if _, err := postMediaName(publicPath, false); err != nil {
			continue
		}
		var stat unix.Stat_t
		if err := unix.Fstatat(int(dir.Fd()), entry.Name(), &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
			continue
		}
		*result = append(*result, PostMedia{Path: publicPath, Size: stat.Size})
	}
	return nil
}
