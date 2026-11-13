// fuyao-caddy-register is invoked directly by the root operator. Do not grant
// it to the service user through sudo: registration grants a new capability.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddymanage"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type mediaMappings map[string]string

func (m *mediaMappings) String() string { return "" }
func (m *mediaMappings) Set(value string) error {
	key, path, ok := strings.Cut(value, "=")
	if !ok || (*m)[key] != "" {
		return caddymanage.ErrInvalidRequest
	}
	(*m)[key] = path
	return nil
}

func run() error {
	if os.Geteuid() != 0 {
		return caddymanage.ErrPermission
	}
	flags := flag.NewFlagSet("fuyao-caddy-register", flag.ContinueOnError)
	key := flags.String("site-key", "", "Caddy inventory source key")
	apply := flags.Bool("apply", false, "Install the equivalent split source and registration")
	master := flags.String("master", "/etc/caddy/Caddyfile", "Root-owned master Caddyfile")
	sourceRoot := flags.String("source-root", "/etc/caddy/fuyao-managed", "Root-owned source directory")
	templates := flags.String("templates", "/etc/fuyao-caddy-control/templates", "Root-owned template directory")
	siteID := flags.String("site-id", "", "Optional platform site UUID for theme publication")
	themes := flags.String("themes", "", "Comma-separated theme IDs permitted for publication")
	releases := flags.String("releases-root", "/srv/fuyao-admin/data/releases", "Operator-bound platform release root")
	builds := flags.String("builds-root", "/srv/fuyao-admin/data/builds", "Operator-bound frozen build root")
	account := flags.String("source-user", "fuyao-admin", "Owner of frozen build and release inputs")
	configure := flags.Bool("configure-publication", false, "Add finite theme publication to an existing registration")
	media := mediaMappings{}
	flags.Var(&media, "media-map", "Operator-reviewed mediaRelative=existingRoot; may repeat")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *key == "" || flags.NArg() != 0 {
		return caddymanage.ErrInvalidRequest
	}
	options := caddymanage.OperatorRegistration{SiteKey: *key, MasterFile: *master, SourceRoot: *sourceRoot, TemplateRoot: *templates, Apply: *apply}
	options.MediaRoots = media
	if *siteID != "" || *themes != "" {
		user, err := user.Lookup(*account)
		if err != nil {
			return caddymanage.ErrPermission
		}
		uid, err := strconv.ParseUint(user.Uid, 10, 32)
		if err != nil {
			return caddymanage.ErrPermission
		}
		options.Publication = &caddymanage.Publication{SiteID: *siteID, ReleasesRoot: *releases, BuildsRoot: *builds, SourceUID: uint32(uid), AllowedThemes: strings.Split(*themes, ",")}
	} else if len(media) != 0 {
		return caddymanage.ErrInvalidRequest
	}
	runner, err := caddymanage.NewSystemRunner()
	if err != nil {
		return err
	}
	engine := caddymanage.NewEngine(caddymanage.RegistryRoot, caddymanage.StateRoot, runner, runner.GID)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var result caddymanage.RegistrationResult
	if *configure {
		if options.Publication == nil {
			return caddymanage.ErrInvalidRequest
		}
		result, err = engine.ConfigurePublication(ctx, *key, *options.Publication, media, *apply)
	} else {
		result, err = engine.RegisterStatic(ctx, options)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
