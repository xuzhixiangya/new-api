package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

const (
	SmartRouterModelAuto = "auto"

	SmartRouterTierCheap  = "cheap"
	SmartRouterTierMid    = "mid"
	SmartRouterTierStrong = "strong"

	SmartRouterStageLocal      = "local"
	SmartRouterStageClassifier = "classifier"
	SmartRouterStageFallback   = "fallback"

	smartRouterEasyScoreMax            = 30
	smartRouterHardScoreMin            = 70
	smartRouterLongContextMidTokens    = 16000
	smartRouterLongContextStrongTokens = 64000
	smartRouterClassifierMinConfidence = 0.6
)

type SmartRouterDecision struct {
	Requested    string  `json:"requested"`
	Model        string  `json:"model"`
	Tier         string  `json:"tier"`
	Score        int     `json:"score"`
	Stage        string  `json:"stage"`
	Reason       string  `json:"reason"`
	Continuation bool    `json:"continuation"`
	Confidence   float64 `json:"confidence,omitempty"`
}

type SmartRouterError struct {
	StatusCode int
	Code       types.ErrorCode
	MessageID  string
	Params     map[string]any
}

type smartRouterFeatures struct {
	LastUserText       string
	LastUserTokens     int
	LastHasImage       bool
	LastHasFile        bool
	TotalTokens        int
	UserTurns          int
	HasHistoryImage    bool
	HasHistoryFile     bool
	HasHistoryTools    bool
	LongPaste          bool
	LastAssistantText  string
	LastAssistantRunes int
	LastAssistantCode  bool
	ImageCount         int
	FileCount          int
	HasJSONSchema      bool
	ToolCount          int
	HasHistoryHardCore bool
}

type smartRouterClassifier func(c *gin.Context, digest string) (tier string, confidence float64, reason string, ok bool)

var classifySmartRouterComplexity smartRouterClassifier = invokeSmartRouterClassifier

var (
	smartRouterContinuation    = regexp.MustCompile(`(?i)^(继续|接着写|接着|再改|改一下|再补|补充一下|加上测试|继续写|然后呢|为什么|详细说说|展开说说|举个例子|fix this|continue|keep going)\b`)
	smartRouterEasyKeyword     = regexp.MustCompile(`(?i)(翻译|润色|提取|总结|天气|translate|summarize|extract|polish|weather)`)
	smartRouterReuseEasy       = regexp.MustCompile(`(?i)(总结|提取|summarize|extract)`)
	smartRouterHardCore        = regexp.MustCompile(`(?i)(架构|并发|分布式|事务|从零|形式化|死锁|一致性|prove|architecture|distributed|transaction)`)
	smartRouterHardSoft        = regexp.MustCompile(`(?i)(设计|证明|实现|排查|design|implement|debug)`)
	smartRouterAck             = regexp.MustCompile(`(?i)^(谢谢|感谢|好的|收到|嗯|哦|ok|okay|thanks|thx)[。.!！]*$`)
	smartRouterGreeting        = regexp.MustCompile(`(?i)^(你好|您好|嗨|哈喽|hi|hello|hey)[。.!！]*$`)
	smartRouterNewTask         = regexp.MustCompile(`(?i)^(帮我|请帮|请写|写个|写一|写份|写封|生成|发个|发封)`)
	smartRouterCodeFence       = regexp.MustCompile("(?s)```")
	smartRouterClassifierCache sync.Map
)

