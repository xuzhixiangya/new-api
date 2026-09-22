package service

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaybeRouteSmartModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setting := operation_setting.GetSmartRouterSetting()
	original := *setting
	t.Cleanup(func() {
		*setting = original
		classifySmartRouterComplexity = invokeSmartRouterClassifier
		smartRouterClassifierCache = sync.Map{}
	})
	setting.Enabled = true
	setting.CheapModel = "cheap-model"
	setting.MidModel = "mid-model"
	setting.StrongModel = "strong-model"
	classifySmartRouterComplexity = func(*gin.Context, string) (string, float64, string, bool) {
		return SmartRouterTierMid, 0.9, "test_classifier", true
	}

	t.Run("fixed model passes through", func(t *testing.T) {
		c := newSmartRouterContext(t, "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"翻译：Hello"}]}`)
		routed, applied, err := MaybeRouteSmartModel(c, "gpt-4o")
		require.Nil(t, err)
		assert.False(t, applied)
		assert.Empty(t, routed)
	})

	t.Run("auto routes simple request to cheap", func(t *testing.T) {
		c := newSmartRouterContext(t, "/v1/chat/completions", `{"model":"auto","messages":[{"role":"user","content":"翻译：Hello"}]}`)
		routed, applied, err := MaybeRouteSmartModel(c, "auto")
		require.Nil(t, err)
		require.True(t, applied)
		assert.Equal(t, "cheap-model", routed)
		decision := mustSmartRouterDecision(t, c)
		assert.Equal(t, SmartRouterTierCheap, decision.Tier)
		assert.Equal(t, SmartRouterStageLocal, decision.Stage)
	})

	t.Run("auto routes hard request to strong", func(t *testing.T) {
		c := newSmartRouterContext(t, "/v1/chat/completions", `{"model":"Auto","messages":[{"role":"user","content":"从零设计一个支持事务的分布式 KV 存储"}]}`)
		routed, applied, err := MaybeRouteSmartModel(c, "Auto")
		require.Nil(t, err)
		require.True(t, applied)
		assert.Equal(t, "strong-model", routed)
		assert.Equal(t, SmartRouterTierStrong, mustSmartRouterDecision(t, c).Tier)
	})

	t.Run("forced routing rewrites a fixed model", func(t *testing.T) {
		c := newSmartRouterContext(t, "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"翻译：Hello"}]}`)
		common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{SmartRouterForced: true})
		routed, applied, err := MaybeRouteSmartModel(c, "gpt-4o")
		require.Nil(t, err)
		require.True(t, applied)
		assert.Equal(t, "cheap-model", routed)
		assert.Equal(t, "gpt-4o", mustSmartRouterDecision(t, c).Requested)
	})

	t.Run("responses path routes auto", func(t *testing.T) {
		c := newSmartRouterContext(t, "/v1/responses", `{"model":"auto","input":"翻译：Hello"}`)
		routed, applied, err := MaybeRouteSmartModel(c, "auto")
		require.Nil(t, err)
		require.True(t, applied)
		assert.Equal(t, "cheap-model", routed)
	})

	t.Run("non chat path is ignored", func(t *testing.T) {
		c := newSmartRouterContext(t, "/v1/embeddings", `{"model":"auto","input":"hi"}`)
		routed, applied, err := MaybeRouteSmartModel(c, "auto")
		require.Nil(t, err)
		assert.False(t, applied)
		assert.Empty(t, routed)
	})

	t.Run("short continuation inherits history difficulty", func(t *testing.T) {
		c := newSmartRouterContext(t, "/v1/chat/completions", chatBody(t,
			"从零设计一个支持事务的分布式 KV 存储",
			codeAssistantReply(),
			"再改一下",
		))
		routed, applied, err := MaybeRouteSmartModel(c, "auto")
		require.Nil(t, err)
		require.True(t, applied)
		decision := mustSmartRouterDecision(t, c)
		assert.True(t, decision.Continuation, "decision=%+v", decision)
		assert.Equal(t, "strong-model", routed, "decision=%+v", decision)
	})

	t.Run("topic shift scores the new question", func(t *testing.T) {
		c := newSmartRouterContext(t, "/v1/chat/completions", chatBody(t,
			"从零设计一个支持事务的分布式 KV 存储",
			codeAssistantReply(),
			"今天北京天气",
		))
		routed, applied, err := MaybeRouteSmartModel(c, "auto")
		require.Nil(t, err)
		require.True(t, applied)
		decision := mustSmartRouterDecision(t, c)
		assert.False(t, decision.Continuation)
		assert.Equal(t, "cheap-model", routed)
	})

	t.Run("unconfigured auto returns 400", func(t *testing.T) {
		setting.Enabled = false
		t.Cleanup(func() { setting.Enabled = true })
		c := newSmartRouterContext(t, "/v1/chat/completions", `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`)
		_, applied, err := MaybeRouteSmartModel(c, "auto")
		require.NotNil(t, err)
		assert.False(t, applied)
		assert.Equal(t, http.StatusBadRequest, err.StatusCode)
	})

	t.Run("token limit upgrades cheap to an allowed tier", func(t *testing.T) {
		c := newSmartRouterContext(t, "/v1/chat/completions", `{"model":"auto","messages":[{"role":"user","content":"翻译：Hello"}]}`)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"mid-model": true})
		routed, applied, err := MaybeRouteSmartModel(c, "auto")
		require.Nil(t, err)
		require.True(t, applied)
		assert.Equal(t, "mid-model", routed)
	})

	t.Run("token limit rejects when no tier is allowed", func(t *testing.T) {
		c := newSmartRouterContext(t, "/v1/chat/completions", `{"model":"auto","messages":[{"role":"user","content":"翻译：Hello"}]}`)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"other-model": true})
		_, applied, err := MaybeRouteSmartModel(c, "auto")
		require.NotNil(t, err)
		assert.False(t, applied)
		assert.Equal(t, http.StatusForbidden, err.StatusCode)
	})

	t.Run("classifier timeout falls back to mid", func(t *testing.T) {
		classifySmartRouterComplexity = func(*gin.Context, string) (string, float64, string, bool) {
			return "", 0, "", false
		}
		t.Cleanup(func() {
			classifySmartRouterComplexity = func(*gin.Context, string) (string, float64, string, bool) {
				return SmartRouterTierMid, 0.9, "test_classifier", true
			}
		})
		content := strings.Repeat("请把这段说明按条目列清楚，方便同事阅读。", 20)
		body := `{"model":"auto","messages":[{"role":"user","content":"` + content + `"}]}`
		c := newSmartRouterContext(t, "/v1/chat/completions", body)
		routed, applied, err := MaybeRouteSmartModel(c, "auto")
		require.Nil(t, err)
		require.True(t, applied)
		decision := mustSmartRouterDecision(t, c)
		assert.Equal(t, SmartRouterTierMid, decision.Tier)
		assert.Equal(t, "mid-model", routed)
		assert.Equal(t, SmartRouterStageFallback, decision.Stage)
	})

	t.Run("skip flag disables routing", func(t *testing.T) {
		c := newSmartRouterContext(t, "/v1/chat/completions", `{"model":"auto","messages":[{"role":"user","content":"翻译：Hello"}]}`)
		common.SetContextKey(c, constant.ContextKeySmartRouterSkip, true)
		routed, applied, err := MaybeRouteSmartModel(c, "auto")
		require.Nil(t, err)
		assert.False(t, applied)
		assert.Empty(t, routed)
	})
}

