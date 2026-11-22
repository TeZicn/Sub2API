//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateWebSearchConfig_AcceptsExa(t *testing.T) {
	cfg := &WebSearchEmulationConfig{
		Enabled:   true,
		Providers: []WebSearchProviderConfig{{Type: "exa", QuotaLimit: int64Ptr(1000)}},
	}
	require.NoError(t, validateWebSearchConfig(cfg))
}

func TestValidateWebSearchConfig_AcceptsDisconnectedDevinProvider(t *testing.T) {
	cfg := &WebSearchEmulationConfig{
		Enabled:   true,
		Providers: []WebSearchProviderConfig{{Type: "devin"}},
	}
	require.NoError(t, validateWebSearchConfig(cfg))
}
