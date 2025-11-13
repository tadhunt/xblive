package xblive

import (
	"bytes"
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
	// OAuth endpoints
	deviceCodeEndpoint = "https://login.microsoftonline.com/consumers/oauth2/v2.0/devicecode"
	tokenEndpoint      = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"

	// Xbox endpoints
	userAuthEndpoint = "https://user.auth.xboxlive.com/user/authenticate"
	xstsAuthEndpoint = "https://xsts.auth.xboxlive.com/xsts/authorize"

	// OAuth scopes
	scopes = "Xboxlive.signin Xboxlive.offline_access"
)

// authenticateDeviceCode performs the device code OAuth flow
func (c *Client) authenticateDeviceCode(ctx context.Context) error {
	// Step 1: Request device code
	deviceCode, err := c.requestDeviceCode(ctx)
	if err != nil {
		return fmt.Errorf("failed to request device code: %w", err)
	}

	// Display instructions to user
	fmt.Printf("\n")
	fmt.Printf("To sign in, use a web browser to open the page:\n")
	fmt.Printf("    %s\n", deviceCode.VerificationURI)
	fmt.Printf("\n")
	fmt.Printf("And enter the code:\n")
	fmt.Printf("    %s\n", deviceCode.UserCode)
	fmt.Printf("\n")

	// Step 2: Poll for token
	token, err := c.pollForToken(ctx, deviceCode)
	if err != nil {
		return fmt.Errorf("failed to obtain token: %w", err)
	}

	// Cache the tokens
	notAfter := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	if err := c.cache.SetAccessToken(ctx, token.AccessToken, notAfter); err != nil {
		return fmt.Errorf("failed to cache access token: %w", err)
	}
	if err := c.cache.SetRefreshToken(ctx, token.RefreshToken); err != nil {
		return fmt.Errorf("failed to cache refresh token: %w", err)
	}

	fmt.Printf("Authentication successful!\n\n")
	return nil
}

// requestDeviceCode requests a device code from Microsoft
func (c *Client) requestDeviceCode(ctx context.Context) (*DeviceCodeResponse, error) {
	data := url.Values{}
	data.Set("client_id", c.clientID)
	data.Set("scope", scopes)

	req, err := http.NewRequestWithContext(ctx, "POST", deviceCodeEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device code request failed: %s - %s", resp.Status, string(body))
	}

	var deviceCode DeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&deviceCode); err != nil {
		return nil, err
	}

	return &deviceCode, nil
}

// pollForToken polls the token endpoint until the user completes authentication
func (c *Client) pollForToken(ctx context.Context, deviceCode *DeviceCodeResponse) (*TokenResponse, error) {
	interval := time.Duration(deviceCode.Interval) * time.Second
	timeout := time.Duration(deviceCode.ExpiresIn) * time.Second
	deadline := time.Now().Add(timeout)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("device code expired")
			}

			token, err := c.tryGetToken(ctx, deviceCode.DeviceCode)
			if err != nil {
				// Check if it's a "pending" error (user hasn't completed auth yet)
				if strings.Contains(err.Error(), "authorization_pending") {
					continue // Keep polling
				}
				return nil, err
			}

			return token, nil
		}
	}
}

// tryGetToken attempts to exchange the device code for an access token
func (c *Client) tryGetToken(ctx context.Context, deviceCode string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	data.Set("client_id", c.clientID)
	data.Set("device_code", deviceCode)

	req, err := http.NewRequestWithContext(ctx, "POST", tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		// Parse error response
		var errorResp struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return nil, fmt.Errorf("%s: %s", errorResp.Error, errorResp.ErrorDescription)
		}
		return nil, fmt.Errorf("token request failed: %s - %s", resp.Status, string(body))
	}

	var token TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}

	return &token, nil
}

// refreshAccessToken refreshes the access token using the refresh token
func (c *Client) refreshAccessToken(ctx context.Context) error {
	refreshToken, ok := c.cache.GetRefreshToken(ctx)
	if !ok {
		return fmt.Errorf("no refresh token available")
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", c.clientID)
	data.Set("refresh_token", refreshToken)
	data.Set("scope", scopes)

	req, err := http.NewRequestWithContext(ctx, "POST", tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token refresh failed: %s - %s", resp.Status, string(body))
	}

	var token TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return err
	}

	// Cache the new tokens
	notAfter := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	if err := c.cache.SetAccessToken(ctx, token.AccessToken, notAfter); err != nil {
		return err
	}
	if token.RefreshToken != "" {
		if err := c.cache.SetRefreshToken(ctx, token.RefreshToken); err != nil {
			return err
		}
	}

	return nil
}

// getXboxUserToken exchanges the Microsoft access token for an Xbox user token
func (c *Client) getXboxUserToken(ctx context.Context, accessToken string) (*XboxUserTokenResponse, error) {
	reqBody := XboxUserTokenRequest{
		RelyingParty: "http://auth.xboxlive.com",
		TokenType:    "JWT",
		Properties: XboxUserTokenRequestProperties{
			AuthMethod: "RPS",
			SiteName:   "user.auth.xboxlive.com",
			RpsTicket:  "d=" + accessToken,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", userAuthEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-xbl-contract-version", "1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("user token request failed: %s - %s", resp.Status, string(body))
	}

	var userToken XboxUserTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&userToken); err != nil {
		return nil, err
	}

	return &userToken, nil
}

