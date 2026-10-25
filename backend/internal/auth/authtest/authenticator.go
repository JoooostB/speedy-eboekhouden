// Package authtest is a software passkey for tests: it answers the
// creation and request options of a WebAuthn ceremony the way a platform
// authenticator does, with a P-256 key and "none" attestation. Only tests
// import it, so it is never linked into the server.
package authtest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
)

// Authenticator holds one discoverable credential, like a passkey in a
// password manager.
type Authenticator struct {
	RPID   string
	Origin string

	key          *ecdsa.PrivateKey
	credentialID []byte
	// UserHandle is options.publicKey.user.id from the creation options; the
	// authenticator returns it with every assertion.
	UserHandle []byte
	signCount  uint32
}

// New returns an authenticator for the relying party rpID at origin.
func New(rpID, origin string) (*Authenticator, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	return &Authenticator{RPID: rpID, Origin: origin, key: key, credentialID: id}, nil
}

type creationOptions struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
		User      struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"publicKey"`
}

type requestOptions struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
	} `json:"publicKey"`
}

// Create answers navigator.credentials.create for the JSON options the
// server returned and remembers the user handle from them. It returns the
// body the browser posts to the finish endpoint.
func (a *Authenticator) Create(optionsJSON []byte) ([]byte, error) {
	var opts creationOptions
	if err := json.Unmarshal(optionsJSON, &opts); err != nil {
		return nil, fmt.Errorf("creation options: %w", err)
	}
	handle, err := base64.RawURLEncoding.DecodeString(opts.PublicKey.User.ID)
	if err != nil {
		return nil, fmt.Errorf("user id: %w", err)
	}
	a.UserHandle = handle

	pub, err := a.key.PublicKey.ECDH()
	if err != nil {
		return nil, err
	}
	point := pub.Bytes() // 0x04 || X || Y
	coseKey, err := webauthncbor.Marshal(map[int]any{
		1:  2,           // kty: EC2
		3:  -7,          // alg: ES256
		-1: 1,           // crv: P-256
		-2: point[1:33], // x
		-3: point[33:],  // y
	})
	if err != nil {
		return nil, err
	}

	attested := make([]byte, 16) // AAGUID, all zero
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(a.credentialID)))
	attested = append(attested, a.credentialID...)
	attested = append(attested, coseKey...)

	flags := protocol.FlagUserPresent | protocol.FlagUserVerified | protocol.FlagAttestedCredentialData
	authData := a.authData(flags, attested)

	clientData, err := a.clientData("webauthn.create", opts.PublicKey.Challenge)
	if err != nil {
		return nil, err
	}
	attestation, err := webauthncbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authData,
	})
	if err != nil {
		return nil, err
	}

	id := base64.RawURLEncoding.EncodeToString(a.credentialID)
	return json.Marshal(map[string]any{
		"id":    id,
		"rawId": id,
		"type":  "public-key",
		"response": map[string]any{
			"attestationObject": base64.RawURLEncoding.EncodeToString(attestation),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
		},
	})
}

// Get answers navigator.credentials.get for discoverable login options,
// returning the stored user handle like a real passkey does.
func (a *Authenticator) Get(optionsJSON []byte) ([]byte, error) {
	var opts requestOptions
	if err := json.Unmarshal(optionsJSON, &opts); err != nil {
		return nil, fmt.Errorf("request options: %w", err)
	}
	a.signCount++
	authData := a.authData(protocol.FlagUserPresent|protocol.FlagUserVerified, nil)
	clientData, err := a.clientData("webauthn.get", opts.PublicKey.Challenge)
	if err != nil {
		return nil, err
	}
	clientHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		return nil, err
	}

	id := base64.RawURLEncoding.EncodeToString(a.credentialID)
	return json.Marshal(map[string]any{
		"id":    id,
		"rawId": id,
		"type":  "public-key",
		"response": map[string]any{
			"authenticatorData": base64.RawURLEncoding.EncodeToString(authData),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
			"signature":         base64.RawURLEncoding.EncodeToString(sig),
			"userHandle":        base64.RawURLEncoding.EncodeToString(a.UserHandle),
		},
	})
}

func (a *Authenticator) authData(flags protocol.AuthenticatorFlags, attested []byte) []byte {
	rpHash := sha256.Sum256([]byte(a.RPID))
	data := append([]byte{}, rpHash[:]...)
	data = append(data, byte(flags))
	data = binary.BigEndian.AppendUint32(data, a.signCount)
	return append(data, attested...)
}

func (a *Authenticator) clientData(ceremony, challenge string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":        ceremony,
		"challenge":   challenge,
		"origin":      a.Origin,
		"crossOrigin": false,
	})
}
