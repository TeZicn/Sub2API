package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	deepSeekSearchToolName = "deepseek_web_search"
	smartSearchMaxQueries  = 3
	smartSearchMaxResults  = 5
	smartSearchMaxBody     = 16 << 20
)

type smartSearchDecisionContextKey struct{}

func markSmartSearchDecisionRequest(ctx context.Context) context.Context {
	return context.WithValue(ctx, smartSearchDecisionContextKey{}, true)
}

func shouldProcessDeepSeekWebSearch(ctx context.Context, body []byte) bool {
	if internal, _ := ctx.Value(smartSearchDecisionContextKey{}).(bool); internal {
		return false
	}
	return hasDeepSeekWebSearchTool(body)
}

type smartSearchCall struct {
	id      string
	queries []string
	results []websearch.SearchResult
	failed  bool
}

type smartSearchQueryResult struct {
	query   string
	results []websearch.SearchResult
	failed  bool
}

func removeDeepSeekWebSearchTools(body []byte) ([]byte, error) {
	return mutateDeepSeekWebSearchTools(body, false)
}

func replaceDeepSeekWebSearchTools(body []byte) ([]byte, error) {
	return mutateDeepSeekWebSearchTools(body, true)
}

func mutateDeepSeekWebSearchTools(body []byte, replace bool) ([]byte, error) {
	var request map[string]json.RawMessage
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("decode request: %w", err)
	}
	var tools []json.RawMessage
	if raw := request["tools"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &tools); err != nil {
			return nil, fmt.Errorf("decode tools: %w", err)
		}
	}
	filtered := make([]json.RawMessage, 0, len(tools))
	searchToolAdded := false
	for _, raw := range tools {
		var tool map[string]json.RawMessage
		if err := json.Unmarshal(raw, &tool); err != nil {
			filtered = append(filtered, raw)
			continue
		}
		if !isDeepSeekSearchTool(tool) {
			filtered = append(filtered, raw)
			continue
		}
		if !replace || searchToolAdded {
			continue
		}
		converted, err := json.Marshal(map[string]any{
			"name":        deepSeekSearchToolName,
			"description": "Search the public web when the user's question needs current information, external facts, or source verification. Do not use for greetings, common knowledge, or creative tasks. Use one focused query, with at most two additional queries in queries.",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":   map[string]any{"type": "string", "description": "The primary focused web search query."},
					"queries": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": smartSearchMaxQueries - 1, "description": "Optional additional focused queries."},
				},
				"required":             []string{"query"},
				"additionalProperties": false,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("encode search tool: %w", err)
		}
		filtered = append(filtered, converted)
		searchToolAdded = true
	}
	if replace && !searchToolAdded {
		return nil, errors.New("search tool was not found")
	}
	if len(filtered) == 0 {
		delete(request, "tools")
	} else {
		encoded, err := json.Marshal(filtered)
		if err != nil {
			return nil, fmt.Errorf("encode tools: %w", err)
		}
		request["tools"] = encoded
	}
	if !replace && toolChoiceTargetsDeepSeekSearch(request["tool_choice"]) {
		delete(request, "tool_choice")
	}
	updated, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	return updated, nil
}

func isDeepSeekSearchTool(tool map[string]json.RawMessage) bool {
	var name, toolType string
	_ = json.Unmarshal(tool["name"], &name)
	_ = json.Unmarshal(tool["type"], &toolType)
	return name == deepSeekSearchToolName || toolType == deepSeekSearchToolName
}

func toolChoiceTargetsDeepSeekSearch(raw json.RawMessage) bool {
	var choice map[string]json.RawMessage
	if json.Unmarshal(raw, &choice) != nil {
		var name string
		_ = json.Unmarshal(raw, &name)
		return name == deepSeekSearchToolName
	}
	var name string
	_ = json.Unmarshal(choice["name"], &name)
	return name == deepSeekSearchToolName
}

