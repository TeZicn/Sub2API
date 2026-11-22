//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"github.com/stretchr/testify/require"
)

type devinOAuthTestRepository struct{ values map[string]string }

func (r *devinOAuthTestRepository) Get(_ context.Context, key string) (*Setting, error) {
	value, ok := r.values[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: value}, nil
}
func (r *devinOAuthTestRepository) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}
func (r *devinOAuthTestRepository) Set(_ context.Context, key, value string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	r.values[key] = value
	return nil
}
func (r *devinOAuthTestRepository) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, nil
}
func (r *devinOAuthTestRepository) SetMultiple(context.Context, map[string]string) error { return nil }
func (r *devinOAuthTestRepository) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}
func (r *devinOAuthTestRepository) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

type devinOAuthRoundTripper func(*http.Request) (*http.Response, error)

func (f devinOAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func resetWebSearchConfigCacheForTest() {
	webSearchEmulationCache.Store(&cachedWebSearchEmulationConfig{config: &WebSearchEmulationConfig{}, expiresAt: 0})
}

func TestDevinOAuthFlowIsOwnerBoundSingleUseAndKeepsTokenMasked(t *testing.T) {
	resetWebSearchConfigCacheForTest()
	repo := &devinOAuthTestRepository{values: make(map[string]string)}
	svc := NewSettingService(repo, nil)
	var requestVerifier string
	svc.devinOAuthHTTPClient = &http.Client{Transport: devinOAuthRoundTripper(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, devinOAuthTokenPath, req.URL.String())
		var body struct {
			Code         string `json:"code"`
			CodeVerifier string `json:"code_verifier"`
		}
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		require.Equal(t, "one-time-code", body.Code)
		requestVerifier = body.CodeVerifier
		return devinOAuthTestResponse(http.StatusOK, `{"token":"devin-session-secret"}`), nil
	})}

	flow, err := svc.StartDevinOAuth(41)
	require.NoError(t, err)
	parsedURL, err := url.Parse(flow.URL)
	require.NoError(t, err)
	require.Equal(t, flow.ID, parsedURL.Query().Get("state"))
	require.Equal(t, "S256", parsedURL.Query().Get("code_challenge_method"))
	verifier := svc.devinOAuthFlows[flow.ID].verifier
	require.NotEmpty(t, verifier)
	require.NotEqual(t, verifier, parsedURL.Query().Get("code_challenge"))

	_, err = svc.CompleteDevinOAuth(context.Background(), 99, flow.ID, "one-time-code")
	var oauthErr *DevinOAuthError
	require.ErrorAs(t, err, &oauthErr)
	require.Equal(t, http.StatusBadRequest, oauthErr.StatusCode)
	require.Contains(t, svc.devinOAuthFlows, flow.ID, "another admin cannot consume the flow")

	status, err := svc.CompleteDevinOAuth(context.Background(), 41, flow.ID, "one-time-code")
	require.NoError(t, err)
	require.True(t, status.Connected)
	require.Equal(t, verifier, requestVerifier)

	_, err = svc.CompleteDevinOAuth(context.Background(), 41, flow.ID, "one-time-code")
	require.ErrorAs(t, err, &oauthErr)
	require.Equal(t, http.StatusBadRequest, oauthErr.StatusCode)
	require.NotContains(t, err.Error(), "devin-session-secret")

	cfg, err := svc.GetWebSearchEmulationConfig(context.Background())
	require.NoError(t, err)
	require.Len(t, cfg.Providers, 1)
	require.Equal(t, websearch.ProviderTypeDevin, cfg.Providers[0].Type)
	require.Equal(t, "devin-session-secret", cfg.Providers[0].APIKey)
	masked := SanitizeWebSearchConfig(context.Background(), cfg)
	require.Empty(t, masked.Providers[0].APIKey)
	require.True(t, masked.Providers[0].APIKeyConfigured)
	require.NotContains(t, string(devinMustJSON(t, masked)), "devin-session-secret")

	_, err = svc.DisconnectDevinOAuth(context.Background())
	require.NoError(t, err)
	cfg, err = svc.GetWebSearchEmulationConfig(context.Background())
	require.NoError(t, err)
	require.Empty(t, cfg.Providers)
	resetWebSearchConfigCacheForTest()
}

func TestDevinOAuthExpiredStateIsRejectedAndRemoved(t *testing.T) {
	svc := NewSettingService(&devinOAuthTestRepository{values: make(map[string]string)}, nil)
	flow, err := svc.StartDevinOAuth(7)
	require.NoError(t, err)
	pending := svc.devinOAuthFlows[flow.ID]
	pending.expiresAt = time.Now().Add(-time.Second)
	svc.devinOAuthFlows[flow.ID] = pending

	_, err = svc.CompleteDevinOAuth(context.Background(), 7, flow.ID, "code")
	var oauthErr *DevinOAuthError
	require.ErrorAs(t, err, &oauthErr)
	require.Equal(t, http.StatusBadRequest, oauthErr.StatusCode)
	require.NotContains(t, svc.devinOAuthFlows, flow.ID)
}

func TestExchangeDevinCodeRejectsUnsafeResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "rejected code", status: http.StatusUnauthorized, body: `{"error":"private detail"}`},
		{name: "missing token", status: http.StatusOK, body: `{"token":""}`},
		{name: "oversized response", status: http.StatusOK, body: strings.Repeat("x", devinOAuthMaxBody+1)},
		{name: "redirect", status: http.StatusFound, body: "redirect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: devinOAuthRoundTripper(func(*http.Request) (*http.Response, error) {
				return devinOAuthTestResponse(tc.status, tc.body), nil
			})}
			_, err := exchangeDevinCode(context.Background(), client, "https://auth.example/token", "code", "verifier")
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private detail")
		})
	}

	client := &http.Client{Transport: devinOAuthRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("private network detail")
	})}
	_, err := exchangeDevinCode(context.Background(), client, "https://auth.example/token", "code", "verifier")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private network detail")
}

func devinOAuthTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func devinMustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}
