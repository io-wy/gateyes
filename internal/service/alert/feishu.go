package alert

import (
	"context"
	"encoding/json"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// FeishuChannel delivers alerts to Feishu (Lark) via the official OpenAPI SDK.
// It supports sending text or interactive card messages to a user or chat.
type FeishuChannel struct {
	name          string
	client        *lark.Client
	receiveID     string
	receiveIDType string // open_id | user_id | email | chat_id
	msgType       string // text | interactive
	labels        map[string]string
}

// NewFeishuChannel creates a Feishu alert channel.
func NewFeishuChannel(name, appID, appSecret, receiveID, receiveIDType, msgType string, labels map[string]string) *FeishuChannel {
	if receiveIDType == "" {
		receiveIDType = "chat_id"
	}
	if msgType == "" {
		msgType = "text"
	}
	return &FeishuChannel{
		name:          name,
		client:        lark.NewClient(appID, appSecret),
		receiveID:     receiveID,
		receiveIDType: receiveIDType,
		msgType:       msgType,
		labels:        labels,
	}
}

func (f *FeishuChannel) Name() string { return f.name }

func (f *FeishuChannel) Match(labels map[string]string) bool {
	if len(f.labels) == 0 {
		return true
	}
	for k, v := range f.labels {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func (f *FeishuChannel) Send(ctx context.Context, alert Alert) error {
	content, err := f.buildContent(alert)
	if err != nil {
		return fmt.Errorf("build feishu message content: %w", err)
	}

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(f.receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(f.receiveID).
			MsgType(f.msgType).
			Content(content).
			Build()).
		Build()

	resp, err := f.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu send message: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu send message failed: code=%d, msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (f *FeishuChannel) buildContent(alert Alert) (string, error) {
	payload, err := json.Marshal(alert.Payload)
	if err != nil {
		return "", fmt.Errorf("marshal alert payload: %w", err)
	}
	switch f.msgType {
	case "interactive":
		card := map[string]any{
			"config": map[string]any{"wide_screen_mode": true},
			"header": map[string]any{
				"title": map[string]any{
					"tag":     "plain_text",
					"content": fmt.Sprintf("[%s] Gateyes %s", alert.Severity, alert.Type),
				},
			},
			"elements": []any{
				map[string]any{
					"tag": "div",
					"text": map[string]any{
						"tag":     "plain_text",
						"content": string(payload),
					},
				},
			},
		}
		b, err := json.Marshal(card)
		if err != nil {
			return "", err
		}
		return string(b), nil
	default:
		b, err := json.Marshal(map[string]any{"text": fmt.Sprintf("[%s] %s\n%s", alert.Severity, alert.Type, string(payload))})
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}

// compile-time interface check.
var _ Channel = (*FeishuChannel)(nil)
