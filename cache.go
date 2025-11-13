package xblive

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TokenCache is an interface for managing cached authentication tokens
type TokenCache interface {
	GetAccessToken(ctx context.Context) (string, bool)
	GetRefreshToken(ctx context.Context) (string, bool)
	GetUserToken(ctx context.Context) (string, bool)
	GetXSTSToken(ctx context.Context) (token string, userHash string, ok bool)
	SetAccessToken(ctx context.Context, token string, notAfter time.Time) error
	SetRefreshToken(ctx context.Context, token string) error
	SetUserToken(ctx context.Context, token string, notAfter time.Time) error
	SetXSTSToken(ctx context.Context, token string, userHash string, notAfter time.Time) error
	Clear(ctx context.Context) error
}

// FileTokenCache is a file-based implementation of TokenCache
type FileTokenCache struct {
	filePath string
	tokens   *CachedTokens
}

// NewFileTokenCache creates a new file-based token cache in the default location (~/.xblive/tokens.json)
func NewFileTokenCache() (*FileTokenCache, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	cacheDir := filepath.Join(homeDir, ".xblive")
	filePath := filepath.Join(cacheDir, "tokens.json")
	return NewFileTokenCacheWithPath(filePath)
}

// NewFileTokenCacheWithPath creates a new file-based token cache at a custom path
func NewFileTokenCacheWithPath(filePath string) (*FileTokenCache, error) {
	cacheDir := filepath.Dir(filePath)
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	cache := &FileTokenCache{
		filePath: filePath,
		tokens:   &CachedTokens{},
	}

	// Try to load existing tokens
	_ = cache.load()

	return cache, nil
}

// load reads tokens from disk
func (c *FileTokenCache) load() error {
	data, err := os.ReadFile(c.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No cached tokens yet
		}
		return fmt.Errorf("failed to read token cache: %w", err)
	}

	if err := json.Unmarshal(data, c.tokens); err != nil {
		return fmt.Errorf("failed to parse token cache: %w", err)
	}

	return nil
}

// save writes tokens to disk
func (c *FileTokenCache) save() error {
	data, err := json.MarshalIndent(c.tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tokens: %w", err)
	}

	if err := os.WriteFile(c.filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write token cache: %w", err)
	}

	return nil
}

// GetAccessToken returns the cached access token if valid
func (c *FileTokenCache) GetAccessToken(ctx context.Context) (string, bool) {
	if c.tokens.AccessToken == "" {
		return "", false
	}
	if time.Now().After(c.tokens.AccessTokenExpiry) {
		return "", false
	}
	return c.tokens.AccessToken, true
}

// GetRefreshToken returns the cached refresh token
func (c *FileTokenCache) GetRefreshToken(ctx context.Context) (string, bool) {
	if c.tokens.RefreshToken == "" {
		return "", false
	}
	return c.tokens.RefreshToken, true
}

// GetUserToken returns the cached user token if valid
func (c *FileTokenCache) GetUserToken(ctx context.Context) (string, bool) {
	if c.tokens.UserToken == "" {
		return "", false
	}
	if time.Now().After(c.tokens.UserTokenExpiry) {
		return "", false
	}
	return c.tokens.UserToken, true
}

// GetXSTSToken returns the cached XSTS token and user hash if valid
func (c *FileTokenCache) GetXSTSToken(ctx context.Context) (token string, userHash string, ok bool) {
	if c.tokens.XSTSToken == "" || c.tokens.UserHash == "" {
		return "", "", false
	}
	if time.Now().After(c.tokens.XSTSTokenExpiry) {
		return "", "", false
	}
	return c.tokens.XSTSToken, c.tokens.UserHash, true
}

// SetAccessToken stores the access token
func (c *FileTokenCache) SetAccessToken(ctx context.Context, token string, notAfter time.Time) error {
	c.tokens.AccessToken = token
	c.tokens.AccessTokenExpiry = notAfter
	return c.save()
}

// SetRefreshToken stores the refresh token
func (c *FileTokenCache) SetRefreshToken(ctx context.Context, token string) error {
	c.tokens.RefreshToken = token
	return c.save()
}

// SetUserToken stores the user token
func (c *FileTokenCache) SetUserToken(ctx context.Context, token string, notAfter time.Time) error {
	c.tokens.UserToken = token
	c.tokens.UserTokenExpiry = notAfter
	return c.save()
}

// SetXSTSToken stores the XSTS token and user hash
func (c *FileTokenCache) SetXSTSToken(ctx context.Context, token string, userHash string, notAfter time.Time) error {
	c.tokens.XSTSToken = token
	c.tokens.UserHash = userHash
	c.tokens.XSTSTokenExpiry = notAfter
	return c.save()
}

// Clear removes all cached tokens
func (c *FileTokenCache) Clear(ctx context.Context) error {
	c.tokens = &CachedTokens{}
	if err := os.Remove(c.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove token cache: %w", err)
	}
	return nil
}