func MaybeRouteSmartModel(c *gin.Context, requestedModel string) (string, bool, *SmartRouterError) {
	if c == nil || common.GetContextKeyBool(c, constant.ContextKeySmartRouterSkip) {
		return "", false, nil
	}
	if !smartRouterEligiblePath(c) {
		return "", false, nil
	}

	requested := strings.TrimSpace(requestedModel)
	forced := smartRouterForced(c)
	if !forced && !strings.EqualFold(requested, SmartRouterModelAuto) {
		return "", false, nil
	}

	setting := operation_setting.GetSmartRouterSetting()
	if !setting.Ready() {
		return "", false, &SmartRouterError{
			StatusCode: http.StatusBadRequest,
			Code:       types.ErrorCodeInvalidRequest,
			MessageID:  i18n.MsgDistributorSmartRouterNotConfigured,
		}
	}

	features := extractSmartRouterFeatures(c)
	score, continuation, reason := scoreSmartRouter(features)
	tier, stage := pickSmartRouterTier(score)
	var confidence float64
	if stage == SmartRouterStageClassifier && reason != "easy_with_context" {
		classified, classifiedStage, classifiedReason, classifiedConfidence := classifySmartRouterTier(c, features, continuation)
		tier = classified
		stage = classifiedStage
		confidence = classifiedConfidence
		if classifiedReason != "" {
			reason = classifiedReason
		}
	}
	if reason == "easy_with_context" {
		tier = maxSmartRouterTier(tier, SmartRouterTierMid)
		stage = SmartRouterStageLocal
	}
	tier = applySmartRouterFloors(tier, features)

	limit, limitEnabled, limitErr := smartRouterTokenLimit(c)
	if limitErr != nil {
		return "", false, limitErr
	}
	modelName, tier, allowed := firstAllowedSmartRouterModel(setting, tier, limit, limitEnabled)
	if !allowed {
		return "", false, &SmartRouterError{
			StatusCode: http.StatusForbidden,
			Code:       types.ErrorCodeInvalidRequest,
			MessageID:  i18n.MsgDistributorTokenModelForbidden,
			Params:     map[string]any{"Model": setting.ModelForTier(tier)},
		}
	}

	decision := SmartRouterDecision{
		Requested:    requested,
		Model:        modelName,
		Tier:         tier,
		Score:        score,
		Stage:        stage,
		Reason:       reason,
		Continuation: continuation,
		Confidence:   confidence,
	}
	common.SetContextKey(c, constant.ContextKeyRequestedModel, requested)
	common.SetContextKey(c, constant.ContextKeySmartRouterDecision, decision)
	return modelName, true, nil
}

func ShouldExposeSmartRouterModel(limit map[string]bool, limitEnabled bool, available []string) bool {
	setting := operation_setting.GetSmartRouterSetting()
	if !setting.Ready() {
		return false
	}
	availableSet := map[string]bool{}
	for _, name := range available {
		availableSet[name] = true
	}
	for _, tier := range []string{SmartRouterTierCheap, SmartRouterTierMid, SmartRouterTierStrong} {
		modelName := setting.ModelForTier(tier)
		if modelName == "" {
			continue
		}
		if !availableSet[modelName] && !availableSet[ratio_setting.RoutingMatchModelName(modelName)] {
			continue
		}
		if smartRouterTokenAllows(limit, limitEnabled, modelName) {
			return true
		}
	}
	return false
}

func smartRouterEligiblePath(c *gin.Context) bool {
	if c.Request == nil || c.Request.Method != http.MethodPost || c.Request.URL == nil {
		return false
	}
	path := c.Request.URL.Path
	return path == "/v1/chat/completions" ||
		path == "/v1/responses" ||
		strings.HasPrefix(path, "/pg/chat/completions")
}

func smartRouterForced(c *gin.Context) bool {
	setting, ok := common.GetContextKeyType[dto.UserSetting](c, constant.ContextKeyUserSetting)
	return ok && setting.SmartRouterForced
}

func extractSmartRouterFeatures(c *gin.Context) smartRouterFeatures {
	if c.Request == nil {
		return smartRouterFeatures{}
	}
	path := ""
	if c.Request.URL != nil {
		path = c.Request.URL.Path
	}
	if path == "/v1/responses" {
		var req dto.OpenAIResponsesRequest
		if err := common.UnmarshalBodyReusable(c, &req); err != nil {
			return smartRouterFeatures{}
		}
		return featuresFromResponsesRequest(&req)
	}
	var req dto.GeneralOpenAIRequest
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return smartRouterFeatures{}
	}
	return featuresFromChatRequest(&req)
}

