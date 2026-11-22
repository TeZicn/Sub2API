package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
)

const (
	devinOAuthTTL       = 5 * time.Minute
	devinOAuthMaxFlows  = 8
	devinOAuthMaxBody   = 64 * 1024
	devinOAuthTimeout   = 15 * time.Second
	devinOAuthTokenPath = "https://api.devin.ai/auth/cli/token"
)

type pendingDevinOAuthFlow struct {
	ownerID   int64
	verifier  string
	expiresAt time.Time
}

// DevinOAuthStart contains the browser authorization URL and opaque state ID.
type DevinOAuthStart struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expires_at"`
}

// DevinOAuthStatus deliberately exposes connection state only, never the session token.
type DevinOAuthStatus struct {
	Connected bool `json:"connected"`
}

// DevinOAuthError carries a safe HTTP status and message for the admin handler.
type DevinOAuthError struct {
	StatusCode int
	Message    string
}

func (e *DevinOAuthError) Error() string { return e.Message }

func (s *SettingService) StartDevinOAuth(ownerID int64) (*DevinOAuthStart, error) {
	if ownerID <= 0 {
		return nil, &DevinOAuthError{StatusCode: http.StatusUnauthorized, Message: "administrator identity is required"}
	}
	verifierBytes := make([]byte, 64)
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return nil, fmt.Errorf("devin oauth: generate verifier: %w", err)
	}
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, fmt.Errorf("devin oauth: generate state: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	state := hex.EncodeToString(stateBytes)
	challenge := sha256.Sum256([]byte(verifier))
	expiresAt := time.Now().Add(devinOAuthTTL)

	s.devinOAuthMu.Lock()
	if s.devinOAuthFlows == nil {
		s.devinOAuthFlows = make(map[string]pendingDevinOAuthFlow)
	}
	for id, flow := range s.devinOAuthFlows {
		if flow.expiresAt.Before(time.Now()) || flow.ownerID == ownerID {
			delete(s.devinOAuthFlows, id)
		}
	}
	if len(s.devinOAuthFlows) >= devinOAuthMaxFlows {
		s.devinOAuthMu.Unlock()
		return nil, &DevinOAuthError{StatusCode: http.StatusTooManyRequests, Message: "too many authorization flows are active"}
	}
	s.devinOAuthFlows[state] = pendingDevinOAuthFlow{ownerID: ownerID, verifier: verifier, expiresAt: expiresAt}
	s.devinOAuthMu.Unlock()

	authURL := "https://app.devin.ai/auth/cli/continue?" + "state=" + state +
		"&prompt=select_account&code_challenge=" + base64.RawURLEncoding.EncodeToString(challenge[:]) +
		"&code_challenge_method=S256"
	return &DevinOAuthStart{ID: state, URL: authURL, ExpiresAt: expiresAt.Unix()}, nil
}

func (s *SettingService) CompleteDevinOAuth(ctx context.Context, ownerID int64, state, code string) (*DevinOAuthStatus, error) {
	state = strings.TrimSpace(state)
	code = strings.TrimSpace(code)
	if ownerID <= 0 || state == "" {
		return nil, &DevinOAuthError{StatusCode: http.StatusBadRequest, Message: "authorization flow is invalid; restart the connection"}
	}
	if len(code) == 0 || len(code) > 8192 || !isPrintableASCII(code) {
		return nil, &DevinOAuthError{StatusCode: http.StatusBadRequest, Message: "enter the complete authorization code"}
	}

	s.devinOAuthMu.Lock()
	flow, ok := s.devinOAuthFlows[state]
	if ok && flow.ownerID == ownerID {
		delete(s.devinOAuthFlows, state)
	} else {
		ok = false
	}
	s.devinOAuthMu.Unlock()
	if !ok {
		return nil, &DevinOAuthError{StatusCode: http.StatusBadRequest, Message: "authorization flow is invalid or already used; restart the connection"}
	}
	if time.Now().After(flow.expiresAt) {
		return nil, &DevinOAuthError{StatusCode: http.StatusBadRequest, Message: "authorization expired; restart the connection"}
	}

	client := s.devinOAuthHTTPClient
	if client == nil {
		client = &http.Client{Timeout: devinOAuthTimeout}
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }

	token, err := exchangeDevinCode(ctx, &clientCopy, devinOAuthTokenPath, code, flow.verifier)
	if err != nil {
		var oauthErr *DevinOAuthError
		if errors.As(err, &oauthErr) {
			return nil, oauthErr
		}
		return nil, &DevinOAuthError{StatusCode: http.StatusBadGateway, Message: "could not reach Devin authorization service"}
	}
	if err := s.saveDevinCredential(ctx, token); err != nil {
		return nil, fmt.Errorf("devin oauth: save configuration: %w", err)
	}
	return &DevinOAuthStatus{Connected: true}, nil
}

func (s *SettingService) DisconnectDevinOAuth(ctx context.Context) (*DevinOAuthStatus, error) {
	current, err := s.GetWebSearchEmulationConfig(ctx)
	if err != nil {
		return nil, err
	}
	cfg := *current
	providers := make([]WebSearchProviderConfig, 0, len(current.Providers))
	for _, provider := range current.Providers {
		if provider.Type != websearch.ProviderTypeDevin {
			providers = append(providers, provider)
		}
	}
	cfg.Providers = providers
	if err := s.SaveWebSearchEmulationConfig(ctx, &cfg); err != nil {
		return nil, err
	}
	return &DevinOAuthStatus{Connected: false}, nil
}

func (s *SettingService) saveDevinCredential(ctx context.Context, token string) error {
	current, err := s.GetWebSearchEmulationConfig(ctx)
	if err != nil {
		return err
	}
	cfg := *current
	cfg.Providers = append([]WebSearchProviderConfig(nil), current.Providers...)
	for i := range cfg.Providers {
		if cfg.Providers[i].Type == websearch.ProviderTypeDevin {
			cfg.Providers[i].APIKey = token
			return s.SaveWebSearchEmulationConfig(ctx, &cfg)
		}
	}
	cfg.Providers = append(cfg.Providers, WebSearchProviderConfig{Type: websearch.ProviderTypeDevin, APIKey: token})
	return s.SaveWebSearchEmulationConfig(ctx, &cfg)
}

func exchangeDevinCode(ctx context.Context, client *http.Client, endpoint, code, verifier string) (string, error) {
	body, err := json.Marshal(map[string]string{"code": code, "code_verifier": verifier})
	if err != nil {
		return "", fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", &DevinOAuthError{StatusCode: http.StatusBadRequest, Message: "authorization endpoint is invalid"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", &DevinOAuthError{StatusCode: http.StatusBadGateway, Message: "could not reach Devin authorization service"}
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", &DevinOAuthError{StatusCode: http.StatusBadGateway, Message: "Devin did not accept the authorization code"}
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, devinOAuthMaxBody+1))
	if err != nil || len(responseBody) > devinOAuthMaxBody {
		return "", &DevinOAuthError{StatusCode: http.StatusBadGateway, Message: "Devin authorization response is invalid"}
	}
	var response struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil || len(response.Token) == 0 || len(response.Token) > 16384 || !isPrintableASCII(response.Token) {
		return "", &DevinOAuthError{StatusCode: http.StatusBadGateway, Message: "Devin authorization response is invalid"}
	}
	return response.Token, nil
}

func isPrintableASCII(value string) bool {
	for _, char := range value {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}
