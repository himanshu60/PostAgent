package publisher

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/config"
	"github.com/clodura/linkedin-post-agent/internal/models"
)

// composeBody builds the post text: a bold title heading, the body, then
// hashtags — each separated by a blank line so it renders with clear spacing.
// littleTextReserved are the characters LinkedIn's "little Text Format"
// treats as formatting/entity markers in the Posts API commentary field.
// Any UNESCAPED occurrence silently drops everything after it from the
// published post (e.g. one "(" truncated an entire post mid-sentence).
// "#" is deliberately NOT escaped: hashtags must stay raw to become links.
// Backslash must be escaped first so we don't double-escape our own escapes.
var littleTextEscaper = strings.NewReplacer(
	`\`, `\\`,
	`|`, `\|`,
	`{`, `\{`,
	`}`, `\}`,
	`@`, `\@`,
	`[`, `\[`,
	`]`, `\]`,
	`(`, `\(`,
	`)`, `\)`,
	`<`, `\<`,
	`>`, `\>`,
	`*`, `\*`,
	`_`, `\_`,
	`~`, `\~`,
)

func escapeLittleText(s string) string {
	return littleTextEscaper.Replace(s)
}

func composeBody(draft *models.Draft) string {
	var b strings.Builder

	body := strings.TrimSpace(draft.Body)
	title := strings.TrimSpace(draft.Title)

	// Add the title as a bold heading line, unless the body already opens with it.
	if title != "" && !strings.HasPrefix(strings.ToLower(body), strings.ToLower(title)) {
		b.WriteString(escapeLittleText(toBoldUnicode(title)))
		b.WriteString("\n\n")
	}
	b.WriteString(escapeLittleText(body))

	// Hashtags are appended unescaped: they contain only # + alphanumerics
	// (sanitized at generation), and # must stay raw for LinkedIn to link them.
	if len(draft.Hashtags) > 0 {
		b.WriteString("\n\n")
		b.WriteString(strings.Join(draft.Hashtags, " "))
	}

	// LinkedIn rejects posts longer than 3000 characters; trim as a safeguard.
	out := b.String()
	if r := []rune(out); len(r) > 3000 {
		out = strings.TrimSpace(string(r[:2999])) + "…"
	}
	return out
}

// toBoldUnicode converts ASCII letters and digits to their Unicode
// mathematical sans-serif bold equivalents. LinkedIn posts are plain text and
// don't support rich formatting, so this is how a heading renders as "bold".
func toBoldUnicode(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(0x1D5D4 + (r - 'A')) // 𝗔–𝗭
		case r >= 'a' && r <= 'z':
			b.WriteRune(0x1D5EE + (r - 'a')) // 𝗮–𝘇
		case r >= '0' && r <= '9':
			b.WriteRune(0x1D7EC + (r - '0')) // 𝟬–𝟵
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

type Client struct {
	oauth      *OAuthClient
	personID   string
	apiVersion string
	imageGen   ImageGenConfig
	logger     *zap.Logger
}

// SetImageGen configures optional live image generation (e.g. OpenAI) used at
// publish time. When unset or disabled, the draft's SourceURL image is used.
func (c *Client) SetImageGen(cfg ImageGenConfig) {
	c.imageGen = cfg
}

func NewClient(cfg config.LinkedInConfig, logger *zap.Logger) *Client {
	oauth := NewOAuthClient(OAuthConfig{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURI:  cfg.RedirectURI,
		AccessToken:  cfg.AccessToken,
		RefreshToken: cfg.RefreshToken,
	}, logger)

	apiVersion := cfg.APIVersion
	if apiVersion == "" {
		apiVersion = "202506"
	}

	return &Client{
		oauth:      oauth,
		personID:   cfg.PersonID,
		apiVersion: apiVersion,
		logger:     logger,
	}
}

func (c *Client) IsConfigured() bool {
	return c.oauth.IsAuthenticated() && c.personID != ""
}

func (c *Client) CreatePost(ctx context.Context, draft *models.Draft) (postID string, postURL string, err error) {
	if !c.IsConfigured() {
		return "", "", fmt.Errorf("publisher not configured: run auth flow first")
	}

	token, err := c.oauth.GetToken(ctx)
	if err != nil {
		return "", "", fmt.Errorf("get token: %w", err)
	}

	body := composeBody(draft)

	linkedInReq := linkedInPostRequest{
		Author:         "urn:li:person:" + c.personID,
		Commentary:     body,
		Visibility:     "PUBLIC",
		LifecycleState: "PUBLISHED",
		Distribution: distribution{
			FeedDistribution: "MAIN_FEED",
		},
	}

	// Resolve the image URN to attach. Preference order:
	//   1. pre-uploaded asset URN
	//   2. user-uploaded local file
	//   3. Gemini / OpenAI generated image (if configured) from the stored prompt
	//   4. the draft's SourceURL image (e.g. Pollinations) as a fallback
	if draft.Media != nil {
		assetURN := draft.Media.LinkedInAssetURN

		// A user-uploaded local file takes priority over generated images.
		if assetURN == "" && draft.Media.LocalPath != "" {
			uploaded, uerr := c.UploadImage(ctx, draft.Media.LocalPath)
			if uerr != nil {
				c.logger.Warn("Local image upload failed, trying other sources", zap.Error(uerr))
			} else {
				assetURN = uploaded
			}
		}

		if assetURN == "" && c.imageGen.geminiEnabled() && draft.Media.Prompt != "" {
			uploaded, uerr := c.uploadGeminiImage(ctx, draft.Media.Prompt)
			if uerr != nil {
				c.logger.Warn("Gemini image failed, falling back", zap.Error(uerr))
			} else {
				assetURN = uploaded
			}
		}

		if assetURN == "" && c.imageGen.openAIEnabled() && draft.Media.Prompt != "" {
			uploaded, uerr := c.uploadOpenAIImage(ctx, draft.Media.Prompt)
			if uerr != nil {
				c.logger.Warn("OpenAI image failed, falling back to source image", zap.Error(uerr))
			} else {
				assetURN = uploaded
			}
		}

		if assetURN == "" && draft.Media.SourceURL != "" {
			uploaded, uerr := c.UploadImageFromURL(ctx, draft.Media.SourceURL)
			if uerr != nil {
				c.logger.Warn("Image upload failed, posting without image",
					zap.String("source_url", draft.Media.SourceURL), zap.Error(uerr))
			} else {
				assetURN = uploaded
			}
		}

		if assetURN != "" {
			mc := &mediaContent{ID: assetURN}
			if draft.Media.AltText != "" {
				mc.AltText = draft.Media.AltText
			}
			linkedInReq.Content = &postContent{Media: mc}
		}
	}

	resp, err := c.postToLinkedIn(ctx, token, linkedInReq)
	if err != nil {
		return "", "", fmt.Errorf("linkedin post: %w", err)
	}

	return resp.ID, fmt.Sprintf("https://www.linkedin.com/feed/update/%s", resp.ID), nil
}

func (c *Client) RefreshToken() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return c.oauth.Refresh(ctx)
}

func (c *Client) AccessToken() string {
	return c.oauth.AccessToken()
}

func (c *Client) RefreshTokenStr() string {
	return c.oauth.RefreshToken()
}

func (c *Client) AuthURL(state string) string {
	return c.oauth.AuthURL(state)
}

func (c *Client) ExchangeCode(ctx context.Context, code string) error {
	return c.oauth.ExchangeCode(ctx, code)
}

func (c *Client) SetPersonID(ctx context.Context) (string, error) {
	personID, err := c.oauth.SetPersonID(ctx)
	if err != nil {
		return "", err
	}
	c.personID = personID
	return personID, nil
}

func (c *Client) HasTokens() bool {
	return c.oauth.AccessToken() != ""
}
