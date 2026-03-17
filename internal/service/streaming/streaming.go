package streaming

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type Streaming struct{}

func NewStreaming() *Streaming {
	return &Streaming{}
}

func (s *Streaming) ProxyChat(c *gin.Context, stream <-chan string, errCh <-chan error) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	var totalTokens int
	for {
		select {
		case data, ok := <-stream:
			if !ok {
				// Stream done
				s.sendFinal(c, flusher, totalTokens)
				return
			}

			// Count tokens from the chunk
			totalTokens += s.countTokens(data)

			// Forward the chunk
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			flusher.Flush()

		case err := <-errCh:
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

		case <-c.Request.Context().Done():
			// Client disconnected
			return
		}
	}
}

func (s *Streaming) sendFinal(c *gin.Context, flusher http.Flusher, tokens int) {
	final := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"completion_tokens": tokens,
			"total_tokens":      tokens,
		},
	}
	data, _ := json.Marshal(final)
	fmt.Fprintf(c.Writer, "data: %s\n\n", data)
	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	flusher.Flush()
}

func (s *Streaming) countTokens(data string) int {
	// Rough estimation: ~4 characters per token
	// For accurate counting, use tiktoken or similar
	if data == "" || strings.Contains(data, "[DONE]") {
		return 0
	}
	return len(data) / 4
}

func (s *Streaming) ProxyCompletion(c *gin.Context, upstreamURL string, reqBody io.Reader) error {
	upstreamReq, err := http.NewRequestWithContext(c.Request.Context(), "POST", upstreamURL, reqBody)
	if err != nil {
		return err
	}

	upstreamReq.Header = make(http.Header)
	for k, v := range c.Request.Header {
		upstreamReq.Header[k] = v
	}
	upstreamReq.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(upstreamReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	c.Header("Content-Type", resp.Header.Get("Content-Type"))
	c.Header("Cache-Control", resp.Header.Get("Cache-Control"))
	c.Header("Connection", resp.Header.Get("Connection"))

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	var totalTokens int
	dec := json.NewDecoder(resp.Body)
	for {
		var chunk map[string]interface{}
		if err := dec.Decode(&chunk); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		data, _ := json.Marshal(chunk)
		totalTokens += s.countTokens(string(data))

		fmt.Fprintf(c.Writer, "data: %s\n\n", string(data))
		flusher.Flush()

		if chunk["done"] == true {
			break
		}
	}

	return nil
}
