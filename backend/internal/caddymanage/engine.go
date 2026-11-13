package caddymanage

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Runner interface {
	Adapt(context.Context, string) ([]byte, error)
	Validate(context.Context, string) error
	Reload(context.Context, string) error
	Active(context.Context) ([]byte, error)
}
type Engine struct {
	registryRoot string
	stateRoot    string
	runner       Runner
	ownerUID     uint32
	workGID      int
}

func NewEngine(registryRoot, stateRoot string, runner Runner, workGID int) *Engine {
	return &Engine{registryRoot: registryRoot, stateRoot: stateRoot, runner: runner, ownerUID: 0, workGID: workGID}
}

type currentState struct {
	SourceDigest   string   `json:"sourceDigest"`
	RootRevisionID string   `json:"rootRevisionId"`
	Settings       Settings `json:"settings"`
	Uncertain      bool     `json:"uncertain,omitempty"`
}
type rootRevision struct {
	ID           string
	SiteKey      string
	PolicyDigest string
	Source       []byte
	Settings     Settings
	Applied      bool
}
type validationPlan struct {
	ID                    string
	SiteKey               string
	PolicyDigest          string
	SourceDigest          string
	BaseConfigDigest      string
	CandidateConfigDigest string
	CandidateDigest       string
	Settings              Settings
	CreatedAt             time.Time
}
type policy struct {
	Registration
	source         []byte
	base           []byte
	master         []byte
	masterTemplate []byte
	digest         string
}

func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// Render substitutes only fixed directive strings into operator-owned slots.
// Preserve fragments are root trust data, never accepted from a draft.
func Render(reg Registration, settings Settings, template []byte) ([]byte, error) {
	settings, err := NormalizeSettings(settings)
	if err != nil {
		return nil, err
	}
	compression := ""
	switch settings.Compression {
	case "preserve":
		compression = reg.Preserve.Compression
	case "none":
	case "gzip":
		compression = "encode gzip\n"
	case "zstd":
		compression = "encode zstd\n"
	case "gzip_zstd":
		compression = "encode zstd gzip\n"
	}
	cache := reg.Preserve.CacheControl
	if settings.CacheControl == "no_store" {
		cache = "header Cache-Control \"no-store\"\n"
	}
	headers := reg.Preserve.SecurityHeaders
	if settings.SecurityHeaders == "baseline" {
		headers = reg.Preserve.SecurityHeaders + "\nheader {\n ?X-Content-Type-Options nosniff\n ?Referrer-Policy strict-origin-when-cross-origin\n}\n"
	}
	value := string(template)
	for _, slot := range []struct {
		token, choice, replacement string
		enabled                    bool
	}{
		{"__FUYAO_COMPRESSION__", settings.Compression, compression, reg.Capabilities.Compression},
		{"__FUYAO_CACHE_CONTROL__", settings.CacheControl, cache, reg.Capabilities.CacheControl},
		{"__FUYAO_SECURITY_HEADERS__", settings.SecurityHeaders, headers, reg.Capabilities.SecurityHeaders},
	} {
		count := strings.Count(value, slot.token)
		if count > 1 || (slot.enabled && count != 1) {
			return nil, ErrPermission
		}
		if !slot.enabled && slot.choice != "preserve" {
			return nil, ErrUnsupported
		}
		value = strings.ReplaceAll(value, slot.token, slot.replacement)
	}
	if strings.Contains(value, "__FUYAO_") {
		return nil, ErrPermission
	}
	return []byte(value), nil
}
func RenderMaster(template []byte, sourceFile string) ([]byte, error) {
	if strings.Count(string(template), SourceMarker) != 1 {
		return nil, ErrPermission
	}
	return []byte(strings.Replace(string(template), SourceMarker, strconv.Quote(sourceFile), 1)), nil
}