func featuresFromChatRequest(req *dto.GeneralOpenAIRequest) smartRouterFeatures {
	features := smartRouterFeatures{
		ToolCount:     len(req.Tools),
		HasJSONSchema: req.ResponseFormat != nil && (req.ResponseFormat.Type == "json_schema" || len(req.ResponseFormat.JsonSchema) > 0),
	}
	if len(req.Functions) > 0 {
		features.ToolCount++
	}
	var builder strings.Builder
	var lastUser dto.Message
	var lastAssistant dto.Message
	hasLastUser := false
	hasLastAssistant := false
	for i := range req.Messages {
		msg := req.Messages[i]
		text := msg.StringContent()
		builder.WriteString(text)
		parts := msg.ParseContent()
		hasImage, hasFile := mediaFlags(parts)
		if hasImage {
			features.HasHistoryImage = true
			features.ImageCount++
		}
		if hasFile {
			features.HasHistoryFile = true
			features.FileCount++
		}
		if len(msg.ToolCalls) > 0 || msg.ToolCallId != "" {
			features.HasHistoryTools = true
		}
		switch strings.ToLower(msg.Role) {
		case "user":
			features.UserTurns++
			lastUser = msg
			hasLastUser = true
			if utf8.RuneCountInString(text) >= 4000 {
				features.LongPaste = true
			}
			if smartRouterHardCore.MatchString(text) {
				features.HasHistoryHardCore = true
			}
		case "assistant":
			lastAssistant = msg
			hasLastAssistant = true
		}
	}
	features.TotalTokens = estimateSmartRouterTokens(builder.String())
	if hasLastUser {
		features.LastUserText = lastUser.StringContent()
		features.LastUserTokens = estimateSmartRouterTokens(features.LastUserText)
		lastImage, lastFile := mediaFlags(lastUser.ParseContent())
		features.LastHasImage = lastImage
		features.LastHasFile = lastFile
	}
	if hasLastAssistant {
		text := lastAssistant.StringContent()
		features.LastAssistantText = text
		features.LastAssistantRunes = utf8.RuneCountInString(text)
		features.LastAssistantCode = smartRouterCodeFence.MatchString(text)
	}
	return features
}

func featuresFromResponsesRequest(req *dto.OpenAIResponsesRequest) smartRouterFeatures {
	features := smartRouterFeatures{
		HasJSONSchema: len(req.Text) > 0 && bytes.Contains(req.Text, []byte("json_schema")),
	}
	if len(req.Tools) > 0 {
		features.ToolCount = 1
		features.HasHistoryTools = true
	}
	inputs := req.ParseInput()
	var builder strings.Builder
	var lastUser strings.Builder
	for _, input := range inputs {
		switch input.Type {
		case "input_text", "":
			builder.WriteString(input.Text)
			lastUser.Reset()
			lastUser.WriteString(input.Text)
			features.UserTurns++
			if utf8.RuneCountInString(input.Text) >= 4000 {
				features.LongPaste = true
			}
			if smartRouterHardCore.MatchString(input.Text) {
				features.HasHistoryHardCore = true
			}
		case "input_image":
			features.HasHistoryImage = true
			features.LastHasImage = true
			features.ImageCount++
		case "input_file":
			features.HasHistoryFile = true
			features.LastHasFile = true
			features.FileCount++
		}
	}
	if len(req.Instructions) > 0 {
		builder.Write(req.Instructions)
	}
	features.LastUserText = lastUser.String()
	features.LastUserTokens = estimateSmartRouterTokens(features.LastUserText)
	features.TotalTokens = estimateSmartRouterTokens(builder.String())
	if features.UserTurns == 0 && features.LastUserText != "" {
		features.UserTurns = 1
	}
	return features
}

