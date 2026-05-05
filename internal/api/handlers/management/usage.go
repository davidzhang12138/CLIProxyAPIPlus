package management

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/redisqueue"
	legacyusage "github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

type usageQueueRecord []byte

func (r usageQueueRecord) MarshalJSON() ([]byte, error) {
	if json.Valid(r) {
		return append([]byte(nil), r...), nil
	}
	return json.Marshal(string(r))
}

// GetUsage returns the legacy in-memory usage statistics snapshot.
func (h *Handler) GetUsage(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler unavailable"})
		return
	}

	c.JSON(http.StatusOK, legacyusage.GetRequestStatistics().Snapshot())
}

// ExportUsage returns a legacy usage snapshot wrapped with export metadata.
func (h *Handler) ExportUsage(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler unavailable"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"version":     1,
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"usage":       legacyusage.GetRequestStatistics().Snapshot(),
	})
}

// ImportUsage merges a legacy usage snapshot into the in-memory usage store.
func (h *Handler) ImportUsage(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler unavailable"})
		return
	}

	var payload json.RawMessage
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var wrapped struct {
		Usage *legacyusage.StatisticsSnapshot `json:"usage"`
	}
	if err := json.Unmarshal(payload, &wrapped); err == nil && wrapped.Usage != nil {
		result := legacyusage.GetRequestStatistics().MergeSnapshot(*wrapped.Usage)
		c.JSON(http.StatusOK, gin.H{
			"added":   result.Added,
			"skipped": result.Skipped,
			"total_requests": legacyusage.GetRequestStatistics().
				Snapshot().TotalRequests,
		})
		return
	}

	var snapshot legacyusage.StatisticsSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result := legacyusage.GetRequestStatistics().MergeSnapshot(snapshot)
	c.JSON(http.StatusOK, gin.H{
		"added":          result.Added,
		"skipped":        result.Skipped,
		"total_requests": legacyusage.GetRequestStatistics().Snapshot().TotalRequests,
	})
}

// GetUsageQueue pops queued usage records from the usage queue.
func (h *Handler) GetUsageQueue(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler unavailable"})
		return
	}

	count, errCount := parseUsageQueueCount(c.Query("count"))
	if errCount != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errCount.Error()})
		return
	}

	items := redisqueue.PopOldest(count)
	records := make([]usageQueueRecord, 0, len(items))
	for _, item := range items {
		records = append(records, usageQueueRecord(append([]byte(nil), item...)))
	}

	c.JSON(http.StatusOK, records)
}

func parseUsageQueueCount(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 1, nil
	}
	count, errCount := strconv.Atoi(value)
	if errCount != nil || count <= 0 {
		return 0, errors.New("count must be a positive integer")
	}
	return count, nil
}
