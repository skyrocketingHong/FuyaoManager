package httpapi

import (
	"errors"
	"net/http"
)

func (a *API) sessionCookieName() string {
	if a.cfg.CookieSecure {
		return secureSessionCookieName
	}
	return sessionCookieName
}
func (a *API) authTxCookieName() string {
	if a.cfg.CookieSecure {
		return secureAuthTxCookieName
	}
	return authTxCookieName
}
func (a *API) authTxCookiePath() string {
	if a.cfg.CookieSecure {
		return "/"
	}
	return authTxCookiePath
}

// __Host- prohibits parent-domain/path cookie tossing in HTTPS deployments.
// Do not accept old names or select the first of ambiguous same-name cookies.
func uniqueCookie(r *http.Request, name string) (*http.Cookie, error) {
	values := r.CookiesNamed(name)
	if len(values) != 1 {
		return nil, errors.New("missing or ambiguous authentication cookie")
	}
	return values[0], nil
}
