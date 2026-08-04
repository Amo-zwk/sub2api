package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type AccountHealthHandler struct {
	controller *service.AccountHealthController
}

func NewAccountHealthHandler(controller *service.AccountHealthController) *AccountHealthHandler {
	return &AccountHealthHandler{controller: controller}
}

func (h *AccountHealthHandler) GetSummary(c *gin.Context) {
	summary, err := h.controller.Summary(c.Request.Context(), parseHealthGroupID(c))
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, summary)
}

func (h *AccountHealthHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	params := service.AccountHealthListParams{
		Page: page, PageSize: pageSize,
		Search:  strings.TrimSpace(c.Query("search")),
		State:   strings.TrimSpace(c.Query("state")),
		GroupID: parseHealthGroupID(c),
	}
	items, total, err := h.controller.List(c.Request.Context(), params)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *AccountHealthHandler) GetSettings(c *gin.Context) {
	response.Success(c, h.controller.Settings())
}

func (h *AccountHealthHandler) UpdateSettings(c *gin.Context) {
	var settings service.AccountHealthSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := h.controller.UpdateSettings(c.Request.Context(), settings); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, h.controller.Settings())
}

func (h *AccountHealthHandler) RunNow(c *gin.Context) {
	var req struct {
		GroupID int64 `json:"group_id"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	count, err := h.controller.RunNow(c.Request.Context(), req.GroupID)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Accepted(c, gin.H{"queued": count})
}

func (h *AccountHealthHandler) Events(c *gin.Context) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		response.InternalError(c, "streaming is not supported")
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	events, unsubscribe := h.controller.Subscribe()
	defer unsubscribe()
	writeHealthEvent(c, "ready", service.AccountHealthEvent{Type: "ready"})
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			writeHealthEvent(c, event.Type, event)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = fmt.Fprint(c.Writer, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func writeHealthEvent(c *gin.Context, eventName string, event service.AccountHealthEvent) {
	payload, _ := json.Marshal(event)
	_, _ = fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", eventName, payload)
}

func parseHealthGroupID(c *gin.Context) int64 {
	value := strings.TrimSpace(c.Query("group_id"))
	if value == "" {
		return 0
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 0 {
		return 0
	}
	return id
}
