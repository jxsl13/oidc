package disk

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/Bplotka/oidc"
)

// DefaultTokenCachePath is default path for OIDC tokens.
const DefaultTokenCachePath = "$HOME/.oidc_keys"

// TokenCache is a oidc Token caching structure that stores all tokens on disk.
// Tokens cache files are named after clientID and arg[0].
// NOTE: There is no logic for cleaning cache in case of change in clientID.
type TokenCache struct {
	storePath string
	clientID  string
}

// NewTokenCache constructs disk cache.
func NewTokenCache(clientID string, path string) *TokenCache {
	return &TokenCache{storePath: os.ExpandEnv(path), clientID: clientID}
}

func (c *TokenCache) getOrCreateStoreDir() (string, error) {
	err := os.MkdirAll(c.storePath, os.ModeDir|0700)
	return c.storePath, err
}

func (c *TokenCache) tokenCacheFileName() string {
	cliToolName := filepath.Base(os.Args[0])
	return fmt.Sprintf("token_%s_%s", cliToolName, c.clientID)
}

// Token retrieves token from file.
func (c *TokenCache) Token() (*oidc.Token, error) {
	storeDir, err := c.getOrCreateStoreDir()
	if err != nil {
		return nil, fmt.Errorf("Failed to create store dir. Err: %v", err)
	}

	bytes, err := ioutil.ReadFile(filepath.Join(storeDir, c.tokenCacheFileName()))
	if os.IsNotExist(err) {
		// Probably a no such file err.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Failed to get cached token code. Err: %v", err)
	}
	token := &oidc.Token{}
	if err := json.Unmarshal(bytes, token); err != nil {
		return nil, fmt.Errorf("Failed to unmarshal token JSON. Err: %v", err)
	}

	return token, nil
}

// SetToken saves token in file.
func (c *TokenCache) SetToken(token *oidc.Token) error {
	storeDir, err := c.getOrCreateStoreDir()
	if err != nil {
		return err
	}

	marshaledToken, err := json.Marshal(token)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(filepath.Join(storeDir, c.tokenCacheFileName()), marshaledToken, 0600)
	if err != nil {
		return fmt.Errorf("Failed caching access token. Err: %v", err)
	}

	return nil
}
