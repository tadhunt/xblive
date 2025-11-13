package xblive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// API endpoints
	searchEndpoint = "https://peoplehub.xboxlive.com/users/me/people/search/decoration/detail,preferredColor"
)

// Config contains configuration for the Xbox Live client
type Config struct {
	// ClientID is your Microsoft Entra ID application client ID (required)
	ClientID string

	// Cache is the token cache implementation to use (optional)
	// If nil, defaults to file-based cache at ~/.xblive/tokens.json
	Cache TokenCache
}

// Client is the main Xbox Live API client
type Client struct {
	clientID   string
	httpClient *http.Client
	cache      TokenCache
}

// New creates a new Xbox Live client
func New(config Config) (*Client, error) {
	if config.ClientID == "" {
		return nil, fmt.Errorf("client ID is required")
	}

	// Use provided cache or default to file cache
	cache := config.Cache
	if cache == nil {
		var err error
		cache, err = NewFileTokenCache()
		if err != nil {
			return nil, fmt.Errorf("failed to initialize token cache: %w", err)
		}
	}

	return &Client{
		clientID:   config.ClientID,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		cache:      cache,
	}, nil
}

// Authenticate performs the OAuth device code flow
// This will prompt the user to visit a URL and enter a code
func (c *Client) Authenticate(ctx context.Context) error {
	return c.authenticateDeviceCode(ctx)
}

// ClearCache clears all cached authentication tokens
func (c *Client) ClearCache(ctx context.Context) error {
	return c.cache.Clear(ctx)
}

// GamertagToXUID converts a single gamertag to XUID
func (c *Client) GamertagToXUID(ctx context.Context, gamertag string) (string, error) {
	if gamertag == "" {
		return "", fmt.Errorf("gamertag is required")
	}

	profiles, err := c.searchGamertags(ctx, []string{gamertag})
	if err != nil {
		return "", err
	}

	if len(profiles) == 0 {
		return "", fmt.Errorf("gamertag not found: %s", gamertag)
	}

	return profiles[0].XUID, nil
}

// GamertagsToXUIDs converts multiple gamertags to XUIDs (batch lookup)
// Returns a map of gamertag -> XUID
// Gamertags that are not found will not be in the result map
func (c *Client) GamertagsToXUIDs(ctx context.Context, gamertags []string) (map[string]string, error) {
	if len(gamertags) == 0 {
		return map[string]string{}, nil
	}

	profiles, err := c.searchGamertags(ctx, gamertags)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, profile := range profiles {
		// Match by case-insensitive gamertag comparison
		for _, gt := range gamertags {
			if strings.EqualFold(profile.Gamertag, gt) {
				result[gt] = profile.XUID
				break
			}
		}
	}

	return result, nil
}

// GetProfile gets the full profile for a user by XUID
func (c *Client) GetProfile(ctx context.Context, xuid string) (*Profile, error) {
	if xuid == "" {
		return nil, fmt.Errorf("XUID is required")
	}

	// The search endpoint doesn't support XUID lookup directly
	// We need to use the profile endpoint
	// For now, return an error indicating this needs to be implemented
	// In a real implementation, you would use:
	// GET https://profile.xboxlive.com/users/xuid({xuid})/profile/settings
	return nil, fmt.Errorf("GetProfile by XUID not yet implemented")
}

// searchGamertags searches for gamertags and returns their profiles
func (c *Client) searchGamertags(ctx context.Context, gamertags []string) ([]Profile, error) {
	// Ensure we have a valid XSTS token
	xstsToken, userHash, err := c.ensureXSTSToken(ctx)
	if err != nil {
		return nil, err
	}

	// The search endpoint accepts a single query, so we'll need to make multiple requests
	// for true batch support. For now, we'll search for each gamertag individually
	var allProfiles []Profile

	for _, gamertag := range gamertags {
		// Build search URL
		searchURL := fmt.Sprintf("%s?q=%s&maxItems=25", searchEndpoint, url.QueryEscape(gamertag))

		req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
		if err != nil {
			return nil, err
		}

		// Set required headers
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-xbl-contract-version", "3")
		req.Header.Set("Authorization", fmt.Sprintf("XBL3.0 x=%s;%s", userHash, xstsToken))
		req.Header.Set("Accept-Language", "en-us")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("search request failed: %w", err)
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("search request failed: %s - %s", resp.Status, string(body))
		}

		var searchResp SearchResponse
		if err := json.Unmarshal(body, &searchResp); err != nil {
			return nil, fmt.Errorf("failed to parse search response: %w", err)
		}

		// Find exact match (case-insensitive)
		for _, profile := range searchResp.People {
			if strings.EqualFold(profile.Gamertag, gamertag) {
				allProfiles = append(allProfiles, profile)
				break
			}
		}
	}

	return allProfiles, nil
}
