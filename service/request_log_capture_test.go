package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestRequestLogProtocolNormalization(t *testing.T) {
	t.Run("chat and claude roles", func(t *testing.T) {
		toolNames := map[string]struct{}{}
		messages := normalizeRoleMessages([]any{
			map[string]any{"role": "system", "content": "secret system prompt"},
			map[string]any{"role": "user", "content": "keep this user message"},
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "text", "text": strings.Repeat("答", requestLogMaxAssistantRunes+10)},
					map[string]any{"type": "tool_use", "name": "lookup", "input": map[string]any{"secret": "discard"}},
				},
			},
			map[string]any{"role": "tool", "content": strings.Repeat("tool output", 100)},
		}, toolNames)

		require.Len(t, messages, 3)
		assert.Equal(t, "keep this user message", messages[0].Parts[0].Text)
		assert.Equal(t, "assistant", messages[1].Role)
		assert.True(t, messages[1].Truncated)
		assert.Equal(t, requestLogMaxAssistantRunes, utf8.RuneCountInString(messages[1].Parts[0].Text))
		assert.Equal(t, "tool_call", messages[1].Parts[1].Type)
		assert.Equal(t, "lookup", messages[1].Parts[1].Name)
		assert.Equal(t, "tool_result", messages[2].Parts[0].Type)
		assert.Contains(t, toolNames, "lookup")
	})

	t.Run("responses input", func(t *testing.T) {
		toolNames := map[string]struct{}{}
		messages := normalizeResponsesInput([]any{
			map[string]any{"role": "developer", "content": "hidden"},
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "question"},
					map[string]any{"type": "input_image", "image_url": "data:image/png;base64,discard"},
				},
			},
			map[string]any{"type": "function_call", "name": "search", "arguments": strings.Repeat("x", 1000)},
			map[string]any{"type": "function_call_output", "output": strings.Repeat("y", 1000)},
		}, toolNames)

		require.Len(t, messages, 3)
		assert.Equal(t, []RequestLogPart{
			{Type: "text", Text: "question"},
			{Type: "image"},
		}, messages[0].Parts)
		assert.Equal(t, "tool_call", messages[1].Parts[0].Type)
		assert.Equal(t, "tool_result", messages[2].Parts[0].Type)
		assert.Contains(t, toolNames, "search")
	})

	t.Run("gemini roles and parts", func(t *testing.T) {
		toolNames := map[string]struct{}{}
		messages := normalizeGeminiContents([]any{
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{"text": "question"},
					map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": "discard"}},
				},
			},
			map[string]any{
				"role": "model",
				"parts": []any{
					map[string]any{"functionCall": map[string]any{"name": "lookup", "args": map[string]any{"discard": true}}},
				},
			},
		}, toolNames)

		require.Len(t, messages, 2)
		assert.Equal(t, "user", messages[0].Role)
		assert.Equal(t, "image", messages[0].Parts[1].Type)
		assert.Equal(t, "assistant", messages[1].Role)
		assert.Equal(t, "tool_call", messages[1].Parts[0].Type)
		assert.Contains(t, toolNames, "lookup")
	})

	t.Run("responses output items remain one choice", func(t *testing.T) {
		capture := &RequestLogCapture{
			protocol:  "responses",
			responses: map[int]*requestLogResponse{},
			toolNames: map[string]struct{}{},
		}
		capture.observeResponsesResponse(map[string]any{
			"output": []any{
				map[string]any{"type": "function_call", "id": "call-1", "name": "lookup"},
				map[string]any{
					"type": "message",
					"content": []any{
						map[string]any{"type": "output_text", "text": "answer"},
					},
				},
			},
		}, false)

		require.Len(t, capture.responses, 1)
		response := capture.responses[0]
		require.NotNil(t, response)
		assert.Equal(t, []RequestLogPart{
			{Type: "tool_call", Name: "lookup"},
			{Type: "text", Text: "answer"},
		}, response.parts)
	})
}