func (s *GatewayService) handleDeepSeekSmartSearch(
	ctx context.Context, c *gin.Context, account *Account, parsed *ParsedRequest,
) (*ForwardResult, error) {
	start := time.Now()
	originalBody := parsed.Body.Bytes()
	firstBody, err := replaceDeepSeekWebSearchTools(originalBody)
	if err != nil {
		return nil, fmt.Errorf("prepare search decision request: %w", err)
	}
	firstBody, err = sjson.SetBytes(firstBody, "stream", false)
	if err != nil {
		return nil, fmt.Errorf("disable internal search decision stream: %w", err)
	}
	firstParsed, err := parsed.CloneForBody(firstBody)
	if err != nil {
		return nil, err
	}
	firstParsed.OnUpstreamAccepted = parsed.OnUpstreamAccepted

	originalWriter := c.Writer
	originalParsed, hadParsed := c.Get("parsed_request")
	firstCapture := newSmartSearchCaptureWriter(originalWriter)
	c.Writer = firstCapture
	c.Set("parsed_request", firstParsed)
	firstResult, forwardErr := s.Forward(markSmartSearchDecisionRequest(ctx), c, account, firstParsed)
	c.Writer = originalWriter
	if hadParsed {
		c.Set("parsed_request", originalParsed)
	} else {
		c.Set("parsed_request", parsed)
	}
	if forwardErr != nil {
		if firstCapture.body.Len() > 0 && firstCapture.Status() >= http.StatusBadRequest {
			if writeErr := firstCapture.writeTo(c, false); writeErr != nil {
				return firstResult, errors.Join(forwardErr, writeErr)
			}
		}
		return firstResult, forwardErr
	}
	if firstResult == nil {
		return nil, errors.New("search decision returned no forward result")
	}

	message, calls, err := parseSmartSearchCalls(firstCapture.body.Bytes())
	if err != nil {
		return nil, fmt.Errorf("parse search decision response: %w", err)
	}
	if len(calls) == 0 {
		firstResult.Duration = time.Since(start)
		firstResult.Stream = parsed.Stream
		firstResult.FirstTokenMs = nil
		if err := firstCapture.writeTo(c, parsed.Stream); err != nil {
			return nil, err
		}
		if parsed.Stream {
			firstTokenMs := int(time.Since(start).Milliseconds())
			firstResult.FirstTokenMs = &firstTokenMs
		}
		return firstResult, nil
	}

	queries := uniqueSmartSearchQueries(calls)
	queryResults, err := s.executeSmartSearches(ctx, account, queries)
	if err != nil {
		return nil, err
	}
	queryByText := make(map[string]smartSearchQueryResult, len(queryResults))
	for _, result := range queryResults {
		queryByText[result.query] = result
	}
	for i := range calls {
		if len(calls[i].queries) == 0 {
			calls[i].failed = true
		}
		for _, query := range calls[i].queries {
			if result, ok := queryByText[query]; ok {
				calls[i].results = append(calls[i].results, result.results...)
				calls[i].failed = calls[i].failed || result.failed
			}
		}
	}
	if hasNonSearchToolUse(message.Content, calls) {
		partialBody, err := buildSmartSearchPartialResponse(firstCapture.body.Bytes(), calls, firstResult.Usage)
		if err != nil {
			return nil, err
		}
		if parsed.Stream {
			copySmartSearchHeaders(c.Writer, firstCapture.header)
			setSSEHeaders(c)
			err = writeSmartSearchMessageStream(c.Writer, partialBody)
		} else {
			copySmartSearchHeaders(c.Writer, firstCapture.header)
			c.Writer.WriteHeader(firstCapture.Status())
			_, err = c.Writer.Write(partialBody)
		}
		if err != nil {
			return firstResult, err
		}
		firstResult.Duration = time.Since(start)
		firstResult.Stream = parsed.Stream
		firstResult.SearchCount += len(queries)
		if parsed.Stream {
			firstTokenMs := int(firstResult.Duration.Milliseconds())
			firstResult.FirstTokenMs = &firstTokenMs
		}
		return firstResult, nil
	}

	finalBody, err := buildSmartSearchFollowup(originalBody, message.Content, calls)
	if err != nil {
		return nil, err
	}
	finalParsed, err := parsed.CloneForBody(finalBody)
	if err != nil {
		return nil, err
	}
	finalParsed.OnUpstreamAccepted = nil

	var finalResult *ForwardResult
	finalRequestStart := time.Since(start)
	if parsed.Stream {
		c.Writer = &smartSearchEvidenceWriter{ResponseWriter: originalWriter, calls: calls, firstUsage: firstResult.Usage}
		c.Set("parsed_request", finalParsed)
		finalResult, forwardErr = s.Forward(ctx, c, account, finalParsed)
		c.Writer = originalWriter
	} else {
		finalCapture := newSmartSearchCaptureWriter(originalWriter)
		c.Writer = finalCapture
		c.Set("parsed_request", finalParsed)
		finalResult, forwardErr = s.Forward(ctx, c, account, finalParsed)
		c.Writer = originalWriter
		if forwardErr == nil && finalResult != nil {
			finalResult.Usage = addClaudeUsage(firstResult.Usage, finalResult.Usage)
			if err := finalCapture.appendSearchBlocks(calls, finalResult.Usage); err != nil {
				return finalResult, err
			}
			forwardErr = finalCapture.writeTo(c, false)
		}
	}
	if hadParsed {
		c.Set("parsed_request", originalParsed)
	} else {
		c.Set("parsed_request", parsed)
	}
	if forwardErr != nil {
		return finalResult, forwardErr
	}
	if finalResult == nil {
		return nil, errors.New("search follow-up returned no forward result")
	}
	if parsed.Stream {
		finalResult.Usage = addClaudeUsage(firstResult.Usage, finalResult.Usage)
	}
	finalResult.SearchCount += len(queries)
	finalResult.Duration = time.Since(start)
	if parsed.Stream {
		finalTokenMs := int(finalRequestStart.Milliseconds())
		if finalResult.FirstTokenMs != nil {
			finalTokenMs += *finalResult.FirstTokenMs
		}
		finalResult.FirstTokenMs = &finalTokenMs
	}
	return finalResult, nil
}