func TestSmartRouterOfficeScenarios(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setting := operation_setting.GetSmartRouterSetting()
	original := *setting
	t.Cleanup(func() {
		*setting = original
		classifySmartRouterComplexity = invokeSmartRouterClassifier
		smartRouterClassifierCache = sync.Map{}
	})
	setting.Enabled = true
	setting.CheapModel = "cheap-model"
	setting.MidModel = "mid-model"
	setting.StrongModel = "strong-model"
	classifySmartRouterComplexity = func(*gin.Context, string) (string, float64, string, bool) {
		return SmartRouterTierMid, 0.9, "test_classifier", true
	}

	type scenario struct {
		name         string
		body         string
		wantTier     string
		wantModel    string
		continuation *bool
	}
	yes, no := true, false
	cases := []scenario{
		{
			name:      "hr leave note stays cheap",
			body:      `{"model":"auto","messages":[{"role":"user","content":"帮我写个请假理由，明天身体不舒服"}]}`,
			wantTier:  SmartRouterTierCheap,
			wantModel: "cheap-model",
		},
		{
			name:      "marketing copy with 设计 is not strong",
			body:      `{"model":"auto","messages":[{"role":"user","content":"设计一个生日祝福文案"}]}`,
			wantTier:  SmartRouterTierMid,
			wantModel: "mid-model",
		},
		{
			name:      "hello world 实现 is not strong",
			body:      `{"model":"auto","messages":[{"role":"user","content":"帮我实现一个 hello world"}]}`,
			wantTier:  SmartRouterTierMid,
			wantModel: "mid-model",
		},
		{
			name:      "excel formula 实现 is not strong",
			body:      `{"model":"auto","messages":[{"role":"user","content":"帮我实现一个 Excel 求和公式"}]}`,
			wantTier:  SmartRouterTierMid,
			wantModel: "mid-model",
		},
		{
			name:      "english meeting decline stays cheap",
			body:      `{"model":"auto","messages":[{"role":"user","content":"Please draft a polite reply declining this meeting."}]}`,
			wantTier:  SmartRouterTierCheap,
			wantModel: "cheap-model",
		},
		{
			name:         "thanks after architecture does not inherit strong",
			body:         chatBody(t, "从零设计一个支持事务的分布式 KV 存储", codeAssistantReply(), "谢谢"),
			wantTier:     SmartRouterTierCheap,
			wantModel:    "cheap-model",
			continuation: &no,
		},
		{
			name:         "ok after architecture does not inherit strong",
			body:         chatBody(t, "从零设计一个支持事务的分布式 KV 存储", codeAssistantReply(), "好的"),
			wantTier:     SmartRouterTierCheap,
			wantModel:    "cheap-model",
			continuation: &no,
		},
		{
			name:         "why after architecture inherits strong",
			body:         chatBody(t, "从零设计一个支持事务的分布式 KV 存储", codeAssistantReply(), "为什么"),
			wantTier:     SmartRouterTierStrong,
			wantModel:    "strong-model",
			continuation: &yes,
		},
		{
			name:         "summarize previous long answer is at least mid",
			body:         chatBody(t, "从零设计一个支持事务的分布式 KV 存储", codeAssistantReply(), "总结一下"),
			wantTier:     SmartRouterTierMid,
			wantModel:    "mid-model",
			continuation: &no,
		},
		{
			name:      "json schema floors to mid",
			body:      `{"model":"auto","messages":[{"role":"user","content":"翻译：Hello"}],"response_format":{"type":"json_schema","json_schema":{"name":"item"}}}`,
			wantTier:  SmartRouterTierMid,
			wantModel: "mid-model",
		},
		{
			name:      "image floors to mid",
			body:      `{"model":"auto","messages":[{"role":"user","content":[{"type":"text","text":"这是什么"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`,
			wantTier:  SmartRouterTierMid,
			wantModel: "mid-model",
		},
		{
			name:         "script follow-up does not jump to strong",
			body:         chatBody(t, "帮我写一个 Python 脚本读取 CSV 并打印表格", codeAssistantReply(), "再改一下"),
			wantTier:     SmartRouterTierMid,
			wantModel:    "mid-model",
			continuation: &yes,
		},
		{
			name:      "weekly report stays cheap",
			body:      `{"model":"auto","messages":[{"role":"user","content":"帮我写一下本周工作周报，列三条"}]}`,
			wantTier:  SmartRouterTierCheap,
			wantModel: "cheap-model",
		},
		{
			name:      "translate plus distributed stays strong",
			body:      `{"model":"auto","messages":[{"role":"user","content":"翻译这段说明，并设计一个支持事务的分布式方案"}]}`,
			wantTier:  SmartRouterTierStrong,
			wantModel: "strong-model",
		},
		{
			name:      "three tools floor to mid",
			body:      `{"model":"auto","messages":[{"role":"user","content":"翻译：Hello"}],"tools":[{"type":"function","function":{"name":"a"}},{"type":"function","function":{"name":"b"}},{"type":"function","function":{"name":"c"}}]}`,
			wantTier:  SmartRouterTierMid,
			wantModel: "mid-model",
		},
		{
			name:         "leave request after architecture is a new task",
			body:         chatBody(t, "从零设计一个支持事务的分布式 KV 存储", codeAssistantReply(), "帮我写个请假理由"),
			wantTier:     SmartRouterTierCheap,
			wantModel:    "cheap-model",
			continuation: &no,
		},
		{
			name:         "tomorrow weather after architecture is a topic shift",
			body:         chatBody(t, "从零设计一个支持事务的分布式 KV 存储", codeAssistantReply(), "明天天气如何？"),
			wantTier:     SmartRouterTierCheap,
			wantModel:    "cheap-model",
			continuation: &no,
		},
		{
			name:         "hello after architecture does not inherit strong",
			body:         chatBody(t, "从零设计一个支持事务的分布式 KV 存储", codeAssistantReply(), "你好"),
			wantTier:     SmartRouterTierCheap,
			wantModel:    "cheap-model",
			continuation: &no,
		},
		{
			name:         "ok continue after architecture still inherits",
			body:         chatBody(t, "从零设计一个支持事务的分布式 KV 存储", codeAssistantReply(), "好的，继续"),
			wantTier:     SmartRouterTierStrong,
			wantModel:    "strong-model",
			continuation: &yes,
		},
		{
			name:         "extract after architecture stays mid",
			body:         chatBody(t, "从零设计一个支持事务的分布式 KV 存储", codeAssistantReply(), "提取一下要点"),
			wantTier:     SmartRouterTierMid,
			wantModel:    "mid-model",
			continuation: &no,
		},
		{
			name:         "soft tone after email stays cheap",
			body:         chatBody(t, "帮我写封拒绝会议的邮件", "好的，这是一封简短回复。", "语气软一点"),
			wantTier:     SmartRouterTierCheap,
			wantModel:    "cheap-model",
			continuation: &yes,
		},
		{
			name:      "new hard question after cheap translation",
			body:      chatBody(t, "翻译：Hello", "Hello", "从零设计一个支持事务的分布式 KV 存储"),
			wantTier:  SmartRouterTierStrong,
			wantModel: "strong-model",
		},
		{
			name:         "three turn thanks then why inherits strong",
			body:         chatTurns(t, "从零设计一个支持事务的分布式 KV 存储", codeAssistantReply(), "谢谢", "不客气。", "为什么"),
			wantTier:     SmartRouterTierStrong,
			wantModel:    "strong-model",
			continuation: &yes,
		},
		{
			name:         "switch to python after script stays mid",
			body:         chatBody(t, "帮我写一个 Python 脚本读取 CSV 并打印表格", codeAssistantReply(), "换成 Python"),
			wantTier:     SmartRouterTierMid,
			wantModel:    "mid-model",
			continuation: &yes,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newSmartRouterContext(t, "/v1/chat/completions", tc.body)
			routed, applied, err := MaybeRouteSmartModel(c, "auto")
			require.Nil(t, err)
			require.True(t, applied)
			decision := mustSmartRouterDecision(t, c)
			assert.Equal(t, tc.wantModel, routed, "decision=%+v", decision)
			assert.Equal(t, tc.wantTier, decision.Tier, "decision=%+v", decision)
			if tc.continuation != nil {
				assert.Equal(t, *tc.continuation, decision.Continuation, "decision=%+v", decision)
			}
		})
	}
}

