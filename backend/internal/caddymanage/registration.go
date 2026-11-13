package caddymanage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyconfig"
)

type OperatorRegistration struct {
	SiteKey      string
	MasterFile   string
	SourceRoot   string
	TemplateRoot string
	Apply        bool
	Publication  *Publication
	MediaRoots   map[string]string
}

type RegistrationResult struct {
	SiteKey      string   `json:"siteKey"`
	Registered   bool     `json:"registered"`
	SourceDigest string   `json:"sourceDigest"`
	Hosts        []string `json:"hosts"`
}

// RegisterStatic is a root-only operator action, deliberately absent from the
// stdin capability. No unprivileged caller may authorize a new site or path.
func (e *Engine) RegisterStatic(ctx context.Context, options OperatorRegistration) (RegistrationResult, error) {
	result := RegistrationResult{SiteKey: options.SiteKey}
	if uint32(os.Geteuid()) != e.ownerUID || !keyPattern.MatchString(options.SiteKey) || e.runner == nil {
		return result, ErrPermission
	}
	if err := e.ensureDir(e.stateRoot, 0710); err != nil {
		return result, err
	}
	lock, err := e.lock(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() }()
	if _, err := e.trustedFile(filepath.Join(e.registryRoot, options.SiteKey+".json")); err == nil {
		return result, ErrConflict
	} else if !os.IsNotExist(err) {
		return result, err
	}
	master, err := e.trustedFile(options.MasterFile)
	if err != nil {
		return result, err
	}
	before, err := e.runner.Adapt(ctx, options.MasterFile)
	if err != nil {
		return result, ErrValidation
	}
	beforeDigest, err := canonicalConfigDigest(before, nil)
	if err != nil {
		return result, err
	}
	active, err := e.runner.Active(ctx)
	if err != nil {
		return result, ErrUnavailable
	}
	activeDigest, err := canonicalConfigDigest(active, nil)
	if err != nil || beforeDigest != activeDigest {
		return result, ErrConflict
	}
	sites, err := caddyconfig.Parse(before)
	if err != nil {
		return result, ErrValidation
	}
	var target *caddyconfig.Site
	for index := range sites {
		if sites[index].ID == options.SiteKey {
			target = &sites[index]
		}
	}
	if target == nil || target.Kind != caddyconfig.KindStatic || target.Root == "" || target.RootHasPlaceholder || target.DuplicateCount > 1 {
		return result, fmt.Errorf("site is not an unambiguous static directory: %w", ErrUnsupported)
	}
	if protectedEntry(*target) {
		return result, fmt.Errorf("protected manager control or public entry: %w", ErrUnsupported)
	}
	info, err := os.Stat(target.Root)
	if err != nil || !info.IsDir() {
		return result, ErrUnsupported
	}
	blocks, err := extractBlocks(master)
	if err != nil {
		return result, fmt.Errorf("master cannot be safely segmented into complete blocks: %w", err)
	}
	var selected *sourceBlock
	document := master
	masterStart, masterEnd := 0, 0
	oldSource := ""
	for index := range blocks {
		if blockMatches(blocks[index], *target) {
			if selected != nil {
				return result, fmt.Errorf("site matches multiple source blocks: %w", ErrUnsupported)
			}
			selected = &blocks[index]
		}
	}
	if selected == nil {
		document, selected, masterStart, masterEnd, oldSource, err = e.importedBlock(master, *target)
		if err != nil {
			return result, err
		}
	} else {
		masterStart, masterEnd = selected.start, selected.end
	}
	for _, other := range sites {
		if other.ID != target.ID && blockMatches(*selected, other) {
			return result, fmt.Errorf("source block crosses multiple inventory boundaries: %w", ErrUnsupported)
		}
	}
	source := append([]byte(nil), document[selected.start:selected.end]...)
	open := selected.open - selected.start
	if oldSource != "" {
		source = append([]byte(nil), document...)
		open = selected.open
	}
	if bytes.Contains(source, []byte("__FUYAO_")) {
		return result, ErrUnsupported
	}
	reg := Registration{SiteKey: target.ID, Kind: "static", StaticRoot: target.Root, MasterFile: options.MasterFile,
		SourceFile: filepath.Join(options.SourceRoot, target.ID+".caddy"), BaseTemplate: filepath.Join(options.TemplateRoot, target.ID+".caddy"),
		MasterTemplate: filepath.Join(options.TemplateRoot, target.ID+".master.caddy"), InitialDigest: Digest(source), Capabilities: Capabilities{SecurityHeaders: true}}
	if !newSourceOutsideRoots(before, reg.SourceFile) {
		return result, fmt.Errorf("source filename is not proven outside all static roots: %w", ErrUnsupported)
	}
	comparisonDigest := beforeDigest
	originalConfigName := options.MasterFile
	if oldSource != "" {
		if !newSourceOutsideRoots(before, oldSource) {
			return result, fmt.Errorf("imported filename is not proven outside all static roots: %w", ErrUnsupported)
		}
		originalConfigName = oldSource
	}
	base := append(append(append([]byte(nil), source[:open]...), []byte("__FUYAO_SECURITY_HEADERS__")...), source[open:]...)
	if options.Publication != nil {
		base, reg.Publication, err = AttachPublication(base, target.Root, *options.Publication, options.MediaRoots)
		if err != nil {
			return result, err
		}
	}
	preserved, err := Render(reg, Settings{}, base)
	if err != nil || !bytes.Equal(preserved, source) {
		return result, ErrPermission
	}
	masterTemplate := append(append(append([]byte(nil), master[:masterStart]...), []byte("import "+SourceMarker+"\n")...), master[masterEnd:]...)
	candidate, err := RenderMaster(masterTemplate, reg.SourceFile)
	if err != nil {
		return result, err
	}
	work := filepath.Join(e.stateRoot, "work", "register-"+newID())
	if err := e.ensureDir(work, 0750); err != nil {
		return result, err
	}
	defer os.RemoveAll(work)
	workSource := filepath.Join(work, "site.caddy")
	workMaster := filepath.Join(work, "master.caddy")
	workData, err := RenderMaster(masterTemplate, workSource)
	if err != nil {
		return result, err
	}
	if e.writeFile(workSource, source, 0640) != nil || e.writeFile(workMaster, workData, 0640) != nil {
		return result, ErrPermission
	}
	if e.runner.Validate(ctx, workMaster) != nil {
		return result, ErrValidation
	}
	adapted, err := e.runner.Adapt(ctx, workMaster)
	if err != nil {
		return result, ErrValidation
	}
	equivalent, err := canonicalConfigDigest(adapted, map[string]string{workMaster: options.MasterFile, workSource: originalConfigName})
	if err != nil || equivalent != comparisonDigest {
		return result, fmt.Errorf("source extraction changed adapted configuration at %s: %w", registrationDelta(before, adapted, map[string]string{workMaster: options.MasterFile, workSource: originalConfigName}), ErrConflict)
	}
	result.SourceDigest = reg.InitialDigest
	result.Hosts = target.Hosts
	if !options.Apply {
		return result, nil
	}
	// Recheck the complete source immediately before the privileged cutover.
	current, err := e.trustedFile(options.MasterFile)
	if err != nil || !bytes.Equal(current, master) {
		return result, ErrConflict
	}
	for _, path := range []string{reg.SourceFile, reg.BaseTemplate, reg.MasterTemplate, filepath.Join(e.registryRoot, reg.SiteKey+".json")} {
		if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
			return result, ErrConflict
		}
	}
	backup := filepath.Join(e.stateRoot, "registration-backups", newID()+".caddy")
	if err := e.writeFile(backup, master, 0600); err != nil {
		return result, err
	}
	installed := []string{reg.SourceFile, reg.BaseTemplate, reg.MasterTemplate, filepath.Join(e.registryRoot, reg.SiteKey+".json")}
	keepFiles := false
	defer func() {
		if !keepFiles {
			for _, path := range installed {
				_ = os.Remove(path)
			}
		}
	}()
	if e.writeFile(reg.SourceFile, source, 0640) != nil || e.writeFile(reg.BaseTemplate, base, 0600) != nil || e.writeFile(reg.MasterTemplate, masterTemplate, 0600) != nil {
		return result, ErrPermission
	}
	restore := func() error {
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if e.replaceSource(options.MasterFile, master) != nil || e.runner.Validate(recovery, options.MasterFile) != nil || e.runner.Reload(recovery, options.MasterFile) != nil {
			keepFiles = true
			return ErrUnavailable
		}
		actual, err := e.runner.Active(recovery)
		if err != nil {
			keepFiles = true
			return ErrUnavailable
		}
		digest, err := canonicalConfigDigest(actual, nil)
		if err != nil || digest != beforeDigest {
			keepFiles = true
			return ErrUnavailable
		}
		return nil
	}
	if err := e.replaceSource(options.MasterFile, candidate); err != nil {
		if restore() != nil {
			return result, ErrUnavailable
		}
		return result, err
	}
	if e.runner.Validate(ctx, options.MasterFile) != nil {
		if restore() != nil {
			return result, ErrUnavailable
		}
		return result, ErrValidation
	}
	adapted, err = e.runner.Adapt(ctx, options.MasterFile)
	if err != nil {
		if restore() != nil {
			return result, ErrUnavailable
		}
		return result, ErrValidation
	}
	equivalent, err = canonicalConfigDigest(adapted, map[string]string{reg.SourceFile: originalConfigName})
	if err != nil || equivalent != comparisonDigest {
		if restore() != nil {
			return result, ErrUnavailable
		}
		return result, ErrConflict
	}
	if e.runner.Reload(ctx, options.MasterFile) != nil {
		if restore() != nil {
			return result, ErrUnavailable
		}
		return result, ErrUnavailable
	}
	active, err = e.runner.Active(ctx)
	if err != nil {
		if restore() != nil {
			return result, ErrUnavailable
		}
		return result, ErrUnavailable
	}
	activeDigest, err = canonicalConfigDigest(active, map[string]string{reg.SourceFile: originalConfigName})
	if err != nil || activeDigest != comparisonDigest {
		if restore() != nil {
			return result, ErrUnavailable
		}
		return result, ErrConflict
	}
	if err := e.writeRecord(filepath.Join(e.registryRoot, reg.SiteKey+".json"), reg); err != nil {
		if restore() != nil {
			return result, ErrUnavailable
		}
		return result, err
	}
	result.Registered = true
	keepFiles = true
	return result, nil
}

