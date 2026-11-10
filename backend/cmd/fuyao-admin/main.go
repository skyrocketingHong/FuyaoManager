// Command fuyao-admin is the local management backend CLI: database
// migration, loopback API server, first-administrator bootstrap, and site
// creation.
package main

import (
	"os"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
