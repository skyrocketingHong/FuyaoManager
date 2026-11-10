// Package testauth provides a synthetic WebAuthn authenticator used by the
// integration tests. It implements the client side of the registration and
// assertion ceremonies exactly as a browser platform authenticator would:
// ES256 keys, "none" attestation, and UP/UV flags. It is a test client, not
// a server-side bypass; the backend still verifies every signature.
package testauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
)

// Authenticator is one synthetic credential source with a stable private key.
type Authenticator struct {
	key       *ecdsa.PrivateKey
	credID    []byte
	signCount uint32
}

// New generates a fresh ES256 credential.
func New() *Authenticator {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic("testauth: key generation failed: " + err.Error())
	}
	credID := make([]byte, 32)
	if _, err := rand.Read(credID); err != nil {
		panic("testauth: random failed: " + err.Error())
	}
	return &Authenticator{key: key, credID: credID}
}

// clientData is the CollectedClientData JSON.
type clientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin"`
}

// CreationResponse mirrors the PublicKeyCredential JSON shape produced by
// navigator.credentials.create() and expected by the backend's verify route.
type CreationResponse struct {
	ID        string         `json:"id"`
	RawID     string         `json:"rawId"`
	Type      string         `json:"type"`
	Response  CreationBody   `json:"response"`
	ClientExt map[string]any `json:"clientExtensionResults"`
}

// CreationBody carries the attestation response members.
type CreationBody struct {
	ClientDataJSON    string   `json:"clientDataJSON"`
	AttestationObject string   `json:"attestationObject"`
	Transports        []string `json:"transports"`
}

// AssertionResponse mirrors the PublicKeyCredential JSON shape produced by
// navigator.credentials.get().
type AssertionResponse struct {
	ID        string         `json:"id"`
	RawID     string         `json:"rawId"`
	Type      string         `json:"type"`
	Response  AssertionBody  `json:"response"`
	ClientExt map[string]any `json:"clientExtensionResults"`
}

// AssertionBody carries the assertion response members.
type AssertionBody struct {
	ClientDataJSON    string `json:"clientDataJSON"`
	AuthenticatorData string `json:"authenticatorData"`
	Signature         string `json:"signature"`
	UserHandle        string `json:"userHandle"`
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func marshalClientData(ceremonyType, challenge, origin string) string {
	body, _ := json.Marshal(clientData{Type: ceremonyType, Challenge: challenge, Origin: origin})
	return string(body)
}

func paddedCoordinate(v []byte) []byte {
	out := make([]byte, 32)
	copy(out[32-len(v):], v)
	return out
}

func coseKey(pub *ecdsa.PublicKey) []byte {
	key, err := cbor.Marshal(map[int]any{
		1:  2,                               // kty: EC2
		3:  -7,                              // alg: ES256
		-1: 1,                               // crv: P-256
		-2: paddedCoordinate(pub.X.Bytes()), // x
		-3: paddedCoordinate(pub.Y.Bytes()), // y
	})
	if err != nil {
		panic("testauth: cose encoding failed: " + err.Error())
	}
	return key
}

func rpIDHash(rpid string) []byte {
	sum := sha256.Sum256([]byte(rpid))
	return sum[:]
}

func signCountBE(count uint32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], count)
	return buf[:]
}

// CreateCredential builds a registration response for the given relying
// party, origin, and base64url challenge. AAGUID is all zeroes and the
// attestation format is "none", which matches the backend's attestation
// preference.
func (a *Authenticator) CreateCredential(rpid, origin, challenge string) *CreationResponse {
	clientJSON := marshalClientData("webauthn.create", challenge, origin)

	authData := make([]byte, 0, 37+2+len(a.credID)+91)
	authData = append(authData, rpIDHash(rpid)...)
	authData = append(authData, 0x45) // UP | UV | AT (attested credential data follows)
	authData = append(authData, signCountBE(0)...)
	authData = append(authData, make([]byte, 16)...) // zero AAGUID
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(a.credID)))
	authData = append(authData, lenBuf[:]...)
	authData = append(authData, a.credID...)
	authData = append(authData, coseKey(&a.key.PublicKey)...)

	attObj, err := cbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authData,
	})
	if err != nil {
		panic("testauth: attestation encoding failed: " + err.Error())
	}

	cred := b64url(a.credID)
	return &CreationResponse{
		ID:    cred,
		RawID: cred,
		Type:  "public-key",
		Response: CreationBody{
			ClientDataJSON:    b64url([]byte(clientJSON)),
			AttestationObject: b64url(attObj),
			Transports:        []string{"internal"},
		},
		ClientExt: map[string]any{},
	}
}

// Assert builds a discoverable-login assertion response for the user handle.
// The signature counter advances on every call so replayed counters do not
// trigger clone warnings.
func (a *Authenticator) Assert(rpid, origin, challenge string, userHandle []byte) *AssertionResponse {
	a.signCount++
	clientJSON := marshalClientData("webauthn.get", challenge, origin)

	authData := make([]byte, 0, 37)
	authData = append(authData, rpIDHash(rpid)...)
	authData = append(authData, 0x05) // UP | UV
	authData = append(authData, signCountBE(a.signCount)...)

	signed := append(append([]byte{}, authData...), sha256Sum([]byte(clientJSON))...)
	digest := sha256.Sum256(signed)
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		panic("testauth: signing failed: " + err.Error())
	}

	cred := b64url(a.credID)
	return &AssertionResponse{
		ID:    cred,
		RawID: cred,
		Type:  "public-key",
		Response: AssertionBody{
			ClientDataJSON:    b64url([]byte(clientJSON)),
			AuthenticatorData: b64url(authData),
			Signature:         b64url(sig),
			UserHandle:        b64url(userHandle),
		},
		ClientExt: map[string]any{},
	}
}

// ExportCredential returns the credential ID and raw COSE public key bytes
// for direct persistence in upgrade-scenario tests.
func (a *Authenticator) ExportCredential() (id []byte, publicKey []byte) {
	return a.credID, coseKey(&a.key.PublicKey)
}

func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}
