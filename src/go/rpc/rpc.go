package server

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"whatsapp-rpc/src/go/whatsapp"
)

// JSON-RPC 2.0 message types
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// RPCHandler processes JSON-RPC requests
type RPCHandler struct {
	service *whatsapp.Service
	logger  *logrus.Logger
}

// NewRPCHandler creates a new RPC handler
func NewRPCHandler(service *whatsapp.Service, logger *logrus.Logger) *RPCHandler {
	return &RPCHandler{
		service: service,
		logger:  logger,
	}
}

// HandleRequest processes a JSON-RPC request and returns a response
func (h *RPCHandler) HandleRequest(req *RPCRequest) RPCResponse {
	resp := RPCResponse{JSONRPC: "2.0", ID: req.ID}

	switch req.Method {
	case "status":
		resp.Result = h.service.GetStatus()

	case "start":
		if err := h.service.Start(); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = map[string]string{"message": "Started"}
		}

	case "stop":
		h.service.Shutdown()
		resp.Result = map[string]string{"message": "Stopped"}

	case "restart":
		// Full restart: cleanup QR codes, reset session, then start fresh
		h.service.CleanupQRCodes()
		if err := h.service.Reset(); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else if err := h.service.Start(); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = map[string]string{"message": "Restarted (session cleared)"}
		}

	case "reset":
		if err := h.service.Reset(); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = map[string]string{"message": "Reset"}
		}

	case "diagnostics":
		resp.Result = h.service.GetDiagnostics()

	case "qr":
		if qr := h.service.GetCurrentQRCode(); qr != nil {
			result := map[string]interface{}{
				"has_qr":   true,
				"code":     qr.Code,
				"filename": qr.Filename,
			}
			// Read PNG file and encode to base64 for Docker compatibility
			// qr.Filename already contains full path like "data/qr/qr_xxx.png"
			if data, err := os.ReadFile(qr.Filename); err == nil {
				result["image_data"] = base64.StdEncoding.EncodeToString(data)
			}
			resp.Result = result
		} else {
			resp.Error = &RPCError{Code: -32001, Message: "No QR available"}
		}

	case "send":
		var msgReq whatsapp.MessageRequest
		if err := json.Unmarshal(req.Params, &msgReq); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else if err := h.service.SendEnhancedMessage(&msgReq); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = map[string]string{"message": "Sent"}
		}

	case "media":
		var p struct {
			MessageID string `json:"message_id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else if data, mime, err := h.service.DownloadMedia(p.MessageID); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = map[string]interface{}{
				"data":      data,
				"mime_type": mime,
			}
		}

	case "groups":
		if groups, err := h.service.GetGroups(); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = groups
		}

	case "group_info":
		var p struct {
			GroupID string `json:"group_id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else if p.GroupID == "" {
			resp.Error = &RPCError{Code: -32602, Message: "group_id is required"}
		} else if info, err := h.service.GetGroupInfo(p.GroupID); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = info
		}

	case "group_update":
		var p whatsapp.GroupUpdateRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else if p.GroupID == "" {
			resp.Error = &RPCError{Code: -32602, Message: "group_id is required"}
		} else if p.Name == "" && p.Topic == "" {
			resp.Error = &RPCError{Code: -32602, Message: "name or topic is required"}
		} else if err := h.service.UpdateGroupInfo(p.GroupID, p.Name, p.Topic); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = map[string]string{"message": "Updated"}
		}

	case "contact_check":
		var p struct {
			Phones []string `json:"phones"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else if len(p.Phones) == 0 {
			resp.Error = &RPCError{Code: -32602, Message: "phones array is required and must not be empty"}
		} else if results, err := h.service.CheckContacts(p.Phones); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = results
		}

	case "contact_profile_pic":
		var p struct {
			JID     string `json:"jid"`
			Preview bool   `json:"preview"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else if p.JID == "" {
			resp.Error = &RPCError{Code: -32602, Message: "jid is required"}
		} else if result, err := h.service.GetProfilePicture(p.JID, p.Preview); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = result
		}

	case "contacts":
		var p struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			// If params parsing fails, use empty query (list all)
			p.Query = ""
		}
		if contacts, err := h.service.GetContacts(p.Query); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = map[string]interface{}{
				"contacts": contacts,
				"total":    len(contacts),
			}
		}

	case "contact_info":
		var p struct {
			Phone string `json:"phone"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else if p.Phone == "" {
			resp.Error = &RPCError{Code: -32602, Message: "phone is required"}
		} else if info, err := h.service.GetContactInfo(p.Phone); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = info
		}

	case "typing":
		var p struct {
			JID   string `json:"jid"`
			State string `json:"state"`
			Media string `json:"media"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else if p.JID == "" {
			resp.Error = &RPCError{Code: -32602, Message: "jid is required"}
		} else if p.State == "" {
			resp.Error = &RPCError{Code: -32602, Message: "state is required ('composing' or 'paused')"}
		} else if err := h.service.SendTyping(p.JID, p.State, p.Media); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = map[string]string{"message": "Typing indicator sent"}
		}

	case "presence":
		var p struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else if p.Status == "" {
			resp.Error = &RPCError{Code: -32602, Message: "status is required ('available' or 'unavailable')"}
		} else if err := h.service.SetPresence(p.Status); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = map[string]string{"message": "Presence set"}
		}

	case "mark_read":
		var p struct {
			MessageIDs []string `json:"message_ids"`
			ChatJID    string   `json:"chat_jid"`
			SenderJID  string   `json:"sender_jid"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else if len(p.MessageIDs) == 0 {
			resp.Error = &RPCError{Code: -32602, Message: "message_ids array is required"}
		} else if p.ChatJID == "" {
			resp.Error = &RPCError{Code: -32602, Message: "chat_jid is required"}
		} else if err := h.service.MarkRead(p.MessageIDs, p.ChatJID, p.SenderJID); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = map[string]string{"message": "Messages marked as read"}
		}

	case "group_participants_add":
		var p struct {
			GroupID      string   `json:"group_id"`
			Participants []string `json:"participants"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else if p.GroupID == "" {
			resp.Error = &RPCError{Code: -32602, Message: "group_id is required"}
		} else if len(p.Participants) == 0 {
			resp.Error = &RPCError{Code: -32602, Message: "participants array is required and must not be empty"}
		} else if result, err := h.service.AddGroupParticipants(p.GroupID, p.Participants); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = result
		}

	case "group_participants_remove":
		var p struct {
			GroupID      string   `json:"group_id"`
			Participants []string `json:"participants"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else if p.GroupID == "" {
			resp.Error = &RPCError{Code: -32602, Message: "group_id is required"}
		} else if len(p.Participants) == 0 {
			resp.Error = &RPCError{Code: -32602, Message: "participants array is required and must not be empty"}
		} else if result, err := h.service.RemoveGroupParticipants(p.GroupID, p.Participants); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = result
		}

	case "group_invite_link":
		var p struct {
			GroupID string `json:"group_id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else if p.GroupID == "" {
			resp.Error = &RPCError{Code: -32602, Message: "group_id is required"}
		} else if result, err := h.service.GetGroupInviteLink(p.GroupID); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = result
		}

	case "group_revoke_invite":
		var p struct {
			GroupID string `json:"group_id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else if p.GroupID == "" {
			resp.Error = &RPCError{Code: -32602, Message: "group_id is required"}
		} else if result, err := h.service.RevokeGroupInviteLink(p.GroupID); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = result
		}

	case "chat_history":
		var p struct {
			ChatID      string `json:"chat_id"`
			Phone       string `json:"phone"`
			GroupID     string `json:"group_id"`
			Limit       int    `json:"limit"`
			Offset      int    `json:"offset"`
			SenderPhone string `json:"sender_phone"`
			TextOnly    bool   `json:"text_only"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else {
			// Resolve chat_id from phone or group_id if not directly provided
			chatID := p.ChatID
			if chatID == "" {
				if p.Phone != "" {
					chatID = p.Phone + "@s.whatsapp.net"
				} else if p.GroupID != "" {
					chatID = p.GroupID
				} else {
					resp.Error = &RPCError{Code: -32602, Message: "Either chat_id, phone, or group_id is required"}
					break
				}
			}

			// Set defaults
			limit := p.Limit
			if limit <= 0 {
				limit = 50
			}
			if limit > 500 {
				limit = 500
			}

			if result, err := h.service.GetChatHistory(chatID, limit, p.Offset, p.SenderPhone, p.TextOnly); err != nil {
				resp.Error = &RPCError{Code: -32000, Message: err.Error()}
			} else {
				resp.Result = result
			}
		}

	case "rate_limit_get":
		resp.Result = map[string]interface{}{
			"config": h.service.GetRateLimitConfig(),
			"stats":  h.service.GetRateLimitStats(),
		}

	case "rate_limit_set":
		var config whatsapp.RateLimitConfig
		if err := json.Unmarshal(req.Params, &config); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
		} else if err := h.service.SetRateLimitConfig(&config); err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = map[string]interface{}{
				"message": "Rate limit config updated",
				"config":  h.service.GetRateLimitConfig(),
			}
		}

	case "rate_limit_stats":
		resp.Result = h.service.GetRateLimitStats()

	case "rate_limit_unpause":
		h.service.UnpauseRateLimiting()
		resp.Result = map[string]interface{}{
			"message": "Rate limiting unpaused",
			"stats":   h.service.GetRateLimitStats(),
		}

	default:
		resp.Error = &RPCError{Code: -32601, Message: "Method not found: " + req.Method}
	}

	return resp
}

// ForwardEvents sends WhatsApp events as JSON-RPC notifications
func (h *RPCHandler) ForwardEvents(conn *websocket.Conn, mu *sync.Mutex, done chan struct{}) {
	eventChan := h.service.GetEventChannel()
	for {
		select {
		case <-done:
			return
		case event, ok := <-eventChan:
			if !ok {
				return
			}
			notif := RPCRequest{
				JSONRPC: "2.0",
				Method:  "event." + event.Type,
				Params:  mustMarshal(event.Data),
			}
			mu.Lock()
			if err := conn.WriteJSON(notif); err != nil {
				mu.Unlock()
				h.logger.Errorf("Failed to send event: %v", err)
				return
			}
			mu.Unlock()
		}
	}
}

func mustMarshal(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
