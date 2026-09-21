package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

const (
	requestLogSchemaVersion       = 1
	requestLogMaxConversationSize = 2 << 20
	requestLogMaxAssistantRunes   = 5000
	requestLogMaxResponseCapture  = 4 << 20
	requestLogMaxSSELineSize      = 512 << 10
)

type RequestLogPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"`
}

type RequestLogMessage struct {
	Role         string           `json:"role"`
	OriginalRole string           `json:"original_role,omitempty"`
	Parts        []RequestLogPart `json:"parts"`
	Source       string           `json:"source"`
	ChoiceIndex  *int             `json:"choice_index,omitempty"`
	Partial      bool             `json:"partial,omitempty"`
	Truncated    bool             `json:"truncated,omitempty"`
}

type requestLogResponse struct {
	parts      []RequestLogPart
	textRunes  int
	truncated  bool
	toolEvents map[string]struct{}
}

type RequestLogCapture struct {
	mu sync.Mutex

	protocol          string
	requestMessages   []RequestLogMessage
	responses         map[int]*requestLogResponse
	toolNames         map[string]struct{}
	blockTypes        map[int]string
	blockNames        map[int]string
	historyComplete   bool
	historyReference  string
	startedAt         time.Time
	isStream          bool
	streamCompleted   bool
	streamParseFailed bool
	sseLine           []byte
	skipSSELine       bool
	responseBody      bytes.Buffer
	responseOverflow  bool
	responseModel     string
	writer            *requestLogResponseWriter
	info              *relaycommon.RelayInfo
}

type requestLogResponseWriter struct {
	gin.ResponseWriter
	capture *RequestLogCapture
}

func (w *requestLogResponseWriter) Write(data []byte) (int, error) {
	w.capture.observeWrite(data)
	return w.ResponseWriter.Write(data)
}

func (w *requestLogResponseWriter) WriteString(data string) (int, error) {
	w.capture.observeWrite([]byte(data))
	return w.ResponseWriter.WriteString(data)
}

func BeginRequestLogCapture(c *gin.Context, relayFormat relaytypes.RelayFormat, request dto.Request, info *relaycommon.RelayInfo) *RequestLogCapture {
	if c == nil || info == nil || request == nil || !common.RequestLogEnabled {
		return nil
	}

	protocol := ""
	switch {
	case relayFormat == relaytypes.RelayFormatOpenAI && info.RelayMode == relayconstant.RelayModeChatCompletions:
		protocol = "chat_completions"
	case relayFormat == relaytypes.RelayFormatOpenAIResponses && info.RelayMode == relayconstant.RelayModeResponses:
		protocol = "responses"
	case relayFormat == relaytypes.RelayFormatClaude:
		protocol = "claude"
	case relayFormat == relaytypes.RelayFormatGemini && !strings.Contains(c.Request.URL.Path, "embed"):
		protocol = "gemini"
	default:
		return nil
	}

	capture := &RequestLogCapture{
		protocol:        protocol,
		responses:       map[int]*requestLogResponse{},
		toolNames:       map[string]struct{}{},
		blockTypes:      map[int]string{},
		blockNames:      map[int]string{},
		historyComplete: true,
		startedAt:       info.StartTime,
		isStream:        info.IsStream,
		info:            info,
	}
	if capture.startedAt.IsZero() {
		capture.startedAt = time.Now()
	}
	if err := capture.normalizeRequest(request); err != nil {
		common.SysLog(fmt.Sprintf("request log capture skipped: request_id=%s reason=request_normalize_failed error=%v", info.RequestId, err))
		return nil
	}

	writer := &requestLogResponseWriter{ResponseWriter: c.Writer, capture: capture}
	capture.writer = writer
	c.Writer = writer
	return capture
}