type smartSearchAssistantResponse struct {
	Content []json.RawMessage `json:"content"`
}

type smartSearchContentBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func parseSmartSearchCalls(body []byte) (*smartSearchAssistantResponse, []smartSearchCall, error) {
	var response struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, nil, fmt.Errorf("decode message: %w", err)
	}
	message := &smartSearchAssistantResponse{Content: response.Content}
	calls := make([]smartSearchCall, 0, 1)
	for _, raw := range response.Content {
		var block smartSearchContentBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			continue
		}
		if block.Type != "tool_use" || block.Name != deepSeekSearchToolName {
			continue
		}
		if block.ID == "" || len(block.ID) > 128 {
			return nil, nil, errors.New("search tool call has an invalid id")
		}
		calls = append(calls, smartSearchCall{id: block.ID, queries: extractSmartSearchQueries(block.Input)})
	}
	return message, calls, nil
}

func extractSmartSearchQueries(input json.RawMessage) []string {
	var values struct {
		Query   string            `json:"query"`
		Queries []json.RawMessage `json:"queries"`
	}
	if json.Unmarshal(input, &values) != nil {
		return nil
	}
	queries := make([]string, 0, smartSearchMaxQueries)
	if query := normalizeSmartSearchQuery(values.Query); query != "" {
		queries = append(queries, query)
	}
	for _, raw := range values.Queries {
		var query string
		if json.Unmarshal(raw, &query) != nil {
			var object struct {
				Query string `json:"query"`
				Q     string `json:"q"`
			}
			if json.Unmarshal(raw, &object) == nil {
				query = object.Query
				if query == "" {
					query = object.Q
				}
			}
		}
		if normalized := normalizeSmartSearchQuery(query); normalized != "" {
			queries = uniqueQueryTexts(append(queries, normalized))
			if len(queries) >= smartSearchMaxQueries {
				break
			}
		}
	}
	return uniqueQueryTexts(queries)
}

func normalizeSmartSearchQuery(query string) string {
	query = strings.Join(strings.Fields(strings.TrimSpace(query)), " ")
	if query == "" {
		return ""
	}
	runes := []rune(query)
	if len(runes) > 1000 {
		query = string(runes[:1000])
	}
	return query
}

