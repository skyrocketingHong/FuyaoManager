// Package themeregistry loads theme roots from the local filesystem into the
// catalog: it validates a theme's theme.json and configuration schema against
// the contracts/theme schemas, enforces the trusted build-adapter list, and
// writes the resulting row through the store. Registration is operator-driven
// (the fuyao-admin register-theme CLI); the management HTTP API never accepts
// file paths, shell commands, or Git addresses.
package themeregistry

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/themecontract"
)

// TrustedAdapters enumerates the build adapters the platform is willing to
// execute. It mirrors the adapter enum in contracts/theme/manifest.schema.json
// (its single source of truth); extend both in the same change.
var TrustedAdapters = map[string]bool{
	"node22-sveltekit-static": true,
}

// ThemeWriter is the store surface registration needs.
type ThemeWriter interface {
	Register(ctx context.Context, theme model.Theme) (bool, error)
}

// ErrAdapterNotTrusted marks a manifest naming an adapter outside
// TrustedAdapters.
var ErrAdapterNotTrusted = errors.New("theme adapter is not in the trusted adapter list")

// LoadThemeRoot validates the theme rooted at themeRoot against the contract
// schemas in contractsDir and returns the catalog row. The recorded source
// path is the absolute real path of themeRoot itself — the same directory the
// manifest and configuration schema were validated from — so the build
// executor can never be pointed at a different tree than the registered
// definition. The manifest digest covers the exact theme.json bytes read
// here.
func LoadThemeRoot(ctx context.Context, contractsDir, themeRoot string) (model.Theme, error) {
	_ = ctx // file loading only; no cancellable IO today
	set, err := themecontract.Load(contractsDir)
	if err != nil {
		return model.Theme{}, err
	}
	// Resolve to an absolute real path first: EvalSymlinks on a plain
	// relative path returns another relative path, and the recorded source
	// must be absolute and directory-shaped regardless of how the operator
	// spelled the theme root on the command line.
	absRoot, err := filepath.Abs(themeRoot)
	if err != nil {
		return model.Theme{}, fmt.Errorf("resolving theme root: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return model.Theme{}, fmt.Errorf("resolving theme root: %w", err)
	}
	info, err := os.Stat(realRoot)
	if err != nil || !info.IsDir() {
		return model.Theme{}, fmt.Errorf("theme root %q must be an existing directory", themeRoot)
	}

	manifestData, err := os.ReadFile(filepath.Join(realRoot, "theme.json"))
	if err != nil {
		return model.Theme{}, fmt.Errorf("reading theme.json: %w", err)
	}
	if err := set.ValidateManifest(manifestData); err != nil {
		return model.Theme{}, err
	}
	view, err := model.ParseThemeManifest(manifestData)
	if err != nil {
		return model.Theme{}, fmt.Errorf("theme.json: %w", err)
	}
	if err := CheckTrustedAdapter(view.Adapter); err != nil {
		return model.Theme{}, err
	}

	var configSchema json.RawMessage
	if view.ConfigSchema != nil {
		schemaPath, err := ResolveWithin(realRoot, view.ConfigSchema.File)
		if err != nil {
			return model.Theme{}, fmt.Errorf("reading config schema %s: %w", view.ConfigSchema.File, err)
		}
		schemaDoc, err := os.ReadFile(schemaPath)
		if err != nil {
			return model.Theme{}, fmt.Errorf("reading config schema %s: %w", view.ConfigSchema.File, err)
		}
		if err := set.ValidateThemeConfigSchema(schemaDoc); err != nil {
			return model.Theme{}, err
		}
		configSchema = schemaDoc
	}

	digest := sha256.Sum256(manifestData)
	return model.Theme{
		ThemeID:        view.ID,
		Version:        view.Version,
		Adapter:        view.Adapter,
		Manifest:       append(json.RawMessage(nil), manifestData...),
		ManifestDigest: digest[:],
		ConfigSchema:   configSchema,
		SourcePath:     realRoot,
	}, nil
}

// CheckTrustedAdapter reports whether the platform is willing to execute the
// manifest's build adapter. The manifest schema already restricts the value
// to its enum; this is the server-side trust check that stays authoritative
// when the schema enum grows.
func CheckTrustedAdapter(adapter string) error {
	if !TrustedAdapters[adapter] {
		return fmt.Errorf("%w: %q", ErrAdapterNotTrusted, adapter)
	}
	return nil
}

// Register loads a theme root and writes the catalog row. It reports whether
// a new row was created; an identical re-registration refreshes the source
// path and reports false, while a changed manifest or configuration schema
// under the same version fails with store.ErrConflict.
func Register(ctx context.Context, w ThemeWriter, contractsDir, themeRoot string) (model.Theme, bool, error) {
	theme, err := LoadThemeRoot(ctx, contractsDir, themeRoot)
	if err != nil {
		return model.Theme{}, false, err
	}
	theme.ID = newUUID()
	created, err := w.Register(ctx, theme)
	if err != nil {
		return model.Theme{}, false, err
	}
	return theme, created, nil
}

// newUUID returns a random UUIDv4 for a newly created catalog row.
func newUUID() uuid.UUID {
	id, err := uuid.NewRandom()
	if err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return id
}

// ResolveWithin joins name onto the realpath of root and requires the fully
// resolved target (symlinks included) to stay inside the realpath of root.
// Shared by the registry loader and the validate-theme CLI.
func ResolveWithin(root, name string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(filepath.Join(realRoot, name)))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(realRoot, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("path %q escapes the theme root", name)
	}
	return resolved, nil
}