func (capture *RequestLogCapture) Finish(c *gin.Context, relayErr *relaytypes.NewAPIError) {
	if capture == nil || c == nil {
		return
	}

	capture.mu.Lock()
	if capture.isStream && len(capture.sseLine) > 0 && !capture.skipSSELine {
		capture.observeSSELine(capture.sseLine)
	}
	if !capture.isStream && capture.responseBody.Len() > 0 {
		capture.observeResponseJSON(capture.responseBody.Bytes(), false)
	}
	messages := make([]RequestLogMessage, 0, len(capture.requestMessages)+len(capture.responses))
	messages = append(messages, capture.requestMessages...)
	responseIndexes := make([]int, 0, len(capture.responses))
	for index := range capture.responses {
		responseIndexes = append(responseIndexes, index)
	}
	slices.Sort(responseIndexes)
	for _, index := range responseIndexes {
		messages = append(messages, capture.responseMessage(index, capture.responses[index]))
	}
	capture.mu.Unlock()

	conversation, truncated, err := marshalRequestLogConversation(messages)
	if err != nil {
		common.SysLog(fmt.Sprintf("request log capture dropped: request_id=%s reason=conversation_marshal_failed error=%v", capture.info.RequestId, err))
		return
	}

	toolNames := make([]string, 0, len(capture.toolNames))
	for name := range capture.toolNames {
		toolNames = append(toolNames, name)
	}
	toolNamesJSON, err := common.Marshal(toolNames)
	if err != nil {
		toolNamesJSON = []byte("[]")
	}
	channelIdsJSON, err := common.Marshal(c.GetStringSlice("use_channel"))
	if err != nil {
		channelIdsJSON = []byte("[]")
	}

	httpStatus := capture.writer.Status()
	if httpStatus == 0 {
		httpStatus = 200
	}
	captureStatus := "success"
	partial := capture.responseOverflow || capture.streamParseFailed
	if relayErr != nil || httpStatus >= 400 {
		captureStatus = "failed"
	}
	streamIncomplete := capture.isStream && !capture.streamCompleted
	if streamIncomplete {
		partial = true
		if captureStatus == "success" {
			captureStatus = "incomplete"
		}
	}
	// Streaming clients often close the socket right after the last chunk.
	// That cancels the request context in Finish(), but it is not a disconnect
	// when the stream already completed or the request was not streaming.
	if requestErr := c.Request.Context().Err(); requestErr != nil && streamIncomplete {
		partial = true
		if errors.Is(requestErr, context.DeadlineExceeded) {
			captureStatus = "timeout"
		} else {
			captureStatus = "client_disconnected"
		}
	}

	errorCode := ""
	if relayErr != nil {
		errorCode = string(relayErr.GetErrorCode())
	}
	responseModel := capture.responseModel
	if responseModel == "" && capture.info.ResponseModel != nil {
		responseModel = capture.info.ResponseModel.ReturnedModel
	}
	createdAt := capture.startedAt.Unix()
	log := &model.RequestLog{
		RequestId:        capture.info.RequestId,
		UserId:           capture.info.UserId,
		Username:         c.GetString("username"),
		TokenId:          capture.info.TokenId,
		TokenName:        c.GetString("token_name"),
		Protocol:         capture.protocol,
		ModelName:        capture.info.OriginModelName,
		ResponseModel:    responseModel,
		Status:           captureStatus,
		HttpStatus:       httpStatus,
		IsStream:         capture.isStream,
		AttemptCount:     len(c.GetStringSlice("use_channel")),
		ChannelIds:       model.JSONValue(channelIdsJSON),
		Conversation:     model.JSONValue(conversation),
		ToolNames:        model.JSONValue(toolNamesJSON),
		HistoryComplete:  capture.historyComplete,
		HistoryReference: capture.historyReference,
		CaptureStatus:    captureStatus,
		RecordTruncated:  truncated || capture.responseOverflow,
		Partial:          partial,
		SchemaVersion:    requestLogSchemaVersion,
		ErrorCode:        errorCode,
		CreatedAt:        createdAt,
		DurationMs:       time.Since(capture.startedAt).Milliseconds(),
		PayloadBytes:     int64(len(conversation) + len(toolNamesJSON) + len(channelIdsJSON)),
	}
	EnqueueRequestLog(log)
}