func uniqueQueryTexts(queries []string) []string {
	seen := make(map[string]struct{}, len(queries))
	result := make([]string, 0, min(len(queries), smartSearchMaxQueries))
	for _, query := range queries {
		key := strings.ToLower(query)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, query)
		if len(result) == smartSearchMaxQueries {
			break
		}
	}
	return result
}

func uniqueSmartSearchQueries(calls []smartSearchCall) []string {
	queries := make([]string, 0, smartSearchMaxQueries)
	for _, call := range calls {
		queries = append(queries, call.queries...)
	}
	return uniqueQueryTexts(queries)
}

func hasNonSearchToolUse(content []json.RawMessage, calls []smartSearchCall) bool {
	searchIDs := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		searchIDs[call.id] = struct{}{}
	}
	for _, raw := range content {
		var block smartSearchContentBlock
		if json.Unmarshal(raw, &block) != nil || block.Type != "tool_use" {
			continue
		}
		if _, isSearch := searchIDs[block.ID]; !isSearch {
			return true
		}
	}
	return false
}

func (s *GatewayService) executeSmartSearches(
	ctx context.Context, account *Account, queries []string,
) ([]smartSearchQueryResult, error) {
	if len(queries) == 0 {
		return nil, nil
	}
	mgr := getWebSearchManager()
	if mgr == nil {
		return nil, errors.New("web search manager is not configured")
	}
	proxyURL := resolveAccountProxyURL(account)
	results := make([]smartSearchQueryResult, len(queries))
	var wg sync.WaitGroup
	for i, query := range queries {
		wg.Add(1)
		go func(index int, currentQuery string) {
			defer wg.Done()
			response, _, err := mgr.SearchWithBestProvider(ctx, websearch.SearchRequest{
				Query: currentQuery, MaxResults: smartSearchMaxResults, ProxyURL: proxyURL,
			})
			results[index].query = currentQuery
			if err != nil {
				if ctx.Err() == nil {
					results[index].failed = true
					slog.Warn("web search request failed", "account_id", account.ID)
				}
				return
			}
			results[index].results = response.Results
		}(i, query)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func buildSmartSearchFollowup(body []byte, content []json.RawMessage, calls []smartSearchCall) ([]byte, error) {
	var request map[string]json.RawMessage
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("decode follow-up request: %w", err)
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(request["messages"], &messages); err != nil {
		return nil, fmt.Errorf("decode follow-up messages: %w", err)
	}
	assistantContent := make([]json.RawMessage, 0, len(content))
	searchIDs := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		searchIDs[call.id] = struct{}{}
	}
	for _, raw := range content {
		var block smartSearchContentBlock
		if json.Unmarshal(raw, &block) == nil && block.Type == "tool_use" {
			if _, isSearch := searchIDs[block.ID]; !isSearch {
				continue
			}
		}
		assistantContent = append(assistantContent, raw)
	}
	assistant, err := json.Marshal(map[string]any{"role": "assistant", "content": assistantContent})
	if err != nil {
		return nil, fmt.Errorf("encode assistant tool request: %w", err)
	}
	messages = append(messages, assistant)
	for _, call := range calls {
		result := map[string]any{
			"queries": call.queries,
			"results": smartSearchResultsForTool(call),
		}
		if call.failed {
			result["warning"] = "Some searches failed; no live evidence was returned for those queries."
		}
		resultJSON, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("encode search results: %w", err)
		}
		toolResult := map[string]any{
			"type": "tool_result", "tool_use_id": call.id, "content": string(resultJSON),
		}
		if call.failed && len(call.results) == 0 {
			toolResult["is_error"] = true
		}
		encoded, err := json.Marshal(map[string]any{"role": "user", "content": []any{toolResult}})
		if err != nil {
			return nil, fmt.Errorf("encode search tool result: %w", err)
		}
		messages = append(messages, encoded)
	}
	encodedMessages, err := json.Marshal(messages)
	if err != nil {
		return nil, fmt.Errorf("encode follow-up messages: %w", err)
	}
	request["messages"] = encodedMessages
	followupBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode follow-up request: %w", err)
	}
	followupBody, err = removeDeepSeekWebSearchTools(followupBody)
	if err != nil {
		return nil, err
	}
	if gjson.GetBytes(body, "tool_choice.type").String() == "tool" &&
		gjson.GetBytes(body, "tool_choice.name").String() == deepSeekSearchToolName {
		followupBody, err = sjson.SetBytes(followupBody, "tool_choice", map[string]string{"type": "auto"})
		if err != nil {
			return nil, fmt.Errorf("reset search tool choice: %w", err)
		}
	}
	return followupBody, nil
}

