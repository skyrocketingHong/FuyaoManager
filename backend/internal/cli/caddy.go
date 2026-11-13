package cli

import (
	"context"
	"flag"
	"fmt"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyinventory"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"os"
	"strings"
)

func caddyReadCommand() []string {
	var out []string
	for _, arg := range strings.Split(os.Getenv("FUYAO_ADMIN_CADDY_READ_CMD"), ",") {
		if arg = strings.TrimSpace(arg); arg != "" {
			out = append(out, arg)
		}
	}
	return out
}
func runSyncCaddy(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("sync-caddy", flag.ContinueOnError)
	username := flags.String("username", "", "Active administrator receiving ownership of newly discovered sites")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *username == "" {
		return fmt.Errorf("--username is required")
	}
	pool, _, err := openPool(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	st := store.New(pool)
	operator, err := st.Users.GetByUsername(ctx, *username)
	if err != nil {
		return err
	}
	items, err := caddyinventory.Read(ctx, caddyReadCommand())
	if err != nil {
		return err
	}
	count, err := st.SyncCaddySites(ctx, operator.ID, items)
	if err != nil {
		return err
	}
	fmt.Printf("synchronized %d Caddy sites\n", count)
	return nil
}