func (capture *RequestLogCapture) responseMessage(index int, response *requestLogResponse) RequestLogMessage {
	choiceIndex := index
	return RequestLogMessage{
		Role:        "assistant",
		Parts:       response.parts,
		Source:      "response",
		ChoiceIndex: &choiceIndex,
		Partial:     capture.isStream && !capture.streamCompleted,
		Truncated:   response.truncated,
	}
}

func (capture *RequestLogCapture) observeWrite(data []byte) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.isStream {
		capture.observeSSEBytes(data)
		return
	}
	remaining := requestLogMaxResponseCapture - capture.responseBody.Len()
	if remaining <= 0 {
		capture.responseOverflow = true
		return
	}
	if len(data) > remaining {
		capture.responseBody.Write(data[:remaining])
		capture.responseOverflow = true
		return
	}
	capture.responseBody.Write(data)
}

func (capture *RequestLogCapture) observeSSEBytes(data []byte) {
	for len(data) > 0 {
		newline := bytes.IndexByte(data, '\n')
		if newline < 0 {
			if capture.skipSSELine {
				return
			}
			if len(capture.sseLine)+len(data) > requestLogMaxSSELineSize {
				capture.sseLine = capture.sseLine[:0]
				capture.skipSSELine = true
				capture.streamParseFailed = true
				return
			}
			capture.sseLine = append(capture.sseLine, data...)
			return
		}

		part := data[:newline]
		data = data[newline+1:]
		if capture.skipSSELine {
			capture.skipSSELine = false
			continue
		}
		if len(capture.sseLine)+len(part) > requestLogMaxSSELineSize {
			capture.sseLine = capture.sseLine[:0]
			capture.streamParseFailed = true
			continue
		}
		capture.sseLine = append(capture.sseLine, part...)
		capture.observeSSELine(capture.sseLine)
		capture.sseLine = capture.sseLine[:0]
	}
}

func (capture *RequestLogCapture) observeSSELine(line []byte) {
	line = bytes.TrimSpace(line)
	if !bytes.HasPrefix(line, []byte("data:")) {
		return
	}
	data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
	if bytes.Equal(data, []byte("[DONE]")) {
		capture.streamCompleted = true
		return
	}
	if len(data) == 0 {
		return
	}
	capture.observeResponseJSON(data, true)
}

func (capture *RequestLogCapture) observeResponseJSON(data []byte, streaming bool) {
	var root map[string]any
	if err := common.Unmarshal(data, &root); err != nil {
		capture.streamParseFailed = capture.streamParseFailed || streaming
		return
	}
	if modelName, ok := root["model"].(string); ok && modelName != "" {
		capture.responseModel = modelName
	}
	switch capture.protocol {
	case "chat_completions":
		capture.observeChatResponse(root)
	case "responses":
		capture.observeResponsesResponse(root, streaming)
	case "claude":
		capture.observeClaudeResponse(root, streaming)
	case "gemini":
		capture.observeGeminiResponse(root)
	}
}

func (capture *RequestLogCapture) observeChatResponse(root map[string]any) {
	choices, _ := root["choices"].([]any)
	for position, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]any)
		if !ok {
			continue
		}
		index := intValue(choice["index"], position)
		message, _ := choice["message"].(map[string]any)
		if message == nil {
			message, _ = choice["delta"].(map[string]any)
		}
		if message == nil {
			continue
		}
		if content, exists := message["content"]; exists {
			capture.appendResponseContent(index, content)
		}
		if toolCalls, ok := message["tool_calls"].([]any); ok {
			for toolIndex, rawTool := range toolCalls {
				tool, _ := rawTool.(map[string]any)
				function, _ := tool["function"].(map[string]any)
				name, _ := function["name"].(string)
				key := stringValue(tool["id"])
				if key == "" {
					key = fmt.Sprintf("%d:%d", index, intValue(tool["index"], toolIndex))
				}
				capture.addResponsePlaceholder(index, "tool_call", name, key)
			}
		}
		if stringValue(choice["finish_reason"]) != "" {
			capture.streamCompleted = true
		}
	}
}

