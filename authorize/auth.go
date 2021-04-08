package authorize

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/jxsl13/oidc"
)

type Authorizer interface {
	// Returns nil if token gives authority for the user.
	IsAuthorized(ctx context.Context, token string) error
}

type authorizer struct {
	config Config

	client   *oidc.Client
	verifier *oidc.IDTokenVerifier
}

func New(ctx context.Context, config Config) (Authorizer, error) {
	client, err := oidc.NewClient(ctx, config.Provider)
	if err != nil {
		return nil, fmt.Errorf("Failed to create OIDC client agains %q provider. Err: %v", config.Provider, err)
	}

	return &authorizer{
		config: config,
		client: client,
		verifier: client.Verifier(oidc.VerificationConfig{
			ClientID: config.ClientID,
		}),
	}, nil
}

func (a *authorizer) IsAuthorized(ctx context.Context, token string) error {
	// Verify checks audience, sign algorithms, expiry and signature itself.
	idToken, err := a.verifier.Verify(ctx, token)
	if err != nil {
		return fmt.Errorf("Unauthenticated. Verification failed. Err: %v", err)
	}

	permsMap := map[string]interface{}{
		a.config.PermsClaim: nil,
	}
	err = idToken.Claims(&permsMap)
	if err != nil {
		// Should not happen.
		return err
	}

	perms, ok := permsMap[a.config.PermsClaim].([]interface{})
	if !ok {
		return fmt.Errorf("Wrong type of %q claim. Expected []interface{}. Got: %v",
			a.config.PermsClaim, reflect.TypeOf(permsMap[a.config.PermsClaim]))
	}

	var permissions []string
	for _, permission := range perms {
		permissionStr, ok := permission.(string)
		if !ok {
			return fmt.Errorf("Wrong type of permission inside %q claim. Expected string. Got: %v",
				a.config.PermsClaim, reflect.TypeOf(permission))
		}
		permissions = append(permissions, permissionStr)
	}

	if isAuthorized := a.config.PermCondition.isSatisfiedBy(permissions); isAuthorized {
		return nil
	}

	return fmt.Errorf("Unauthorized. User %q has permissions %v and needs to have permissions %s.", idToken.Subject, permissions, a.config.PermCondition.stringRepr)
}

func IsRequestAuthorized(req *http.Request, a Authorizer, headerName string) error {
	auth := strings.TrimSpace(req.Header.Get(headerName))
	if auth == "" {
		return fmt.Errorf("Unauthenticated. No %s header.", headerName)
	}
	parts := strings.Split(auth, " ")
	if len(parts) < 2 || strings.ToLower(parts[0]) != "bearer" {
		return fmt.Errorf("Unauthenticated. %s header does not have Bearer format.", headerName)
	}

	return a.IsAuthorized(req.Context(), parts[1])
}