func smartSearchResultsForTool(call smartSearchCall) []map[string]any {
	results := make([]map[string]any, 0, len(call.results))
	for _, result := range call.results {
		results = append(results, map[string]any{
			"title": result.Title, "url": result.URL, "snippet": result.Snippet,
			"page_age": result.PageAge,
		})
	}
	return results
}

func (w *smartSearchCaptureWriter) appendSearchBlocks(calls []smartSearchCall, usage ClaudeUsage) error {
	var message map[string]any
	if err := json.Unmarshal(w.body.Bytes(), &message); err != nil {
		return fmt.Errorf("decode final response: %w", err)
	}
	content, ok := message["content"].([]any)
	if !ok {
		content = make([]any, 0)
	}
	content = append(content, buildSmartSearchResponseBlocks(calls)...)
	message["content"] = content
	mergeClaudeUsageIntoResponse(message, usage)
	updated, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode final response: %w", err)
	}
	w.body.Reset()
	_, _ = w.body.Write(updated)
	w.header.Del("Content-Length")
	return nil
}

func buildSmartSearchPartialResponse(body []byte, calls []smartSearchCall, usage ClaudeUsage) ([]byte, error) {
	var message map[string]any
	if err := json.Unmarshal(body, &message); err != nil {
		return nil, fmt.Errorf("decode partial response: %w", err)
	}
	content, ok := message["content"].([]any)
	if !ok {
		return nil, errors.New("partial response content is invalid")
	}
	searchIDs := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		searchIDs[call.id] = struct{}{}
	}
	filtered := make([]any, 0, len(content)+len(calls)*2)
	for _, item := range content {
		block, ok := item.(map[string]any)
		if ok && block["type"] == "tool_use" {
			if id, ok := block["id"].(string); ok {
				if _, isSearch := searchIDs[id]; isSearch {
					continue
				}
			}
		}
		filtered = append(filtered, item)
	}
	message["content"] = append(filtered, buildSmartSearchResponseBlocks(calls)...)
	mergeClaudeUsageIntoResponse(message, usage)
	encoded, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("encode partial response: %w", err)
	}
	return encoded, nil
}

func mergeClaudeUsageIntoResponse(message map[string]any, usage ClaudeUsage) {
	usageMap, ok := message["usage"].(map[string]any)
	if !ok {
		usageMap = make(map[string]any)
		message["usage"] = usageMap
	}
	usageMap["input_tokens"] = usage.InputTokens
	usageMap["output_tokens"] = usage.OutputTokens
	if usage.CacheCreationInputTokens != 0 || usageMap["cache_creation_input_tokens"] != nil {
		usageMap["cache_creation_input_tokens"] = usage.CacheCreationInputTokens
	}
	if usage.CacheReadInputTokens != 0 || usageMap["cache_read_input_tokens"] != nil {
		usageMap["cache_read_input_tokens"] = usage.CacheReadInputTokens
	}
	mergeClaudeCacheCreation(usageMap, usage)
	if usage.ImageOutputTokens != 0 || usageMap["image_output_tokens"] != nil {
		usageMap["image_output_tokens"] = usage.ImageOutputTokens
	}
}

func mergeClaudeCacheCreation(usageMap map[string]any, usage ClaudeUsage) {
	cache, ok := usageMap["cache_creation"].(map[string]any)
	if !ok && usage.CacheCreation5mTokens == 0 && usage.CacheCreation1hTokens == 0 {
		return
	}
	if !ok {
		cache = make(map[string]any)
		usageMap["cache_creation"] = cache
	}
	if usage.CacheCreation5mTokens != 0 || cache["ephemeral_5m_input_tokens"] != nil {
		cache["ephemeral_5m_input_tokens"] = usage.CacheCreation5mTokens
	}
	if usage.CacheCreation1hTokens != 0 || cache["ephemeral_1h_input_tokens"] != nil {
		cache["ephemeral_1h_input_tokens"] = usage.CacheCreation1hTokens
	}
}