func (capture *RequestLogCapture) observeResponsesResponse(root map[string]any, streaming bool) {
	eventType := stringValue(root["type"])
	if streaming && eventType != "" {
		switch eventType {
		case "response.output_text.delta", "response.refusal.delta":
			capture.appendResponseText(0, stringValue(root["delta"]))
		case "response.output_item.added", "response.output_item.done":
			if item, ok := root["item"].(map[string]any); ok {
				if stringValue(item["type"]) != "message" {
					capture.observeResponsesOutputItem(0, item)
				}
			}
		case "response.completed", "response.incomplete":
			capture.streamCompleted = eventType == "response.completed"
			if response, ok := root["response"].(map[string]any); ok {
				if modelName := stringValue(response["model"]); modelName != "" {
					capture.responseModel = modelName
				}
			}
		case "response.failed", "error":
			capture.streamCompleted = true
		}
		return
	}

	if modelName := stringValue(root["model"]); modelName != "" {
		capture.responseModel = modelName
	}
	output, _ := root["output"].([]any)
	for _, rawItem := range output {
		if item, ok := rawItem.(map[string]any); ok {
			capture.observeResponsesOutputItem(0, item)
		}
	}
}

func (capture *RequestLogCapture) observeResponsesOutputItem(index int, item map[string]any) {
	itemType := stringValue(item["type"])
	switch itemType {
	case "message":
		capture.appendResponseContent(index, item["content"])
	case "function_call", "custom_tool_call", "computer_call", "web_search_call", "file_search_call":
		name := stringValue(item["name"])
		if name == "" {
			name = itemType
		}
		key := stringValue(item["id"])
		if key == "" {
			key = stringValue(item["call_id"])
		}
		capture.addResponsePlaceholder(index, "tool_call", name, key)
	case "function_call_output", "computer_call_output":
		key := stringValue(item["call_id"])
		capture.addResponsePlaceholder(index, "tool_result", "", key)
	}
}

func (capture *RequestLogCapture) observeClaudeResponse(root map[string]any, streaming bool) {
	if !streaming {
		capture.responseModel = stringValue(root["model"])
		capture.appendResponseContent(0, root["content"])
		return
	}

	eventType := stringValue(root["type"])
	switch eventType {
	case "message_start":
		if message, ok := root["message"].(map[string]any); ok {
			capture.responseModel = stringValue(message["model"])
		}
	case "content_block_start":
		index := intValue(root["index"], 0)
		if block, ok := root["content_block"].(map[string]any); ok {
			blockType := stringValue(block["type"])
			capture.blockTypes[index] = blockType
			capture.blockNames[index] = stringValue(block["name"])
			if blockType == "text" {
				capture.appendResponseText(0, stringValue(block["text"]))
			} else if blockType == "tool_use" || blockType == "server_tool_use" {
				capture.addResponsePlaceholder(0, "tool_call", capture.blockNames[index], fmt.Sprintf("claude:%d", index))
			}
		}
	case "content_block_delta":
		index := intValue(root["index"], 0)
		if delta, ok := root["delta"].(map[string]any); ok {
			if text := stringValue(delta["text"]); text != "" {
				capture.appendResponseText(0, text)
			} else if capture.blockTypes[index] == "tool_use" || capture.blockTypes[index] == "server_tool_use" {
				capture.addResponsePlaceholder(0, "tool_call", capture.blockNames[index], fmt.Sprintf("claude:%d", index))
			}
		}
	case "message_stop":
		capture.streamCompleted = true
	case "error":
		capture.streamCompleted = true
	}
}

func (capture *RequestLogCapture) observeGeminiResponse(root map[string]any) {
	candidates, _ := root["candidates"].([]any)
	for position, rawCandidate := range candidates {
		candidate, ok := rawCandidate.(map[string]any)
		if !ok {
			continue
		}
		index := intValue(candidate["index"], position)
		if content, ok := candidate["content"].(map[string]any); ok {
			capture.appendResponseContent(index, content["parts"])
		}
		if stringValue(candidate["finishReason"]) != "" || stringValue(candidate["finish_reason"]) != "" {
			capture.streamCompleted = true
		}
	}
	if !capture.isStream && len(candidates) > 0 {
		capture.streamCompleted = true
	}
}

