package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publicationmedia"
)

// CI assets are self-contained. Only production builds create the separate
// frozen media product; its absence must never cause a live-content fallback.
func previewFrozenMediaRoot(mode, buildsRoot string) func(uuid.UUID, uuid.UUID) (string, error) {
	if mode != "production" {
		return nil
	}
	return func(siteID, buildID uuid.UUID) (string, error) {
		frozen := filepath.Join(buildsRoot, buildID.String(), "publication-media")
		info, err := os.Lstat(frozen)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", publicationmedia.ErrUnsafe
		}
		file, err := os.OpenFile(filepath.Join(frozen, publicationmedia.ManifestName), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return "", err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, publicationmedia.MaxManifestBytes+1))
		if err != nil || len(data) > publicationmedia.MaxManifestBytes {
			return "", publicationmedia.ErrUnsafe
		}
		var manifest publicationmedia.Manifest
		if json.Unmarshal(data, &manifest) != nil || manifest.Version != 1 || manifest.SiteID != siteID.String() || manifest.BuildID != buildID.String() {
			return "", publicationmedia.ErrUnsafe
		}
		return frozen, nil
	}
}
