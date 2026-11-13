package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/buildrunner"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

type previewWrapperIssuer interface {
	WrapperEnabled() bool
	WrapperHostname() string
	IssueWrapper(uuid.UUID, uuid.UUID, string) (string, time.Time, error)
	LookupWrapper(string, string) (buildrunner.PreviewLease, error)
	FrameWrapper(string, string, string) (buildrunner.PreviewFrame, error)
}

func (a *API) previewHost(ctx context.Context, siteID, userID uuid.UUID) (string, error) {
	site, err := a.store.Sites.GetForUser(ctx, siteID, userID)
	if err != nil {
		return "", err
	}
	entry, err := a.store.GetCaddySite(ctx, siteID, userID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	if err == nil && entry.Present {
		var definition struct {
			Hosts []string `json:"hosts"`
		}
		if json.Unmarshal(entry.Definition, &definition) == nil {
			for _, host := range definition.Hosts {
				host = strings.ToLower(host)
				if buildrunner.ValidPreviewHost(host) {
					return host, nil
				}
			}
		}
	}
	return site.Site.Slug, nil
}

func (a *API) previewHostRoutes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrappers, ok := a.previews.(previewWrapperIssuer)
		if ok && wrappers.WrapperEnabled() && previewRequestHost(r) == wrappers.WrapperHostname() && wrappers.WrapperHostname() != "" {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "method not allowed", 405)
				return
			}
			if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			a.handlePreviewWrapper(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func previewRequestHost(r *http.Request) string {
	if strings.ContainsAny(r.Host, "/\\@?# \t\r\n") {
		return ""
	}
	parsed, err := url.Parse("http://" + r.Host)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func (a *API) handlePreviewWrapper(w http.ResponseWriter, r *http.Request) {
	wrappers, ok := a.previews.(previewWrapperIssuer)
	if !ok || !wrappers.WrapperEnabled() {
		http.NotFound(w, r)
		return
	}
	if previewRequestHost(r) == "" || previewRequestHost(r) != wrappers.WrapperHostname() {
		http.NotFound(w, r)
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	host := parts[0]
	leaseToken := r.URL.Query().Get("lease")
	_, err := wrappers.LookupWrapper(leaseToken, host)
	if err != nil {
		writeProblem(w, 410, "preview_closed", "This preview lease is closed or expired. Open a new preview explicitly.")
		return
	}
	if r.URL.Query().Get("__fuyao_status") == "1" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	next := "/"
	if len(parts) == 2 {
		next += parts[1]
	}
	query := r.URL.Query()
	query.Del("lease")
	query.Del("__fuyao_status")
	if len(query) > 0 {
		next += "?" + query.Encode()
	}
	frame, err := wrappers.FrameWrapper(leaseToken, host, next)
	if err != nil {
		writeProblem(w, 410, "preview_closed", "This preview lease is closed or expired.")
		return
	}
	nonce, err := buildrunner.NewPreviewToken()
	if err != nil {
		writeProblem(w, 500, "internal_error", "Preview wrapper could not be rendered.")
		return
	}
	probe := "/" + host + "/?" + url.Values{"lease": {leaseToken}, "__fuyao_status": {"1"}}.Encode()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'unsafe-inline'; frame-src "+frame.Origin+"; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	if err := previewWrapperTemplate.Execute(w, struct{ Host, Nonce, Origin, Entry, Probe, Instance, Prefix string }{host, nonce, frame.Origin, frame.EntryURL, probe, frame.InstanceID, "/" + host}); err != nil {
		a.logger.Error("render preview wrapper", "error", err)
	}
}

var previewWrapperTemplate = template.Must(template.New("preview-wrapper").Parse(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Fuyao Manager - {{.Host}}</title><style>html,body,iframe{margin:0;width:100%;height:100%;border:0}body{background:#f2f5fa;font:14px system-ui;color:#1e293b}p{padding:24px;text-align:center}</style></head><body><iframe id="preview" title="{{.Host}}" sandbox="allow-scripts allow-same-origin"></iframe><script nonce="{{.Nonce}}">
(()=>{const frame=document.getElementById('preview'),origin={{.Origin}},instance={{.Instance}},prefix={{.Prefix}},probe={{.Probe}};let done=false,busy=false,failures=0,timer=0;
const finish=()=>{if(done)return;done=true;clearInterval(timer);frame.remove();const p=document.createElement('p');p.textContent=navigator.language.startsWith('zh')?'预览已关闭，请返回管理台重新开启。':'Preview closed. Return to the manager to open another preview.';document.body.append(p);};
const check=async()=>{if(done||busy)return;busy=true;const c=new AbortController(),timeout=setTimeout(()=>c.abort(),2500);try{const r=await fetch(probe,{credentials:'same-origin',cache:'no-store',signal:c.signal});if(r.status===204)failures=0;else if([401,403,404,410].includes(r.status))finish();else if(++failures>=2)finish();}catch{if(++failures>=2)finish();}finally{clearTimeout(timeout);busy=false;}};
addEventListener('message',event=>{if(done||event.source!==frame.contentWindow||event.origin!==origin||event.data?.type!=='fuyao-preview-route'||event.data?.instance!==instance||typeof event.data.path!=='string')return;try{const u=new URL(event.data.path,origin);if(u.origin!==origin||!u.pathname.startsWith('/')||u.pathname.startsWith('//')||event.data.path.length>4096)return;const own=new URL(location.href),q=new URLSearchParams(u.search);q.delete('__fuyao_status');q.set('lease',own.searchParams.get('lease'));history.replaceState(null,'',prefix+u.pathname+'?'+q+u.hash);}catch{}});
const start=()=>{clearInterval(timer);timer=setInterval(check,1500);check();};addEventListener('pagehide',()=>clearInterval(timer));addEventListener('pageshow',start);
const entry=new URL({{.Entry}});entry.hash=location.hash;frame.src=entry.href;start();
})();</script></body></html>`))