func (capture *RequestLogCapture) appendResponseContent(index int, content any) {
	for _, part := range normalizeRequestLogParts(content, "assistant", capture.toolNames) {
		switch part.Type {
		case "text":
			capture.appendResponseText(index, part.Text)
		default:
			capture.addResponsePlaceholder(index, part.Type, part.Name, fmt.Sprintf("%s:%s:%d", part.Type, part.Name, len(capture.response(index).parts)))
		}
	}
}

func (capture *RequestLogCapture) appendResponseText(index int, text string) {
	if text == "" {
		return
	}
	response := capture.response(index)
	remaining := requestLogMaxAssistantRunes - response.textRunes
	if remaining <= 0 {
		response.truncated = true
		return
	}
	runes := []rune(text)
	if len(runes) > remaining {
		runes = runes[:remaining]
		response.truncated = true
	}
	text = string(runes)
	response.textRunes += len(runes)
	if len(response.parts) > 0 && response.parts[len(response.parts)-1].Type == "text" {
		response.parts[len(response.parts)-1].Text += text
		return
	}
	response.parts = append(response.parts, RequestLogPart{Type: "text", Text: text})
}

func (capture *RequestLogCapture) addResponsePlaceholder(index int, partType string, name string, key string) {
	response := capture.response(index)
	if key == "" {
		key = fmt.Sprintf("%s:%s", partType, name)
	}
	seenKey := partType + ":" + key
	if _, exists := response.toolEvents[seenKey]; exists {
		return
	}
	response.toolEvents[seenKey] = struct{}{}
	response.parts = append(response.parts, RequestLogPart{Type: partType, Name: name})
	if name != "" {
		capture.toolNames[name] = struct{}{}
	}
}

func (capture *RequestLogCapture) response(index int) *requestLogResponse {
	response := capture.responses[index]
	if response == nil {
		response = &requestLogResponse{toolEvents: map[string]struct{}{}}
		capture.responses[index] = response
	}
	return response
}

func (capture *RequestLogCapture) normalizeRequest(request dto.Request) error {
	data, err := common.Marshal(request)
	if err != nil {
		return err
	}
	var root map[string]any
	if err := common.Unmarshal(data, &root); err != nil {
		return err
	}

	switch capture.protocol {
	case "chat_completions", "claude":
		capture.requestMessages = normalizeRoleMessages(root["messages"], capture.toolNames)
	case "responses":
		capture.requestMessages = normalizeResponsesInput(root["input"], capture.toolNames)
		previousResponseID := stringValue(root["previous_response_id"])
		conversation := stringValue(root["conversation"])
		if previousResponseID != "" || conversation != "" {
			capture.historyComplete = false
			if previousResponseID != "" {
				capture.historyReference = "previous_response_id:" + previousResponseID
			} else {
				capture.historyReference = "conversation:" + conversation
			}
		}
	case "gemini":
		capture.requestMessages = normalizeGeminiContents(root["contents"], capture.toolNames)
		if cachedContent := stringValue(root["cachedContent"]); cachedContent != "" {
			capture.historyComplete = false
			capture.historyReference = "cached_content:" + cachedContent
		}
	}
	return nil
}

