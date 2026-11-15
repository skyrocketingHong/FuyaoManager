package httpapi

import (
	"context"
	"net/url"
	"testing"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/db"
)

func TestDatabasePoolsUseUTC(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	dsn, err := url.Parse(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	query := dsn.Query()
	query.Set("timezone", "Asia/Shanghai")
	dsn.RawQuery = query.Encode()
	pool, err := db.OpenPool(ctx, dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var isolatedZone, defaultZone string
	if err := env.pool.QueryRow(ctx, "SHOW TIMEZONE").Scan(&isolatedZone); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SHOW TIMEZONE").Scan(&defaultZone); err != nil {
		t.Fatal(err)
	}
	if isolatedZone != "UTC" || defaultZone != "UTC" {
		t.Fatalf("timezones: isolated=%q default=%q", isolatedZone, defaultZone)
	}
}
