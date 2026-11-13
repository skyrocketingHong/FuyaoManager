package buildrunner

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
)

// previewStatusPath is the lifecycle probe the injected page script polls:
// 204 while the origin is live, 410 once closed, expired, unauthorized, or
// when the probe's instance id does not match this origin.
const previewStatusPath = "/__fuyao_preview__/status"

// previewInstanceParam carries the injecting origin's instance id on every
// probe. Each previewOrigin gets its own instance id, so a page opened
// before a close-and-reopen can never be revived: after the same build (or
// a reused port) reopens, the fresh cookie alone does not satisfy the old
// page's probe — the ids must match strictly, while the fresh instance's
// cookies stay untouched.
const previewInstanceParam = "instance"

// servePreviewHTML injects the lifecycle script into one served HTML
// document. The transform is response-only: frozen artifacts and their
// digests remain immutable, and non-HTML files keep their exact bytes.
// Every served HTML document gets a working stop mechanism — before
// </body>, before </html>, appended at the end, or as a streaming head for
// documents beyond the buffering cap.
func servePreviewHTML(w http.ResponseWriter, r *http.Request, info os.FileInfo, source io.ReadSeeker, instanceID string, parentOrigin ...string) {
	const maxDocumentSize = 8 << 20
	script := []byte(previewLifecycleScript(instanceID, parentOrigin...))
	if info.Size() > maxDocumentSize {
		streamLifecycleHead(w, source, script)
		return
	}
	data, err := io.ReadAll(io.LimitReader(source, maxDocumentSize+1))
	if err != nil || len(data) > maxDocumentSize {
		_, _ = source.Seek(0, io.SeekStart)
		streamLifecycleHead(w, source, script)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, info.Name(), info.ModTime(), bytes.NewReader(injectLifecycleScript(data, script)))
}

// streamLifecycleHead writes the stop script ahead of the unchanged frozen
// bytes, for documents that are not buffered whole.
func streamLifecycleHead(w http.ResponseWriter, source io.Reader, script []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(script); err != nil {
		return
	}
	_, _ = io.Copy(w, source)
}

// injectLifecycleScript inserts the stop script before </body>, before
// </html>, or — for documents with neither marker — at the very end, where
// HTML error recovery still executes it.
func injectLifecycleScript(data, script []byte) []byte {
	lower := bytes.ToLower(data)
	at := bytes.LastIndex(lower, []byte("</body>"))
	if at < 0 {
		at = bytes.LastIndex(lower, []byte("</html>"))
	}
	out := make([]byte, 0, len(data)+len(script))
	if at < 0 {
		out = append(out, data...)
		return append(out, script...)
	}
	out = append(out, data[:at]...)
	out = append(out, script...)
	return append(out, data[at:]...)
}

// previewLifecycleScript builds the stop script for one origin instance. It
// polls the status endpoint carrying the instance id; when the origin is
// gone, its token expired, or a later reopen replaced the instance, it
// navigates to a local blob URL — a fresh document that terminates the
// theme's timers, workers and in-flight requests even if the manager tab
// that opened the preview was reloaded. Polling pauses on pagehide (the
// page may enter the back/forward cache) and restarts on every pageshow,
// so a bfcache return resumes probing. Preview service workers are
// rejected at the origin, so no offline cache keeps a closed preview
// "running"; the probe decides liveness, never the service worker. Frozen
// artifacts stay untouched — the script exists only in served responses.
func previewLifecycleScript(instanceID string, parentOrigin ...string) string {
	parent := ""
	if len(parentOrigin) > 0 {
		parent = parentOrigin[0]
	}
	encodedParent, _ := json.Marshal(parent)
	return `<script data-fuyao-preview-lifecycle>
(()=>{let ended=false,failures=0,busy=false,timer=0;const inst='` + instanceID + `';const zh=navigator.language.toLowerCase().startsWith('zh');
const parentOrigin=` + string(encodedParent) + `;const publish=()=>{if(parentOrigin&&parent!==window)parent.postMessage({type:'fuyao-preview-route',instance:inst,path:location.pathname+location.search+location.hash},parentOrigin);};
if(parentOrigin){for(const name of ['pushState','replaceState']){const original=history[name];history[name]=function(...args){const result=Reflect.apply(original,this,args);publish();return result;};}addEventListener('popstate',publish);addEventListener('hashchange',publish);addEventListener('pageshow',publish);publish();}
const finish=(lost)=>{if(ended)return;ended=true;clearInterval(timer);timer=0;const title=zh?(lost?'预览连接已断开':'预览已关闭'):(lost?'Preview disconnected':'Preview closed');const detail=zh?'返回管理台可重新开启预览。':'Return to the manager to open another preview.';const page='<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Fuyao Manager - '+title+'</title><style>body{margin:0;min-height:100vh;display:grid;place-content:center;background:#f2f5fa;color:#1e293b;font:14px system-ui;text-align:center}h1{font-size:20px;font-weight:600}p{color:#526176}</style></head><body><h1>'+title+'</h1><p>'+detail+'</p></body></html>';location.replace(URL.createObjectURL(new Blob([page],{type:'text/html'})));};
const check=async()=>{if(ended||busy)return;busy=true;const controller=new AbortController();const timeout=setTimeout(()=>controller.abort(),2500);try{const r=await fetch('/__fuyao_preview__/status?'+new URLSearchParams({instance:inst}),{cache:'no-store',credentials:'same-origin',signal:controller.signal});if(r.status===204)failures=0;else if(r.status===410||r.status===401||r.status===404)finish(false);else if(++failures>=2)finish(true);}catch{if(++failures>=2)finish(true);}finally{clearTimeout(timeout);busy=false;}};
const start=()=>{if(timer)clearInterval(timer);timer=setInterval(check,1500);};const stop=()=>{clearInterval(timer);timer=0;};
addEventListener('pageshow',()=>{if(!ended){start();check();}});
addEventListener('pagehide',stop);
start();check();
})();</script>`
}
