package themeimport

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	maxArchive  = 128 << 20
	maxExpanded = 512 << 20
	maxFiles    = 40000
)

func extractGitHub(input io.Reader, dst string) error {
	compressed := &io.LimitedReader{R: input, N: maxArchive + 1}
	stream, err := gzip.NewReader(compressed)
	if err != nil {
		return err
	}
	defer stream.Close()
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()
	files := tar.NewReader(stream)
	var total int64
	var prefix string
	for i := 0; ; i++ {
		header, err := files.Next()
		if errors.Is(err, io.EOF) {
			// Verify the gzip footer and reject oversized trailing data.
			if i == 0 || compressed.N <= 1 {
				return ErrIncompatible
			}
			padding, err := io.ReadAll(io.LimitReader(stream, 10<<10+1))
			if err != nil || len(padding) > 10<<10 || len(bytes.Trim(padding, "\x00")) != 0 || compressed.N <= 1 {
				return errors.New("invalid GitHub archive footer")
			}
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if i >= maxFiles || header.Size < 0 || header.Size > maxExpanded-total {
			return errors.New("theme archive exceeds installation limits")
		}
		total += header.Size
		full := strings.TrimSuffix(header.Name, "/")
		first, name, hasChild := strings.Cut(full, "/")
		if !validPath(full) || first == "." {
			return errors.New("unsafe theme archive path")
		}
		if prefix == "" {
			prefix = first
		}
		if first != prefix || (!hasChild && header.Typeflag != tar.TypeDir) {
			return errors.New("invalid GitHub archive root")
		}
		if !hasChild {
			continue
		}
		if skipPackageEntry(name) {
			continue
		}
		local := filepath.FromSlash(name)
		switch header.Typeflag {
		case tar.TypeDir:
			err = root.MkdirAll(local, 0o755)
		case tar.TypeReg:
			err = extractFile(root, local, files, header.Mode)
		default:
			return errors.New("theme archive contains a link or special file")
		}
		if err != nil {
			return err
		}
	}
}

func skipPackageEntry(name string) bool {
	for _, part := range strings.Split(strings.ToLower(name), "/") {
		if part == ".git" || part == "node_modules" || part == ".npmrc" || part == ".env" || strings.HasPrefix(part, ".env.") {
			return true
		}
	}
	return false
}

func extractFile(root *os.Root, name string, content io.Reader, mode int64) error {
	if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(0o644|mode&0o111))
	if err != nil {
		return fmt.Errorf("extract %s: %w", path.Base(name), err)
	}
	_, copyErr := io.Copy(file, content)
	return errors.Join(copyErr, file.Close())
}
