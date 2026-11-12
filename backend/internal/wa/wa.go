// Package wa bridges the persisted user and passkey records with the
// go-webauthn library: it builds the Relying Party configuration, adapts
// model records to the webauthn.User interface, and converts credentials in
// both directions.
package wa

import (
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// Params configures the WebAuthn relying party. Ceremonies require
// discoverable credentials (passkeys) with user verification.
type Params struct {
	RPID          string
	RPDisplayName string
	RPOrigins     []string
	ChallengeTTL  time.Duration
}

// New builds the library instance. The server-side session expiry is enforced
// by the library in addition to the auth_states rows.
func New(params Params) (*webauthnlib.WebAuthn, error) {
	requireResident := true
	return webauthnlib.New(&webauthnlib.Config{
		RPID:          params.RPID,
		RPDisplayName: params.RPDisplayName,
		RPOrigins:     params.RPOrigins,
		Timeouts: webauthnlib.TimeoutsConfig{
			Login:        timeoutConfig(params.ChallengeTTL),
			Registration: timeoutConfig(params.ChallengeTTL),
		},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			RequireResidentKey: &requireResident,
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   protocol.VerificationRequired,
		},
		AttestationPreference: protocol.PreferNoAttestation,
	})
}

func timeoutConfig(ttl time.Duration) webauthnlib.TimeoutConfig {
	return webauthnlib.TimeoutConfig{
		Enforce:    true,
		Timeout:    ttl,
		TimeoutUVD: ttl,
	}
}

// User adapts model.User plus its stored credentials to the webauthn.User
// interface.
type User struct {
	Profile     model.User
	Credentials []webauthnlib.Credential
}

// WebAuthnID returns the stable random user handle.
func (u *User) WebAuthnID() []byte { return u.Profile.WebAuthnUserHandle }

// WebAuthnName returns the account name.
func (u *User) WebAuthnName() string { return u.Profile.Username }

// WebAuthnDisplayName returns the display name, falling back to the username.
func (u *User) WebAuthnDisplayName() string {
	if u.Profile.DisplayName != "" {
		return u.Profile.DisplayName
	}
	return u.Profile.Username
}

// WebAuthnCredentials returns every stored credential of the user; the
// discoverable-login flow matches the presented credential against this list.
func (u *User) WebAuthnCredentials() []webauthnlib.Credential { return u.Credentials }

// ToCredential converts a stored passkey into the library representation.
func ToCredential(pk model.Passkey) webauthnlib.Credential {
	transports := make([]protocol.AuthenticatorTransport, 0, len(pk.Transports))
	for _, t := range pk.Transports {
		transports = append(transports, protocol.AuthenticatorTransport(t))
	}
	return webauthnlib.Credential{
		ID:                pk.CredentialID,
		PublicKey:         pk.PublicKey,
		AttestationType:   pk.AttestationType,
		AttestationFormat: pk.AttestationFormat,
		Transport:         transports,
		Flags: webauthnlib.CredentialFlags{
			UserPresent:    true,
			UserVerified:   pk.UserVerified,
			BackupEligible: pk.BackupEligible,
			BackupState:    pk.BackupState,
		},
		Authenticator: webauthnlib.Authenticator{
			AAGUID:    pk.AAGUID,
			SignCount: pk.SignCount,
		},
	}
}

// FromCredential converts a freshly verified registration credential into the
// persistence model.
func FromCredential(userID uuid.UUID, c *webauthnlib.Credential) model.Passkey {
	transports := make([]string, 0, len(c.Transport))
	for _, t := range c.Transport {
		transports = append(transports, string(t))
	}
	return model.Passkey{
		UserID:            userID,
		CredentialID:      c.ID,
		PublicKey:         c.PublicKey,
		AttestationType:   c.AttestationType,
		AttestationFormat: c.AttestationFormat,
		Transports:        transports,
		SignCount:         c.Authenticator.SignCount,
		BackupEligible:    c.Flags.BackupEligible,
		BackupState:       c.Flags.BackupState,
		UserVerified:      c.Flags.UserVerified,
		AAGUID:            c.Authenticator.AAGUID,
	}
}