func (e *Engine) Run(ctx context.Context, request Request) (Result, error) {
	data, _ := json.Marshal(request)
	if _, err := DecodeRequest(bytes.NewReader(data)); err != nil {
		return Result{}, err
	}
	if uint32(os.Geteuid()) != e.ownerUID {
		return Result{}, ErrPermission
	}
	if err := e.ensureDir(e.stateRoot, 0710); err != nil {
		return Result{}, err
	}
	lock, err := e.lock(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() }()
	p, err := e.loadPolicy(request.SiteKey)
	if err != nil {
		return Result{}, err
	}
	state, err := e.loadState(request.SiteKey)
	if err != nil {
		return Result{}, err
	}
	result := Result{Registered: true, Adopted: state != nil, SourceDigest: Digest(p.source), Capabilities: p.Capabilities}
	result.Settings, _ = NormalizeSettings(Settings{})
	if state != nil {
		result.Settings = state.Settings
		result.RootRevisionID = state.RootRevisionID
		result.RuntimeUncertain = state.Uncertain
		result.Drifted = state.SourceDigest != result.SourceDigest || state.Uncertain
	}
	if request.Operation == "inspect" {
		if e.runner != nil && e.checkActive(ctx, p) != nil {
			result.RuntimeUncertain = true
			result.Drifted = true
		}
		return result, nil
	}
	if request.ExpectedDigest != result.SourceDigest {
		return Result{}, ErrConflict
	}
	if e.runner == nil {
		return Result{}, ErrUnavailable
	}
	switch request.Operation {
	case "adopt":
		if state != nil && !result.Drifted {
			if err := e.checkActive(ctx, p); err != nil {
				return Result{}, err
			}
			return result, nil
		}
		defaults, _ := NormalizeSettings(Settings{})
		rendered, err := Render(p.Registration, defaults, p.base)
		if err != nil {
			return Result{}, err
		}
		if Digest(p.source) != p.InitialDigest || !bytes.Equal(rendered, p.source) {
			return Result{}, ErrConflict
		}
		if e.runner.Validate(ctx, p.MasterFile) != nil {
			return Result{}, ErrValidation
		}
		if err := e.checkActive(ctx, p); err != nil {
			return Result{}, err
		}
		revision := rootRevision{ID: newID(), SiteKey: request.SiteKey, PolicyDigest: p.digest, Source: p.source, Settings: defaults, Applied: true}
		if err := e.saveRevision(revision); err != nil {
			return Result{}, err
		}
		if err := e.saveState(request.SiteKey, currentState{SourceDigest: result.SourceDigest, RootRevisionID: revision.ID, Settings: defaults}); err != nil {
			return Result{}, err
		}
		result.Adopted = true
		result.Settings = defaults
		result.RootRevisionID = revision.ID
		result.Drifted = false
		result.RuntimeUncertain = false
		return result, nil
	case "validate":
		if state == nil || result.Drifted {
			return Result{}, ErrConflict
		}
		settings, err := NormalizeSettings(request.Settings)
		if err != nil {
			return Result{}, err
		}
		candidate, err := Render(p.Registration, settings, p.base)
		if err != nil {
			return Result{}, err
		}
		plan, err := e.validate(ctx, p, request.SiteKey, candidate, settings)
		if err != nil {
			return Result{}, err
		}
		result.ValidationID = plan.ID
		result.CandidateDigest = plan.CandidateDigest
		result.Settings = settings
		return result, nil
	case "deploy":
		if state == nil || result.Drifted {
			return Result{}, ErrConflict
		}
		var plan validationPlan
		if err := e.readRecord(filepath.Join(e.stateRoot, "plans", request.ValidationID+".json"), &plan); err != nil {
			return Result{}, ErrConflict
		}
		if plan.SiteKey != request.SiteKey || plan.SourceDigest != result.SourceDigest || plan.PolicyDigest != p.digest || time.Since(plan.CreatedAt) > 30*time.Minute || time.Since(plan.CreatedAt) < 0 {
			return Result{}, ErrConflict
		}
		candidate, err := e.trustedFile(filepath.Join(e.stateRoot, "work", plan.ID, "site.caddy"))
		if err != nil {
			return Result{}, ErrConflict
		}
		if Digest(candidate) != plan.CandidateDigest {
			return Result{}, ErrConflict
		}
		return e.apply(ctx, p, request.SiteKey, *state, plan, candidate)
	case "rollback":
		if state == nil {
			return Result{}, ErrConflict
		}
		var revision rootRevision
		if err := e.readRecord(filepath.Join(e.stateRoot, "revisions", request.RevisionID+".json"), &revision); err != nil {
			return Result{}, ErrConflict
		}
		if revision.SiteKey != request.SiteKey || !revision.Applied || revision.PolicyDigest != p.digest {
			return Result{}, ErrConflict
		}
		candidate, err := Render(p.Registration, revision.Settings, p.base)
		if err != nil || !bytes.Equal(candidate, revision.Source) {
			return Result{}, ErrConflict
		}
		plan, err := e.validate(ctx, p, request.SiteKey, candidate, revision.Settings)
		if err != nil {
			return Result{}, err
		}
		return e.apply(ctx, p, request.SiteKey, *state, plan, candidate)
	}
	return Result{}, ErrInvalidRequest
}