func addUsageNumber(usage map[string]any, field string, amount int) {
	if amount == 0 {
		return
	}
	current, _ := usage[field].(float64)
	usage[field] = current + float64(amount)
}

func mergeClaudeInputUsage(usageMap map[string]any, usage ClaudeUsage) {
	addUsageNumber(usageMap, "input_tokens", usage.InputTokens)
	addUsageNumber(usageMap, "cache_creation_input_tokens", usage.CacheCreationInputTokens)
	addUsageNumber(usageMap, "cache_read_input_tokens", usage.CacheReadInputTokens)
	cache, ok := usageMap["cache_creation"].(map[string]any)
	if !ok && (usage.CacheCreation5mTokens != 0 || usage.CacheCreation1hTokens != 0) {
		cache = make(map[string]any)
		usageMap["cache_creation"] = cache
	}
	if cache != nil {
		addUsageNumber(cache, "ephemeral_5m_input_tokens", usage.CacheCreation5mTokens)
		addUsageNumber(cache, "ephemeral_1h_input_tokens", usage.CacheCreation1hTokens)
	}
}

func mergeClaudeOutputUsage(usageMap map[string]any, usage ClaudeUsage) {
	addUsageNumber(usageMap, "output_tokens", usage.OutputTokens)
	addUsageNumber(usageMap, "image_output_tokens", usage.ImageOutputTokens)
}

func copySmartSearchHeaders(target gin.ResponseWriter, headers http.Header) {
	for key, values := range headers {
		target.Header()[key] = append([]string(nil), values...)
	}
}

func buildSmartSearchResponseBlocks(calls []smartSearchCall) []any {
	blocks := make([]any, 0, len(calls)*2)
	for _, call := range calls {
		id := webSearchToolUseIDPrefix + uuid.NewString()[:16]
		queryLabel := strings.Join(call.queries, "; ")
		blocks = append(blocks,
			map[string]any{"type": "server_tool_use", "id": id, "name": toolNameWebSearch, "input": map[string]string{"query": queryLabel}},
			map[string]any{"type": "web_search_tool_result", "tool_use_id": id, "content": buildSearchResultBlocks(call.results)},
		)
	}
	return blocks
}

func addClaudeUsage(first, final ClaudeUsage) ClaudeUsage {
	final.InputTokens += first.InputTokens
	final.OutputTokens += first.OutputTokens
	final.CacheCreationInputTokens += first.CacheCreationInputTokens
	final.CacheReadInputTokens += first.CacheReadInputTokens
	final.CacheCreation5mTokens += first.CacheCreation5mTokens
	final.CacheCreation1hTokens += first.CacheCreation1hTokens
	final.ImageOutputTokens += first.ImageOutputTokens
	final.cacheCreationInputTokensPresent = final.cacheCreationInputTokensPresent || first.cacheCreationInputTokensPresent
	final.cacheCreationInputTokensFromDetails = final.cacheCreationInputTokensFromDetails || first.cacheCreationInputTokensFromDetails
	final.promptTotalTokens += first.promptTotalTokens
	final.promptTotalPresent = final.promptTotalPresent || first.promptTotalPresent
	return final
}

type smartSearchCaptureWriter struct {
	gin.ResponseWriter
	header http.Header
	status int
	body   bytes.Buffer
}

func newSmartSearchCaptureWriter(underlying gin.ResponseWriter) *smartSearchCaptureWriter {
	return &smartSearchCaptureWriter{ResponseWriter: underlying, header: make(http.Header)}
}

func (w *smartSearchCaptureWriter) Header() http.Header { return w.header }
func (w *smartSearchCaptureWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *smartSearchCaptureWriter) WriteHeaderNow() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
}
func (w *smartSearchCaptureWriter) Write(data []byte) (int, error) {
	if w.body.Len()+len(data) > smartSearchMaxBody {
		return 0, errors.New("internal search response exceeded size limit")
	}
	w.WriteHeaderNow()
	return w.body.Write(data)
}
func (w *smartSearchCaptureWriter) WriteString(value string) (int, error) {
	return w.Write([]byte(value))
}
func (w *smartSearchCaptureWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}
func (w *smartSearchCaptureWriter) Size() int     { return w.body.Len() }
func (w *smartSearchCaptureWriter) Written() bool { return w.status != 0 || w.body.Len() > 0 }
func (w *smartSearchCaptureWriter) Flush()        {}