func TestShouldExposeSmartRouterModel(t *testing.T) {
	setting := operation_setting.GetSmartRouterSetting()
	original := *setting
	t.Cleanup(func() { *setting = original })
	setting.Enabled = true
	setting.CheapModel = "cheap-model"
	setting.MidModel = "mid-model"
	setting.StrongModel = "strong-model"

	assert.True(t, ShouldExposeSmartRouterModel(nil, false, []string{"cheap-model"}))
	assert.False(t, ShouldExposeSmartRouterModel(nil, false, []string{"other"}))
	assert.True(t, ShouldExposeSmartRouterModel(map[string]bool{"mid-model": true}, true, []string{"mid-model"}))
	assert.False(t, ShouldExposeSmartRouterModel(map[string]bool{"other": true}, true, []string{"cheap-model"}))
	setting.Enabled = false
	assert.False(t, ShouldExposeSmartRouterModel(nil, false, []string{"cheap-model"}))
}

func TestBuildSmartRouterClassifierPayloadDisablesThinkingAndUsesJSONSchema(t *testing.T) {
	payload := buildSmartRouterClassifierPayload("cheap-model", "turns=1\nlast=设计一个生日祝福文案")
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)

	var got struct {
		Thinking struct {
			Type string `json:"type"`
		} `json:"thinking"`
		EnableThinking     bool `json:"enable_thinking"`
		ChatTemplateKwargs struct {
			EnableThinking bool `json:"enable_thinking"`
		} `json:"chat_template_kwargs"`
		Reasoning struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
		ResponseFormat struct {
			Type       string `json:"type"`
			JSONSchema struct {
				Name   string `json:"name"`
				Schema struct {
					Required []string `json:"required"`
				} `json:"schema"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	require.NoError(t, common.Unmarshal(encoded, &got))
	assert.Equal(t, "disabled", got.Thinking.Type)
	assert.False(t, got.EnableThinking)
	assert.False(t, got.ChatTemplateKwargs.EnableThinking)
	assert.Equal(t, "none", got.Reasoning.Effort)
	assert.Equal(t, "json_schema", got.ResponseFormat.Type)
	assert.Equal(t, "smart_router_tier", got.ResponseFormat.JSONSchema.Name)
	assert.Equal(t, []string{"tier", "confidence"}, got.ResponseFormat.JSONSchema.Schema.Required)
}

func TestBuildSmartRouterClassifierDigestOmitsLocalScore(t *testing.T) {
	digest := buildSmartRouterClassifierDigest(smartRouterFeatures{
		LastUserText:      "这个方案漏了什么",
		LastAssistantText: "分布式 KV 的完整实现说明",
		UserTurns:         2,
		TotalTokens:       180,
	}, false)
	assert.NotContains(t, digest, "score=")
	assert.Contains(t, digest, "last=这个方案漏了什么")
	assert.Contains(t, digest, "prev=分布式 KV 的完整实现说明")
	assert.Contains(t, digest, "continuation=false")
	assert.Contains(t, digest, "turns=2")
}

func codeAssistantReply() string {
	return "```go\n" + strings.Repeat("完整实现和边界情况说明。", 30) + "\n```"
}

func chatBody(t *testing.T, firstUser, assistant, lastUser string) string {
	t.Helper()
	return chatTurns(t, firstUser, assistant, lastUser)
}

func chatTurns(t *testing.T, parts ...string) string {
	t.Helper()
	messages := make([]map[string]string, 0, len(parts))
	for i, content := range parts {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		messages = append(messages, map[string]string{"role": role, "content": content})
	}
	body, err := common.Marshal(map[string]any{"model": "auto", "messages": messages})
	require.NoError(t, err)
	return string(body)
}

func newSmartRouterContext(t *testing.T, path, body string) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c
}

func mustSmartRouterDecision(t *testing.T, c *gin.Context) SmartRouterDecision {
	t.Helper()
	decision, ok := common.GetContextKeyType[SmartRouterDecision](c, constant.ContextKeySmartRouterDecision)
	require.True(t, ok)
	return decision
}
