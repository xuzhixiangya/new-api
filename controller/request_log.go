package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func ListRequestLogs(c *gin.Context) {
	var query struct {
		UserId         int    `form:"user_id" binding:"gte=0"`
		TokenId        int    `form:"token_id" binding:"gte=0"`
		Username       string `form:"username" binding:"max=64"`
		TokenName      string `form:"token_name" binding:"max=191"`
		ModelName      string `form:"model" binding:"max=191"`
		Protocol       string `form:"protocol" binding:"max=32"`
		Status         string `form:"status" binding:"max=32"`
		RequestId      string `form:"request_id" binding:"max=64"`
		StartTimestamp int64  `form:"start_timestamp" binding:"gte=0"`
		EndTimestamp   int64  `form:"end_timestamp" binding:"gte=0"`
		Offset         int    `form:"offset" binding:"gte=0"`
		Limit          int    `form:"limit" binding:"gte=0,lte=100"`
	}
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request log filters"})
		return
	}
	if query.StartTimestamp > 0 && query.EndTimestamp > 0 && query.StartTimestamp > query.EndTimestamp {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "start timestamp must not be after end timestamp"})
		return
	}

	items, total, err := model.ListRequestLogs(model.RequestLogFilter{
		UserId:         query.UserId,
		TokenId:        query.TokenId,
		Username:       query.Username,
		TokenName:      query.TokenName,
		ModelName:      query.ModelName,
		Protocol:       query.Protocol,
		Status:         query.Status,
		RequestId:      query.RequestId,
		StartTimestamp: query.StartTimestamp,
		EndTimestamp:   query.EndTimestamp,
	}, query.Offset, query.Limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    items,
		"total":   total,
	})
}

func GetRequestLog(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request log id"})
		return
	}
	item, err := model.GetRequestLogById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "request log not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    item,
	})
}