func (e *Engine) loadPolicy(key string) (policy, error) {
	var p policy
	raw, err := e.trustedFile(filepath.Join(e.registryRoot, key+".json"))
	if os.IsNotExist(err) {
		return p, ErrNotRegistered
	}
	if err != nil {
		return p, err
	}
	if strictJSON(raw, &p.Registration) != nil || p.SiteKey != key || p.Kind != "static" || !digestPattern.MatchString(p.InitialDigest) {
		return p, ErrUnsupported
	}
	if !filepath.IsAbs(p.StaticRoot) {
		return p, ErrUnsupported
	}
	info, err := os.Stat(p.StaticRoot)
	if err != nil || !info.IsDir() {
		return p, ErrUnsupported
	}
	if p.SourceFile == p.MasterFile || filepath.Ext(p.SourceFile) != ".caddy" {
		return p, ErrPermission
	}
	if p.source, err = e.trustedFile(p.SourceFile); err != nil {
		return p, err
	}
	if p.base, err = e.trustedFile(p.BaseTemplate); err != nil {
		return p, err
	}
	if p.master, err = e.trustedFile(p.MasterFile); err != nil {
		return p, err
	}
	if p.masterTemplate, err = e.trustedFile(p.MasterTemplate); err != nil {
		return p, err
	}
	master, err := RenderMaster(p.masterTemplate, p.SourceFile)
	if err != nil || !bytes.Equal(master, p.master) {
		return p, ErrConflict
	}
	encoded, _ := json.Marshal(p.Registration)
	p.digest = Digest(append(append(encoded, p.base...), p.masterTemplate...))
	return p, nil
}

func (e *Engine) validate(ctx context.Context, p policy, key string, candidate []byte, settings Settings) (validationPlan, error) {
	plan := validationPlan{ID: newID(), SiteKey: key, PolicyDigest: p.digest, SourceDigest: Digest(p.source), CandidateDigest: Digest(candidate), Settings: settings, CreatedAt: time.Now().UTC()}
	before, err := e.runner.Adapt(ctx, p.MasterFile)
	if err != nil {
		return plan, ErrValidation
	}
	plan.BaseConfigDigest, err = canonicalConfigDigest(before, nil)
	if err != nil {
		return plan, ErrValidation
	}
	active, err := e.runner.Active(ctx)
	if err != nil {
		return plan, ErrUnavailable
	}
	activeDigest, err := canonicalConfigDigest(active, nil)
	if err != nil || activeDigest != plan.BaseConfigDigest {
		return plan, ErrConflict
	}
	work := filepath.Join(e.stateRoot, "work", plan.ID)
	if err := e.ensureDir(work, 0750); err != nil {
		return plan, err
	}
	source := filepath.Join(work, "site.caddy")
	master := filepath.Join(work, "master.caddy")
	if err := e.writeFile(source, candidate, 0640); err != nil {
		return plan, err
	}
	masterData, err := RenderMaster(p.masterTemplate, source)
	if err != nil {
		return plan, err
	}
	if err := e.writeFile(master, masterData, 0640); err != nil {
		return plan, err
	}
	if e.runner.Validate(ctx, master) != nil {
		return plan, ErrValidation
	}
	adapted, err := e.runner.Adapt(ctx, master)
	if err != nil {
		return plan, ErrValidation
	}
	plan.CandidateConfigDigest, err = canonicalConfigDigest(adapted, map[string]string{master: p.MasterFile, source: p.SourceFile})
	if err != nil {
		return plan, ErrValidation
	}
	if err := e.writeRecord(filepath.Join(e.stateRoot, "plans", plan.ID+".json"), plan); err != nil {
		return plan, err
	}
	return plan, nil
}