func TestRequestLogStreamCaptureAndConversationLimit(t *testing.T) {
	capture := &RequestLogCapture{
		protocol:  "chat_completions",
		responses: map[int]*requestLogResponse{},
		toolNames: map[string]struct{}{},
		isStream:  true,
	}
	capture.observeSSEBytes([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hel"))
	capture.observeSSEBytes([]byte("lo\"}}]}\n\ndata: [DONE]\n\n"))

	response := capture.responses[0]
	require.NotNil(t, response)
	require.Len(t, response.parts, 1)
	assert.Equal(t, "hello", response.parts[0].Text)
	assert.True(t, capture.streamCompleted)

	messages := []RequestLogMessage{
		{Role: "user", Source: "request", Parts: []RequestLogPart{{Type: "text", Text: strings.Repeat("a", requestLogMaxConversationSize)}}},
		{Role: "user", Source: "request", Parts: []RequestLogPart{{Type: "text", Text: strings.Repeat("b", requestLogMaxConversationSize)}}},
		{Role: "assistant", Source: "response", Parts: []RequestLogPart{{Type: "text", Text: "latest response"}}},
	}
	data, truncated, err := marshalRequestLogConversation(messages)
	require.NoError(t, err)
	assert.True(t, truncated)
	assert.LessOrEqual(t, len(data), requestLogMaxConversationSize)

	var normalized []RequestLogMessage
	require.NoError(t, common.Unmarshal(data, &normalized))
	require.NotEmpty(t, normalized)
	assert.Equal(t, "response", normalized[len(normalized)-1].Source)
	assert.Equal(t, "latest response", normalized[len(normalized)-1].Parts[0].Text)
}

func TestRequestLogFinishStatusUsesStreamCompletion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name        string
		body        string
		wantStatus  string
		wantPartial bool
	}{
		{
			name:        "canceled context after finish_reason stays success",
			body:        "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\n",
			wantStatus:  "success",
			wantPartial: false,
		},
		{
			name:        "canceled context before stream end is client disconnected",
			body:        "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n",
			wantStatus:  "client_disconnected",
			wantPartial: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			written := make(chan *model.RequestLog, 1)
			writer := newRequestLogWriter(requestLogWriterConfig{
				queueCapacity:  1,
				workerCount:    1,
				batchSize:      1,
				flushInterval:  time.Hour,
				maxQueuedBytes: requestLogMaxConversationSize,
				insert: func(_ context.Context, logs []*model.RequestLog) error {
					written <- logs[0]
					return nil
				},
			})
			previousWriter := requestLogWriter
			previousEnabled := common.RequestLogEnabled
			requestLogWriter = writer
			common.RequestLogEnabled = true
			t.Cleanup(func() {
				_ = writer.shutdown(t.Context())
				requestLogWriter = previousWriter
				common.RequestLogEnabled = previousEnabled
			})
			writer.start()

			recorder := httptest.NewRecorder()
			ginContext, _ := gin.CreateTestContext(recorder)
			ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{
				RequestId:       test.name,
				UserId:          7,
				TokenId:         9,
				OriginModelName: "gpt-5",
				RelayMode:       relayconstant.RelayModeChatCompletions,
				IsStream:        true,
				StartTime:       time.Now(),
			}
			request := &dto.GeneralOpenAIRequest{}
			require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-5","messages":[{"role":"user","content":"question"}]}`), request))
			capture := BeginRequestLogCapture(ginContext, relaytypes.RelayFormatOpenAI, request, info)
			require.NotNil(t, capture)
			_, err := ginContext.Writer.Write([]byte(test.body))
			require.NoError(t, err)
			canceled, cancel := context.WithCancel(ginContext.Request.Context())
			cancel()
			ginContext.Request = ginContext.Request.WithContext(canceled)
			capture.Finish(ginContext, nil)

			select {
			case log := <-written:
				assert.Equal(t, test.wantStatus, log.Status)
				assert.Equal(t, test.wantPartial, log.Partial)
			case <-time.After(time.Second):
				t.Fatal("captured request log was not written")
			}
		})
	}
}

