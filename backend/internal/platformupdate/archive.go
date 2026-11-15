package platformupdate

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func safeName(name string) bool {
	return name != "" && name != "." && !strings.Contains(name, "\\") && !strings.HasPrefix(name, "/") && path.Clean(name) == name && name != ".." && !strings.HasPrefix(name, "../")
}

// Stage is the initial-install operator entry. Routine updates call the same
// extraction and verification functions before changing the current pointer.
func Stage(archive, dir, repository string, expected Version) (Manifest, error) {
	if !shaPattern.MatchString(expected.Commit) || expected.RunID < 1 || expected.Attempt < 1 || (expected.Branch != "main" && expected.Branch != "dev") {
		return Manifest{}, errors.New("invalid expected CI identity")
	}
	if err := os.Mkdir(dir, 0755); err != nil {
		return Manifest{}, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dir)
		}
	}()
	if err := extractArchive(archive, dir); err != nil {
		return Manifest{}, err
	}
	m, err := verifyRelease(dir, repository, expected)
	if err != nil {
		return m, err
	}
	ok = true
	return m, nil
}

// ASVS 5.2.2/5.3.1: extract only regular, bounded, unique files into a new
// root-owned staging directory. No links, inherited executable modes or paths.
func extractArchive(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if !safeName(h.Name) || seen[h.Name] || len(seen) >= 10000 || h.Typeflag != tar.TypeReg || h.Size < 0 || h.Size > 128<<20 {
			return errors.New("invalid archive entry")
		}
		total += h.Size
		if total > 512<<20 {
			return errors.New("archive exceeds expanded size limit")
		}
		seen[h.Name] = true
		dst := filepath.Join(dir, filepath.FromSlash(h.Name))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return err
		}
		_, err = io.CopyN(out, tr, h.Size)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	// Force gzip trailer/checksum validation; tar EOF alone does not read it.
	if _, err := io.Copy(io.Discard, io.LimitReader(gz, 1<<20)); err != nil {
		return err
	}
	return nil
}

func verifyRelease(dir, repository string, expected Version) (Manifest, error) {
	m, err := readManifest(dir)
	if err != nil {
		return m, err
	}
	if m.Format != 1 || m.Repository != repository || m.Architecture != "linux/amd64" || m.Commit != expected.Commit || m.RunID != expected.RunID || m.Attempt != expected.Attempt || m.Branch != expected.Branch || m.DatabaseSchema <= 0 || len(m.Files) == 0 {
		return m, errors.New("release provenance mismatch")
	}
	seen := map[string]bool{}
	err = filepath.WalkDir(dir, func(p string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "release.json" {
			return nil
		}
		if !safeName(rel) || entry.Type() != 0 || m.Files[rel] == "" {
			return errors.New("unlisted release file")
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		_ = f.Close()
		if err != nil {
			return err
		}
		if hex.EncodeToString(h.Sum(nil)) != m.Files[rel] {
			return fmt.Errorf("file digest mismatch: %s", rel)
		}
		seen[rel] = true
		return nil
	})
	if err != nil {
		return m, err
	}
	if len(seen) != len(m.Files) {
		return m, errors.New("release file missing")
	}
	for _, name := range []string{"fuyao-admin", "fuyao-manager-updater", "fuyao-caddy-control", "fuyao-caddy-inventory", "fuyao-caddy-register"} {
		if !seen[name] {
			return m, errors.New("release executable missing")
		}
		binary, err := elf.Open(filepath.Join(dir, name))
		if err != nil {
			return m, errors.New("release executable is not ELF")
		}
		valid := binary.Class == elf.ELFCLASS64 && binary.Machine == elf.EM_X86_64 && (binary.Type == elf.ET_EXEC || binary.Type == elf.ET_DYN)
		_ = binary.Close()
		if !valid {
			return m, errors.New("release executable architecture invalid")
		}
		if err := os.Chmod(filepath.Join(dir, name), 0755); err != nil {
			return m, err
		}
	}
	for _, name := range []string{"frontend/dist/index.html", "contracts/openapi/openapi.yaml", "backend/go.mod"} {
		if !seen[name] {
			return m, errors.New("release resource missing")
		}
	}
	return m, nil
}
