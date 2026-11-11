package httpapi

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRouteContractCoverage asserts the Go route table and the OpenAPI
// contract describe exactly the same operation set: every registered route
// has a path+method entry with a matching operationId in
// contracts/openapi/management.yaml, and the contract declares no operation
// the server does not register. Generating types and linting the file is not
// enough — an implemented route missing from the contract (or vice versa)
// fails here.
func TestRouteContractCoverage(t *testing.T) {
	contractOps := parseOpenAPIOperations(t, filepath.Join(repoRoot(t),
		"contracts", "openapi", "management.yaml"))

	routes := (&API{}).routes()
	if len(routes) == 0 {
		t.Fatal("route table is empty")
	}
	seen := map[string]bool{}
	for _, route := range routes {
		key := route.Method + " " + route.Pattern
		op, ok := contractOps[key]
		if !ok {
			t.Errorf("route %s has no OpenAPI operation", key)
			continue
		}
		if op != route.OperationID {
			t.Errorf("route %s declares operationId %q but the contract says %q",
				key, route.OperationID, op)
		}
		if seen[key] {
			t.Errorf("route %s registered more than once", key)
		}
		seen[key] = true
	}
	for key := range contractOps {
		if !seen[key] {
			t.Errorf("OpenAPI operation %s has no registered route", key)
		}
	}
}

// parseOpenAPIOperations extracts the "METHOD path -> operationId" mapping
// from the contract with a small line-state scan: path keys sit at two-space
// indentation, HTTP verbs at four, operationId at six. The file's formatting
// is enforced by redocly lint in CI, so the indentation contract is stable.
func parseOpenAPIOperations(t *testing.T, path string) map[string]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open contract: %v", err)
	}
	defer file.Close()

	ops := map[string]string{}
	currentPath := ""
	currentMethod := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "  /"):
			currentPath = strings.TrimSuffix(strings.TrimPrefix(line, "  "), ":")
			currentMethod = ""
		case strings.HasPrefix(line, "    get:"),
			strings.HasPrefix(line, "    post:"),
			strings.HasPrefix(line, "    put:"),
			strings.HasPrefix(line, "    patch:"),
			strings.HasPrefix(line, "    delete:"):
			currentMethod = strings.TrimSuffix(strings.TrimSpace(line), ":")
		case strings.HasPrefix(line, "      operationId:"):
			if currentPath == "" || currentMethod == "" {
				t.Fatalf("operationId outside a path operation at %q", line)
			}
			id := strings.TrimSpace(strings.TrimPrefix(line, "      operationId:"))
			ops[strings.ToUpper(currentMethod)+" "+currentPath] = id
		case strings.HasPrefix(line, "components:"):
			return ops
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan contract: %v", err)
	}
	return ops
}