func mediaFlags(parts []dto.MediaContent) (hasImage bool, hasFile bool) {
	for _, part := range parts {
		switch part.Type {
		case dto.ContentTypeImageURL, "input_image":
			hasImage = true
		case "file", "input_file", "video_url":
			hasFile = true
		}
	}
	return hasImage, hasFile
}

func estimateSmartRouterTokens(text string) int {
	runes := utf8.RuneCountInString(text)
	if runes == 0 {
		return 0
	}
	return max(1, (runes+3)/4)
}

func scoreSmartRouter(features smartRouterFeatures) (int, bool, string) {
	current := scoreFromTokens(features.LastUserTokens)
	reason := "last_turn"
	coreHits := len(smartRouterHardCore.FindAllString(features.LastUserText, -1))
	softHits := len(smartRouterHardSoft.FindAllString(features.LastUserText, -1))
	if smartRouterEasyKeyword.MatchString(features.LastUserText) && coreHits == 0 {
		current -= 16
		reason = "easy_keyword"
	}
	if coreHits > 0 || softHits >= 2 {
		current += 28
		if coreHits+softHits >= 2 || utf8.RuneCountInString(features.LastUserText) >= 16 {
			current += 25
		}
		reason = "hard_keyword"
	} else if softHits == 1 {
		current += 12
		reason = "soft_hard_keyword"
	}
	if features.LastHasImage {
		current += 12
	}
	if features.LastHasFile {
		current += 12
	}
	if features.ToolCount > 0 || features.HasJSONSchema {
		current += 14
	}
	if reason == "easy_keyword" && smartRouterReuseEasy.MatchString(features.LastUserText) && hasSmartRouterPriorContext(features) {
		current = max(current, 42)
		reason = "easy_with_context"
	}
	current = clampSmartRouterScore(current)

	history := 0
	if features.UserTurns >= 3 {
		history += 12
	}
	history += scoreFromTokens(features.TotalTokens) / 2
	if features.LongPaste {
		history += 20
	}
	if features.HasHistoryImage || features.HasHistoryFile || features.HasHistoryTools {
		history += 10
	}
	if features.LastAssistantRunes >= 400 {
		history += 12
	}
	if features.LastAssistantCode {
		history += 18
	}
	history = clampSmartRouterScore(history)

	continuation := isSmartRouterContinuation(features)
	if continuation {
		score := max(current, history)
		if features.HasHistoryHardCore || history >= smartRouterHardScoreMin {
			score = max(score, smartRouterHardScoreMin+1)
		}
		return score, true, "continuation"
	}
	return current, false, reason
}

func isSmartRouterContinuation(features smartRouterFeatures) bool {
	text := strings.TrimSpace(features.LastUserText)
	if text == "" || features.UserTurns < 2 {
		return false
	}
	if smartRouterAck.MatchString(text) || smartRouterGreeting.MatchString(text) || smartRouterNewTask.MatchString(text) {
		return false
	}
	if utf8.RuneCountInString(text) > 30 {
		return false
	}
	if smartRouterEasyKeyword.MatchString(text) && !smartRouterContinuation.MatchString(text) {
		return false
	}
	return smartRouterContinuation.MatchString(text) || !hasSmartRouterHardKeyword(text)
}

func hasSmartRouterHardKeyword(text string) bool {
	return smartRouterHardCore.MatchString(text) || smartRouterHardSoft.MatchString(text)
}

func hasSmartRouterPriorContext(features smartRouterFeatures) bool {
	return features.UserTurns >= 2 && (features.LastAssistantRunes >= 400 || features.LastAssistantCode || features.LongPaste || features.TotalTokens >= 200)
}