func (w *smartSearchCaptureWriter) writeTo(c *gin.Context, asStream bool) error {
	if asStream && w.Status() >= http.StatusOK && w.Status() < http.StatusMultipleChoices {
		setSSEHeaders(c)
		return writeSmartSearchMessageStream(c.Writer, w.body.Bytes())
	}
	for key, values := range w.header {
		c.Writer.Header()[key] = append([]string(nil), values...)
	}
	status := w.Status()
	if status == 0 {
		status = http.StatusOK
	}
	c.Writer.WriteHeader(status)
	_, err := c.Writer.Write(w.body.Bytes())
	return err
}

func writeSmartSearchMessageStream(w gin.ResponseWriter, body []byte) error {
	var message map[string]any
	if err := json.Unmarshal(body, &message); err != nil {
		return fmt.Errorf("decode buffered message: %w", err)
	}
	startMessage := make(map[string]any, len(message))
	for key, value := range message {
		startMessage[key] = value
	}
	startMessage["content"] = []any{}
	startMessage["stop_reason"] = nil
	startMessage["stop_sequence"] = nil
	usage, _ := message["usage"].(map[string]any)
	startUsage := make(map[string]any, len(usage))
	for key, value := range usage {
		startUsage[key] = value
	}
	startUsage["output_tokens"] = 0
	startMessage["usage"] = startUsage
	if err := writeSmartSearchSSE(w, "message_start", map[string]any{"type": "message_start", "message": startMessage}); err != nil {
		return err
	}
	content, _ := message["content"].([]any)
	for index, rawBlock := range content {
		block, ok := rawBlock.(map[string]any)
		if !ok {
			continue
		}
		if err := writeSmartSearchContentBlock(w, index, block); err != nil {
			return err
		}
	}
	delta := map[string]any{"stop_reason": message["stop_reason"], "stop_sequence": message["stop_sequence"]}
	if err := writeSmartSearchSSE(w, "message_delta", map[string]any{"type": "message_delta", "delta": delta, "usage": usage}); err != nil {
		return err
	}
	return writeSmartSearchSSE(w, "message_stop", map[string]string{"type": "message_stop"})
}

func writeSmartSearchContentBlock(w gin.ResponseWriter, index int, block map[string]any) error {
	kind, _ := block["type"].(string)
	initial := make(map[string]any, len(block))
	for key, value := range block {
		initial[key] = value
	}
	switch kind {
	case "text":
		text, _ := block["text"].(string)
		initial["text"] = ""
		if err := writeSmartSearchBlockStart(w, index, initial); err != nil {
			return err
		}
		if text != "" {
			if err := writeSmartSearchSSE(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "text_delta", "text": text}}); err != nil {
				return err
			}
		}
	case "thinking":
		thinking, _ := block["thinking"].(string)
		signature, _ := block["signature"].(string)
		initial["thinking"] = ""
		delete(initial, "signature")
		if err := writeSmartSearchBlockStart(w, index, initial); err != nil {
			return err
		}
		if thinking != "" {
			if err := writeSmartSearchSSE(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "thinking_delta", "thinking": thinking}}); err != nil {
				return err
			}
		}
		if signature != "" {
			if err := writeSmartSearchSSE(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "signature_delta", "signature": signature}}); err != nil {
				return err
			}
		}
	case "tool_use":
		input := block["input"]
		initial["input"] = map[string]any{}
		if err := writeSmartSearchBlockStart(w, index, initial); err != nil {
			return err
		}
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		if err := writeSmartSearchSSE(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(encoded)}}); err != nil {
			return err
		}
	default:
		if err := writeSmartSearchBlockStart(w, index, initial); err != nil {
			return err
		}
	}
	return writeSmartSearchSSE(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
}