func normalizeRoleMessages(value any, toolNames map[string]struct{}) []RequestLogMessage {
	items, _ := value.([]any)
	messages := make([]RequestLogMessage, 0, len(items))
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		role := stringValue(item["role"])
		if role == "system" || role == "developer" {
			continue
		}
		normalizedRole := role
		if role == "model" {
			normalizedRole = "assistant"
		}
		var parts []RequestLogPart
		if role == "tool" || role == "function" {
			parts = []RequestLogPart{{Type: "tool_result"}}
		} else {
			parts = normalizeRequestLogParts(item["content"], normalizedRole, toolNames)
			if toolCalls, ok := item["tool_calls"].([]any); ok {
				for _, rawTool := range toolCalls {
					tool, _ := rawTool.(map[string]any)
					function, _ := tool["function"].(map[string]any)
					name := stringValue(function["name"])
					parts = append(parts, RequestLogPart{Type: "tool_call", Name: name})
					if name != "" {
						toolNames[name] = struct{}{}
					}
				}
			}
		}
		if len(parts) == 0 {
			continue
		}
		message := RequestLogMessage{Role: normalizedRole, OriginalRole: role, Parts: parts, Source: "request"}
		truncateRequestLogAssistant(&message)
		messages = append(messages, message)
	}
	return messages
}

func normalizeResponsesInput(value any, toolNames map[string]struct{}) []RequestLogMessage {
	if text, ok := value.(string); ok {
		return []RequestLogMessage{{Role: "user", OriginalRole: "user", Parts: []RequestLogPart{{Type: "text", Text: text}}, Source: "request"}}
	}
	items, _ := value.([]any)
	messages := make([]RequestLogMessage, 0, len(items))
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		role := stringValue(item["role"])
		itemType := stringValue(item["type"])
		if role == "system" || role == "developer" {
			continue
		}
		switch itemType {
		case "function_call", "custom_tool_call", "computer_call":
			name := stringValue(item["name"])
			if name != "" {
				toolNames[name] = struct{}{}
			}
			messages = append(messages, RequestLogMessage{Role: "assistant", OriginalRole: role, Parts: []RequestLogPart{{Type: "tool_call", Name: name}}, Source: "request"})
			continue
		case "function_call_output", "computer_call_output":
			messages = append(messages, RequestLogMessage{Role: "user", OriginalRole: role, Parts: []RequestLogPart{{Type: "tool_result"}}, Source: "request"})
			continue
		}
		if role == "" {
			role = "user"
		}
		parts := normalizeRequestLogParts(item["content"], role, toolNames)
		if len(parts) == 0 {
			parts = normalizeRequestLogParts(item, role, toolNames)
		}
		if len(parts) == 0 {
			continue
		}
		message := RequestLogMessage{Role: role, OriginalRole: role, Parts: parts, Source: "request"}
		truncateRequestLogAssistant(&message)
		messages = append(messages, message)
	}
	return messages
}

func normalizeGeminiContents(value any, toolNames map[string]struct{}) []RequestLogMessage {
	items, _ := value.([]any)
	messages := make([]RequestLogMessage, 0, len(items))
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		originalRole := stringValue(item["role"])
		role := originalRole
		if role == "model" {
			role = "assistant"
		}
		if role == "" {
			role = "user"
		}
		parts := normalizeRequestLogParts(item["parts"], role, toolNames)
		if len(parts) == 0 {
			continue
		}
		message := RequestLogMessage{Role: role, OriginalRole: originalRole, Parts: parts, Source: "request"}
		truncateRequestLogAssistant(&message)
		messages = append(messages, message)
	}
	return messages
}

