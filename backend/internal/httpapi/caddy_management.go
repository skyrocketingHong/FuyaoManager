package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyconfig"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddymanage"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

type caddyCapabilities struct {
	Convert  bool `json:"convert"`
	Edit     bool `json:"edit"`
	Validate bool `json:"validate"`
	Deploy   bool `json:"deploy"`
}
type caddyState struct {
	store.CaddySite
	ManagementMode       string                   `json:"managementMode"`
	Management           *store.CaddyManagement   `json:"management"`
	Capabilities         caddyCapabilities        `json:"capabilities"`
	SettingsCapabilities caddymanage.Capabilities `json:"settingsCapabilities"`
	UnavailableReason    string                   `json:"unavailableReason,omitempty"`
}

func caddyStatic(c store.CaddySite) bool {
	var def caddyconfig.Site
	return c.Kind == "static" && json.Unmarshal(c.Definition, &def) == nil && def.Root != "" && !def.RootHasPlaceholder && !caddymanage.ProtectedEntry(def)
}

func caddyProtected(c store.CaddySite) bool {
	var def caddyconfig.Site
	return json.Unmarshal(c.Definition, &def) == nil && caddymanage.ProtectedEntry(def)
}

func (a *API) writeCaddyState(w http.ResponseWriter, r *http.Request, auth store.AuthSession, id uuid.UUID) {
	c, err := a.store.GetCaddySite(r.Context(), id, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	m, err := a.store.GetCaddyManagement(r.Context(), id)
	if err != nil {
		a.caddyError(w, r, err)
		return
	}
	state := caddyState{CaddySite: c, ManagementMode: "caddy", Management: m}
	if m != nil {
		state.ManagementMode = "platform"
	}
	site, err := a.store.Sites.GetForUser(r.Context(), id, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	owner := auth.User.PlatformRole == model.PlatformRoleAdmin && site.Role == model.RoleOwner
	switch {
	case caddyProtected(c):
		state.UnavailableReason = "protected_control_plane"
	case !caddyStatic(c):
		state.UnavailableReason = "site_kind_unsupported"
	case !c.Present:
		state.UnavailableReason = "source_missing"
	case a.cfg.CaddyControl == nil:
		state.UnavailableReason = "operator_registration_required"
	default:
		result, readErr := a.cfg.CaddyControl(r.Context(), caddymanage.Request{Operation: "inspect", SiteKey: c.SourceKey})
		if readErr != nil {
			state.UnavailableReason = caddyErrorCode(readErr)
		} else {
			state.SettingsCapabilities = result.Capabilities
			state.Capabilities.Convert = owner && m == nil && result.Registered
			state.Capabilities.Edit = owner && m != nil && result.Registered
			state.Capabilities.Validate = state.Capabilities.Edit
			state.Capabilities.Deploy = state.Capabilities.Edit && a.cfg.CaddyManagementEnabled
			if result.RuntimeUncertain {
				state.UnavailableReason = "caddy_runtime_uncertain"
				state.Capabilities.Convert = false
				state.Capabilities.Validate = false
				state.Capabilities.Deploy = false
			} else if result.Drifted || (m != nil && (m.SourceDigest != result.SourceDigest || m.RootRevisionID != result.RootRevisionID)) {
				state.UnavailableReason = "source_config_changed"
				state.Capabilities.Convert = false
				state.Capabilities.Validate = false
				state.Capabilities.Deploy = false
			} else if m != nil && !a.cfg.CaddyManagementEnabled {
				state.UnavailableReason = "caddy_deployment_disabled"
			}
		}
	}
	w.Header().Set("ETag", store.CaddyETag(id, m))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, state)
}

func caddyErrorCode(err error) string {
	switch {
	case errors.Is(err, caddymanage.ErrNotRegistered):
		return "operator_registration_required"
	case errors.Is(err, caddymanage.ErrConflict):
		return "source_config_changed"
	case errors.Is(err, caddymanage.ErrValidation):
		return "caddy_validation_failed"
	case errors.Is(err, caddymanage.ErrPermission):
		return "caddy_capability_denied"
	case errors.Is(err, caddymanage.ErrUnsupported):
		return "site_kind_unsupported"
	case errors.Is(err, caddymanage.ErrInvalidRequest):
		return "validation_error"
	default:
		return "caddy_unavailable"
	}
}
func (a *API) caddyError(w http.ResponseWriter, r *http.Request, err error) {
	code := caddyErrorCode(err)
	status := 503
	switch {
	case errors.Is(err, store.ErrPreconditionFailed):
		status = 412
		code = "precondition_failed"
	case errors.Is(err, store.ErrNotFound):
		status = 404
		code = "not_found"
	case errors.Is(err, store.ErrOperatorForbidden):
		status = 403
		code = "forbidden"
	case errors.Is(err, store.ErrConflict):
		status = 409
		code = "caddy_state_conflict"
	case errors.Is(err, caddymanage.ErrConflict):
		status = 409
	case errors.Is(err, caddymanage.ErrValidation), errors.Is(err, caddymanage.ErrInvalidRequest):
		status = 422
	case errors.Is(err, caddymanage.ErrUnsupported):
		status = 409
	}
	writeProblem(w, status, code, "The Caddy operation could not be completed. Refresh the configuration and inspect its management status.")
}

func (a *API) handleCaddyConvert(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	a.mutateCaddy(w, r, auth, "convertCaddySite")
}
func (a *API) handleCaddyDraft(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	a.mutateCaddy(w, r, auth, "saveCaddyDraft")
}
func (a *API) handleCaddyValidate(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	a.mutateCaddy(w, r, auth, "validateCaddyDraft")
}
func (a *API) handleCaddyDeploy(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	a.mutateCaddy(w, r, auth, "deployCaddyDraft")
}
func (a *API) handleCaddyRollback(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	a.mutateCaddy(w, r, auth, "rollbackCaddyConfiguration")
}

func (a *API) mutateCaddy(w http.ResponseWriter, r *http.Request, auth store.AuthSession, operation string) {
	id, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if r.Header.Get("If-Match") == "" {
		writeProblem(w, 428, "precondition_required", "An exact If-Match ETag is required.")
		return
	}
	var input struct {
		Settings *caddymanage.Settings `json:"settings,omitempty"`
		Revision *int64                `json:"revision,omitempty"`
	}
	if !decodeJSON(w, r, &input, true) {
		return
	}
	if (operation == "saveCaddyDraft") != (input.Settings != nil) || (operation == "rollbackCaddyConfiguration") != (input.Revision != nil) {
		writeProblem(w, 422, "validation_error", "Supply only the fields required by this operation.")
		return
	}
	if a.cfg.CaddyControl == nil {
		writeProblem(w, 503, "caddy_unavailable", "The fixed Caddy management capability is not configured.")
		return
	}
	if (operation == "deployCaddyDraft" || operation == "rollbackCaddyConfiguration") && !a.cfg.CaddyManagementEnabled {
		writeProblem(w, 503, "caddy_deployment_disabled", "Source configuration deployment is disabled.")
		return
	}
	_, err := a.store.MutateCaddyManagement(r.Context(), auth.User.ID, id, r.Header.Get("If-Match"), operation,
		func(c store.CaddySite, current *store.CaddyManagement) (*store.CaddyManagement, string, error) {
			if !caddyStatic(c) {
				return nil, "", caddymanage.ErrUnsupported
			}
			request := caddymanage.Request{SiteKey: c.SourceKey}
			if current != nil {
				request.ExpectedDigest = current.SourceDigest
			}
			status := "draft"
			if operation == "convertCaddySite" {
				if current != nil {
					return nil, "", store.ErrConflict
				}
				request.Operation = "inspect"
				observed, err := a.cfg.CaddyControl(r.Context(), request)
				if err != nil {
					return nil, "", err
				}
				if observed.RuntimeUncertain || observed.Drifted {
					return nil, "", caddymanage.ErrConflict
				}
				request.ExpectedDigest = observed.SourceDigest
				request.Operation = "adopt"
				result, err := a.cfg.CaddyControl(r.Context(), request)
				if err != nil {
					return nil, "", err
				}
				settings, _ := json.Marshal(result.Settings)
				revision := int64(1)
				return &store.CaddyManagement{Revision: revision, AppliedRevision: &revision, SourceDigest: result.SourceDigest, Settings: settings, RootRevisionID: result.RootRevisionID}, "applied", nil
			}
			if current == nil {
				return nil, "", store.ErrConflict
			}
			observed, err := a.cfg.CaddyControl(r.Context(), caddymanage.Request{Operation: "inspect", SiteKey: c.SourceKey})
			if err != nil {
				return nil, "", err
			}
			if observed.SourceDigest != current.SourceDigest || observed.RootRevisionID != current.RootRevisionID || observed.Drifted || observed.RuntimeUncertain {
				return nil, "", caddymanage.ErrConflict
			}
			next := *current
			switch operation {
			case "saveCaddyDraft":
				settings := *input.Settings
				if !validCaddySettings(settings) {
					return nil, "", caddymanage.ErrInvalidRequest
				}
				if (!observed.Capabilities.Compression && settings.Compression != "preserve") || (!observed.Capabilities.CacheControl && settings.CacheControl != "preserve") || (!observed.Capabilities.SecurityHeaders && settings.SecurityHeaders != "preserve") {
					return nil, "", caddymanage.ErrUnsupported
				}
				next.Settings, _ = json.Marshal(settings)
				next.Revision++
				next.ValidatedRevision = nil
				next.ValidationID = ""
			case "validateCaddyDraft":
				request.Operation = "validate"
				if json.Unmarshal(next.Settings, &request.Settings) != nil {
					return nil, "", store.ErrConflict
				}
				result, err := a.cfg.CaddyControl(r.Context(), request)
				if err != nil {
					return nil, "", err
				}
				revision := next.Revision
				next.ValidatedRevision = &revision
				next.ValidationID = result.ValidationID
				status = "validated"
			case "deployCaddyDraft":
				if next.ValidatedRevision == nil || *next.ValidatedRevision != next.Revision || next.ValidationID == "" {
					return nil, "", store.ErrConflict
				}
				request.Operation = "deploy"
				request.ValidationID = next.ValidationID
				result, err := a.cfg.CaddyControl(r.Context(), request)
				if err != nil {
					return nil, "", err
				}
				revision := next.Revision
				next.AppliedRevision = &revision
				next.SourceDigest = result.SourceDigest
				next.RootRevisionID = result.RootRevisionID
				next.ValidationID = ""
				status = "applied"
			case "rollbackCaddyConfiguration":
				var target *store.CaddyRevision
				for i := range next.History {
					if next.History[i].Revision == *input.Revision && next.History[i].Status == "applied" {
						target = &next.History[i]
						break
					}
				}
				if target == nil || target.RootRevisionID == "" {
					return nil, "", store.ErrConflict
				}
				request.Operation = "rollback"
				request.RevisionID = target.RootRevisionID
				result, err := a.cfg.CaddyControl(r.Context(), request)
				if err != nil {
					return nil, "", err
				}
				next.Revision++
				revision := next.Revision
				next.AppliedRevision = &revision
				next.ValidatedRevision = &revision
				next.Settings = target.Settings
				next.SourceDigest = result.SourceDigest
				next.RootRevisionID = result.RootRevisionID
				next.ValidationID = ""
				status = "applied"
			}
			return &next, status, nil
		})
	if err != nil {
		a.caddyError(w, r, err)
		return
	}
	if operation == "convertCaddySite" && a.provisioner != nil {
		site, readErr := a.store.Sites.GetForUser(r.Context(), id, auth.User.ID)
		if readErr != nil {
			writeSiteLookupError(w, readErr)
			return
		}
		host, readErr := a.previewHost(r.Context(), id, auth.User.ID)
		if readErr != nil {
			writeSiteLookupError(w, readErr)
			return
		}
		if provisionErr := a.provisioner.Provision(site.Site, site.Settings, "https://"+host); provisionErr != nil {
			writeProblem(w, 503, "provision_failed", "The configuration is adopted but its content workspace could not be initialized.")
			return
		}
	}
	a.writeCaddyState(w, r, auth, id)
}

func validCaddySettings(s caddymanage.Settings) bool {
	compression := s.Compression == "preserve" || s.Compression == "none" || s.Compression == "gzip" || s.Compression == "zstd" || s.Compression == "gzip_zstd"
	return compression && (s.CacheControl == "preserve" || s.CacheControl == "no_store") && (s.SecurityHeaders == "preserve" || s.SecurityHeaders == "baseline")
}
