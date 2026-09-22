package model

import (
	"context"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RequestLog struct {
	Id               int64     `json:"id" gorm:"primaryKey;index:idx_request_logs_created,priority:2;index:idx_request_logs_user_created,priority:3;index:idx_request_logs_token_created,priority:3"`
	RequestId        string    `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	UserId           int       `json:"user_id" gorm:"index:idx_request_logs_user_created,priority:1"`
	Username         string    `json:"username" gorm:"type:varchar(64);index"`
	TokenId          int       `json:"token_id" gorm:"index:idx_request_logs_token_created,priority:1"`
	TokenName        string    `json:"token_name" gorm:"type:varchar(191);index"`
	Protocol         string    `json:"protocol" gorm:"type:varchar(32);index"`
	ModelName        string    `json:"model_name" gorm:"type:varchar(191);index"`
	ResponseModel    string    `json:"response_model,omitempty" gorm:"type:varchar(191)"`
	Status           string    `json:"status" gorm:"type:varchar(32);index"`
	HttpStatus       int       `json:"http_status"`
	IsStream         bool      `json:"is_stream"`
	AttemptCount     int       `json:"attempt_count"`
	ChannelIds       JSONValue `json:"channel_ids" gorm:"type:json"`
	Conversation     JSONValue `json:"conversation,omitempty" gorm:"type:json"`
	ToolNames        JSONValue `json:"tool_names,omitempty" gorm:"type:json"`
	HistoryComplete  bool      `json:"history_complete"`
	HistoryReference string    `json:"history_reference,omitempty" gorm:"type:varchar(255)"`
	CaptureStatus    string    `json:"capture_status" gorm:"type:varchar(32);index"`
	RecordTruncated  bool      `json:"record_truncated"`
	Partial          bool      `json:"partial"`
	SchemaVersion    int       `json:"schema_version"`
	ErrorCode        string    `json:"error_code,omitempty" gorm:"type:varchar(128)"`
	CreatedAt        int64     `json:"created_at" gorm:"bigint;index:idx_request_logs_created,priority:1;index:idx_request_logs_user_created,priority:2;index:idx_request_logs_token_created,priority:2"`
	DurationMs       int64     `json:"duration_ms"`
	ExpiresAt        int64     `json:"expires_at" gorm:"bigint;index"`
	PayloadBytes     int64     `json:"-" gorm:"-"`
}

type RequestLogFilter struct {
	UserId         int
	TokenId        int
	Username       string
	TokenName      string
	ModelName      string
	Protocol       string
	Status         string
	RequestId      string
	StartTimestamp int64
	EndTimestamp   int64
}

type RequestLogListItem struct {
	Id              int64  `json:"id"`
	RequestId       string `json:"request_id"`
	UserId          int    `json:"user_id"`
	Username        string `json:"username"`
	TokenId         int    `json:"token_id"`
	TokenName       string `json:"token_name"`
	Protocol        string `json:"protocol"`
	ModelName       string `json:"model_name"`
	ResponseModel   string `json:"response_model,omitempty"`
	Status          string `json:"status"`
	HttpStatus      int    `json:"http_status"`
	IsStream        bool   `json:"is_stream"`
	AttemptCount    int    `json:"attempt_count"`
	CaptureStatus   string `json:"capture_status"`
	RecordTruncated bool   `json:"record_truncated"`
	Partial         bool   `json:"partial"`
	ErrorCode       string `json:"error_code,omitempty"`
	CreatedAt       int64  `json:"created_at"`
	DurationMs      int64  `json:"duration_ms"`
}

func CreateRequestLogs(ctx context.Context, logs []*RequestLog) error {
	if len(logs) == 0 {
		return nil
	}
	return DB.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "request_id"}}, DoNothing: true}).
		CreateInBatches(logs, 5).Error
}

func ListRequestLogs(filter RequestLogFilter, offset int, limit int) ([]RequestLogListItem, int64, error) {
	if limit <= 0 {
		limit = common.ItemsPerPage
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	query, err := applyRequestLogFilter(DB.Model(&RequestLog{}), filter)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]RequestLogListItem, 0)
	err = query.
		Select("id, request_id, user_id, username, token_id, token_name, protocol, model_name, response_model, status, http_status, is_stream, attempt_count, capture_status, record_truncated, partial, error_code, created_at, duration_ms").
		Order("created_at DESC, id DESC").
		Offset(offset).
		Limit(limit).
		Scan(&items).Error
	return items, total, err
}

func GetRequestLogById(id int64) (*RequestLog, error) {
	var log RequestLog
	if err := DB.First(&log, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &log, nil
}

func CountOldRequestLog(ctx context.Context, targetTimestamp int64) (int64, error) {
	var total int64
	if err := DB.WithContext(ctx).Model(&RequestLog{}).Where("created_at < ?", targetTimestamp).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func DeleteExpiredRequestLogsBatch(ctx context.Context, cutoff int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 500
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	var ids []int64
	if err := DB.WithContext(ctx).
		Model(&RequestLog{}).
		Select("id").
		Where("created_at < ?", cutoff).
		Order("id ASC").
		Limit(limit).
		Scan(&ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := DB.WithContext(ctx).Where("id IN ?", ids).Delete(&RequestLog{})
	return result.RowsAffected, result.Error
}

func applyRequestLogFilter(query *gorm.DB, filter RequestLogFilter) (*gorm.DB, error) {
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if filter.TokenId > 0 {
		query = query.Where("token_id = ?", filter.TokenId)
	}
	username := prefixLikePattern(filter.Username)
	if strings.Contains(username, "%") {
		pattern, err := sanitizeLikePattern(username)
		if err != nil {
			return query, err
		}
		query = query.Where("username LIKE ? ESCAPE '!'", pattern)
	} else if username != "" {
		query = query.Where("username = ?", username)
	}
	if filter.TokenName != "" {
		query = query.Where("token_name = ?", filter.TokenName)
	}
	if filter.ModelName != "" {
		query = query.Where("model_name = ?", filter.ModelName)
	}
	if filter.Protocol != "" {
		query = query.Where("protocol = ?", filter.Protocol)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.RequestId != "" {
		query = query.Where("request_id = ?", filter.RequestId)
	}
	if filter.StartTimestamp > 0 {
		query = query.Where("created_at >= ?", filter.StartTimestamp)
	}
	if filter.EndTimestamp > 0 {
		query = query.Where("created_at <= ?", filter.EndTimestamp)
	}
	return query, nil
}
