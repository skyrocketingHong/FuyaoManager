package buildrunner

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var frozenMediaPrefixes = []struct{ url, relative string }{
	{"/images/", "images"},
	{"/wp-content/", "wp-content"},
	{"/media/albums/photos/", "albums/photos"},
	{"/media/albums/thumbnails/", "albums/thumbnails"},
}

func frozenMediaPath(value string) bool {
	for _, prefix := range frozenMediaPrefixes {
		if strings.HasPrefix(value, prefix.url) || value == strings.TrimSuffix(prefix.url, "/") {
			return true
		}
	}
	return false
}

func (o *previewOrigin) serveFrozenMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	root, err := o.frozenMediaRoot()
	if err != nil || root == "" {
		http.NotFound(w, r)
		return
	}
	for _, prefix := range frozenMediaPrefixes {
		if strings.HasPrefix(r.URL.Path, prefix.url) {
			servePreviewMediaFile(w, r, filepath.Join(root, filepath.FromSlash(prefix.relative)), strings.TrimPrefix(r.URL.Path, prefix.url), false)
			return
		}
	}
	http.NotFound(w, r)
}

func servePreviewMediaFile(w http.ResponseWriter, r *http.Request, root, rel string, imageOnly bool) {
	if root == "" || rel == "" {
		http.NotFound(w, r)
		return
	}
	name := path.Clean("/" + rel)
	if name == "/" || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	rel = strings.TrimPrefix(name, "/")
	if !fs.ValidPath(rel) {
		http.NotFound(w, r)
		return
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	target := filepath.Join(resolvedRoot, filepath.FromSlash(rel))
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if resolved != resolvedRoot && !strings.HasPrefix(resolved, resolvedRoot+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(resolved)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	if imageOnly {
		switch strings.ToLower(filepath.Ext(resolved)) {
		case ".jpg", ".jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		case ".webp":
			w.Header().Set("Content-Type", "image/webp")
		case ".gif":
			w.Header().Set("Content-Type", "image/gif")
		default:
			http.NotFound(w, r)
			return
		}
	}
	// *os.File implements io.ReadSeeker, giving range support for images.
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}
