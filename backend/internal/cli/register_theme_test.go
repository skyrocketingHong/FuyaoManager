package cli

import (
	"context"
	"strings"
	"testing"
)

// TestRegisterThemeFlagValidation covers the argument contract without
// touching a database: the contracts directory and theme root are required.
func TestRegisterThemeFlagValidation(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no flags", []string{}, "--contracts is required"},
		{"no theme root", []string{"--contracts", "/tmp/contracts"}, "usage:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runRegisterTheme(context.Background(), tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v, want it to contain %q", err, tc.want)
			}
		})
	}
}