func scoreFromTokens(tokens int) int {
	switch {
	case tokens == 0:
		return 10
	case tokens < 24:
		return 18
	case tokens < 80:
		return 28
	case tokens < 200:
		return 42
	case tokens < 600:
		return 58
	default:
		return 72
	}
}

func clampSmartRouterScore(score int) int {
	return min(100, max(0, score))
}

func pickSmartRouterTier(score int) (string, string) {
	if score < smartRouterEasyScoreMax {
		return SmartRouterTierCheap, SmartRouterStageLocal
	}
	if score > smartRouterHardScoreMin {
		return SmartRouterTierStrong, SmartRouterStageLocal
	}
	return SmartRouterTierMid, SmartRouterStageClassifier
}

func applySmartRouterFloors(tier string, features smartRouterFeatures) string {
	if features.TotalTokens >= smartRouterLongContextStrongTokens {
		return SmartRouterTierStrong
	}
	if features.TotalTokens >= smartRouterLongContextMidTokens {
		return maxSmartRouterTier(tier, SmartRouterTierMid)
	}
	if features.ImageCount > 0 || features.FileCount > 1 || features.ToolCount >= 3 || features.HasJSONSchema {
		return maxSmartRouterTier(tier, SmartRouterTierMid)
	}
	return tier
}

func maxSmartRouterTier(left, right string) string {
	rank := map[string]int{
		SmartRouterTierCheap:  1,
		SmartRouterTierMid:    2,
		SmartRouterTierStrong: 3,
	}
	if rank[right] > rank[left] {
		return right
	}
	return left
}

func classifySmartRouterTier(c *gin.Context, features smartRouterFeatures, continuation bool) (string, string, string, float64) {
	digest := buildSmartRouterClassifierDigest(features, continuation)
	if cached, ok := loadSmartRouterClassifierCache(digest); ok {
		return cached, SmartRouterStageClassifier, "classifier_cache", 0
	}
	if classifySmartRouterComplexity == nil {
		return SmartRouterTierMid, SmartRouterStageFallback, "classifier_unavailable", 0
	}
	tier, confidence, reason, ok := classifySmartRouterComplexity(c, digest)
	if !ok || confidence < smartRouterClassifierMinConfidence {
		return SmartRouterTierMid, SmartRouterStageFallback, "classifier_uncertain", confidence
	}
	if tier != SmartRouterTierCheap && tier != SmartRouterTierMid && tier != SmartRouterTierStrong {
		return SmartRouterTierMid, SmartRouterStageFallback, "classifier_invalid_tier", confidence
	}
	smartRouterClassifierCache.Store(smartRouterClassifierCacheKey(digest), tier)
	if reason == "" {
		reason = "classifier"
	}
	return tier, SmartRouterStageClassifier, reason, confidence
}

func buildSmartRouterClassifierDigest(features smartRouterFeatures, continuation bool) string {
	var builder strings.Builder
	builder.WriteString("turns=")
	builder.WriteString(strconv.Itoa(features.UserTurns))
	builder.WriteString(" tokens=")
	builder.WriteString(strconv.Itoa(features.TotalTokens))
	builder.WriteString(" continuation=")
	builder.WriteString(strconv.FormatBool(continuation))
	builder.WriteString(" image=")
	builder.WriteString(strconv.FormatBool(features.LastHasImage || features.HasHistoryImage))
	builder.WriteString(" file=")
	builder.WriteString(strconv.FormatBool(features.LastHasFile || features.HasHistoryFile))
	builder.WriteString(" tools=")
	builder.WriteString(strconv.Itoa(features.ToolCount))
	builder.WriteString(" json_schema=")
	builder.WriteString(strconv.FormatBool(features.HasJSONSchema))
	builder.WriteString("\nlast=")
	builder.WriteString(truncateRunes(features.LastUserText, 800))
	if features.LastAssistantText != "" {
		builder.WriteString("\nprev=")
		builder.WriteString(truncateRunes(features.LastAssistantText, 300))
	}
	return builder.String()
}

