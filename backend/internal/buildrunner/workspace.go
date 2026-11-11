package buildrunner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// workspaceExcludes names the registered source's entries a task never
// copies: installed dependencies (reused via the repository's node_modules
// resolution instead), the theme's own mutable build state, and VCS noise.
// Everything else — sources, scripts, configs, static assets, sample
// fixtures, tests — is part of the buildable snapshot.
var workspaceExcludes = map[string]bool{
	"node_modules": true,
	".fuyao":       true,
	"build":        true,
	".svelte-kit":  true,
	".git":         true,
	".DS_Store":    true,
}

// workspaceScope names the copy rule for provenance records.
const workspaceScope = "theme-root-except(node_modules,.fuyao,build,.svelte-kit,.git,.DS_Store" +
	",*.env,*.env.*,*.npmrc,*.yarnrc,*.yarnrc.yml,*.pem,*.key,*.p12,*.pfx)"

// sensitiveFileExcludes are file names (or .env. prefixes) never copied
// into a task, at any depth: private environment secrets and package
// registry credentials are not build inputs. The rule is name-based and
// applies to any source tree, Git or not.
var sensitiveFileExcludes = map[string]bool{
	".env": true, ".npmrc": true, ".yarnrc": true, ".yarnrc.yml": true,
}

// sensitiveFileName reports whether a regular file must be skipped.
func sensitiveFileName(name string) bool {
	if sensitiveFileExcludes[name] || strings.HasPrefix(name, ".env.") {
		return true
	}
	switch filepath.Ext(name) {
	case ".pem", ".key", ".p12", ".pfx":
		return true
	}
	return false
}

// sourceManifest is the per-task record of exactly which source bytes the
// build consumed: one digest per copied file plus the aggregate digest over
// the sorted (path, digest) list. This is deliberately broader than the
// registered theme.json digest — it captures the whole copied tree.
type sourceManifest struct {
	Scope       string            `json:"scope"`
	SourceRoot  string            `json:"sourceRoot"`
	FileCount   int               `json:"fileCount"`
	Files       map[string]string `json:"files"`
	Aggregate   string            `json:"aggregate"`
	ManifestSHA string            `json:"manifestSha256"`
}

// copyThemeSource materializes the task's private working copy of the
// registered theme root into dst and returns the source manifest. The copy
// refuses symlinks (a moved or replaced tree fails loudly instead of
// silently building from elsewhere) and never writes into src: the
// registered source stays read-only for the task's whole lifetime.
func copyThemeSource(dst, src string) (sourceManifest, error) {
	manifest := sourceManifest{Scope: workspaceScope, Files: map[string]string{}}
	realSrc, err := filepath.EvalSymlinks(src)
	if err != nil {
		return sourceManifest{}, err
	}
	manifest.SourceRoot = realSrc
	if err := copyThemeTree(realSrc, dst, "", manifest.Files); err != nil {
		return sourceManifest{}, err
	}
	manifest.FileCount = len(manifest.Files)
	manifest.Aggregate = aggregateDigest(manifest.Files)
	// The theme's own registered definition digest — the same value the
	// catalog stores for theme.json, distinct from the whole-tree aggregate.
	manifest.ManifestSHA = manifest.Files["theme.json"]
	return manifest, nil
}

