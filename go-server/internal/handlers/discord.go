package handlers

import (
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = crand.Read(b)
	return hex.EncodeToString(b)
}

type discordTokenResponse struct {
	AccessToken string `json:"access_token"`
}

type discordUserResponse struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
}

var discordHTTPClient = &http.Client{Timeout: 10 * time.Second}

func exchangeDiscordCode(clientID, clientSecret, redirectURI, code string) (discordID, discordUsername string, err error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)

	req, err := http.NewRequest(http.MethodPost, "https://discord.com/api/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := discordHTTPClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("discord token exchange failed: %d %s", resp.StatusCode, string(body))
	}
	var tokenRes discordTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenRes); err != nil {
		return "", "", err
	}
	if tokenRes.AccessToken == "" {
		return "", "", fmt.Errorf("discord token response missing access_token")
	}

	userReq, err := http.NewRequest(http.MethodGet, "https://discord.com/api/users/@me", nil)
	if err != nil {
		return "", "", err
	}
	userReq.Header.Set("Authorization", "Bearer "+tokenRes.AccessToken)
	userResp, err := discordHTTPClient.Do(userReq)
	if err != nil {
		return "", "", err
	}
	defer userResp.Body.Close()
	if userResp.StatusCode >= 400 {
		body, _ := io.ReadAll(userResp.Body)
		return "", "", fmt.Errorf("discord user fetch failed: %d %s", userResp.StatusCode, string(body))
	}
	var du discordUserResponse
	if err := json.NewDecoder(userResp.Body).Decode(&du); err != nil {
		return "", "", err
	}
	if du.ID == "" {
		return "", "", fmt.Errorf("discord user response missing id")
	}
	username := du.Username
	if du.Discriminator != "" && du.Discriminator != "0" {
		username = username + "#" + du.Discriminator
	}
	return du.ID, username, nil
}
