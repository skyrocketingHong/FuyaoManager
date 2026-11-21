package platformupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

const archiveName = "fuyao-manager-linux-amd64.tar.gz"

type githubClient struct {
	repository string
	token      string
	base       string
	client     *http.Client
}

func newGitHub(repository, token string) *githubClient {
	return &githubClient{repository: repository, token: token, base: "https://api.github.com", client: &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) > 5 || r.URL.Scheme != "https" {
				return errors.New("invalid download redirect")
			}
			switch r.URL.Hostname() {
			case "github.com", "api.github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
			default:
				return errors.New("untrusted download host")
			}
			if r.URL.Hostname() != "api.github.com" {
				r.Header.Del("Authorization")
			}
			return nil
		},
	}}
}

// ASVS 13.2.1/13.3.1: credentials stay on the fixed GitHub API origin;
// downloaded artifacts are not allowed to select API or command destinations.
func (g *githubClient) get(ctx context.Context, path string, out any) error {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, g.base+"/repos/"+g.repository+path, nil)
	if err != nil {
		return err
	}
	r.Header.Set("Accept", "application/vnd.github+json")
	r.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.token != "" {
		r.Header.Set("Authorization", "Bearer "+g.token)
	}
	res, err := g.client.Do(r)
	if err != nil {
		return errors.New("GitHub connection failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("GitHub API HTTP %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out)
}

type workflowRun struct {
	ID         int64  `json:"id"`
	Attempt    int    `json:"run_attempt"`
	Commit     string `json:"head_sha"`
	Branch     string `json:"head_branch"`
	Event      string `json:"event"`
	Conclusion string `json:"conclusion"`
	Path       string `json:"path"`
	CreatedAt  string `json:"created_at"`
	URL        string `json:"html_url"`
	Repository struct {
		Name string `json:"full_name"`
	} `json:"head_repository"`
}

type githubRelease struct {
	Tag    string        `json:"tag_name"`
	Name   string        `json:"name"`
	Draft  bool          `json:"draft"`
	Assets []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

func (g *githubClient) versions(ctx context.Context, branch string) ([]Version, error) {
	var result struct {
		Runs []workflowRun `json:"workflow_runs"`
	}
	err := g.get(ctx, "/actions/workflows/ci.yml/runs?status=success&per_page=30&branch="+url.QueryEscape(branch), &result)
	if err != nil {
		return nil, err
	}
	versions := []Version{}
	for _, run := range result.Runs {
		if run.Branch != branch || run.Repository.Name != g.repository || run.Path != ".github/workflows/ci.yml" || run.Conclusion != "success" || (run.Event != "push" && run.Event != "workflow_dispatch") || !shaPattern.MatchString(run.Commit) {
			continue
		}
		versions = append(versions, Version{Commit: run.Commit, RunID: run.ID, Attempt: run.Attempt, Branch: branch, CreatedAt: run.CreatedAt, URL: run.URL})
	}
	if len(versions) == 0 {
		return versions, nil
	}
	var releases []githubRelease
	if err := g.get(ctx, "/releases?per_page=100", &releases); err != nil {
		return nil, err
	}
	names := make(map[string]string, len(releases))
	for _, release := range releases {
		if !release.Draft && slices.ContainsFunc(release.Assets, func(asset githubAsset) bool {
			return asset.Name == archiveName
		}) {
			names[release.Tag] = release.Name
		}
	}
	published := versions[:0]
	for _, version := range versions {
		name, released := names[releaseTag(version)]
		if !released {
			continue
		}
		label, matched := strings.CutSuffix(name, " · "+version.Branch+" · "+version.Commit[:8])
		if _, valid := productVersion(label); matched && valid {
			version.Version = label
		}
		published = append(published, version)
	}
	slices.SortStableFunc(published, func(a, b Version) int {
		return compareProductVersions(b.Version, a.Version)
	})
	return published, nil
}

func (g *githubClient) download(ctx context.Context, v Version, target string) error {
	var run workflowRun
	if err := g.get(ctx, fmt.Sprintf("/actions/runs/%d/attempts/%d", v.RunID, v.Attempt), &run); err != nil {
		return err
	}
	if run.ID != v.RunID || run.Attempt != v.Attempt || run.Commit != v.Commit || run.Branch != v.Branch || run.Repository.Name != g.repository || run.Path != ".github/workflows/ci.yml" || run.Conclusion != "success" || (run.Event != "push" && run.Event != "workflow_dispatch") {
		return errors.New("CI run provenance changed")
	}
	var release githubRelease
	if err := g.get(ctx, "/releases/tags/"+releaseTag(v), &release); err != nil {
		return err
	}
	if release.Draft || release.Tag != releaseTag(v) {
		return errors.New("invalid release identity")
	}
	for _, a := range release.Assets {
		if a.Name != archiveName {
			continue
		}
		expectedURL := "https://github.com/" + g.repository + "/releases/download/" + releaseTag(v) + "/" + archiveName
		if a.URL != expectedURL || a.Size < 1 || a.Size > 256<<20 || !strings.HasPrefix(a.Digest, "sha256:") || len(a.Digest) != 71 {
			return errors.New("release asset metadata invalid")
		}
		r, err := http.NewRequestWithContext(ctx, "GET", a.URL, nil)
		if err != nil {
			return err
		}
		res, err := g.client.Do(r)
		if err != nil {
			return errors.New("release download failed")
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return fmt.Errorf("release download HTTP %d", res.StatusCode)
		}
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, a.Size+1))
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if n != a.Size || hex.EncodeToString(h.Sum(nil)) != strings.TrimPrefix(a.Digest, "sha256:") {
			return errors.New("release SHA-256 mismatch")
		}
		return nil
	}
	return errors.New("release has no deployment archive")
}
