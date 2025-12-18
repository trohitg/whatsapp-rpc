package whatsapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// SendMessage - Legacy simple message sending (kept for backward compatibility)
func (s *Service) SendMessage(phone, message string) error {
	req := &MessageRequest{
		Phone:   phone,
		Message: message,
		Type:    "text",
	}
	return s.SendEnhancedMessage(req)
}

// SendEnhancedMessage - Comprehensive message sending with all WhatsApp features
func (s *Service) SendEnhancedMessage(req *MessageRequest) error {
	if !s.client.IsConnected() {
		return fmt.Errorf("WhatsApp not connected")
	}

	var targetJID types.JID
	var err error

	// Log received data for debugging
	s.logger.Infof("SendEnhancedMessage: Type=%s, Phone='%s', GroupID='%s'", req.Type, req.Phone, req.GroupID)

	// Determine target JID (individual or group)
	if req.GroupID != "" {
		targetJID, err = types.ParseJID(req.GroupID)
		if err != nil {
			return fmt.Errorf("invalid group ID '%s': %w", req.GroupID, err)
		}
	} else if req.Phone != "" {
		// Validate phone number is not empty after trimming
		if len(req.Phone) == 0 {
			return fmt.Errorf("phone number cannot be empty")
		}
		targetJID = types.NewJID(req.Phone, types.DefaultUserServer)
	} else {
		return fmt.Errorf("either phone or group_id is required (received: phone='%s', group_id='%s')", req.Phone, req.GroupID)
	}

	// Determine recipient for rate limiting (use phone or group ID)
	recipient := req.Phone
	if recipient == "" {
		recipient = req.GroupID
	}

	// Check rate limits before proceeding
	if err := s.CheckRateLimit(recipient); err != nil {
		s.logger.Warnf("Rate limit check failed: %v", err)
		return err
	}

	// Send typing indicator if enabled (simulates human behavior)
	s.SendTypingIfEnabled(targetJID.String())

	// Check if message contains links for extra delay
	hasLinks := strings.Contains(req.Message, "http://") || strings.Contains(req.Message, "https://")

	// Apply message delay (with randomization for human-like behavior)
	s.ApplyMessageDelay(hasLinks)

	// Create message based on type
	var message *waE2E.Message
	switch req.Type {
	case "text":
		message = s.createTextMessage(req)
	case "image":
		message, err = s.createImageMessage(req)
	case "document":
		message, err = s.createDocumentMessage(req)
	case "audio":
		message, err = s.createAudioMessage(req)
	case "video":
		message, err = s.createVideoMessage(req)
	case "location":
		message = s.createLocationMessage(req)
	case "sticker":
		message, err = s.createStickerMessage(req)
	case "contact":
		message = s.createContactMessage(req)
	default:
		return fmt.Errorf("unsupported message type: %s", req.Type)
	}

	if err != nil {
		return fmt.Errorf("failed to create %s message: %w", req.Type, err)
	}

	// Add reply context if specified
	if req.Reply != nil {
		msgText := message.GetConversation()
		message.ExtendedTextMessage = &waE2E.ExtendedTextMessage{
			Text: &msgText,
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:      proto.String(req.Reply.MessageID),
				Participant:   proto.String(req.Reply.Sender),
				QuotedMessage: &waE2E.Message{Conversation: proto.String(req.Reply.Content)},
			},
		}
		message.Conversation = nil
	}

	// Send the message
	resp, err := s.client.SendMessage(context.Background(), targetJID, message)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	// Record message sent for rate limiting stats
	s.RecordMessageSent(recipient)

	// Log successful send
	s.logger.Infof("Message sent successfully: %s to %s", resp.ID, targetJID.String())

	// Send event notification
	s.safeEventSend(Event{
		Type: "message_sent",
		Data: map[string]interface{}{
			"message_id": resp.ID,
			"to":         targetJID.String(),
			"type":       req.Type,
			"timestamp":  resp.Timestamp,
		},
		Time: time.Now(),
	})

	return nil
}

