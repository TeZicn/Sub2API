//go:build unit

package service

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestReplaceAndRemoveDeepSeekWebSearchTool(t *testing.T) {
	request := []byte(`{"tools":[{"type":"deepseek_web_search","name":"deepseek_web_search"},{"type":"function","name":"lookup","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"deepseek_web_search"}}`)

	replaced, err := replaceDeepSeekWebSearchTools(request)
	require.NoError(t, err)
	var mapped map[string]any
	require.NoError(t, json.Unmarshal(replaced, &mapped))
	tools := mapped["tools"].([]any)
	require.Len(t, tools, 2)
	search := tools[0].(map[string]any)
	require.Equal(t, deepSeekSearchToolName, search["name"])
	require.Equal(t, "lookup", tools[1].(map[string]any)["name"])
	require.Equal(t, deepSeekSearchToolName, gjson.GetBytes(replaced, "tool_choice.name").String())

	removed, err := removeDeepSeekWebSearchTools(request)
	require.NoError(t, err)
	require.Equal(t, "lookup", gjsonString(removed, "tools.0.name"))
	require.False(t, gjsonExists(removed, "tools.1"))
	require.False(t, gjsonExists(removed, "tool_choice"))
}

func TestSmartSearchDecisionRequestDoesNotReenterSearchInterceptor(t *testing.T) {
	body := []byte(`{"tools":[{"name":"deepseek_web_search"}]}`)
	require.True(t, shouldProcessDeepSeekWebSearch(t.Context(), body))
	require.False(t, shouldProcessDeepSeekWebSearch(markSmartSearchDecisionRequest(t.Context()), body))
}

func TestShouldEmulateDeepSeekWebSearchRespectsSwitchesAndToolChoiceNone(t *testing.T) {
	SetWebSearchManager(websearch.NewManager([]websearch.ProviderConfig{{Type: websearch.ProviderTypeDevin, APIKey: "session"}}, nil))
	setGlobalWebSearchConfig(&WebSearchEmulationConfig{
		Enabled:   true,
		Providers: []WebSearchProviderConfig{{Type: websearch.ProviderTypeDevin, APIKey: "session"}},
	})
	t.Cleanup(func() {
		SetWebSearchManager(nil)
		clearGlobalWebSearchConfig()
	})

	setting := &SettingService{}
	account := newAnthropicAPIKeyAccount(WebSearchModeEnabled)
	request := []byte(`{"tools":[{"type":"deepseek_web_search"}]}`)
	require.True(t, shouldEmulateDeepSeekWebSearchFor(t.Context(), setting, nil, account, nil, request))
	require.False(t, shouldEmulateDeepSeekWebSearchFor(t.Context(), setting, nil, account, nil,
		[]byte(`{"tools":[{"type":"deepseek_web_search"}],"tool_choice":"none"}`)))

	account = newAnthropicAPIKeyAccount(WebSearchModeDisabled)
	require.False(t, shouldEmulateDeepSeekWebSearchFor(t.Context(), setting, nil, account, nil, request))

	setGlobalWebSearchConfig(&WebSearchEmulationConfig{Enabled: false})
	account = newAnthropicAPIKeyAccount(WebSearchModeEnabled)
	require.False(t, shouldEmulateDeepSeekWebSearchFor(t.Context(), setting, nil, account, nil, request))
}

func TestParseSmartSearchCallsBoundsAndNormalizesQueries(t *testing.T) {
	body := []byte(`{"content":[{"type":"tool_use","id":"toolu_search","name":"deepseek_web_search","input":{"query":"  Today's news  ","queries":["today's news","weather","markets","ignored"]}}]}`)
	_, calls, err := parseSmartSearchCalls(body)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	require.Equal(t, []string{"Today's news", "weather", "markets"}, calls[0].queries)

	_, _, err = parseSmartSearchCalls([]byte(`{"content":[{"type":"tool_use","id":"","name":"deepseek_web_search","input":{"query":"x"}}]}`))
	require.ErrorContains(t, err, "invalid id")

	long := strings.Repeat("你", 1100)
	require.Len(t, []rune(normalizeSmartSearchQuery(long)), 1000)
}

