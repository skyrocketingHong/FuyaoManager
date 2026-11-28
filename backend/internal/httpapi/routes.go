package httpapi

import (
	"net/http"
	"slices"
)

// routeSpec is one registered management route with the OpenAPI operation it
// implements. The table is the single Go-side source of truth: registration
// loops over it and contract tests compare it against
// contracts/openapi/openapi.yaml.
type routeSpec struct {
	Method      string
	Pattern     string
	OperationID string
	Handler     http.HandlerFunc
}

// routes assembles the domain registries for the router and contract test.
func (a *API) routes() []routeSpec {
	return slices.Concat(
		a.routesPlatform(),
		a.routesAccounts(),
		a.routesSites(),
		a.routesSiteContent(),
		a.routesPublishing(),
		a.platformContentRoutes(),
	)
}