func TestRequestLogHTTPResponseCapture(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name              string
		path              string
		format            relaytypes.RelayFormat
		mode              int
		request           func(*testing.T) dto.Request
		nonStreamResponse string
		streamResponse    string
		wantProtocol      string
		wantResponse      string
	}{
		{
			name:   "chat completions",
			path:   "/v1/chat/completions",
			format: relaytypes.RelayFormatOpenAI,
			mode:   relayconstant.RelayModeChatCompletions,
			request: func(t *testing.T) dto.Request {
				request := &dto.GeneralOpenAIRequest{}
				require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-5","messages":[{"role":"user","content":"question"}]}`), request))
				return request
			},
			nonStreamResponse: `{"model":"gpt-5","choices":[{"index":0,"message":{"role":"assistant","content":"chat answer"}}]}`,
			streamResponse:    "data: {\"model\":\"gpt-5\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"chat answer\"}}]}\n\ndata: [DONE]\n\n",
			wantProtocol:      "chat_completions",
			wantResponse:      "chat answer",
		},
		{
			name:   "responses",
			path:   "/v1/responses",
			format: relaytypes.RelayFormatOpenAIResponses,
			mode:   relayconstant.RelayModeResponses,
			request: func(t *testing.T) dto.Request {
				request := &dto.OpenAIResponsesRequest{}
				require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-5","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"question"}]}]}`), request))
				return request
			},
			nonStreamResponse: `{"model":"gpt-5","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"responses answer"}]}]}`,
			streamResponse:    "data: {\"type\":\"response.output_text.delta\",\"output_index\":1,\"delta\":\"responses answer\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5\"}}\n\n",
			wantProtocol:      "responses",
			wantResponse:      "responses answer",
		},
		{
			name:   "claude",
			path:   "/v1/messages",
			format: relaytypes.RelayFormatClaude,
			request: func(t *testing.T) dto.Request {
				request := &dto.ClaudeRequest{}
				require.NoError(t, common.Unmarshal([]byte(`{"model":"claude-sonnet","max_tokens":100,"messages":[{"role":"user","content":"question"}]}`), request))
				return request
			},
			nonStreamResponse: `{"type":"message","model":"claude-sonnet","content":[{"type":"text","text":"claude answer"}]}`,
			streamResponse:    "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"claude answer\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n",
			wantProtocol:      "claude",
			wantResponse:      "claude answer",
		},
		{
			name:   "gemini",
			path:   "/v1beta/models/gemini-2.5-pro:generateContent",
			format: relaytypes.RelayFormatGemini,
			request: func(t *testing.T) dto.Request {
				request := &dto.GeminiChatRequest{}
				require.NoError(t, common.Unmarshal([]byte(`{"contents":[{"role":"user","parts":[{"text":"question"}]}]}`), request))
				return request
			},
			nonStreamResponse: `{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"gemini answer"}]},"finishReason":"STOP"}]}`,
			streamResponse:    "data: {\"candidates\":[{\"index\":0,\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"gemini answer\"}]},\"finishReason\":\"STOP\"}]}\n\n",
			wantProtocol:      "gemini",
			wantResponse:      "gemini answer",
		},
	}

	for _, test := range tests {
		for _, stream := range []bool{false, true} {
			name := "non-stream"
			if stream {
				name = "stream"
			}
			t.Run(test.name+"/"+name, func(t *testing.T) {
				written := make(chan *model.RequestLog, 1)
				writer := newRequestLogWriter(requestLogWriterConfig{
					queueCapacity:  1,
					workerCount:    1,
					batchSize:      1,
					flushInterval:  time.Hour,
					maxQueuedBytes: requestLogMaxConversationSize,
					insert: func(_ context.Context, logs []*model.RequestLog) error {
						written <- logs[0]
						return nil
					},
				})
				previousWriter := requestLogWriter
				previousEnabled := common.RequestLogEnabled
				requestLogWriter = writer
				common.RequestLogEnabled = true
				t.Cleanup(func() {
					_ = writer.shutdown(t.Context())
					requestLogWriter = previousWriter
					common.RequestLogEnabled = previousEnabled
				})
				writer.start()

				recorder := httptest.NewRecorder()
				ginContext, _ := gin.CreateTestContext(recorder)
				ginContext.Request = httptest.NewRequest(http.MethodPost, test.path, nil)
				ginContext.Set("username", "alice")
				ginContext.Set("token_name", "cursor")
				info := &relaycommon.RelayInfo{
					RequestId:       test.name + "-" + name,
					UserId:          7,
					TokenId:         9,
					OriginModelName: "test-model",
					RelayMode:       test.mode,
					IsStream:        stream,
					StartTime:       time.Now(),
				}
				capture := BeginRequestLogCapture(ginContext, test.format, test.request(t), info)
				require.NotNil(t, capture)
				response := test.nonStreamResponse
				if stream {
					response = test.streamResponse
				}
				_, err := ginContext.Writer.Write([]byte(response))
				require.NoError(t, err)
				canceled, cancel := context.WithCancel(ginContext.Request.Context())
				cancel()
				ginContext.Request = ginContext.Request.WithContext(canceled)
				capture.Finish(ginContext, nil)

				select {
				case log := <-written:
					assert.Equal(t, test.wantProtocol, log.Protocol)
					assert.Equal(t, "success", log.CaptureStatus)
					assert.False(t, log.Partial)
					var conversation []RequestLogMessage
					require.NoError(t, common.Unmarshal(log.Conversation, &conversation))
					require.GreaterOrEqual(t, len(conversation), 2)
					assert.Equal(t, "question", conversation[0].Parts[0].Text)
					responseMessage := conversation[len(conversation)-1]
					assert.Equal(t, "response", responseMessage.Source)
					assert.Equal(t, test.wantResponse, responseMessage.Parts[len(responseMessage.Parts)-1].Text)
				case <-time.After(time.Second):
					t.Fatal("captured request log was not written")
				}
				require.NoError(t, writer.shutdown(t.Context()))
			})
		}
	}
}

