package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/contentmigration"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

// This operator command alone accepts filesystem inputs. It never changes
// the original website or publishes; existing destinations are not replaced.
func runImportContent(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("import-content", flag.ContinueOnError)
	sourceConfig := flags.String("source-config", "", "Existing website configuration directory")
	sourceContent := flags.String("source-content", "", "Existing website content directory")
	sourceFavicon := flags.String("source-favicon", "", "Existing favicon directory")
	platformRoot := flags.String("platform-root", "", "Independent platform content root")
	sitesRoot := flags.String("sites-root", "", "Independent managed sites root")
	siteValue := flags.String("site-id", "", "Existing website receiving an independent configuration copy")
	owner := flags.String("owner", "", "Service account owning newly imported files")
	if err := flags.Parse(args); err != nil {
		return err
	}
	var id uuid.UUID
	var err error
	if *siteValue != "" {
		id, err = uuid.Parse(*siteValue)
		if err != nil {
			return fmt.Errorf("invalid site id")
		}
		pool, _, err := openPool(ctx)
		if err != nil {
			return err
		}
		defer pool.Close()
		var exists bool
		if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sites WHERE id=$1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("site does not exist")
		}
	}
	uid, gid := -1, -1
	if *owner != "" {
		if os.Geteuid() != 0 {
			return fmt.Errorf("--owner requires an administrator execution")
		}
		account, err := user.Lookup(*owner)
		if err != nil {
			return err
		}
		uid, err = strconv.Atoi(account.Uid)
		if err != nil {
			return err
		}
		gid, err = strconv.Atoi(account.Gid)
		if err != nil {
			return err
		}
	}
	report, err := contentmigration.Import(ctx, contentmigration.Options{SourceConfig: *sourceConfig, SourceContent: *sourceContent, SourceFavicon: *sourceFavicon, PlatformRoot: *platformRoot, SitesRoot: *sitesRoot, SiteID: id})
	if err != nil {
		return err
	}
	if uid >= 0 {
		for _, target := range report.Targets {
			if err = os.Chown(filepath.Dir(target.Path), uid, gid); err != nil {
				return err
			}
			if err = filepath.WalkDir(target.Path, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("imported scope contains a link")
				}
				return os.Chown(path, uid, gid)
			}); err != nil {
				return err
			}
		}
	}
	if id != uuid.Nil {
		files, err := content.New(*sitesRoot)
		if err != nil {
			return err
		}
		doc, _, _, err := files.GetSiteConfig(id)
		if err != nil {
			return err
		}
		name, description, locale := strings.TrimSpace(doc.Site.Name), doc.SEO.Description, doc.Site.DefaultLocale
		if name == "" || (locale != "zh-CN" && locale != "en-US") {
			return fmt.Errorf("imported website metadata is invalid")
		}
		pool, _, err := openPool(ctx)
		if err != nil {
			return err
		}
		defer pool.Close()
		var revision int64
		if err = pool.QueryRow(ctx, `SELECT revision FROM site_settings WHERE site_id=$1`, id).Scan(&revision); err != nil {
			return err
		}
		if _, err = store.New(pool).Sites.UpdateSettings(ctx, id, revision, &name, &description, &locale); err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
