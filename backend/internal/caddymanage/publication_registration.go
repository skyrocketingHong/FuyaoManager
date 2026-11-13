package caddymanage

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
)

// ConfigurePublication grants publication to an already registered site. It
// changes only root-owned policy/template bytes and must reproduce the
// existing source exactly; it never reloads or changes the served directory.
func (e *Engine) ConfigurePublication(ctx context.Context, key string, publication Publication, mediaRoots map[string]string, apply bool) (RegistrationResult, error) {
	result := RegistrationResult{SiteKey: key}
	if uint32(os.Geteuid()) != e.ownerUID || !keyPattern.MatchString(key) || e.runner == nil {
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
	p, err := e.loadPolicy(key)
	if err != nil {
		return result, err
	}
	if p.Publication != nil {
		return result, ErrConflict
	}
	if err := e.checkActive(ctx, p); err != nil {
		return result, err
	}
	base, pub, err := AttachPublication(p.base, p.StaticRoot, publication, mediaRoots)
	if err != nil {
		return result, err
	}
	reg := p.Registration
	reg.Publication = pub
	settings, _ := NormalizeSettings(Settings{})
	if state, err := e.loadState(key); err != nil {
		return result, err
	} else if state != nil {
		if state.Uncertain || state.SourceDigest != Digest(p.source) {
			return result, ErrConflict
		}
		settings = state.Settings
	}
	rendered, err := Render(reg, settings, base)
	if err != nil || !bytes.Equal(rendered, p.source) {
		return result, ErrConflict
	}
	result.SourceDigest = Digest(p.source)
	if !apply {
		return result, nil
	}
	// A new path makes the registry update the commit point. A crash before
	// that atomic rename leaves the old template and policy fully intact.
	basePath := filepath.Join(filepath.Dir(reg.BaseTemplate), key+".publication-"+newID()+".caddy")
	if err := e.writeFile(basePath, base, 0600); err != nil {
		return result, err
	}
	reg.BaseTemplate = basePath
	encoded, err := json.Marshal(reg)
	if err != nil {
		return result, ErrUnavailable
	}
	if err := e.replaceSource(filepath.Join(e.registryRoot, key+".json"), append(encoded, '\n')); err != nil {
		return result, err
	}
	result.Registered = true
	return result, nil
}
