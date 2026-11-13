package caddymanage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publicationmedia"
)

var publishMarkerPattern = regexp.MustCompile(`^__FUYAO_PUBLISH_[A-Z0-9_]+__$`)

type publicationSeal struct {
	SiteID, ReleaseID, ManifestDigest, MediaManifestDigest string
	ThemeID                                                string
	LayoutVersion                                          int
}

func validatePublication(value *Publication) error {
	if value == nil {
		return nil
	}
	if !canonicalUUID(value.SiteID) || value.SourceUID == 0 || len(value.AllowedThemes) == 0 || len(value.AllowedThemes) > 16 || len(value.Paths) == 0 || len(value.Paths) > 16 {
		return ErrPermission
	}
	for _, path := range []string{value.ReleasesRoot, value.BuildsRoot} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
			return ErrPermission
		}
	}
	seen := map[string]bool{}
	artifact := false
	for _, path := range value.Paths {
		if !publishMarkerPattern.MatchString(path.Marker) || seen[path.Marker] || path.Preserve == "" || strings.Contains(path.Preserve, "__FUYAO_") {
			return ErrPermission
		}
		seen[path.Marker] = true
		if path.MediaRelative == "" {
			artifact = true
			continue
		}
		if path.MediaRelative != "." && !oneOf(path.MediaRelative, publicationmedia.Paths...) {
			return ErrPermission
		}
	}
	if !artifact {
		return ErrPermission
	}
	for _, theme := range value.AllowedThemes {
		if !keyPattern.MatchString(theme) {
			return ErrPermission
		}
	}
	return nil
}

func (e *Engine) publicationRoot(key, release string) string {
	return filepath.Join(e.stateRoot, "releases", key, release)
}

func (e *Engine) renderPublication(p policy, settings Settings, release string) ([]byte, error) {
	if release == "" {
		return Render(p.Registration, settings, p.base)
	}
	if p.Publication == nil || !canonicalUUID(release) {
		return nil, ErrUnsupported
	}
	root := e.publicationRoot(p.SiteKey, release)
	var seal publicationSeal
	if e.readRecord(filepath.Join(root, "seal.json"), &seal) != nil || seal.SiteID != p.Publication.SiteID || seal.ReleaseID != release || !oneOf(seal.ThemeID, p.Publication.AllowedThemes...) {
		return nil, ErrConflict
	}
	paths := map[string]string{}
	for _, path := range p.Publication.Paths {
		target := filepath.Join(root, "site")
		if path.MediaRelative != "" {
			target = filepath.Join(root, "media", filepath.FromSlash(path.MediaRelative))
		}
		if err := e.ancestors(target); err != nil {
			return nil, ErrPermission
		}
		paths[path.Marker] = strconv.Quote(target)
	}
	return render(p.Registration, settings, p.base, paths)
}

func readPublicationFile(directory, name string, uid uint32, limit int64) ([]byte, error) {
	dir, err := publicationmedia.OpenDirectory(directory)
	if err != nil {
		return nil, ErrPermission
	}
	defer dir.Close()
	file, err := publicationmedia.OpenFile(dir, name, uid)
	if err != nil {
		return nil, ErrPermission
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, ErrPermission
	}
	return data, nil
}