func newSourceOutsideRoots(config []byte, source string) bool {
	var value any
	if json.Unmarshal(config, &value) != nil {
		return false
	}
	safe := true
	var walk func(any)
	walk = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if oneOf(stringValue(node["handler"]), "vars", "file_server") {
				if root, ok := node["root"].(string); ok && root != "{http.vars.root}" {
					if !filepath.IsAbs(root) || strings.Contains(root, "{") {
						safe = false
					} else {
						relative, err := filepath.Rel(root, source)
						if err != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
							safe = false
						}
					}
				}
			}
			for _, child := range node {
				walk(child)
			}
		case []any:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(value)
	return safe
}

func stringValue(value any) string { result, _ := value.(string); return result }

func registrationDelta(before, after []byte, names map[string]string) string {
	a, err := canonicalConfig(before, nil)
	if err != nil {
		return "invalid JSON"
	}
	b, err := canonicalConfig(after, names)
	if err != nil {
		return "invalid JSON"
	}
	var left, right any
	_ = json.Unmarshal(a, &left)
	_ = json.Unmarshal(b, &right)
	var paths []string
	var walk func(any, any, string)
	walk = func(a, b any, path string) {
		if len(paths) >= 8 || reflect.DeepEqual(a, b) {
			return
		}
		switch a := a.(type) {
		case map[string]any:
			b, ok := b.(map[string]any)
			if !ok {
				paths = append(paths, path)
				return
			}
			keys := map[string]bool{}
			for key := range a {
				keys[key] = true
			}
			for key := range b {
				keys[key] = true
			}
			var ordered []string
			for key := range keys {
				ordered = append(ordered, key)
			}
			sort.Strings(ordered)
			for _, key := range ordered {
				walk(a[key], b[key], path+"/"+key)
			}
		case []any:
			b, ok := b.([]any)
			if !ok || len(a) != len(b) {
				paths = append(paths, path+"/length")
				return
			}
			for index := range a {
				walk(a[index], b[index], path+"/"+strconv.Itoa(index))
			}
		default:
			paths = append(paths, path)
		}
	}
	walk(left, right, "")
	return strings.Join(paths, ",")
}

// QuotedImport is useful to operator tooling which installs a complete source
// file. It is never accepted as browser-supplied Caddy code.
func QuotedImport(path string) string { return "import " + strconv.Quote(path) + "\n" }