func writeSmartSearchBlockStart(w gin.ResponseWriter, index int, block map[string]any) error {
	return writeSmartSearchSSE(w, "content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": block})
}

func writeSmartSearchSSE(w gin.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	if err == nil {
		w.Flush()
	}
	return err
}

type smartSearchEvidenceWriter struct {
	gin.ResponseWriter
	calls      []smartSearchCall
	firstUsage ClaudeUsage
	pending    []byte
	nextIndex  int
	injected   bool
}

func (w *smartSearchEvidenceWriter) Write(data []byte) (int, error) {
	w.pending = append(w.pending, data...)
	for {
		index := bytes.Index(w.pending, []byte("\n\n"))
		if index < 0 {
			break
		}
		frame := append([]byte(nil), w.pending[:index+2]...)
		w.pending = w.pending[index+2:]
		if err := w.writeFrame(frame); err != nil {
			return 0, err
		}
	}
	return len(data), nil
}

func (w *smartSearchEvidenceWriter) WriteString(value string) (int, error) {
	return w.Write([]byte(value))
}

func (w *smartSearchEvidenceWriter) Flush() {
	if len(w.pending) > 0 {
		_, _ = w.ResponseWriter.Write(w.pending)
		w.pending = nil
	}
	w.ResponseWriter.Flush()
}

func (w *smartSearchEvidenceWriter) writeFrame(frame []byte) error {
	var event struct {
		Type  string `json:"type"`
		Index int    `json:"index"`
	}
	data := sseData(frame)
	outputFrame := frame
	if len(data) > 0 {
		_ = json.Unmarshal(data, &event)
		if event.Type == "message_start" || event.Type == "message_delta" {
			var payload map[string]any
			if json.Unmarshal(data, &payload) == nil {
				if event.Type == "message_start" {
					message, _ := payload["message"].(map[string]any)
					usage, _ := message["usage"].(map[string]any)
					if usage != nil {
						mergeClaudeInputUsage(usage, w.firstUsage)
					}
				} else {
					usage, _ := payload["usage"].(map[string]any)
					if usage != nil {
						mergeClaudeOutputUsage(usage, w.firstUsage)
					}
				}
				encoded, err := json.Marshal(payload)
				if err != nil {
					return err
				}
				outputFrame = replaceSSEData(frame, encoded)
			}
		}
	}
	if event.Type == "content_block_start" && event.Index >= w.nextIndex {
		w.nextIndex = event.Index + 1
	}
	if !w.injected && (event.Type == "message_delta" || event.Type == "message_stop") {
		if err := w.writeEvidence(); err != nil {
			return err
		}
		w.injected = true
	}
	_, err := w.ResponseWriter.Write(outputFrame)
	return err
}

func replaceSSEData(frame, data []byte) []byte {
	var updated bytes.Buffer
	for _, line := range bytes.Split(frame, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("data:")) {
			updated.WriteString("data: ")
			updated.Write(data)
			updated.WriteByte('\n')
			continue
		}
		updated.Write(line)
		updated.WriteByte('\n')
	}
	return updated.Bytes()
}

func (w *smartSearchEvidenceWriter) writeEvidence() error {
	for _, block := range buildSmartSearchResponseBlocks(w.calls) {
		contentBlock, _ := block.(map[string]any)
		index := w.nextIndex
		w.nextIndex++
		if err := writeSmartSearchSSE(w.ResponseWriter, "content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": contentBlock}); err != nil {
			return err
		}
		if err := writeSmartSearchSSE(w.ResponseWriter, "content_block_stop", map[string]any{"type": "content_block_stop", "index": index}); err != nil {
			return err
		}
	}
	return nil
}

func sseData(frame []byte) []byte {
	var data []byte
	for _, line := range bytes.Split(frame, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if bytes.HasPrefix(line, []byte("data:")) {
			value := bytes.TrimPrefix(line, []byte("data:"))
			value = bytes.TrimPrefix(value, []byte(" "))
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, value...)
		}
	}
	return data
}

var _ io.StringWriter = (*smartSearchCaptureWriter)(nil)
var _ io.StringWriter = (*smartSearchEvidenceWriter)(nil)
