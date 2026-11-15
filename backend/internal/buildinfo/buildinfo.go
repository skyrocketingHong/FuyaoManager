// Package buildinfo identifies the exact application and CI inputs in service.
package buildinfo

var (
	Commit  = "development"
	BuiltAt = ""
)

type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"builtAt"`
}

func Current() Info { return Info{Version, Commit, BuiltAt} }