func TestRequestLogPersistenceAndCleanup(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var dialector gorm.Dialector
			switch dialect {
			case "sqlite":
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "request-logs.db"))
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				dialector = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				dialector = postgres.Open(dsn)
			}

			db, err := gorm.Open(dialector, &gorm.Config{
				NamingStrategy: schema.NamingStrategy{TablePrefix: "request_log_test_"},
			})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			previousDB := model.DB
			model.DB = db
			t.Cleanup(func() {
				model.DB = previousDB
				require.NoError(t, db.Migrator().DropTable(&model.RequestLog{}, &model.Option{}))
				require.NoError(t, sqlDB.Close())
			})

			versionQuery := "SELECT version()"
			if dialect == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database version: %s", version)

			require.NoError(t, db.AutoMigrate(&model.Option{}))
			require.NoError(t, db.Create(&model.Option{Key: "request-log-upgrade-sentinel", Value: "preserve"}).Error)
			require.NoError(t, db.AutoMigrate(&model.RequestLog{}))
			require.NoError(t, db.AutoMigrate(&model.RequestLog{}))
			var sentinel model.Option
			require.NoError(t, db.Where(map[string]any{"key": "request-log-upgrade-sentinel"}).First(&sentinel).Error)
			assert.Equal(t, "preserve", sentinel.Value)
			logs := []*model.RequestLog{
				{
					RequestId:     "req-old",
					UserId:        1,
					TokenId:       10,
					Protocol:      "chat_completions",
					ModelName:     "gpt-5",
					Status:        "success",
					CaptureStatus: "success",
					Conversation:  model.JSONValue(`[]`),
					ToolNames:     model.JSONValue(`[]`),
					ChannelIds:    model.JSONValue(`[]`),
					CreatedAt:     100,
				},
				{
					RequestId:     "req-new",
					UserId:        2,
					TokenId:       20,
					Protocol:      "claude",
					ModelName:     "claude-sonnet",
					Status:        "failed",
					CaptureStatus: "failed",
					Conversation:  model.JSONValue(`[{"role":"user","parts":[{"type":"text","text":"question"}],"source":"request"}]`),
					ToolNames:     model.JSONValue(`[]`),
					ChannelIds:    model.JSONValue(`["3"]`),
					CreatedAt:     200,
				},
			}
			require.NoError(t, model.CreateRequestLogs(t.Context(), logs))
			require.NoError(t, model.CreateRequestLogs(t.Context(), logs))

			items, total, err := model.ListRequestLogs(model.RequestLogFilter{UserId: 2}, 0, 10)
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
			require.Len(t, items, 1)
			assert.Equal(t, "req-new", items[0].RequestId)

			oldCount, err := model.CountOldRequestLog(t.Context(), 150)
			require.NoError(t, err)
			assert.EqualValues(t, 1, oldCount)

			deleted, err := model.DeleteExpiredRequestLogsBatch(t.Context(), 150, 10)
			require.NoError(t, err)
			assert.EqualValues(t, 1, deleted)

			oldCount, err = model.CountOldRequestLog(t.Context(), 150)
			require.NoError(t, err)
			assert.Zero(t, oldCount)
		})
	}
}

