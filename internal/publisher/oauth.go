package publisher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	AccessToken  string
	RefreshToken string
}

type OAuthClient struct {
	config       OAuthConfig
	accessToken  string
	refreshToken string
	logger       *zap.Logger
}

func NewOAuthClient(cfg OAuthConfig, logger *zap.Logger) *OAuthClient {
	return &OAuthClient{
		config:       cfg,
		accessToken:  cfg.AccessToken,
		refreshToken: cfg.RefreshToken,
		logger:       logger,
	}
}

func (o *OAuthClient) IsAuthenticated() bool {
	return o.accessToken != ""
}

func (o *OAuthClient) AuthURL(state string) string {
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", o.config.ClientID)
	params.Set("redirect_uri", o.config.RedirectURI)
	params.Set("scope", "openid profile email w_member_social")
	params.Set("state", state)

	return "https://www.linkedin.com/oauth/v2/authorization?" + params.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

func (o *OAuthClient) ExchangeCode(ctx context.Context, code string) error {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("client_id", o.config.ClientID)
	data.Set("client_secret", o.config.ClientSecret)
	data.Set("redirect_uri", o.config.RedirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://www.linkedin.com/oauth/v2/accessToken",
		strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token exchange failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return fmt.Errorf("parse token response: %w", err)
	}

	o.accessToken = tr.AccessToken
	o.refreshToken = tr.RefreshToken
	o.logger.Info("LinkedIn OAuth2 tokens obtained")

	return nil
}

func (o *OAuthClient) Refresh(ctx context.Context) error {
	if o.refreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", o.refreshToken)
	data.Set("client_id", o.config.ClientID)
	data.Set("client_secret", o.config.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://www.linkedin.com/oauth/v2/accessToken",
		strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token refresh failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return fmt.Errorf("parse refresh response: %w", err)
	}

	o.accessToken = tr.AccessToken
	if tr.RefreshToken != "" {
		o.refreshToken = tr.RefreshToken
	}

	o.logger.Info("LinkedIn token refreshed")
	return nil
}

func (o *OAuthClient) GetToken(ctx context.Context) (string, error) {
	if o.accessToken == "" {
		return "", fmt.Errorf("not authenticated: run auth flow first")
	}
	return o.accessToken, nil
}

func (o *OAuthClient) AccessToken() string {
	return o.accessToken
}

func (o *OAuthClient) RefreshToken() string {
	return o.refreshToken
}

func (o *OAuthClient) SetPersonID(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.linkedin.com/v2/userinfo", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+o.accessToken)

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var userInfo struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return "", fmt.Errorf("parse userinfo response: %w", err)
	}

	if userInfo.Sub == "" {
		return "", fmt.Errorf("empty sub in userinfo response: %s", string(body))
	}

	return userInfo.Sub, nil
}