func TestBuildSmartSearchFollowupAndPartialResponse(t *testing.T) {
	original := []byte(`{"tools":[{"type":"deepseek_web_search"}],"tool_choice":{"type":"tool","name":"deepseek_web_search"},"messages":[{"role":"user","content":"latest news"}]}`)
	content := []json.RawMessage{
		json.RawMessage(`{"type":"thinking","thinking":"Need current sources."}`),
		json.RawMessage(`{"type":"tool_use","id":"toolu_search","name":"deepseek_web_search","input":{"query":"latest news"}}`),
		json.RawMessage(`{"type":"text","text":"Searching."}`),
	}
	calls := []smartSearchCall{{
		id: "toolu_search", queries: []string{"latest news"},
		results: []websearch.SearchResult{{Title: "Example", URL: "https://example.com", Snippet: "Evidence"}},
	}}

	followup, err := buildSmartSearchFollowup(original, content, calls)
	require.NoError(t, err)
	require.Equal(t, "auto", gjsonString(followup, "tool_choice.type"))
	require.False(t, gjsonExists(followup, "tools.0"))
	require.Equal(t, "assistant", gjsonString(followup, "messages.1.role"))
	require.Equal(t, 3, gjsonLength(followup, "messages.1.content"))
	require.Equal(t, "tool_use", gjsonString(followup, "messages.1.content.1.type"))
	require.Equal(t, "toolu_search", gjsonString(followup, "messages.1.content.1.id"))
	require.Equal(t, "user", gjsonString(followup, "messages.2.role"))
	require.Equal(t, "toolu_search", gjsonString(followup, "messages.2.content.0.tool_use_id"))
	require.Contains(t, gjsonString(followup, "messages.2.content.0.content"), "https://example.com")
	require.NotContains(t, string(followup), "authority")
	require.NotContains(t, string(followup), "rerank_score")

	response := []byte(`{"id":"msg_1","model":"claude-test","stop_reason":"tool_use","content":[{"type":"text","text":"I found a source."},{"type":"tool_use","id":"toolu_search","name":"deepseek_web_search","input":{"query":"latest news"}},{"type":"tool_use","id":"toolu_client","name":"lookup","input":{"id":1}}],"usage":{"input_tokens":7,"output_tokens":4}}`)
	message, parsedCalls, err := parseSmartSearchCalls(response)
	require.NoError(t, err)
	require.True(t, hasNonSearchToolUse(message.Content, parsedCalls))
	partial, err := buildSmartSearchPartialResponse(response, calls, ClaudeUsage{InputTokens: 12, OutputTokens: 9})
	require.NoError(t, err)
	require.Equal(t, 12, gjsonInt(partial, "usage.input_tokens"))
	require.Equal(t, 9, gjsonInt(partial, "usage.output_tokens"))
	require.Equal(t, "lookup", gjsonString(partial, "content.1.name"))
	require.Equal(t, "server_tool_use", gjsonString(partial, "content.2.type"))
	require.NotEqual(t, deepSeekSearchToolName, gjsonString(partial, "content.1.name"))
}

func TestSmartSearchEvidenceWriterAddsSourcesAndFirstCallUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	writer := &smartSearchEvidenceWriter{
		ResponseWriter: ctx.Writer,
		firstUsage:     ClaudeUsage{InputTokens: 3, OutputTokens: 2, CacheReadInputTokens: 1},
		calls: []smartSearchCall{{
			queries: []string{"latest news"},
			results: []websearch.SearchResult{{Title: "Example", URL: "https://example.com"}},
		}},
	}

	require.NoError(t, writer.writeFrame([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"cache_read_input_tokens\":4}}}\n\n")))
	require.NoError(t, writer.writeFrame([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n")))
	require.NoError(t, writer.writeFrame([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":8}}\n\n")))

	output := recorder.Body.String()
	require.Contains(t, output, `"input_tokens":13`)
	require.Contains(t, output, `"cache_read_input_tokens":5`)
	require.Contains(t, output, `"output_tokens":10`)
	require.Contains(t, output, `"index":1`)
	require.Contains(t, output, `"index":2`)
	require.Contains(t, output, "https://example.com")
}

func gjsonString(body []byte, path string) string {
	return gjson.GetBytes(body, path).String()
}

func gjsonExists(body []byte, path string) bool {
	return gjson.GetBytes(body, path).Exists()
}

func gjsonInt(body []byte, path string) int {
	return int(gjson.GetBytes(body, path).Int())
}

func gjsonLength(body []byte, path string) int {
	return len(gjson.GetBytes(body, path).Array())
}
