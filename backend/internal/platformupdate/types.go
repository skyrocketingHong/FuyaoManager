// Package platformupdate adapts robot-platform's independent update manager,
// checked candidates, pinned channels and verified rollback to GitHub releases
// and FuyaoManager's systemd installation. The application never installs files.
package platformupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var shaPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type Config struct {
	Repository     string `json:"repository"`
	Root           string `json:"root"`
	Socket         string `json:"socket"`
	Group          string `json:"group"`
	HealthURL      string `json:"healthUrl"`
	PrepareCommand string `json:"prepareCommand"`
}

func (c Config) Validate() error {
	if !repositoryPattern.MatchString(c.Repository) || c.Repository == "../.." {
		return errors.New("invalid GitHub repository")
	}
	if !filepath.IsAbs(c.Root) || !filepath.IsAbs(c.Socket) || !filepath.IsAbs(c.PrepareCommand) || c.Group == "" {
		return errors.New("root, socket, group and prepare command must be configured")
	}
	if c.HealthURL != "http://127.0.0.1:8787/api/v1/version" {
		return errors.New("health URL must be the fixed local application endpoint")
	}
	return nil
}

type Version struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	RunID     int64  `json:"runId"`
	Attempt   int    `json:"attempt"`
	Branch    string `json:"branch"`
	CreatedAt string `json:"createdAt"`
	URL       string `json:"url"`
}

type Manifest struct {
	Version
	Format         int               `json:"format"`
	Repository     string            `json:"repository"`
	Architecture   string            `json:"architecture"`
	DatabaseSchema int               `json:"databaseSchema"`
	Files          map[string]string `json:"files"`
}

type Action struct {
	Action  string  `json:"action"`
	RunID   int64   `json:"runId,omitempty"`
	Commit  string  `json:"commit,omitempty"`
	Channel string  `json:"channel,omitempty"`
	On      *bool   `json:"on,omitempty"`
	Token   *string `json:"token,omitempty"`
}

type Task struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Phase      string `json:"phase"`
	Commit     string `json:"commit"`
	Previous   string `json:"-"`
	Candidate  string `json:"-"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt,omitempty"`
	Error      string `json:"error,omitempty"`
}

type Status struct {
	Enabled       bool      `json:"enabled"`
	Repository    string    `json:"repository"`
	Channel       string    `json:"channel"`
	Auto          bool      `json:"auto"`
	Pinned        bool      `json:"pinned"`
	Checking      bool      `json:"checking"`
	Updating      bool      `json:"updating"`
	TokenSet      bool      `json:"tokenSet"`
	ManagerCommit string    `json:"managerCommit"`
	Current       Version   `json:"current"`
	Previous      *Version  `json:"previous"`
	Versions      []Version `json:"versions"`
	Task          *Task     `json:"task"`
	LastCheck     string    `json:"lastCheck"`
	LastError     string    `json:"lastError"`
	Logs          []string  `json:"logs"`
}

type diskState struct {
	Channel       string   `json:"channel"`
	Auto          bool     `json:"auto"`
	Pinned        bool     `json:"pinned"`
	Previous      string   `json:"previous"`
	Task          *Task    `json:"task"`
	TaskPrevious  string   `json:"taskPrevious"`
	TaskCandidate string   `json:"taskCandidate"`
	LastCheck     string   `json:"lastCheck"`
	LastError     string   `json:"lastError"`
	Logs          []string `json:"logs"`
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func readManifest(dir string) (Manifest, error) {
	var m Manifest
	raw, err := os.ReadFile(filepath.Join(dir, "release.json"))
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(raw, &m)
	return m, err
}

func atomicJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func releaseTag(v Version) string { return fmt.Sprintf("build-%d-%d", v.RunID, v.Attempt) }
