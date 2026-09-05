package proxy

import (
	"io"
	"net/http"
	"time"

	"gpt-load/internal/channel"
	"gpt-load/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func (ps *ProxyServer) handleStreamingResponse(c *gin.Context, resp *http.Response) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		logrus.Error("Streaming unsupported by the writer, falling back to normal response")
		ps.handleNormalResponse(c, resp)
		return
	}

	buf := make([]byte, 4*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := c.Writer.Write(buf[:n]); writeErr != nil {
				logUpstreamError("writing stream to client", writeErr)
				return
			}
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			logUpstreamError("reading from upstream", err)
			return
		}
	}
}

func (ps *ProxyServer) handleNormalResponse(c *gin.Context, resp *http.Response) {
	if _, err := io.Copy(c.Writer, resp.Body); err != nil {
		logUpstreamError("copying response body", err)
	}
}

// handleCacheableResponse reads the full response body, caches it, and writes it to the client.
// Used for cacheable-channel successful responses (e.g. Tavily / Fengniao / Doubao).
func (ps *ProxyServer) handleCacheableResponse(c *gin.Context, resp *http.Response, originalGroup, group *models.Group, apiKey *models.APIKey, channelHandler channel.ChannelProxy, requestBody []byte) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logUpstreamError("reading response body for caching", err)
		c.Status(resp.StatusCode)
		return
	}

	// 成功路径的被动耗尽检测：风鸟/豆包等渠道把额度错误承载在 HTTP 200 响应体中，
	// 不会进入失败/重试分支，必须在这里补检一次（MarkExhausted 会封顶 used_quota，
	// 纠正此前对失败请求的 IncrementUsed 计数）。
	if channel.IsQuotaExhausted(channelHandler, resp.StatusCode, body) && ps.quotaTracker != nil {
		ps.quotaTracker.MarkExhausted(apiKey.ID)
	}

	// Only cache responses the channel considers a real business success —
	// e.g. Fengniao/Doubao report errors inside HTTP 200 bodies.
	if channel.IsCacheableResponse(channelHandler, resp.StatusCode, body) {
		endpoint := cacheEndpoint(c.Request, originalGroup.Name)
		if cacheKey, err := models.GenerateCacheKey(group.ID, c.Request.Method, endpoint, requestBody); err == nil {
			if putErr := ps.cacheService.Put(cacheKey, group.ID, endpoint, string(body), resp.StatusCode, ps.cacheService.GetTTLForGroup(group.ID)); putErr != nil {
				logrus.WithFields(logrus.Fields{
					"group":    group.Name,
					"endpoint": endpoint,
					"error":    putErr,
				}).Warn("Failed to cache response")
			}
		}
	}

	c.Status(resp.StatusCode)
	c.Writer.Write(body)
}

// handleCachedResponse serves a cached response directly to the client, bypassing key selection and upstream calls.
func (ps *ProxyServer) handleCachedResponse(c *gin.Context, cached *models.SearchCache, startTime time.Time, channelHandler channel.ChannelProxy, originalGroup, group *models.Group, bodyBytes []byte) {
	c.Header("Content-Type", "application/json")
	c.Header("X-Cache", "HIT")
	c.Status(cached.StatusCode)
	c.Writer.Write([]byte(cached.ResponseBody))

	duration := time.Since(startTime)
	logrus.WithFields(logrus.Fields{
		"group":    group.Name,
		"endpoint": cached.Endpoint,
		"duration": duration.String(),
		"hits":     cached.HitCount,
	}).Debug("Served cached response")

	// Log the cached request.
	ps.logRequest(c, originalGroup, group, nil, startTime, cached.StatusCode, nil, false, "", channelHandler, bodyBytes, models.RequestTypeFinal)
}
