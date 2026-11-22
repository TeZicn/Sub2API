package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type devinRoundTripFunc func(*http.Request) (*http.Response, error)

func (f devinRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func devinResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestDevinProvider_NameAndManagerFactory(t *testing.T) {
	require.Equal(t, ProviderTypeDevin, NewDevinProvider("token", nil).Name())
	require.IsType(t, &DevinProvider{}, (&Manager{}).buildProvider(ProviderConfig{Type: ProviderTypeDevin, APIKey: "token"}, nil))
}

func TestDevinProvider_SearchBuildsProtocolRequestAndNormalizesResults(t *testing.T) {
	var request *http.Request
	var requestBody []byte
	client := &http.Client{Transport: devinRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		request = req
		requestBody, _ = io.ReadAll(req.Body)
		return devinResponse(http.StatusOK, `{"results":[{"url":"https://example.com/a","title":"Example","summary":"Summary"},{"url":"javascript:alert(1)","title":"Bad"}]}`), nil
	})}
	provider := NewDevinProvider("session-secret", client)

	result, err := provider.Search(context.Background(), SearchRequest{Query: "today news", MaxResults: 99})

	require.NoError(t, err)
	require.Equal(t, "https://server.codeium.com"+devinSearchPath, request.URL.String())
	require.Equal(t, http.MethodPost, request.Method)
	require.Equal(t, "1", request.Header.Get("Connect-Protocol-Version"))
	require.Equal(t, "application/json", request.Header.Get("Content-Type"))
	var sent devinSearchRequest
	require.NoError(t, json.Unmarshal(requestBody, &sent))
	require.Equal(t, "devin-session-token$session-secret", sent.Metadata.APIKey)
	require.Equal(t, "today news", sent.Query)
	require.Equal(t, devinMaxResults, sent.Limit)
	require.Equal(t, &SearchResponse{Query: "today news", Results: []SearchResult{{URL: "https://example.com/a", Title: "Example", Snippet: "Summary"}}}, result)
}

func TestDevinProvider_SearchFailsOverOnlyForAuthOrServerErrors(t *testing.T) {
	hosts := make([]string, 0, 2)
	client := &http.Client{Transport: devinRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		hosts = append(hosts, req.URL.Host)
		if len(hosts) == 1 {
			return devinResponse(http.StatusUnauthorized, `{"token":"must-not-leak"}`), nil
		}
		return devinResponse(http.StatusOK, `{"results":[]}`), nil
	})}
	result, err := NewDevinProvider("token", client).Search(context.Background(), SearchRequest{Query: "q"})
	require.NoError(t, err)
	require.Empty(t, result.Results)
	require.Equal(t, []string{"server.codeium.com", "server.self-serve.windsurf.com"}, hosts)

	client = &http.Client{Transport: devinRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return devinResponse(http.StatusTooManyRequests, `{"error":"secret"}`), nil
	})}
	_, err = NewDevinProvider("token", client).Search(context.Background(), SearchRequest{Query: "q"})
	require.ErrorContains(t, err, "rate limit")
	require.NotContains(t, err.Error(), "secret")
}

func TestDevinProvider_SearchRejectsSecretsRedirectsAndOversizedResponses(t *testing.T) {
	client := &http.Client{Transport: devinRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return devinResponse(http.StatusOK, `{"results":[{"url":"https://example.com/token-secret","title":"token-secret","snippet":"token-secret"}]}`), nil
	})}
	result, err := NewDevinProvider("token-secret", client).Search(context.Background(), SearchRequest{Query: "q"})
	require.NoError(t, err)
	require.Empty(t, result.Results)

	client = &http.Client{Transport: devinRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return devinResponse(http.StatusFound, "redirect body"), nil
	})}
	_, err = NewDevinProvider("token", client).Search(context.Background(), SearchRequest{Query: "q"})
	require.ErrorContains(t, err, "status 302")
	require.NotContains(t, err.Error(), "redirect body")

	client = &http.Client{Transport: devinRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return devinResponse(http.StatusOK, strings.Repeat("x", maxResponseSize+1)), nil
	})}
	_, err = NewDevinProvider("token", client).Search(context.Background(), SearchRequest{Query: "q"})
	require.ErrorContains(t, err, "size limit")
}

func TestDevinProvider_SearchHonorsCancellationAndRejectsMalformedResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewDevinProvider("token", nil).Search(ctx, SearchRequest{Query: "q"})
	require.ErrorIs(t, err, context.Canceled)

	client := &http.Client{Transport: devinRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return devinResponse(http.StatusOK, `{"results":"not-an-array"}`), nil
	})}
	_, err = NewDevinProvider("token", client).Search(context.Background(), SearchRequest{Query: "q"})
	require.ErrorContains(t, err, "invalid search response")

	client = &http.Client{Transport: devinRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network detail")
	})}
	_, err = NewDevinProvider("token", client).Search(context.Background(), SearchRequest{Query: "q"})
	require.ErrorContains(t, err, "connection failed")
	require.NotContains(t, err.Error(), "network detail")
}
