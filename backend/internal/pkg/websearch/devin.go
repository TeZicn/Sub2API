package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	devinProviderName  = ProviderTypeDevin
	devinSessionPrefix = "devin-session-token$"
	devinSearchPath    = "/exa.api_server_pb.ApiServerService/GetWebSearchResults"
	devinMaxResults    = 10
)

var devinSearchHosts = [...]string{
	"https://server.codeium.com",
	"https://server.self-serve.windsurf.com",
}

// DevinProvider uses a Devin OAuth session with the Windsurf web-search protocol.
type DevinProvider struct {
	sessionToken string
	httpClient   *http.Client
}

// NewDevinProvider creates a Devin search provider.
func NewDevinProvider(sessionToken string, httpClient *http.Client) *DevinProvider {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	client := *httpClient
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &DevinProvider{sessionToken: strings.TrimSpace(sessionToken), httpClient: &client}
}

func (d *DevinProvider) Name() string { return devinProviderName }

func (d *DevinProvider) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, errors.New("devin: empty search query")
	}
	if d.sessionToken == "" {
		return nil, errors.New("devin: authorization is not configured")
	}
	limit := req.MaxResults
	if limit <= 0 || limit > devinMaxResults {
		limit = devinMaxResults
	}
	apiKey := devinSearchCredential(d.sessionToken)
	body, err := json.Marshal(devinSearchRequest{
		Metadata: devinSearchMetadata{
			APIKey: apiKey, IDEName: "windsurf", IDEVersion: "1.9600.41",
			ExtensionName: "windsurf", ExtensionVersion: "1.9600.41", Locale: "en",
		},
		Query: query,
		Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("devin: encode request: %w", err)
	}

	rejected := 0
	var lastErr error
	for _, host := range devinSearchHosts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+devinSearchPath, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("devin: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Connect-Protocol-Version", "1")
		req.Header.Set("User-Agent", "windsurf/1.9600.41")
		resp, err := d.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = errors.New("devin: search service connection failed")
			continue
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("devin: read response: %w", readErr)
		}
		if len(responseBody) > maxResponseSize {
			return nil, errors.New("devin: response exceeded size limit")
		}
		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			return decodeDevinSearchResponse(responseBody, query, limit, []string{apiKey, d.sessionToken})
		}
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			rejected++
			continue
		case http.StatusTooManyRequests:
			return nil, errors.New("devin: search service rate limit reached")
		default:
			if resp.StatusCode < http.StatusInternalServerError {
				return nil, fmt.Errorf("devin: search service returned status %d", resp.StatusCode)
			}
			lastErr = fmt.Errorf("devin: search service returned status %d", resp.StatusCode)
		}
	}
	if rejected == len(devinSearchHosts) {
		return nil, errors.New("devin: authorization expired or search access denied")
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("devin: search service unavailable")
}

func devinSearchCredential(token string) string {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(token, devinSessionPrefix) || strings.HasPrefix(token, "sk-ws-") {
		return token
	}
	return devinSessionPrefix + token
}

func decodeDevinSearchResponse(body []byte, query string, limit int, secrets []string) (*SearchResponse, error) {
	var raw struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(body, &raw); err != nil || raw.Results == nil {
		return nil, errors.New("devin: invalid search response")
	}
	results := make([]SearchResult, 0, min(len(raw.Results), limit))
	for _, row := range raw.Results {
		urlValue := devinFirstString(row, []string{"url", "sourceUrl", "webUrl", "link"}, 4096, nil)
		parsed, err := url.Parse(urlValue)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || containsDevinSecret(urlValue, secrets) {
			continue
		}
		result := SearchResult{
			URL:     urlValue,
			Title:   devinFirstString(row, []string{"title", "name", "webTitle"}, 512, secrets),
			Snippet: devinFirstString(row, []string{"snippet", "summary", "text", "content"}, 4096, secrets),
		}
		results = append(results, result)
		if len(results) == limit {
			break
		}
	}
	return &SearchResponse{Results: results, Query: query}, nil
}

func devinFirstString(row map[string]any, names []string, maxLen int, secrets []string) string {
	for _, name := range names {
		value, ok := row[name].(string)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		for _, secret := range secrets {
			if secret != "" {
				value = strings.ReplaceAll(value, secret, "[redacted]")
			}
		}
		value = strings.TrimSpace(value)
		if utf8.RuneCountInString(value) > maxLen {
			value = string([]rune(value)[:maxLen])
		}
		return value
	}
	return ""
}

func containsDevinSecret(value string, secrets []string) bool {
	for _, secret := range secrets {
		if secret != "" && strings.Contains(value, secret) {
			return true
		}
	}
	return false
}

type devinSearchRequest struct {
	Metadata devinSearchMetadata `json:"metadata"`
	Query    string              `json:"query"`
	Limit    int                 `json:"limit"`
}

type devinSearchMetadata struct {
	APIKey           string `json:"apiKey"`
	IDEName          string `json:"ideName"`
	IDEVersion       string `json:"ideVersion"`
	ExtensionName    string `json:"extensionName"`
	ExtensionVersion string `json:"extensionVersion"`
	Locale           string `json:"locale"`
}