// SendTyping sends typing indicator
// TODO: Fix context API breaking changes
/*
func (s *Service) SendTyping(jidStr string, typing bool) error {
	if !s.client.IsConnected() {
		return fmt.Errorf("WhatsApp not connected")
	}

	var presence types.Presence
	if typing {
		presence = types.PresenceAvailable
	} else {
		presence = types.PresenceUnavailable
	}

	sendErr := s.client.SendPresence(context.Background(), presence)
	if sendErr != nil {
		return fmt.Errorf("failed to send typing indicator: %w", sendErr)
	}

	return nil
}

// MarkMessageAsRead marks a message as read
func (s *Service) MarkMessageAsRead(messageID, senderJID string) error {
	if !s.client.IsConnected() {
		return fmt.Errorf("WhatsApp not connected")
	}

	jid, err := types.ParseJID(senderJID)
	if err != nil {
		return fmt.Errorf("invalid sender JID: %w", err)
	}

	err = s.client.MarkRead(context.Background(), []string{messageID}, time.Now(), jid, jid)
	if err != nil {
		return fmt.Errorf("failed to mark message as read: %w", err)
	}

	return nil
}
*/

// Helper functions to create different message types

func (s *Service) createTextMessage(req *MessageRequest) *waE2E.Message {
	return &waE2E.Message{
		Conversation: proto.String(req.Message),
	}
}

func (s *Service) createImageMessage(req *MessageRequest) (*waE2E.Message, error) {
	if req.MediaData == nil {
		return nil, fmt.Errorf("media_data is required for image messages")
	}

	// Decode base64 media data
	mediaBytes, err := base64.StdEncoding.DecodeString(req.MediaData.Data)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 media data: %w", err)
	}

	// Upload media to WhatsApp servers
	uploaded, err := s.client.Upload(context.Background(), mediaBytes, whatsmeow.MediaImage)
	if err != nil {
		return nil, fmt.Errorf("failed to upload image: %w", err)
	}

	// Create image message
	message := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			Caption:        proto.String(req.MediaData.Caption),
			Mimetype:       proto.String(req.MediaData.MimeType),
			URL:            proto.String(uploaded.URL),
			DirectPath:     proto.String(uploaded.DirectPath),
			MediaKey:       uploaded.MediaKey,
			FileEncSHA256:  uploaded.FileEncSHA256,
			FileSHA256:     uploaded.FileSHA256,
			FileLength:     proto.Uint64(uint64(len(mediaBytes))),
		},
	}

	return message, nil
}

func (s *Service) createDocumentMessage(req *MessageRequest) (*waE2E.Message, error) {
	if req.MediaData == nil {
		return nil, fmt.Errorf("media_data is required for document messages")
	}

	// Decode base64 media data
	mediaBytes, err := base64.StdEncoding.DecodeString(req.MediaData.Data)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 media data: %w", err)
	}

	// Upload media to WhatsApp servers
	uploaded, err := s.client.Upload(context.Background(), mediaBytes, whatsmeow.MediaDocument)
	if err != nil {
		return nil, fmt.Errorf("failed to upload document: %w", err)
	}

	// Create document message
	message := &waE2E.Message{
		DocumentMessage: &waE2E.DocumentMessage{
			Title:          proto.String(req.MediaData.Filename),
			FileName:       proto.String(req.MediaData.Filename),
			Mimetype:       proto.String(req.MediaData.MimeType),
			URL:            proto.String(uploaded.URL),
			DirectPath:     proto.String(uploaded.DirectPath),
			MediaKey:       uploaded.MediaKey,
			FileEncSHA256:  uploaded.FileEncSHA256,
			FileSHA256:     uploaded.FileSHA256,
			FileLength:     proto.Uint64(uint64(len(mediaBytes))),
		},
	}

	return message, nil
}