func (e *Engine) apply(ctx context.Context, p policy, key string, old currentState, plan validationPlan, candidate []byte) (Result, error) {
	live, err := e.runner.Adapt(ctx, p.MasterFile)
	if err != nil {
		return Result{}, ErrValidation
	}
	digest, err := canonicalConfigDigest(live, nil)
	if err != nil || digest != plan.BaseConfigDigest {
		return Result{}, ErrConflict
	}
	active, err := e.runner.Active(ctx)
	if err != nil {
		return Result{}, ErrUnavailable
	}
	activeDigest, err := canonicalConfigDigest(active, nil)
	if err != nil || activeDigest != plan.BaseConfigDigest {
		return Result{}, ErrConflict
	}
	current, err := e.trustedFile(p.SourceFile)
	if err != nil || Digest(current) != plan.SourceDigest {
		return Result{}, ErrConflict
	}
	revision := rootRevision{ID: newID(), SiteKey: key, PolicyDigest: p.digest, Source: candidate, Settings: plan.Settings}
	if err := e.saveRevision(revision); err != nil {
		return Result{}, err
	}
	if err := e.replaceSource(p.SourceFile, candidate); err != nil {
		return Result{}, err
	}
	restore := func() {
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if e.replaceSource(p.SourceFile, current) != nil || e.runner.Validate(recovery, p.MasterFile) != nil || e.runner.Reload(recovery, p.MasterFile) != nil || e.checkActive(recovery, p) != nil {
			old.Uncertain = true
			_ = e.saveState(key, old)
		}
	}
	if e.runner.Validate(ctx, p.MasterFile) != nil {
		restore()
		return Result{}, ErrValidation
	}
	adapted, err := e.runner.Adapt(ctx, p.MasterFile)
	if err != nil {
		restore()
		return Result{}, ErrValidation
	}
	digest, err = canonicalConfigDigest(adapted, nil)
	if err != nil || digest != plan.CandidateConfigDigest {
		restore()
		return Result{}, ErrConflict
	}
	if e.runner.Reload(ctx, p.MasterFile) != nil {
		restore()
		return Result{}, ErrUnavailable
	}
	active, err = e.runner.Active(ctx)
	if err != nil {
		restore()
		return Result{}, ErrUnavailable
	}
	activeDigest, err = canonicalConfigDigest(active, nil)
	if err != nil || activeDigest != plan.CandidateConfigDigest {
		restore()
		return Result{}, ErrConflict
	}
	revision.Applied = true
	if err := e.saveRevision(revision); err != nil {
		restore()
		return Result{}, err
	}
	state := currentState{SourceDigest: Digest(candidate), RootRevisionID: revision.ID, Settings: plan.Settings}
	if err := e.saveState(key, state); err != nil {
		restore()
		return Result{}, err
	}
	return Result{Registered: true, Adopted: true, SourceDigest: state.SourceDigest, CandidateDigest: plan.CandidateDigest, RootRevisionID: revision.ID, Settings: plan.Settings, Capabilities: p.Capabilities}, nil
}

func (e *Engine) checkActive(ctx context.Context, p policy) error {
	desired, err := e.runner.Adapt(ctx, p.MasterFile)
	if err != nil {
		return ErrUnavailable
	}
	active, err := e.runner.Active(ctx)
	if err != nil {
		return ErrUnavailable
	}
	a, err := canonicalConfigDigest(desired, nil)
	if err != nil {
		return ErrValidation
	}
	b, err := canonicalConfigDigest(active, nil)
	if err != nil || a != b {
		return ErrConflict
	}
	return nil
}

// Caddy automatically hides parsed Caddyfile filenames. Validation uses two
// private temporary filenames, so normalize only those exact hide entries;
// never globally replace arbitrary strings or compare a standalone site.
func canonicalConfigDigest(data []byte, names map[string]string) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return "", ErrValidation
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return "", ErrValidation
	}
	var walk func(any)
	walk = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if node["handler"] == "file_server" {
				if hide, ok := node["hide"].([]any); ok {
					for i, entry := range hide {
						if name, ok := entry.(string); ok {
							if replacement, exists := names[name]; exists {
								hide[i] = replacement
							}
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
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", ErrValidation
	}
	return Digest(encoded), nil
}
func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("random source unavailable")
	}
	return hex.EncodeToString(value[:])
}