// sealPublication reads only the operator-bound site's release directory and
// the matching build's frozen media. No submitted pathname is ever opened.
// ASVS 8.2.2/5.3.2: tenant binding, UUID IDs and descriptor-relative reads.
func (e *Engine) sealPublication(ctx context.Context, p policy, request Request) error {
	pub := p.Publication
	if pub == nil || pub.SiteID != request.SiteID {
		return ErrUnsupported
	}
	destination := e.publicationRoot(p.SiteKey, request.ReleaseID)
	var existing publicationSeal
	if err := e.readRecord(filepath.Join(destination, "seal.json"), &existing); err == nil {
		if existing.LayoutVersion != 1 || existing.SiteID != request.SiteID || existing.ReleaseID != request.ReleaseID || existing.ManifestDigest != request.ManifestDigest || existing.MediaManifestDigest != request.MediaManifestDigest || !oneOf(existing.ThemeID, pub.AllowedThemes...) {
			return ErrConflict
		}
		return nil
	} else if !os.IsNotExist(err) {
		return ErrPermission
	}
	artifactSource := filepath.Join(pub.ReleasesRoot, request.SiteID, request.ReleaseID)
	mediaSource := filepath.Join(pub.BuildsRoot, request.ReleaseID, "publication-media")
	releaseData, err := readPublicationFile(artifactSource, "release.json", pub.SourceUID, maxRequestBytes)
	if err != nil || Digest(releaseData) != request.ManifestDigest {
		return ErrConflict
	}
	var release struct {
		Contract      string `json:"contract"`
		ReleaseID     string `json:"releaseId"`
		SchemaVersion int    `json:"schemaVersion"`
		Theme         struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"theme"`
	}
	if json.Unmarshal(releaseData, &release) != nil || release.Contract != "fuyao.theme.release/v1" || release.SchemaVersion != 1 || release.ReleaseID != request.ReleaseID || !oneOf(release.Theme.ID, pub.AllowedThemes...) {
		return ErrUnsupported
	}
	mediaData, err := readPublicationFile(mediaSource, publicationmedia.ManifestName, pub.SourceUID, publicationmedia.MaxManifestBytes)
	if err != nil || Digest(mediaData) != request.MediaManifestDigest {
		return ErrConflict
	}
	var media publicationmedia.Manifest
	if strictJSON(mediaData, &media) != nil || media.Version != 1 || media.SiteID != request.SiteID || media.BuildID != request.ReleaseID || len(media.Files) > publicationmedia.MaxFiles {
		return ErrConflict
	}
	parent := filepath.Dir(destination)
	if err := e.ensureDir(parent, 0750); err != nil {
		return err
	}
	stage := filepath.Join(parent, ".staging-"+newID())
	if err := e.ensureDir(stage, 0750); err != nil {
		return err
	}
	defer publicationmedia.Remove(stage)
	if publicationmedia.CopyTree(ctx, artifactSource, filepath.Join(stage, "site"), pub.SourceUID) != nil {
		return ErrPermission
	}
	if publicationmedia.CopyTree(ctx, mediaSource, filepath.Join(stage, "media"), pub.SourceUID) != nil {
		return ErrPermission
	}
	sealedRelease, err := os.ReadFile(filepath.Join(stage, "site", "release.json"))
	if err != nil || !bytes.Equal(sealedRelease, releaseData) {
		return ErrConflict
	}
	sealedMedia, err := os.ReadFile(filepath.Join(stage, "media", publicationmedia.ManifestName))
	if err != nil || !bytes.Equal(sealedMedia, mediaData) {
		return ErrConflict
	}
	actual, err := publicationmedia.Inventory(filepath.Join(stage, "media"), request.SiteID, request.ReleaseID)
	if err != nil {
		return ErrConflict
	}
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(media)
	if !bytes.Equal(a, b) {
		return ErrConflict
	}
	if err := e.installPublishedMedia(ctx, stage, media); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(stage, "site", "index.html")); err != nil {
		return ErrUnsupported
	}
	seal := publicationSeal{SiteID: request.SiteID, ReleaseID: request.ReleaseID, ManifestDigest: request.ManifestDigest, MediaManifestDigest: request.MediaManifestDigest, ThemeID: release.Theme.ID, LayoutVersion: 1}
	if err := e.writeRecord(filepath.Join(stage, "seal.json"), seal); err != nil {
		return err
	}
	if publicationmedia.FreezeTree(filepath.Join(stage, "site")) != nil || publicationmedia.FreezeTree(filepath.Join(stage, "media")) != nil {
		return ErrPermission
	}
	if err := os.Chmod(stage, 0750); err != nil {
		return ErrPermission
	}
	if _, err := os.Lstat(destination); err == nil {
		return ErrConflict
	} else if !os.IsNotExist(err) {
		return ErrPermission
	}
	if os.Rename(stage, destination) != nil {
		return ErrUnavailable
	}
	dir, err := os.Open(parent)
	if err != nil {
		return ErrUnavailable
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return ErrUnavailable
	}
	return nil
}

func (e *Engine) installPublishedMedia(ctx context.Context, stage string, manifest publicationmedia.Manifest) error {
	// Production previews reserve each complete URL prefix for frozen media.
	// Replace these subtrees rather than merging artifact-only files back in.
	for _, relative := range publicationmedia.Paths {
		published := relative
		if strings.HasPrefix(relative, "albums/") {
			published = "media/" + relative
		}
		source := filepath.Join(stage, "media", filepath.FromSlash(relative))
		target := filepath.Join(stage, "site", filepath.FromSlash(published))
		if err := publicationmedia.Remove(target); err != nil {
			return ErrPermission
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return ErrUnsupported
		}
		if err := publicationmedia.CopyTree(ctx, source, target, e.ownerUID); err != nil {
			return ErrPermission
		}
	}
	for _, file := range manifest.Files {
		published := file.Path
		if strings.HasPrefix(published, "albums/") {
			published = "media/" + published
		}
		data, err := os.ReadFile(filepath.Join(stage, "site", filepath.FromSlash(published)))
		if err != nil || int64(len(data)) != file.Size || Digest(data) != file.SHA256 {
			return ErrConflict
		}
	}
	return nil
}