func (s *Service) createAudioMessage(req *MessageRequest) (*waE2E.Message, error) {
	if req.MediaData == nil {
		return nil, fmt.Errorf("media_data is required for audio messages")
	}

	// Decode base64 media data
	mediaBytes, err := base64.StdEncoding.DecodeString(req.MediaData.Data)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 media data: %w", err)
	}

	// Upload media to WhatsApp servers
	uploaded, err := s.client.Upload(context.Background(), mediaBytes, whatsmeow.MediaAudio)
	if err != nil {
		return nil, fmt.Errorf("failed to upload audio: %w", err)
	}

	// Create audio message
	message := &waE2E.Message{
		AudioMessage: &waE2E.AudioMessage{
			Mimetype:       proto.String(req.MediaData.MimeType),
			URL:            proto.String(uploaded.URL),
			DirectPath:     proto.String(uploaded.DirectPath),
			MediaKey:       uploaded.MediaKey,
			FileEncSHA256:  uploaded.FileEncSHA256,
			FileSHA256:     uploaded.FileSHA256,
			FileLength:     proto.Uint64(uint64(len(mediaBytes))),
			PTT:            proto.Bool(strings.Contains(req.MediaData.MimeType, "ogg")), // Voice note for OGG
		},
	}

	return message, nil
}

func (s *Service) createVideoMessage(req *MessageRequest) (*waE2E.Message, error) {
	if req.MediaData == nil {
		return nil, fmt.Errorf("media_data is required for video messages")
	}

	// Decode base64 media data
	mediaBytes, err := base64.StdEncoding.DecodeString(req.MediaData.Data)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 media data: %w", err)
	}

	// Upload media to WhatsApp servers
	uploaded, err := s.client.Upload(context.Background(), mediaBytes, whatsmeow.MediaVideo)
	if err != nil {
		return nil, fmt.Errorf("failed to upload video: %w", err)
	}

	// Create video message
	message := &waE2E.Message{
		VideoMessage: &waE2E.VideoMessage{
			Caption:        proto.String(req.MediaData.Caption),
			Mimetype:       proto.String(req.MediaData.MimeType),
			URL:            proto.String(uploaded.URL),
			DirectPath:     proto.String(uploaded.DirectPath),
			MediaKey:       uploaded.MediaKey,
			FileEncSHA256:  uploaded.FileEncSHA256,
			FileSHA256:     uploaded.FileSHA256,
			FileLength:     proto.Uint64(uint64(len(mediaBytes))),
		},
	}

	return message, nil
}

func (s *Service) createLocationMessage(req *MessageRequest) *waE2E.Message {
	if req.Location == nil {
		return &waE2E.Message{Conversation: proto.String("Invalid location data")}
	}

	return &waE2E.Message{
		LocationMessage: &waE2E.LocationMessage{
			DegreesLatitude:  proto.Float64(req.Location.Latitude),
			DegreesLongitude: proto.Float64(req.Location.Longitude),
			Name:             proto.String(req.Location.Name),
			Address:          proto.String(req.Location.Address),
		},
	}
}

func (s *Service) createStickerMessage(req *MessageRequest) (*waE2E.Message, error) {
	if req.MediaData == nil {
		return nil, fmt.Errorf("media_data is required for sticker messages")
	}

	// Decode base64 media data
	mediaBytes, err := base64.StdEncoding.DecodeString(req.MediaData.Data)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 media data: %w", err)
	}

	// Upload media to WhatsApp servers
	uploaded, err := s.client.Upload(context.Background(), mediaBytes, whatsmeow.MediaImage)
	if err != nil {
		return nil, fmt.Errorf("failed to upload sticker: %w", err)
	}

	// Create sticker message
	message := &waE2E.Message{
		StickerMessage: &waE2E.StickerMessage{
			Mimetype:      proto.String(req.MediaData.MimeType),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(mediaBytes))),
		},
	}

	return message, nil
}

func (s *Service) createContactMessage(req *MessageRequest) *waE2E.Message {
	if req.Contact == nil {
		return &waE2E.Message{Conversation: proto.String("Invalid contact data")}
	}

	return &waE2E.Message{
		ContactMessage: &waE2E.ContactMessage{
			DisplayName: proto.String(req.Contact.DisplayName),
			Vcard:       proto.String(req.Contact.VCard),
		},
	}
}