func normalizeRequestLogParts(content any, role string, toolNames map[string]struct{}) []RequestLogPart {
	if content == nil {
		return nil
	}
	if text, ok := content.(string); ok {
		if text == "" {
			return nil
		}
		return []RequestLogPart{{Type: "text", Text: text}}
	}
	if items, ok := content.([]any); ok {
		parts := make([]RequestLogPart, 0, len(items))
		for _, item := range items {
			parts = append(parts, normalizeRequestLogParts(item, role, toolNames)...)
		}
		return parts
	}
	item, ok := content.(map[string]any)
	if !ok {
		return nil
	}

	if functionCall, ok := firstMap(item, "functionCall", "function_call"); ok {
		name := stringValue(functionCall["name"])
		if name != "" {
			toolNames[name] = struct{}{}
		}
		return []RequestLogPart{{Type: "tool_call", Name: name}}
	}
	if _, ok := firstMap(item, "functionResponse", "function_response"); ok {
		return []RequestLogPart{{Type: "tool_result"}}
	}
	if inlineData, ok := firstMap(item, "inlineData", "inline_data"); ok {
		if strings.HasPrefix(stringValue(inlineData["mimeType"]), "image/") || strings.HasPrefix(stringValue(inlineData["mime_type"]), "image/") {
			return []RequestLogPart{{Type: "image"}}
		}
		return []RequestLogPart{{Type: "file"}}
	}
	if _, ok := firstMap(item, "fileData", "file_data"); ok {
		return []RequestLogPart{{Type: "file"}}
	}

	partType := stringValue(item["type"])
	switch partType {
	case "tool_use", "server_tool_use", "function_call", "custom_tool_call", "computer_call":
		name := stringValue(item["name"])
		if name != "" {
			toolNames[name] = struct{}{}
		}
		return []RequestLogPart{{Type: "tool_call", Name: name}}
	case "tool_result", "function_call_output", "computer_call_output":
		return []RequestLogPart{{Type: "tool_result"}}
	case "image", "image_url", "input_image":
		return []RequestLogPart{{Type: "image"}}
	case "file", "input_file", "document", "input_audio", "audio", "video_url":
		return []RequestLogPart{{Type: "file"}}
	}
	if role == "tool" || role == "function" {
		return []RequestLogPart{{Type: "tool_result"}}
	}
	if text := stringValue(item["text"]); text != "" {
		return []RequestLogPart{{Type: "text", Text: text}}
	}
	if nested, exists := item["content"]; exists {
		return normalizeRequestLogParts(nested, role, toolNames)
	}
	return nil
}

func truncateRequestLogAssistant(message *RequestLogMessage) {
	if message.Role != "assistant" {
		return
	}
	remaining := requestLogMaxAssistantRunes
	parts := make([]RequestLogPart, 0, len(message.Parts))
	for _, part := range message.Parts {
		if part.Type != "text" {
			parts = append(parts, part)
			continue
		}
		runes := []rune(part.Text)
		if len(runes) <= remaining {
			parts = append(parts, part)
			remaining -= len(runes)
			continue
		}
		message.Truncated = true
		if remaining > 0 {
			part.Text = string(runes[:remaining])
			parts = append(parts, part)
			remaining = 0
		}
	}
	message.Parts = parts
}

func marshalRequestLogConversation(messages []RequestLogMessage) ([]byte, bool, error) {
	truncated := false
	for {
		data, err := common.Marshal(messages)
		if err != nil {
			return nil, false, err
		}
		if len(data) <= requestLogMaxConversationSize {
			return data, truncated, nil
		}
		truncated = true

		removeIndex := -1
		for index, message := range messages {
			if message.Source == "request" {
				removeIndex = index
				break
			}
		}
		requestCount := 0
		for _, message := range messages {
			if message.Source == "request" {
				requestCount++
			}
		}
		if removeIndex >= 0 && requestCount > 1 {
			messages = append(messages[:removeIndex], messages[removeIndex+1:]...)
			continue
		}

		reduced := false
		for messageIndex := range messages {
			for partIndex := range messages[messageIndex].Parts {
				part := &messages[messageIndex].Parts[partIndex]
				if part.Type != "text" || part.Text == "" {
					continue
				}
				overflow := len(data) - requestLogMaxConversationSize
				targetBytes := max(len(part.Text)-overflow-256, 0)
				part.Text = validUTF8Prefix(part.Text, targetBytes)
				messages[messageIndex].Truncated = true
				reduced = true
				break
			}
			if reduced {
				break
			}
		}
		if !reduced {
			return []byte("[]"), true, nil
		}
	}
}

func validUTF8Prefix(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func firstMap(item map[string]any, keys ...string) (map[string]any, bool) {
	for _, key := range keys {
		if value, ok := item[key].(map[string]any); ok {
			return value, true
		}
	}
	return nil, false
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func intValue(value any, fallback int) int {
	switch number := value.(type) {
	case float64:
		return int(number)
	case int:
		return number
	default:
		return fallback
	}
}

var _ io.StringWriter = (*requestLogResponseWriter)(nil)
