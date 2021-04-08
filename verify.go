package oidc

// This file was heavily inspired by github.com/coreos/go-oidc/verify.go

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jxsl13/oidc/xerrors"
	jose "gopkg.in/square/go-jose.v2"
)

const (
	issuerGoogleAccounts         = "https://accounts.google.com"
	issuerGoogleAccountsNoScheme = "accounts.google.com"
)

//go:generate mockery -name Verifier -case underscore

// Verifier is anything that can verify token and returned parsed standard oidc.NewIDToken.
// For example oidc.IDTokenVerifier.
type Verifier interface {
	Verify(ctx context.Context, rawIDToken string) (*IDToken, error)
}

// IDTokenVerifier provides verification for ID Tokens.
type IDTokenVerifier struct {
	keySet keySet
	cfg    VerificationConfig
	issuer string
}

// VerificationConfig is the configuration for an IDTokenVerifier.
type VerificationConfig struct {
	// Expected Audience of the token. For a majority of the cases this is expected to be
	// the ID of the client that initialized the login flow. It may occasionally differ if
	// the provider supports the authorizing party (azp) claim.
	//
	// If not provided, users must explicitly set SkipClientIDCheck.
	ClientID string

	// ClaimNonce for Verification.
	ClaimNonce string

	// If specified, only this set of algorithms may be used to sign the JWT.
	//
	// Since many providers only support RS256, SupportedSigningAlgs defaults to this value.
	SupportedSigningAlgs []string

	// Time function to check Token expiry. Defaults to time.Now
	Now func() time.Time
}

func newVerifier(keySet keySet, cfg VerificationConfig, issuer string) *IDTokenVerifier {
	// If SupportedSigningAlgs is empty defaults to only support RS256.
	if len(cfg.SupportedSigningAlgs) == 0 {
		cfg.SupportedSigningAlgs = []string{string(jose.RS256)}
	}

	return &IDTokenVerifier{
		keySet: keySet,
		cfg:    cfg,
		issuer: issuer,
	}
}

func parseJWT(p string) ([]byte, error) {
	parts := strings.Split(p, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("oidc: malformed jwt, expected 3 parts got %d", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("oidc: malformed jwt payload: %v", err)
	}
	return payload, nil
}

func contains(sli []string, ele string) bool {
	for _, s := range sli {
		if s == ele {
			return true
		}
	}
	return false
}

// Verify parses a raw ID Token, verifies it's been signed by the provider, preforms
// any additional checks depending on the Config, and returns the payload.
//
// See: https://openid.net/specs/openid-connect-core-1_0.html#IDTokenValidation
//
//    oidcToken, err := client.Exchange(ctx, r.URL.Query().Get("code"))
//    if err != nil {
//        // handle error
//    }
//
//    token, err := verifier.Verify(ctx, oidcToken.IDToken)
//
func (v *IDTokenVerifier) Verify(ctx context.Context, rawIDToken string) (*IDToken, error) {
	jws, err := jose.ParseSigned(rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("oidc: malformed jwt: %v", err)
	}

	// Throw out tokens with invalid claims before trying to verify the token. This lets
	// us do cheap checks before possibly re-syncing keys.
	payload, err := parseJWT(rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("oidc: malformed jwt: %v", err)
	}
	var token IDToken
	if err := json.Unmarshal(payload, &token); err != nil {
		return nil, fmt.Errorf("oidc: failed to unmarshal claims: %v", err)
	}

	token.claims = payload

	// Check issuer.
	if token.Issuer != v.issuer {
		// Google sometimes returns "accounts.google.com" as the issuer claim instead of
		// the required "https://accounts.google.com". Detect this case and allow it only
		// for Google.
		//
		// We will not add hooks to let other providers go off spec like this.
		if !(v.issuer == issuerGoogleAccounts && token.Issuer == issuerGoogleAccountsNoScheme) {
			return nil, fmt.Errorf("oidc: id token issued by a different provider, expected %q got %q", v.issuer, token.Issuer)
		}
	}

	// This check DOES NOT ensure that the ClientID is the party to which the ID Token was issued (i.e. Authorized party).
	if v.cfg.ClientID != "" {
		if !contains(token.Audience, v.cfg.ClientID) {
			return nil, fmt.Errorf("oidc: expected Audience %q got %q", v.cfg.ClientID, token.Audience)
		}
	} else {
		return nil, fmt.Errorf("oidc: Invalid configuration. ClientID must be provided")
	}

	now := time.Now
	if v.cfg.Now != nil {
		now = v.cfg.Now
	}

	if token.Expiry.Time().Before(now()) {
		return nil, fmt.Errorf("oidc: token is expired (Token Expiry: %v)", token.Expiry.Time())
	}

	// If a set of required algorithms/keys has been provided, ensure that the signature verify will use those.
	keyIDs := make(map[string]struct{})
	var gotAlgsForErrLog []string
	for _, sig := range jws.Signatures {
		if len(v.cfg.SupportedSigningAlgs) == 0 || contains(v.cfg.SupportedSigningAlgs, sig.Header.Algorithm) {
			keyIDs[sig.Header.KeyID] = struct{}{}
		} else {
			gotAlgsForErrLog = append(gotAlgsForErrLog, sig.Header.Algorithm)
		}
	}
	if len(keyIDs) == 0 {
		return nil, fmt.Errorf("oidc: no signatures use a supported algorithm, expected %q got %q", v.cfg.SupportedSigningAlgs, gotAlgsForErrLog)
	}

	// Get keys from the remote key set. This will always trigger a re-sync.
	allKeys, err := v.keySet.Keys(ctx)
	if err != nil {
		return nil, fmt.Errorf("oidc: get keys for id token: %v", err)
	}

	var keys []jose.JSONWebKey
	for _, k := range allKeys {
		if _, ok := keyIDs[k.KeyID]; !ok {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("oidc: no keys match signature ID(s) %v. Got keys: %v", keyIDs, allKeys)
	}

	// Try to use a key to validate the signature.
	var gotPayload []byte
	xerr := xerrors.New()
	for _, key := range keys {
		p, err := jws.Verify(&key)
		if err != nil {
			xerr.Add(err)
			continue
		}
		gotPayload = p
		break
	}
	if len(gotPayload) == 0 {
		return nil, fmt.Errorf("oidc: failed to verify id token. Err: %v", xerr.ErrorOrNil())
	}

	// Ensure that the payload returned by the square actually matches the payload parsed earlier.
	if !bytes.Equal(gotPayload, payload) {
		return nil, errors.New("oidc: internal error, payload parsed did not match previous payload")
	}

	// Check the nonce after we've verified the token. We don'token want to allow unverified
	// payloads to trigger a nonce lookup.
	if v.cfg.ClaimNonce != "" {
		if token.Nonce != v.cfg.ClaimNonce {
			return nil, fmt.Errorf("oidc: Invalid configuration. ClaimNonce must match. Got %s, expected %s",
				token.Nonce, v.cfg.ClaimNonce)
		}
	}

	return &token, nil
}