func TestRequestLogWriterBehavior(t *testing.T) {
	t.Run("flushes a full batch immediately", func(t *testing.T) {
		batches := make(chan []string, 1)
		writer := newRequestLogWriter(requestLogWriterConfig{
			queueCapacity:  10,
			workerCount:    1,
			batchSize:      5,
			flushInterval:  time.Hour,
			maxQueuedBytes: 1024,
			insert: func(_ context.Context, logs []*model.RequestLog) error {
				ids := make([]string, 0, len(logs))
				for _, log := range logs {
					ids = append(ids, log.RequestId)
				}
				batches <- ids
				return nil
			},
		})
		writer.start()
		for index := range 5 {
			assert.True(t, writer.enqueue(&model.RequestLog{RequestId: "batch-" + string(rune('0'+index)), PayloadBytes: 1}))
		}

		select {
		case batch := <-batches:
			assert.Equal(t, []string{"batch-0", "batch-1", "batch-2", "batch-3", "batch-4"}, batch)
		case <-time.After(time.Second):
			t.Fatal("full request-log batch was not flushed")
		}
		require.NoError(t, writer.shutdown(t.Context()))
		assert.Zero(t, writer.queuedBytes.Load())
	})

	t.Run("flushes a partial batch on the interval", func(t *testing.T) {
		batches := make(chan int, 1)
		writer := newRequestLogWriter(requestLogWriterConfig{
			queueCapacity:  10,
			workerCount:    1,
			batchSize:      5,
			flushInterval:  10 * time.Millisecond,
			maxQueuedBytes: 1024,
			insert: func(_ context.Context, logs []*model.RequestLog) error {
				batches <- len(logs)
				return nil
			},
		})
		writer.start()
		assert.True(t, writer.enqueue(&model.RequestLog{RequestId: "interval", PayloadBytes: 1}))

		select {
		case count := <-batches:
			assert.Equal(t, 1, count)
		case <-time.After(time.Second):
			t.Fatal("partial request-log batch was not flushed")
		}
		require.NoError(t, writer.shutdown(t.Context()))
	})

	t.Run("runs five writes concurrently", func(t *testing.T) {
		started := make(chan struct{}, requestLogWorkerCount)
		release := make(chan struct{})
		writer := newRequestLogWriter(requestLogWriterConfig{
			queueCapacity:  requestLogWorkerCount,
			workerCount:    requestLogWorkerCount,
			batchSize:      1,
			flushInterval:  time.Hour,
			maxQueuedBytes: 1024,
			insert: func(_ context.Context, _ []*model.RequestLog) error {
				started <- struct{}{}
				<-release
				return nil
			},
		})
		writer.start()
		for index := range requestLogWorkerCount {
			assert.True(t, writer.enqueue(&model.RequestLog{RequestId: "parallel-" + string(rune('0'+index)), PayloadBytes: 1}))
		}
		for range requestLogWorkerCount {
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("five request-log workers did not run concurrently")
			}
		}
		close(release)
		require.NoError(t, writer.shutdown(t.Context()))
	})

	t.Run("drops when queue or byte limit is reached", func(t *testing.T) {
		for _, test := range []struct {
			name           string
			queueCapacity  int
			maxQueuedBytes int64
			firstBytes     int64
			queuedItems    int
		}{
			{name: "queue", queueCapacity: 2, maxQueuedBytes: 1024, firstBytes: 1, queuedItems: 2},
			{name: "bytes", queueCapacity: 2, maxQueuedBytes: 10, firstBytes: 7},
		} {
			t.Run(test.name, func(t *testing.T) {
				started := make(chan struct{}, 1)
				release := make(chan struct{})
				writer := newRequestLogWriter(requestLogWriterConfig{
					queueCapacity:  test.queueCapacity,
					workerCount:    1,
					batchSize:      1,
					flushInterval:  time.Hour,
					maxQueuedBytes: test.maxQueuedBytes,
					insert: func(_ context.Context, _ []*model.RequestLog) error {
						select {
						case started <- struct{}{}:
						default:
						}
						<-release
						return nil
					},
				})
				writer.start()
				assert.True(t, writer.enqueue(&model.RequestLog{RequestId: test.name + "-active", PayloadBytes: test.firstBytes}))
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("request-log writer did not begin the blocking write")
				}

				for index := range test.queuedItems {
					assert.True(t, writer.enqueue(&model.RequestLog{RequestId: test.name + "-queued-" + string(rune('0'+index)), PayloadBytes: 1}))
				}
				assert.False(t, writer.enqueue(&model.RequestLog{RequestId: test.name + "-dropped", PayloadBytes: 4}))
				assert.EqualValues(t, 1, writer.dropped.Load())
				close(release)
				require.NoError(t, writer.shutdown(t.Context()))
			})
		}
	})

	t.Run("discards a failed batch without retry", func(t *testing.T) {
		var calls atomic.Int64
		writer := newRequestLogWriter(requestLogWriterConfig{
			queueCapacity:  5,
			workerCount:    1,
			batchSize:      5,
			flushInterval:  time.Hour,
			maxQueuedBytes: 1024,
			insert: func(_ context.Context, _ []*model.RequestLog) error {
				calls.Add(1)
				return errors.New("forced request-log write failure")
			},
		})
		writer.start()
		assert.True(t, writer.enqueue(&model.RequestLog{RequestId: "failed-batch", PayloadBytes: 10}))
		require.NoError(t, writer.shutdown(t.Context()))
		assert.EqualValues(t, 1, calls.Load())
		assert.EqualValues(t, 1, writer.writeFailed.Load())
		assert.Zero(t, writer.queuedBytes.Load())
	})
}