// assembleWorkspace mirrors the source's dependency topology inside the
// task: the theme copy lands at wsRoot/site, with a private node_modules
// reference on the copy for the source's own installed packages and one on
// wsRoot for the packages hoisted to the source's workspace root. Node's
// module lookup walks the copy, then wsRoot — the same two levels the real
// workspace offers — so resolution is anchored at the registered source no
// matter where the BuildsRoot lives. The references are task-private and
// never enter the sealed snapshot or artifacts.
func assembleWorkspace(wsRoot, sourceRoot, depRoot string) error {
	for link, target := range map[string]string{
		filepath.Join(wsRoot, "site", "node_modules"): filepath.Join(sourceRoot, "node_modules"),
		filepath.Join(wsRoot, "node_modules"):         filepath.Join(depRoot, "node_modules"),
	} {
		if !dirExists(target) {
			continue
		}
		if err := os.Symlink(target, link); err != nil {
			return err
		}
	}
	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// FindWorkspaceRoot walks up from the source root to the nearest ancestor
// package.json declaring workspaces (bounded), returning that root; without
// one the source itself is the dependency root. Build commands get that
// root's node_modules/.bin on PATH so CLIs resolve from the source's real
// install, not from wherever the task directory happens to sit.
func FindWorkspaceRoot(sourceRoot string) string {
	dir := sourceRoot
	for i := 0; i < 8; i++ {
		data, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err == nil {
			var pkg struct {
				Workspaces any `json:"workspaces"`
			}
			if json.Unmarshal(data, &pkg) == nil && pkg.Workspaces != nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return sourceRoot
}

// copyThemeTree copies one directory level, recording per-file digests under
// relative slash paths.
func copyThemeTree(src, dst, rel string, digests map[string]string) error {
	entries, err := os.ReadDir(filepath.Join(src, rel))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dst, rel), 0o700); err != nil {
		return err
	}
	for _, entry := range entries {
		child := entry.Name()
		if rel == "" && workspaceExcludes[child] {
			continue
		}
		childRel := child
		if rel != "" {
			childRel = rel + "/" + child
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to copy symlink source entry %s", childRel)
		}
		if entry.IsDir() {
			if err := copyThemeTree(src, dst, childRel, digests); err != nil {
				return err
			}
			continue
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("refusing irregular source entry %s", childRel)
		}
		if sensitiveFileName(child) {
			continue
		}
		sum, err := copySourceFile(
			filepath.Join(src, filepath.FromSlash(childRel)),
			filepath.Join(dst, filepath.FromSlash(childRel)))
		if err != nil {
			return err
		}
		digests[childRel] = sum
	}
	return nil
}

// copySourceFile copies one file and returns its SHA-256 hex digest.
func copySourceFile(srcPath, dstPath string) (string, error) {
	in, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return "", err
	}
	out, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, hasher), in); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// aggregateDigest hashes the sorted (relative path, digest) entries; equal
// trees always hash equal regardless of directory iteration order.
func aggregateDigest(files map[string]string) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	hasher := sha256.New()
	for _, p := range paths {
		fmt.Fprintf(hasher, "%s %s\n", p, files[p])
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// writeSourceManifest persists the provenance record next to the task's
// other private metadata (mode 0600; never part of the public artifacts).
func writeSourceManifest(taskDir string, manifest sourceManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(taskDir, "source-manifest.json"), append(data, '\n'), 0o600)
}

// SealTree copies a produced tree into a destination directory and freezes
// the copy read-only. The copy is verified against the source before
// freezing. Exported for the publish pipeline, which seals succeeded-build
// artifacts into immutable release directories the same way.
func SealTree(src, dst string) error {
	if err := copyTree(src, dst); err != nil {
		return err
	}
	if err := verifyTreeCopy(src, dst); err != nil {
		return err
	}
	return freezeTree(dst)
}

// copyTree duplicates a directory tree, refusing symlinks; destination
// directories get the write bit during the copy and are frozen by the
// caller once the copy verifies.
func copyTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to copy symlink input")
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()|0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		s := filepath.Join(src, entry.Name())
		d := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyTree(s, d); err != nil {
				return err
			}
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("refusing to copy symlink input")
		}
		if err := copyFile(s, d); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// verifyTreeCopy confirms every regular file copied with identical size; a
// full digest pass is unnecessary because the frozen inputs carry their own
// digests and the build consumer re-verifies them.
func verifyTreeCopy(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target, statErr := os.Stat(filepath.Join(dst, rel))
		if statErr != nil {
			return statErr
		}
		if target.Size() != info.Size() {
			return fmt.Errorf("copy size mismatch at %s", rel)
		}
		return nil
	})
}

// freezeTree marks the harvested artifacts read-only so later tasks cannot
// mutate a succeeded build's product.
func freezeTree(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		perm := os.FileMode(0o444)
		if info.IsDir() {
			perm = 0o555
		}
		return os.Chmod(path, perm)
	})
}

// MakeTreeWritable restores owner write bits on a frozen tree so it can be
// modified or removed; exported for test cleanup of frozen task directories.
func MakeTreeWritable(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return os.Chmod(path, info.Mode().Perm()|0o700)
		}
		return os.Chmod(path, info.Mode().Perm()|0o200)
	})
}

func dirMissing(path string) bool {
	info, err := os.Stat(path)
	return err != nil || !info.IsDir()
}

// TreeDigest hashes a whole directory tree (relative path, mode, content) —
// used by tests to prove the registered source never changed across a build.
func TreeDigest(root string) (string, error) {
	files := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		if !info.Mode().IsRegular() {
			files[filepath.ToSlash(rel)] = fmt.Sprintf("mode=%d", info.Mode().Perm())
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		sum := sha256.Sum256(data)
		files[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		return "", err
	}
	return aggregateDigest(files), nil
}

var _ = fs.ModeDir // keep io/fs referenced for future filters