// getXSTSToken exchanges the Xbox user token for an XSTS token
func (c *Client) getXSTSToken(ctx context.Context, userToken string) (*XSTSTokenResponse, error) {
	reqBody := XSTSTokenRequest{
		RelyingParty: "http://xboxlive.com",
		TokenType:    "JWT",
		Properties: XSTSTokenRequestProperties{
			UserTokens: []string{userToken},
			SandboxId:  "RETAIL",
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", xstsAuthEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-xbl-contract-version", "1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		// Try to parse Xbox error response
		var xboxErr XboxErrorResponse
		if err := json.Unmarshal(body, &xboxErr); err == nil && xboxErr.XErr != 0 {
			return nil, formatXboxError(xboxErr)
		}

		return nil, fmt.Errorf("XSTS token request failed: %s - %s", resp.Status, string(body))
	}

	var xstsToken XSTSTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&xstsToken); err != nil {
		return nil, err
	}

	return &xstsToken, nil
}

// formatXboxError formats an Xbox error response into a user-friendly message
func formatXboxError(err XboxErrorResponse) error {
	switch err.XErr {
	case 2148916233: // 0x8015DC0B
		return fmt.Errorf("no Xbox account found: the Microsoft account you authenticated with doesn't have an Xbox Live profile. Create one at https://www.xbox.com/")
	case 2148916235: // 0x8015DC0D
		//lint:ignore ST1005 Xbox Live is a proper name
		return fmt.Errorf("Xbox Live is not available in your country/region")
	case 2148916236, 2148916237: // 0x8015DC0E, 0x8015DC0F
		return fmt.Errorf("the account needs adult verification. Please verify your account at https://account.microsoft.com/")
	case 2148916238: // 0x8015DC10
		return fmt.Errorf("the account is a child account and cannot proceed unless the parent consents")
	default:
		if err.Message != "" {
			//lint:ignore ST1005 Xbox is a proper name
			return fmt.Errorf("Xbox error %d: %s", err.XErr, err.Message)
		}
		//lint:ignore ST1005 Xbox is a proper name
		return fmt.Errorf("Xbox error code: %d (0x%X)", err.XErr, err.XErr)
	}
}

// ensureXSTSToken ensures we have a valid XSTS token, refreshing if necessary
func (c *Client) ensureXSTSToken(ctx context.Context) (string, string, error) {
	// Check if we have a valid cached XSTS token
	if token, userHash, ok := c.cache.GetXSTSToken(ctx); ok {
		return token, userHash, nil
	}

	// Check if we have a valid cached user token
	if userToken, ok := c.cache.GetUserToken(ctx); ok {
		// Exchange for XSTS token
		xstsResp, err := c.getXSTSToken(ctx, userToken)
		if err == nil {
			userHash := extractUserHash(xstsResp.DisplayClaims)
			if err := c.cache.SetXSTSToken(ctx, xstsResp.Token, userHash, xstsResp.NotAfter); err != nil {
				return "", "", err
			}
			return xstsResp.Token, userHash, nil
		}
	}

	// Check if we have a valid cached access token
	accessToken, ok := c.cache.GetAccessToken(ctx)
	if !ok {
		// Try to refresh
		if err := c.refreshAccessToken(ctx); err != nil {
			return "", "", fmt.Errorf("not authenticated, please call Authenticate() first")
		}
		accessToken, ok = c.cache.GetAccessToken(ctx)
		if !ok {
			return "", "", fmt.Errorf("failed to obtain access token")
		}
	}

	// Exchange access token for user token
	userTokenResp, err := c.getXboxUserToken(ctx, accessToken)
	if err != nil {
		return "", "", fmt.Errorf("failed to get user token: %w", err)
	}

	if err := c.cache.SetUserToken(ctx, userTokenResp.Token, userTokenResp.NotAfter); err != nil {
		return "", "", err
	}

	// Exchange user token for XSTS token
	xstsResp, err := c.getXSTSToken(ctx, userTokenResp.Token)
	if err != nil {
		return "", "", fmt.Errorf("failed to get XSTS token: %w", err)
	}

	userHash := extractUserHash(xstsResp.DisplayClaims)
	if err := c.cache.SetXSTSToken(ctx, xstsResp.Token, userHash, xstsResp.NotAfter); err != nil {
		return "", "", err
	}

	return xstsResp.Token, userHash, nil
}

// extractUserHash extracts the user hash from display claims
func extractUserHash(claims XSTSTokenDisplayClaims) string {
	if len(claims.Xui) > 0 {
		if uhs, ok := claims.Xui[0]["uhs"].(string); ok {
			return uhs
		}
	}
	return ""
}