func smartRouterClassifierCacheKey(digest string) string {
	key := sha256.Sum256([]byte(digest))
	return hex.EncodeToString(key[:])
}

func loadSmartRouterClassifierCache(digest string) (string, bool) {
	value, ok := smartRouterClassifierCache.Load(smartRouterClassifierCacheKey(digest))
	if !ok {
		return "", false
	}
	tier, _ := value.(string)
	return tier, tier != ""
}

func buildSmartRouterClassifierPayload(modelName, digest string) map[string]any {
	return buildSmartRouterClassifierPayloadWithFormat(modelName, digest, true)
}

func buildSmartRouterClassifierPayloadWithFormat(modelName, digest string, jsonSchema bool) map[string]any {
	payload := map[string]any{
		"model": modelName,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "Classify the current user task. last is the current request; prev is the previous assistant reply when present. Reply with JSON only. Prefer mid when unsure.",
			},
			{"role": "user", "content": digest},
		},
		"temperature":          0,
		"max_tokens":           64,
		"thinking":             map[string]string{"type": "disabled"},
		"enable_thinking":      false,
		"chat_template_kwargs": map[string]bool{"enable_thinking": false},
		"reasoning":            map[string]string{"effort": "none"},
	}
	if jsonSchema {
		payload["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "smart_router_tier",
				"strict": true,
				"schema": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"tier": map[string]any{
							"type": "string",
							"enum": []string{SmartRouterTierCheap, SmartRouterTierMid, SmartRouterTierStrong},
						},
						"confidence": map[string]any{
							"type":    "number",
							"minimum": 0,
							"maximum": 1,
						},
					},
					"required": []string{"tier", "confidence"},
				},
			},
		}
		return payload
	}
	payload["response_format"] = map[string]string{"type": "json_object"}
	return payload
}

