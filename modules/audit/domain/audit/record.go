// Package audit 是审计模块的领域层：append-only 审计记录聚合与端口。
//
// 审计记录回答「谁（actor）在何时（occurred_at）对什么（resource）做了
// 什么（action）」，载荷是业务自定义 JSON。写入只有 Append——领域层不
// 提供修改与删除（append-only 是审计的合法性前提，不靠 DB 约束靠端口
// 形状），读取走结构化过滤 + 分页。
package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lynxland/appkit/errcode"
)

// 事件目录（契约②）：与 Module().PublishedEvents、item.json 三方一致。
const (
	// EventLogRecorded 是一条审计记录落库后发布的事件（供 SIEM 类下游
	// 消费；载荷为审计记录的 JSON 形态）。
	EventLogRecorded = "audit.log.recorded"
)

// Source 是记录来源：api = 显式 Record 调用，rpc = 拦截器自动记录。
const (
	SourceAPI = "api"
	SourceRPC = "rpc"
)

var (
	space              = errcode.MustRegister("audit")
	ErrEmptyAction     = space.New("EMPTY_ACTION", "action must not be empty")
	ErrPayloadTooLarge = space.New("PAYLOAD_TOO_LARGE", "payload exceeds the size limit")
)

// MaxPayloadBytes 是单条载荷上限（防写入放大；超限报错由调用方取舍）。
const MaxPayloadBytes = 4 * 1024

// Record 是审计记录聚合（单实体，无生命周期状态机：创建即终态）。
// json tag 供事件载荷与 API 层序列化（audit.log.recorded 的载荷即本形态）。
type Record struct {
	ID           string          `json:"id"`
	ActorID      string          `json:"actor_id,omitempty"`
	ActorEmail   string          `json:"actor_email,omitempty"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type,omitempty"`
	ResourceID   string          `json:"resource_id,omitempty"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	Source       string          `json:"source"`
	OccurredAt   time.Time       `json:"occurred_at"`
}

// NewRecord 构造并校验（action 必填；载荷合法 JSON 且不超限；source 缺省
// SourceAPI）。ID 与时间由调用方（idgen/clock）注入，便于测试确定性。
func NewRecord(id, actorID, actorEmail, action, resourceType, resourceID string, payload json.RawMessage, source string, occurredAt time.Time) (*Record, error) {
	if action == "" {
		return nil, ErrEmptyAction
	}
	if len(payload) > MaxPayloadBytes {
		return nil, fmt.Errorf("%w: %d bytes (limit %d)", ErrPayloadTooLarge, len(payload), MaxPayloadBytes)
	}
	if len(payload) > 0 {
		if !json.Valid(payload) {
			return nil, errors.New("audit: payload is not valid JSON")
		}
	}
	if source == "" {
		source = SourceAPI
	}
	return &Record{
		ID:           id,
		ActorID:      actorID,
		ActorEmail:   actorEmail,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Payload:      payload,
		Source:       source,
		OccurredAt:   occurredAt,
	}, nil
}

// Filter 是结构化查询过滤（列表端口的动态查询语义归模块，不进 appkit）。
type Filter struct {
	ActorID      string
	Action       string
	ResourceType string
	ResourceID   string
}