func invokeSmartRouterClassifier(c *gin.Context, digest string) (string, float64, string, bool) {
	common.SetContextKey(c, constant.ContextKeySmartRouterSkip, true)
	defer common.SetContextKey(c, constant.ContextKeySmartRouterSkip, false)
	setting := operation_setting.GetSmartRouterSetting()
	modelName := setting.ClassifierModelName()
	if modelName == "" {
		return "", 0, "", false
	}
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "" {
		group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	}
	if group == "auto" {
		autoGroups := GetRequestAutoGroups(c, common.GetContextKeyString(c, constant.ContextKeyUserGroup))
		if len(autoGroups) == 0 {
			return "", 0, "", false
		}
		group = autoGroups[0]
	}
	channel, err := model.GetRandomSatisfiedChannel(group, modelName, 0, nil)
	if err != nil || channel == nil {
		return "", 0, "", false
	}
	baseURL := strings.TrimRight(channel.GetBaseURL(), "/")
	if baseURL == "" {
		return "", 0, "", false
	}
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}
	timeout := time.Duration(setting.ClassifierTimeout()) * time.Millisecond
	authKey := firstChannelKey(channel.Key)
	for _, useSchema := range []bool{true, false} {
		payload, marshalErr := common.Marshal(buildSmartRouterClassifierPayloadWithFormat(modelName, digest, useSchema))
		if marshalErr != nil {
			return "", 0, "", false
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(payload))
		if reqErr != nil {
			cancel()
			return "", 0, "", false
		}
		req.Header.Set("Content-Type", "application/json")
		if authKey != "" {
			req.Header.Set("Authorization", "Bearer "+authKey)
		}
		resp, doErr := http.DefaultClient.Do(req)
		if doErr != nil {
			cancel()
			logger.LogDebug(c, "smart router classifier failed: %s", doErr.Error())
			if useSchema {
				continue
			}
			return "", 0, "", false
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		resp.Body.Close()
		cancel()
		if readErr != nil || resp.StatusCode >= 300 {
			logger.LogDebug(c, "smart router classifier http %d schema=%v", resp.StatusCode, useSchema)
			continue
		}
		var completion struct {
			Choices []struct {
				Message struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := common.Unmarshal(body, &completion); err != nil || len(completion.Choices) == 0 {
			continue
		}
		message := completion.Choices[0].Message
		classified, ok := parseSmartRouterClassifierJSON(message.Content)
		if !ok {
			classified, ok = parseSmartRouterClassifierJSON(message.ReasoningContent)
		}
		if !ok {
			continue
		}
		return classified.tier, classified.confidence, classified.reason, true
	}
	return "", 0, "", false
}

type smartRouterClassifierResult struct {
	tier       string
	confidence float64
	reason     string
}

func parseSmartRouterClassifierJSON(raw string) (smartRouterClassifierResult, bool) {
	content := strings.TrimSpace(raw)
	if content == "" {
		return smartRouterClassifierResult{}, false
	}
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	if start := strings.Index(content, "{"); start >= 0 {
		if end := strings.LastIndex(content, "}"); end > start {
			content = content[start : end+1]
		}
	}
	var classified struct {
		Tier       string  `json:"tier"`
		Confidence float64 `json:"confidence"`
		Reason     string  `json:"reason"`
	}
	if err := common.Unmarshal([]byte(content), &classified); err != nil {
		return smartRouterClassifierResult{}, false
	}
	tier := strings.ToLower(strings.TrimSpace(classified.Tier))
	if tier == "" {
		return smartRouterClassifierResult{}, false
	}
	return smartRouterClassifierResult{tier: tier, confidence: classified.Confidence, reason: classified.Reason}, true
}

func firstChannelKey(raw string) string {
	key, _, _ := strings.Cut(strings.TrimSpace(raw), "\n")
	return strings.TrimSpace(key)
}

func smartRouterTokenLimit(c *gin.Context) (map[string]bool, bool, *SmartRouterError) {
	enabled := common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled)
	if !enabled {
		return nil, false, nil
	}
	raw, ok := common.GetContextKey(c, constant.ContextKeyTokenModelLimit)
	if !ok {
		return nil, true, &SmartRouterError{
			StatusCode: http.StatusForbidden,
			Code:       types.ErrorCodeInvalidRequest,
			MessageID:  i18n.MsgDistributorTokenNoModelAccess,
		}
	}
	limit, ok := raw.(map[string]bool)
	if !ok || len(limit) == 0 {
		return nil, true, &SmartRouterError{
			StatusCode: http.StatusForbidden,
			Code:       types.ErrorCodeInvalidRequest,
			MessageID:  i18n.MsgDistributorTokenNoModelAccess,
		}
	}
	return limit, true, nil
}

func firstAllowedSmartRouterModel(setting *operation_setting.SmartRouterSetting, tier string, limit map[string]bool, limitEnabled bool) (string, string, bool) {
	order := []string{SmartRouterTierCheap, SmartRouterTierMid, SmartRouterTierStrong}
	start := 0
	switch tier {
	case SmartRouterTierMid:
		start = 1
	case SmartRouterTierStrong:
		start = 2
	}
	for i := start; i < len(order); i++ {
		candidate := setting.ModelForTier(order[i])
		if smartRouterTokenAllows(limit, limitEnabled, candidate) {
			return candidate, order[i], true
		}
	}
	return "", tier, false
}

func smartRouterTokenAllows(limit map[string]bool, enabled bool, modelName string) bool {
	if !enabled {
		return modelName != ""
	}
	if modelName == "" {
		return false
	}
	if limit[modelName] {
		return true
	}
	if formatted := ratio_setting.FormatMatchingModelName(modelName); limit[formatted] {
		return true
	}
	return limit[ratio_setting.RoutingMatchModelName(modelName)]
}

func truncateRunes(text string, maxRunes int) string {
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	return string(runes[:maxRunes])
}
