package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"

	"whatsapp-rpc/src/go/config"
)

type Service struct {
	mu             sync.Mutex
	client         *whatsmeow.Client
	container      *sqlstore.Container // Database container for proper cleanup
	dbPath         string              // Database file path
	logger         *logrus.Logger
	events         chan Event
	running        bool
	pairing        bool
	shutdown       bool
	lastQRCode     *QRCodeData
	lastQRCodeTime time.Time
	messages       map[string]*events.Message // Store messages by ID for media download
	messageOrder   []string                   // Track message insertion order for FIFO cleanup
	historyStore   *HistoryStore              // Persistent message history store

	// Rate limiting for anti-ban protection
	rateMu           sync.Mutex
	rateLimitConfig  *RateLimitConfig
	rateLimitStats   *RateLimitStats
	messageTimes     []time.Time          // Sliding window of message times
	contactsSeen     map[string]bool      // Track known contacts
	newContactsToday map[string]time.Time // New contacts with timestamp
	dailyResetTime   time.Time            // When daily counters were last reset
}



func NewService(dbConfig config.DatabaseConfig, logger *logrus.Logger) (*Service, error) {
	// Configure device to appear as legitimate WhatsApp Web on Chrome
	// Uses default WhatsApp Web version and Chrome platform type for natural appearance
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_CHROME.Enum()
	store.SetOSInfo("Windows - Zeenie", store.GetWAVersion())

	// Ensure database directory exists
	dbDir := filepath.Dir(dbConfig.Path)
	if dbDir != "" && dbDir != "." {
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	// Setup database with WAL mode for concurrent access and extended busy timeout
	dbLog := waLog.Stdout("Database", "INFO", true)
	ctx := context.Background()
	// Increased busy timeout to 30 seconds and added cache=shared for better concurrency on Windows
	container, err := sqlstore.New(ctx, "sqlite", "file:"+dbConfig.Path+"?_pragma=foreign_keys(1)&_journal_mode=WAL&_busy_timeout=30000&cache=shared", dbLog)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// Get device store
	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get device: %w", err)
	}

	// Create client - NO extra configuration like working example
	clientLog := waLog.Stdout("WhatsApp", "INFO", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	service := &Service{
		client:           client,
		container:        container,
		dbPath:           dbConfig.Path,
		logger:           logger,
		events:           make(chan Event, 100),
		running:          false,
		pairing:          false,
		shutdown:         false,
		messages:         make(map[string]*events.Message),
		messageOrder:     make([]string, 0, 100),
		rateLimitConfig:  DefaultRateLimitConfig(),
		rateLimitStats:   &RateLimitStats{},
		messageTimes:     make([]time.Time, 0, 100),
		contactsSeen:     make(map[string]bool),
		newContactsToday: make(map[string]time.Time),
		dailyResetTime:   time.Now(),
	}

	// Initialize history store for message persistence
	historyStore, err := NewHistoryStore(dbConfig.Path, logger)
	if err != nil {
		logger.Warnf("Failed to init history store (history disabled): %v", err)
	} else {
		service.historyStore = historyStore
		logger.Info("History store initialized")
	}

	// Reset any previous connection state on service creation
	if client.IsConnected() {
		client.Disconnect()
	}

	// Add event handler
	client.AddEventHandler(service.eventHandler)

	return service, nil
}

// HasExistingSession returns true if there's a stored session that can be used to auto-connect
func (s *Service) HasExistingSession() bool {
	hasClient := s.client != nil
	hasStore := hasClient && s.client.Store != nil
	hasID := hasStore && s.client.Store.ID != nil

	s.logger.Infof("HasExistingSession check: client=%v, store=%v, id=%v", hasClient, hasStore, hasID)

	return hasID
}

func (s *Service) safeEventSend(event Event) {
	if s.shutdown {
		return
	}
	
	defer func() {
		if r := recover(); r != nil {
			s.logger.Errorf("Event send panic recovered: %v", r)
		}
	}()
	
	select {
	case s.events <- event:
		// Event sent successfully
	default:
		if s.shutdown {
			s.logger.Info("Service is shutdown, skipping event")
		} else {
			s.logger.Warn("Events channel blocked, skipping event")
		}
	}
}

func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		s.logger.Warn("Service already running")
		return nil
	}

	// Events channel is kept open across restarts (not closed in Shutdown)
	// This allows existing WebSocket ForwardEvents goroutines to continue working

	s.shutdown = false
	s.running = true

	s.logger.Info("Starting WhatsApp service...")

	// Simple flow like working example
	if s.client.Store.ID == nil {
		s.logger.Info("No existing session found, starting pairing")
		// Run pairing synchronously like working example
		go func() {
			if err := s.startPairing(); err != nil {
				s.logger.Errorf("Pairing failed: %v", err)
			}
		}()
		return nil
	}

	// Connect with existing session
	s.logger.Info("Existing session found, connecting...")
	err := s.client.Connect()
	if err != nil {
		s.logger.Errorf("Failed to connect: %v", err)
		return err
	}

	s.logger.Info("Connected successfully")
	return nil
}

func (s *Service) startPairing() error {
	s.logger.Info("Starting pairing process")
	s.pairing = true

	// Get QR channel - use simple context like working example
	qrChan, err := s.client.GetQRChannel(context.Background())
	if err != nil {
		s.logger.Errorf("Failed to get QR channel: %v", err)
		s.pairing = false
		return err
	}

	// Connect to WhatsApp
	err = s.client.Connect()
	if err != nil {
		s.logger.Errorf("Failed to connect: %v", err)
		s.pairing = false
		return err
	}

	// Process QR events - simple loop like working example
	for evt := range qrChan {
		if evt.Event == "code" {
			s.logger.Info("QR code received, generating PNG")
			qrData := s.handleQRCode(evt.Code)

			s.safeEventSend(Event{
				Type: "qr_code",
				Data: map[string]interface{}{
					"code":     qrData.Code,
					"filename": qrData.Filename,
				},
				Time: time.Now(),
			})
		} else if evt.Event == "success" {
			s.logger.Info("Pairing successful")
			s.pairing = false
			s.safeEventSend(Event{
				Type: "connected",
				Data: map[string]interface{}{"status": "connected"},
				Time: time.Now(),
			})
			return nil
		} else {
			s.logger.Infof("QR event: %s", evt.Event)
		}
	}

	s.pairing = false
	return fmt.Errorf("QR channel closed")
}

func (s *Service) handleQRCode(code string) QRCodeData {
	// Delete old QR code files before creating new one
	s.cleanupOldQRCodes()

	// Ensure data/qr directory exists
	qrDir := "data/qr"
	os.MkdirAll(qrDir, 0755)

	filename := filepath.Join(qrDir, fmt.Sprintf("qr_%d.png", time.Now().Unix()))

	s.logger.Infof("Generating QR code PNG file: %s", filename)
	s.logger.Debugf("QR code content (first 50 chars): %s...", code[:min(50, len(code))])

	// Save QR code as PNG
	err := qrcode.WriteFile(code, qrcode.Medium, 256, filename)
	if err != nil {
		s.logger.Errorf("Failed to save QR code to file %s: %v", filename, err)
	} else {
		s.logger.Infof("QR code successfully saved to %s", filename)
	}

	qrData := QRCodeData{
		Code:     code,
		Filename: filename,
		Time:     time.Now(),
	}

	// Store QR code for API retrieval
	s.mu.Lock()
	s.lastQRCode = &qrData
	s.lastQRCodeTime = time.Now()
	s.mu.Unlock()

	s.logger.Info("QR code ready for scanning. Instructions:")
	s.logger.Info("1. Open WhatsApp on your phone")
	s.logger.Info("2. Go to Settings > Linked Devices")
	s.logger.Info("3. Tap 'Link a Device'")
	s.logger.Info("4. Scan the QR code from the generated PNG file or terminal")
	s.logger.Infof("5. QR code file location: %s", filename)

	return qrData
}


func (s *Service) eventHandler(evt interface{}) {
	if s.shutdown {
		return
	}

	switch v := evt.(type) {
	case *events.Connected:
		s.logger.Infof("Successfully connected to WhatsApp! Device ID: %s", s.client.Store.ID.String())
		s.pairing = false

		// Generate groups JSON file asynchronously
		go func() {
			time.Sleep(2 * time.Second) // Wait for connection to stabilize
			if err := s.generateGroupsJSON(); err != nil {
				s.logger.Errorf("Failed to generate groups.json: %v", err)
			}
		}()

		s.safeEventSend(Event{
			Type: "connected",
			Data: map[string]interface{}{
				"status":    "connected",
				"device_id": s.client.Store.ID.String(),
			},
			Time: time.Now(),
		})
	case *events.Disconnected:
		s.logger.Warnf("Disconnected from WhatsApp. Reason: %+v", v)
		s.safeEventSend(Event{
			Type: "disconnected",
			Data: map[string]interface{}{
				"status": "disconnected",
				"reason": fmt.Sprintf("%+v", v),
			},
			Time: time.Now(),
		})
	case *events.ConnectFailure:
		s.logger.Errorf("Connection failure: %+v", v)
		s.safeEventSend(Event{
			Type: "connection_failure",
			Data: map[string]interface{}{
				"error": fmt.Sprintf("%+v", v),
				"reason": v.Reason.String(),
			},
			Time: time.Now(),
		})
	case *events.LoggedOut:
		s.logger.Warnf("=== LOGOUT EVENT RECEIVED ===")
		s.logger.Warnf("OnConnect: %v", v.OnConnect)
		s.logger.Warnf("Reason: %s", v.Reason.String())
		s.logger.Warnf("Full logout event: %+v", v)
		s.logger.Warnf("Device Store ID: %v", s.client.Store.ID)
		s.logger.Warnf("Is Connected: %v", s.client.IsConnected())
		s.logger.Warnf("=== END LOGOUT EVENT ===")
		s.safeEventSend(Event{
			Type: "logged_out",
			Data: map[string]interface{}{
				"on_connect": v.OnConnect,
				"reason":     v.Reason.String(),
				"full_event": fmt.Sprintf("%+v", v),
			},
			Time: time.Now(),
		})
	case *events.TemporaryBan:
		s.logger.Errorf("Temporary ban received: %+v", v)
		s.safeEventSend(Event{
			Type: "temporary_ban",
			Data: map[string]interface{}{
				"code": v.Code.String(),
				"reason": fmt.Sprintf("%+v", v),
			},
			Time: time.Now(),
		})
	case *events.HistorySync:
		s.handleHistorySync(v)
	case *events.Message:
		s.handleIncomingMessage(v)
	default:
		s.logger.Debugf("Unhandled event type: %T, data: %+v", evt, evt)
	}
}

// handleIncomingMessage processes incoming messages and extracts all message types
func (s *Service) handleIncomingMessage(v *events.Message) {
	// Extract basic info
	sender := v.Info.Sender.String()
	chatID := v.Info.Chat.String()
	timestamp := v.Info.Timestamp
	messageID := v.Info.ID
	isGroup := v.Info.IsGroup

	// Resolve LID to phone number (LIDs can appear in both group and individual chats)
	senderPhone := v.Info.Sender.User // Default to JID user part
	if v.Info.Sender.Server == "lid" && s.client.Store.LIDs != nil {
		ctx := context.Background()
		pn, err := s.client.Store.LIDs.GetPNForLID(ctx, v.Info.Sender)
		if err == nil && pn.User != "" {
			senderPhone = pn.User
			s.logger.Debugf("Resolved LID %s to phone %s", sender, senderPhone)
		} else if err != nil {
			s.logger.Debugf("Could not resolve LID %s: %v", sender, err)
		}
	}

	if isGroup {
		s.logger.Debugf("Processing group message from %s (phone: %s) in %s (ID: %s)", sender, senderPhone, chatID, messageID)
	} else {
		s.logger.Debugf("Processing individual message from %s (phone: %s) (ID: %s)", sender, senderPhone, messageID)
	}

	// Store message for later media download (keep last 100 messages to avoid memory issues)
	s.mu.Lock()
	// Check if message already exists (avoid duplicates)
	if _, exists := s.messages[messageID]; !exists {
		// Remove oldest messages if at capacity (FIFO cleanup)
		maxMessages := 100
		for len(s.messageOrder) >= maxMessages {
			oldestID := s.messageOrder[0]
			s.messageOrder = s.messageOrder[1:]
			delete(s.messages, oldestID)
			s.logger.Debugf("Removed oldest message %s from cache (capacity: %d)", oldestID, maxMessages)
		}
		// Add new message
		s.messages[messageID] = v
		s.messageOrder = append(s.messageOrder, messageID)
		s.logger.Debugf("Cached message %s for media download (total: %d)", messageID, len(s.messages))
	}
	s.mu.Unlock()

	// Prepare event data with common fields
	eventData := map[string]interface{}{
		"message_id":   messageID,
		"sender":       sender,
		"sender_phone": senderPhone,
		"chat_id":      chatID,
		"timestamp":    timestamp,
		"is_from_me":   v.Info.IsFromMe,
		"is_group":     isGroup,
	}

	// Add group info for group messages
	if isGroup {
		groupInfo := map[string]interface{}{
			"group_jid":    chatID,
			"sender_jid":   sender,
			"sender_phone": senderPhone,
		}
		// Try to get group name from push name or leave empty
		if v.Info.PushName != "" {
			groupInfo["sender_name"] = v.Info.PushName
		}
		eventData["group_info"] = groupInfo
	}

	// Determine message type and extract content
	msg := v.Message
	messageType := "unknown"
	var messageContent interface{}

	// Helper function to extract forwarded info from ContextInfo
	extractForwardedInfo := func(ctx *waE2E.ContextInfo) {
		if ctx == nil {
			return
		}
		if ctx.GetIsForwarded() {
			eventData["is_forwarded"] = true
			eventData["forwarding_score"] = ctx.GetForwardingScore()
		}
	}

	switch {
	case msg.Conversation != nil && *msg.Conversation != "":
		// Plain text message (no ContextInfo, cannot be forwarded)
		messageType = "text"
		messageContent = *msg.Conversation
		eventData["text"] = *msg.Conversation

	case msg.ExtendedTextMessage != nil:
		// Extended text message (with link preview, quoted message, etc.)
		messageType = "text"
		if msg.ExtendedTextMessage.Text != nil {
			messageContent = *msg.ExtendedTextMessage.Text
			eventData["text"] = *msg.ExtendedTextMessage.Text
		}
		// Check forwarded status
		extractForwardedInfo(msg.ExtendedTextMessage.ContextInfo)
		// Check if it's a reply/quoted message
		if msg.ExtendedTextMessage.ContextInfo != nil && msg.ExtendedTextMessage.ContextInfo.QuotedMessage != nil {
			eventData["is_reply"] = true
			if msg.ExtendedTextMessage.ContextInfo.StanzaID != nil {
				eventData["quoted_message_id"] = *msg.ExtendedTextMessage.ContextInfo.StanzaID
			}
		}

	case msg.ImageMessage != nil:
		// Image message
		messageType = "image"
		imageData := map[string]interface{}{
			"mime_type": msg.ImageMessage.GetMimetype(),
			"file_sha256": fmt.Sprintf("%x", msg.ImageMessage.GetFileSHA256()),
			"file_length": msg.ImageMessage.GetFileLength(),
		}
		if msg.ImageMessage.Caption != nil {
			imageData["caption"] = *msg.ImageMessage.Caption
		}
		if msg.ImageMessage.URL != nil {
			imageData["url"] = *msg.ImageMessage.URL
		}
		messageContent = imageData
		eventData["image"] = imageData
		// Check forwarded status
		extractForwardedInfo(msg.ImageMessage.ContextInfo)

	case msg.VideoMessage != nil:
		// Video message
		messageType = "video"
		videoData := map[string]interface{}{
			"mime_type": msg.VideoMessage.GetMimetype(),
			"file_sha256": fmt.Sprintf("%x", msg.VideoMessage.GetFileSHA256()),
			"file_length": msg.VideoMessage.GetFileLength(),
			"seconds": msg.VideoMessage.GetSeconds(),
		}
		if msg.VideoMessage.Caption != nil {
			videoData["caption"] = *msg.VideoMessage.Caption
		}
		if msg.VideoMessage.URL != nil {
			videoData["url"] = *msg.VideoMessage.URL
		}
		messageContent = videoData
		eventData["video"] = videoData
		// Check forwarded status
		extractForwardedInfo(msg.VideoMessage.ContextInfo)

	case msg.AudioMessage != nil:
		// Audio/Voice message
		messageType = "audio"
		audioData := map[string]interface{}{
			"mime_type": msg.AudioMessage.GetMimetype(),
			"file_sha256": fmt.Sprintf("%x", msg.AudioMessage.GetFileSHA256()),
			"file_length": msg.AudioMessage.GetFileLength(),
			"seconds": msg.AudioMessage.GetSeconds(),
			"ptt": msg.AudioMessage.GetPTT(), // Push-to-talk (voice message)
		}
		if msg.AudioMessage.URL != nil {
			audioData["url"] = *msg.AudioMessage.URL
		}
		messageContent = audioData
		eventData["audio"] = audioData
		// Check forwarded status
		extractForwardedInfo(msg.AudioMessage.ContextInfo)

	case msg.DocumentMessage != nil:
		// Document message
		messageType = "document"
		docData := map[string]interface{}{
			"mime_type": msg.DocumentMessage.GetMimetype(),
			"file_sha256": fmt.Sprintf("%x", msg.DocumentMessage.GetFileSHA256()),
			"file_length": msg.DocumentMessage.GetFileLength(),
		}
		if msg.DocumentMessage.FileName != nil {
			docData["file_name"] = *msg.DocumentMessage.FileName
		}
		if msg.DocumentMessage.Title != nil {
			docData["title"] = *msg.DocumentMessage.Title
		}
		if msg.DocumentMessage.Caption != nil {
			docData["caption"] = *msg.DocumentMessage.Caption
		}
		if msg.DocumentMessage.URL != nil {
			docData["url"] = *msg.DocumentMessage.URL
		}
		messageContent = docData
		eventData["document"] = docData
		// Check forwarded status
		extractForwardedInfo(msg.DocumentMessage.ContextInfo)

	case msg.StickerMessage != nil:
		// Sticker message
		messageType = "sticker"
		stickerData := map[string]interface{}{
			"mime_type": msg.StickerMessage.GetMimetype(),
			"file_sha256": fmt.Sprintf("%x", msg.StickerMessage.GetFileSHA256()),
			"file_length": msg.StickerMessage.GetFileLength(),
			"is_animated": msg.StickerMessage.GetIsAnimated(),
		}
		if msg.StickerMessage.URL != nil {
			stickerData["url"] = *msg.StickerMessage.URL
		}
		messageContent = stickerData
		eventData["sticker"] = stickerData
		// Check forwarded status
		extractForwardedInfo(msg.StickerMessage.ContextInfo)

	case msg.LocationMessage != nil:
		// Location message
		messageType = "location"
		locationData := map[string]interface{}{
			"latitude":  msg.LocationMessage.GetDegreesLatitude(),
			"longitude": msg.LocationMessage.GetDegreesLongitude(),
		}
		if msg.LocationMessage.Name != nil {
			locationData["name"] = *msg.LocationMessage.Name
		}
		if msg.LocationMessage.Address != nil {
			locationData["address"] = *msg.LocationMessage.Address
		}
		messageContent = locationData
		eventData["location"] = locationData

	case msg.ContactMessage != nil:
		// Contact card message
		messageType = "contact"
		contactData := map[string]interface{}{
			"display_name": msg.ContactMessage.GetDisplayName(),
			"vcard": msg.ContactMessage.GetVcard(),
		}
		messageContent = contactData
		eventData["contact"] = contactData

	case msg.ContactsArrayMessage != nil:
		// Multiple contacts
		messageType = "contacts"
		contacts := []map[string]interface{}{}
		for _, contact := range msg.ContactsArrayMessage.Contacts {
			contacts = append(contacts, map[string]interface{}{
				"display_name": contact.GetDisplayName(),
				"vcard": contact.GetVcard(),
			})
		}
		messageContent = contacts
		eventData["contacts"] = contacts

	case msg.ReactionMessage != nil:
		// Reaction to a message - skip broadcasting these
		s.logger.Debugf("Skipping reaction message from %s to message %s", sender, msg.ReactionMessage.GetKey().GetID())
		return

	case msg.ProtocolMessage != nil:
		// Protocol messages (edits, deletes, ephemeral settings) - skip these
		s.logger.Debugf("Skipping protocol message from %s (type: %v)", sender, msg.ProtocolMessage.GetType())
		return

	default:
		// Unknown message type - silently skip (don't log or broadcast)
		return
	}

	eventData["message_type"] = messageType
	eventData["content"] = messageContent

	// Log the message (debug level to reduce noise)
	s.logger.Debugf("Received %s message from %s", messageType, sender)

	// Persist ALL messages to history store for future retrieval
	if s.historyStore != nil {
		textContent := ""
		if t, ok := eventData["text"].(string); ok {
			textContent = t
		}
		record := MessageRecord{
			MessageID:   messageID,
			ChatID:      chatID,
			Sender:      sender,
			SenderPhone: senderPhone,
			MessageType: messageType,
			Text:        textContent,
			Timestamp:   timestamp,
			IsGroup:     isGroup,
			IsFromMe:    v.Info.IsFromMe,
		}
		go func() {
			if err := s.historyStore.StoreMessage(record); err != nil {
				s.logger.Debugf("Failed to store message: %v", err)
			}
		}()
	}

	// Broadcast event
	s.safeEventSend(Event{
		Type: "message_received",
		Data: eventData,
		Time: time.Now(),
	})
}

// handleHistorySync processes history sync events received on first login
// This contains all past conversations and messages from WhatsApp
func (s *Service) handleHistorySync(evt *events.HistorySync) {
	if s.historyStore == nil {
		s.logger.Warn("History sync received but history store not initialized")
		return
	}

	historyData := evt.Data
	if historyData == nil {
		return
	}

	conversations := historyData.GetConversations()
	s.logger.Infof("Processing history sync: %d conversations", len(conversations))

	totalMessages := 0
	for _, conv := range conversations {
		chatJID, err := types.ParseJID(conv.GetID())
		if err != nil {
			s.logger.Warnf("Invalid chat JID in history: %s", conv.GetID())
			continue
		}

		isGroup := chatJID.Server == "g.us"
		chatID := chatJID.String()

		for _, historyMsg := range conv.GetMessages() {
			webMsg := historyMsg.GetMessage()
			if webMsg == nil {
				continue
			}

			// Parse the web message to get standard message format
			parsedMsg, err := s.client.ParseWebMessage(chatJID, webMsg)
			if err != nil {
				continue
			}

			// Extract message info
			msgInfo := parsedMsg.Info
			messageID := msgInfo.ID
			sender := msgInfo.Sender.String()
			senderPhone := msgInfo.Sender.User
			timestamp := msgInfo.Timestamp

			// Determine message type and extract text content
			messageType := "unknown"
			text := ""

			msg := parsedMsg.Message
			if msg == nil {
				// Skip messages with nil content (protocol messages, etc.)
				continue
			}

			switch {
			case msg.Conversation != nil && *msg.Conversation != "":
				messageType = "text"
				text = *msg.Conversation
			case msg.ExtendedTextMessage != nil && msg.ExtendedTextMessage.Text != nil:
				messageType = "text"
				text = *msg.ExtendedTextMessage.Text
			case msg.ImageMessage != nil:
				messageType = "image"
				if msg.ImageMessage.Caption != nil {
					text = *msg.ImageMessage.Caption
				}
			case msg.VideoMessage != nil:
				messageType = "video"
				if msg.VideoMessage.Caption != nil {
					text = *msg.VideoMessage.Caption
				}
			case msg.AudioMessage != nil:
				messageType = "audio"
			case msg.DocumentMessage != nil:
				messageType = "document"
				if msg.DocumentMessage.Caption != nil {
					text = *msg.DocumentMessage.Caption
				}
			case msg.StickerMessage != nil:
				messageType = "sticker"
			case msg.LocationMessage != nil:
				messageType = "location"
			case msg.ContactMessage != nil:
				messageType = "contact"
			default:
				// Skip unknown/protocol messages
				continue
			}

			// Store ALL message types
			record := MessageRecord{
				MessageID:   messageID,
				ChatID:      chatID,
				Sender:      sender,
				SenderPhone: senderPhone,
				MessageType: messageType,
				Text:        text,
				Timestamp:   timestamp,
				IsGroup:     isGroup,
				IsFromMe:    msgInfo.IsFromMe,
			}

			if err := s.historyStore.StoreMessage(record); err != nil {
				s.logger.Debugf("Failed to store history message: %v", err)
			} else {
				totalMessages++
			}
		}
	}

	s.logger.Infof("History sync complete: stored %d messages from %d conversations",
		totalMessages, len(conversations))

	// Broadcast event for frontend notification
	s.safeEventSend(Event{
		Type: "history_sync_complete",
		Data: map[string]interface{}{
			"conversations": len(conversations),
			"messages":      totalMessages,
		},
		Time: time.Now(),
	})
}

// GetChatHistory retrieves stored messages for a chat
func (s *Service) GetChatHistory(chatID string, limit, offset int, senderPhone string, textOnly bool) (*ChatHistoryResult, error) {
	if s.historyStore == nil {
		return nil, fmt.Errorf("history store not initialized")
	}
	return s.historyStore.GetChatHistory(chatID, limit, offset, senderPhone, textOnly)
}

func (s *Service) GetStatus() map[string]interface{} {
	status := map[string]interface{}{
		"connected":   s.client.IsConnected(),
		"has_session": s.client.Store.ID != nil,
		"running":     s.running,
		"pairing":     s.pairing,
		"timestamp":   time.Now(),
	}

	// Add device information if available
	if s.client.Store.ID != nil {
		status["device_id"] = s.client.Store.ID.String()
	}

	return status
}

func (s *Service) GetDiagnostics() map[string]interface{} {
	diagnostics := map[string]interface{}{
		"client_connected":    s.client.IsConnected(),
		"client_logged_in":    s.client.IsLoggedIn(),
		"store_has_id":        s.client.Store.ID != nil,
		"service_running":     s.running,
		"service_pairing":     s.pairing,
		"service_shutdown":    s.shutdown,
		"events_channel_open": !s.shutdown,
		"timestamp":           time.Now(),
	}

	// Add store information if available
	if s.client.Store.ID != nil {
		diagnostics["device_store"] = map[string]interface{}{
			"device_id": s.client.Store.ID.String(),
			"user":      s.client.Store.ID.User,
			"device":    s.client.Store.ID.Device,
			"server":    s.client.Store.ID.Server,
		}
	}

	return diagnostics
}

func (s *Service) GetEventChannel() <-chan Event {
	return s.events
}

func (s *Service) IsConnected() bool {
	return s.client.IsConnected()
}

// DownloadMedia downloads media from a message by ID
func (s *Service) DownloadMedia(messageID string) ([]byte, string, error) {
	s.mu.Lock()
	msg, exists := s.messages[messageID]
	cachedCount := len(s.messages)
	s.mu.Unlock()

	if !exists {
		s.logger.Warnf("Media download failed: message %s not found in cache (cached: %d messages)", messageID, cachedCount)
		return nil, "", fmt.Errorf("message not found or expired (message_id: %s, cached: %d)", messageID, cachedCount)
	}

	// Download media based on message type
	var data []byte
	var err error
	var mimeType string
	var mediaType string

	ctx := context.Background()

	switch {
	case msg.Message.ImageMessage != nil:
		mediaType = "image"
		s.logger.Infof("Downloading image from message %s", messageID)
		data, err = s.client.Download(ctx, msg.Message.ImageMessage)
		mimeType = msg.Message.ImageMessage.GetMimetype()
	case msg.Message.VideoMessage != nil:
		mediaType = "video"
		s.logger.Infof("Downloading video from message %s", messageID)
		data, err = s.client.Download(ctx, msg.Message.VideoMessage)
		mimeType = msg.Message.VideoMessage.GetMimetype()
	case msg.Message.AudioMessage != nil:
		mediaType = "audio"
		s.logger.Infof("Downloading audio from message %s", messageID)
		data, err = s.client.Download(ctx, msg.Message.AudioMessage)
		mimeType = msg.Message.AudioMessage.GetMimetype()
	case msg.Message.DocumentMessage != nil:
		mediaType = "document"
		s.logger.Infof("Downloading document from message %s", messageID)
		data, err = s.client.Download(ctx, msg.Message.DocumentMessage)
		mimeType = msg.Message.DocumentMessage.GetMimetype()
	case msg.Message.StickerMessage != nil:
		mediaType = "sticker"
		s.logger.Infof("Downloading sticker from message %s", messageID)
		data, err = s.client.Download(ctx, msg.Message.StickerMessage)
		mimeType = msg.Message.StickerMessage.GetMimetype()
	default:
		s.logger.Warnf("Media download failed: message %s does not contain downloadable media", messageID)
		return nil, "", fmt.Errorf("message does not contain downloadable media")
	}

	if err != nil {
		s.logger.Errorf("Failed to download %s from message %s: %v", mediaType, messageID, err)
		return nil, "", fmt.Errorf("failed to download %s: %w", mediaType, err)
	}

	s.logger.Infof("Successfully downloaded %s from message %s (%d bytes, %s)", mediaType, messageID, len(data), mimeType)
	return data, mimeType, nil
}

// resolveParticipantPhone gets the real phone number for a participant
// Uses LID store to resolve if PhoneNumber is empty and JID is a LID
func (s *Service) resolveParticipantPhone(ctx context.Context, p types.GroupParticipant) string {
	// First try PhoneNumber field
	if p.PhoneNumber.User != "" {
		return p.PhoneNumber.User
	}

	// Check if JID is a LID (ends with @lid server)
	if p.JID.Server == "lid" && s.client.Store.LIDs != nil {
		// Try to resolve LID to phone number
		pn, err := s.client.Store.LIDs.GetPNForLID(ctx, p.JID)
		if err == nil && pn.User != "" {
			return pn.User
		}
	}

	// Fall back to JID.User
	return p.JID.User
}

// GetContactName looks up a contact name by phone number
// Prefers user's saved name, falls back to push name
func (s *Service) GetContactName(phone string) string {
	if s.client == nil || s.client.Store == nil || s.client.Store.Contacts == nil {
		return ""
	}

	jid := types.NewJID(phone, types.DefaultUserServer)
	contact, err := s.client.Store.Contacts.GetContact(context.Background(), jid)
	if err != nil {
		return ""
	}

	// Prefer user's saved name, fall back to push name
	if contact.FullName != "" {
		return contact.FullName
	}
	return contact.PushName
}

// GetContacts returns all stored contacts with names
func (s *Service) GetContacts(query string) ([]ContactInfo, error) {
	if !s.client.IsConnected() {
		return nil, fmt.Errorf("WhatsApp not connected")
	}

	ctx := context.Background()
	contacts, err := s.client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get contacts: %w", err)
	}

	result := make([]ContactInfo, 0)
	queryLower := strings.ToLower(query)

	for jid, info := range contacts {
		// Skip non-user JIDs (groups, etc.)
		if jid.Server != types.DefaultUserServer {
			continue
		}

		// Filter by query if provided
		if query != "" {
			nameMatch := strings.Contains(strings.ToLower(info.FullName), queryLower) ||
				strings.Contains(strings.ToLower(info.PushName), queryLower) ||
				strings.Contains(jid.User, query)
			if !nameMatch {
				continue
			}
		}

		result = append(result, ContactInfo{
			JID:       jid.String(),
			Phone:     jid.User,
			Name:      info.FullName,
			PushName:  info.PushName,
			IsContact: true,
		})
	}

	s.logger.Infof("Retrieved %d contacts (query: %q)", len(result), query)
	return result, nil
}

// GetContactInfo returns full info for a single contact (for send/reply use case)
func (s *Service) GetContactInfo(phone string) (*ContactInfo, error) {
	if !s.client.IsConnected() {
		return nil, fmt.Errorf("WhatsApp not connected")
	}

	// Clean phone number - remove + prefix if present
	phone = strings.TrimPrefix(phone, "+")

	jid := types.NewJID(phone, types.DefaultUserServer)
	ctx := context.Background()

	result := &ContactInfo{
		JID:   jid.String(),
		Phone: jid.User,
	}

	// Get contact details from store (saved names)
	if s.client.Store != nil && s.client.Store.Contacts != nil {
		contact, err := s.client.Store.Contacts.GetContact(ctx, jid)
		if err == nil {
			result.Name = contact.FullName
			result.PushName = contact.PushName
			result.BusinessName = contact.BusinessName
			result.IsContact = true
		}
	}

	// Check WhatsApp registration + business info
	resp, err := s.client.IsOnWhatsApp(ctx, []string{"+" + jid.User})
	if err == nil && len(resp) > 0 {
		if resp[0].VerifiedName != nil {
			result.IsBusiness = true
			result.BusinessName = resp[0].VerifiedName.Details.GetVerifiedName()
		}
	}

	// Get profile photo URL
	picInfo, err := s.client.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: false})
	if err == nil && picInfo != nil && picInfo.URL != "" {
		result.ProfilePic = picInfo.URL
	}

	s.logger.Infof("Retrieved contact info for %s: name=%q", phone, result.Name)
	return result, nil
}

// generateGroupsJSON fetches all groups and saves to data/groups.json
// Called automatically on WhatsApp connection for fast offline access
func (s *Service) generateGroupsJSON() error {
	if !s.client.IsConnected() {
		return fmt.Errorf("not connected")
	}

	ctx := context.Background()
	groups, err := s.client.GetJoinedGroups(ctx)
	if err != nil {
		return fmt.Errorf("failed to get groups: %w", err)
	}

	// Build indexed structure for fast lookups
	groupsMap := make(map[string]interface{})
	phoneToGroups := make(map[string][]string)

	for _, g := range groups {
		participantsMap := make(map[string]interface{})

		for _, p := range g.Participants {
			phone := s.resolveParticipantPhone(ctx, p)
			participantsMap[phone] = map[string]interface{}{
				"jid":            p.JID.String(),
				"phone":          phone,
				"is_admin":       p.IsAdmin,
				"is_super_admin": p.IsSuperAdmin,
			}
			// Build reverse index: phone -> groups
			phoneToGroups[phone] = append(phoneToGroups[phone], g.JID.String())
		}

		groupsMap[g.JID.String()] = map[string]interface{}{
			"jid":          g.JID.String(),
			"name":         g.Name,
			"topic":        g.Topic,
			"owner":        g.OwnerJID.String(),
			"size":         len(g.Participants),
			"is_announce":  g.IsAnnounce,
			"is_locked":    g.IsLocked,
			"created_at":   g.GroupCreated,
			"participants": participantsMap,
		}
	}

	data := map[string]interface{}{
		"generated_at":    time.Now().UTC(),
		"device_id":       s.client.Store.ID.String(),
		"groups":          groupsMap,
		"phone_to_groups": phoneToGroups,
	}

	// Ensure data directory exists
	if err := os.MkdirAll("data", 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Write JSON file with pretty formatting
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if err := os.WriteFile("data/groups.json", jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	s.logger.Infof("Generated data/groups.json with %d groups", len(groups))
	return nil
}

// GetGroups returns all groups the user is a member of
func (s *Service) GetGroups() ([]GroupInfo, error) {
	if !s.client.IsConnected() {
		return nil, fmt.Errorf("WhatsApp not connected")
	}

	ctx := context.Background()
	groups, err := s.client.GetJoinedGroups(ctx)
	if err != nil {
		s.logger.Errorf("Failed to get joined groups: %v", err)
		return nil, fmt.Errorf("failed to get groups: %w", err)
	}

	result := make([]GroupInfo, 0, len(groups))
	for _, g := range groups {
		participants := make([]GroupParticipant, 0, len(g.Participants))
		for _, p := range g.Participants {
			phone := s.resolveParticipantPhone(ctx, p)
			participants = append(participants, GroupParticipant{
				JID:          p.JID.String(),
				Phone:        phone,
				Name:         s.GetContactName(phone),
				IsAdmin:      p.IsAdmin,
				IsSuperAdmin: p.IsSuperAdmin,
			})
		}

		result = append(result, GroupInfo{
			JID:          g.JID.String(),
			Name:         g.Name,
			Topic:        g.Topic,
			Owner:        g.OwnerJID.String(),
			Participants: participants,
			CreatedAt:    g.GroupCreated,
			Size:         len(g.Participants),
			IsAnnounce:   g.IsAnnounce,
			IsLocked:     g.IsLocked,
			IsCommunity:  g.IsParent,
		})
	}

	s.logger.Infof("Retrieved %d groups", len(result))
	return result, nil
}

// GetGroupInfo returns detailed information about a specific group
func (s *Service) GetGroupInfo(groupJID string) (*GroupInfo, error) {
	if !s.client.IsConnected() {
		return nil, fmt.Errorf("WhatsApp not connected")
	}

	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return nil, fmt.Errorf("invalid group JID: %w", err)
	}

	ctx := context.Background()
	info, err := s.client.GetGroupInfo(ctx, jid)
	if err != nil {
		s.logger.Errorf("Failed to get group info for %s: %v", groupJID, err)
		return nil, fmt.Errorf("failed to get group info: %w", err)
	}

	participants := make([]GroupParticipant, 0, len(info.Participants))
	for _, p := range info.Participants {
		phone := s.resolveParticipantPhone(ctx, p)
		participants = append(participants, GroupParticipant{
			JID:          p.JID.String(),
			Phone:        phone,
			Name:         s.GetContactName(phone),
			IsAdmin:      p.IsAdmin,
			IsSuperAdmin: p.IsSuperAdmin,
		})
	}

	result := &GroupInfo{
		JID:          info.JID.String(),
		Name:         info.Name,
		Topic:        info.Topic,
		Owner:        info.OwnerJID.String(),
		Participants: participants,
		CreatedAt:    info.GroupCreated,
		Size:         len(info.Participants),
		IsAnnounce:   info.IsAnnounce,
		IsLocked:     info.IsLocked,
		IsCommunity:  info.IsParent,
	}

	s.logger.Infof("Retrieved info for group %s (%s)", info.Name, groupJID)
	return result, nil
}

// UpdateGroupInfo updates the name and/or topic of a group
func (s *Service) UpdateGroupInfo(groupJID, name, topic string) error {
	if !s.client.IsConnected() {
		return fmt.Errorf("WhatsApp not connected")
	}

	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return fmt.Errorf("invalid group JID: %w", err)
	}

	ctx := context.Background()

	// Update name if provided
	if name != "" {
		err = s.client.SetGroupName(ctx, jid, name)
		if err != nil {
			s.logger.Errorf("Failed to update group name for %s: %v", groupJID, err)
			return fmt.Errorf("failed to update group name: %w", err)
		}
		s.logger.Infof("Updated group name for %s to: %s", groupJID, name)
	}

	// Update topic/description if provided
	if topic != "" {
		// SetGroupTopic requires: ctx, jid, previousTopicID, newTopicID, topic
		// Using empty strings for topic IDs as they're optional for setting new topic
		err = s.client.SetGroupTopic(ctx, jid, "", "", topic)
		if err != nil {
			s.logger.Errorf("Failed to update group topic for %s: %v", groupJID, err)
			return fmt.Errorf("failed to update group topic: %w", err)
		}
		s.logger.Infof("Updated group topic for %s", groupJID)
	}

	return nil
}

// CheckContacts checks if phone numbers are registered on WhatsApp
func (s *Service) CheckContacts(phones []string) ([]ContactCheckResult, error) {
	if !s.client.IsConnected() {
		return nil, fmt.Errorf("WhatsApp not connected")
	}

	ctx := context.Background()
	results, err := s.client.IsOnWhatsApp(ctx, phones)
	if err != nil {
		s.logger.Errorf("Failed to check contacts: %v", err)
		return nil, fmt.Errorf("failed to check contacts: %w", err)
	}

	response := make([]ContactCheckResult, 0, len(results))
	for _, r := range results {
		result := ContactCheckResult{
			Query:        r.Query,
			IsRegistered: r.IsIn,
		}
		if r.IsIn && r.JID.User != "" {
			result.JID = r.JID.String()
		}
		if r.VerifiedName != nil {
			result.IsBusiness = true
			result.BusinessName = r.VerifiedName.Details.GetVerifiedName()
		}
		response = append(response, result)
	}

	s.logger.Infof("Checked %d phone numbers, %d registered", len(phones), countRegistered(response))
	return response, nil
}

func countRegistered(results []ContactCheckResult) int {
	count := 0
	for _, r := range results {
		if r.IsRegistered {
			count++
		}
	}
	return count
}

// GetProfilePicture gets profile picture for a JID
func (s *Service) GetProfilePicture(jidStr string, preview bool) (*ProfilePicResult, error) {
	if !s.client.IsConnected() {
		return nil, fmt.Errorf("WhatsApp not connected")
	}

	jid, err := types.ParseJID(jidStr)
	if err != nil {
		return nil, fmt.Errorf("invalid JID: %w", err)
	}

	ctx := context.Background()
	params := &whatsmeow.GetProfilePictureParams{
		Preview: preview,
	}

	info, err := s.client.GetProfilePictureInfo(ctx, jid, params)
	if err != nil {
		// Profile picture may not exist - this is not an error
		errStr := err.Error()
		if strings.Contains(errStr, "item-not-found") || strings.Contains(errStr, "not-authorized") {
			return &ProfilePicResult{Exists: false}, nil
		}
		s.logger.Errorf("Failed to get profile picture for %s: %v", jidStr, err)
		return nil, fmt.Errorf("failed to get profile picture: %w", err)
	}

	result := &ProfilePicResult{
		Exists: true,
		URL:    info.URL,
		ID:     info.ID,
	}

	s.logger.Infof("Retrieved profile picture for %s", jidStr)
	return result, nil
}

// SendTyping sends typing indicator to a chat
func (s *Service) SendTyping(jidStr, state, media string) error {
	if !s.client.IsConnected() {
		return fmt.Errorf("WhatsApp not connected")
	}

	jid, err := types.ParseJID(jidStr)
	if err != nil {
		return fmt.Errorf("invalid JID: %w", err)
	}

	// Convert state string to ChatPresence
	var chatPresence types.ChatPresence
	switch state {
	case "composing":
		chatPresence = types.ChatPresenceComposing
	case "paused":
		chatPresence = types.ChatPresencePaused
	default:
		return fmt.Errorf("invalid state: %s (must be 'composing' or 'paused')", state)
	}

	// Convert media string to ChatPresenceMedia
	var chatMedia types.ChatPresenceMedia
	switch media {
	case "", "text":
		chatMedia = types.ChatPresenceMediaText
	case "audio":
		chatMedia = types.ChatPresenceMediaAudio
	default:
		return fmt.Errorf("invalid media: %s (must be 'text' or 'audio')", media)
	}

	ctx := context.Background()
	err = s.client.SendChatPresence(ctx, jid, chatPresence, chatMedia)
	if err != nil {
		s.logger.Errorf("Failed to send typing indicator to %s: %v", jidStr, err)
		return fmt.Errorf("failed to send typing indicator: %w", err)
	}

	s.logger.Infof("Sent typing indicator (%s) to %s", state, jidStr)
	return nil
}

// SetPresence sets online/offline presence status
func (s *Service) SetPresence(status string) error {
	if !s.client.IsConnected() {
		return fmt.Errorf("WhatsApp not connected")
	}

	var presence types.Presence
	switch status {
	case "available", "online":
		presence = types.PresenceAvailable
	case "unavailable", "offline":
		presence = types.PresenceUnavailable
	default:
		return fmt.Errorf("invalid status: %s (must be 'available' or 'unavailable')", status)
	}

	ctx := context.Background()
	err := s.client.SendPresence(ctx, presence)
	if err != nil {
		s.logger.Errorf("Failed to set presence to %s: %v", status, err)
		return fmt.Errorf("failed to set presence: %w", err)
	}

	s.logger.Infof("Set presence to %s", status)
	return nil
}

// MarkRead marks messages as read
func (s *Service) MarkRead(messageIDs []string, chatJID, senderJID string) error {
	if !s.client.IsConnected() {
		return fmt.Errorf("WhatsApp not connected")
	}

	if len(messageIDs) == 0 {
		return fmt.Errorf("at least one message_id is required")
	}

	chat, err := types.ParseJID(chatJID)
	if err != nil {
		return fmt.Errorf("invalid chat_jid: %w", err)
	}

	// For DMs, sender is the same as chat
	// For groups, sender is the actual message sender
	sender := chat
	if senderJID != "" {
		sender, err = types.ParseJID(senderJID)
		if err != nil {
			return fmt.Errorf("invalid sender_jid: %w", err)
		}
	}

	ctx := context.Background()
	err = s.client.MarkRead(ctx, messageIDs, time.Now(), chat, sender)
	if err != nil {
		s.logger.Errorf("Failed to mark messages as read: %v", err)
		return fmt.Errorf("failed to mark messages as read: %w", err)
	}

	s.logger.Infof("Marked %d message(s) as read in %s", len(messageIDs), chatJID)
	return nil
}

// normalizeToJID converts a phone number or JID string to types.JID
func (s *Service) normalizeToJID(input string) (types.JID, error) {
	// If it's already a JID (contains @), parse it directly
	if strings.Contains(input, "@") {
		return types.ParseJID(input)
	}

	// Otherwise, treat as phone number and create user JID
	// Remove any non-digit characters
	phone := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, input)

	if phone == "" {
		return types.JID{}, fmt.Errorf("invalid phone number: %s", input)
	}

	return types.NewJID(phone, types.DefaultUserServer), nil
}

// AddGroupParticipants adds participants to a group
func (s *Service) AddGroupParticipants(groupJID string, participants []string) (*GroupParticipantsResult, error) {
	if !s.client.IsConnected() {
		return nil, fmt.Errorf("WhatsApp not connected")
	}

	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return nil, fmt.Errorf("invalid group JID: %w", err)
	}

	// Convert phone numbers/JIDs to types.JID slice
	participantJIDs := make([]types.JID, 0, len(participants))
	for _, p := range participants {
		pJID, err := s.normalizeToJID(p)
		if err != nil {
			s.logger.Warnf("Invalid participant %s: %v", p, err)
			continue
		}
		participantJIDs = append(participantJIDs, pJID)
	}

	if len(participantJIDs) == 0 {
		return nil, fmt.Errorf("no valid participants provided")
	}

	ctx := context.Background()
	results, err := s.client.UpdateGroupParticipants(ctx, jid, participantJIDs, whatsmeow.ParticipantChangeAdd)
	if err != nil {
		s.logger.Errorf("Failed to add participants to %s: %v", groupJID, err)
		return nil, fmt.Errorf("failed to add participants: %w", err)
	}

	// Build result
	response := &GroupParticipantsResult{
		GroupID: groupJID,
		Action:  "add",
		Results: make([]GroupParticipantChangeResult, 0, len(results)),
	}

	for _, r := range results {
		result := GroupParticipantChangeResult{
			JID:     r.JID.String(),
			Phone:   r.JID.User,
			Success: r.Error == 0, // 0 means success
		}
		if r.Error != 0 {
			result.Error = fmt.Sprintf("error code: %d", r.Error)
			response.Failed++
		} else {
			response.Added++
		}
		response.Results = append(response.Results, result)
	}

	s.logger.Infof("Added %d participants to group %s (failed: %d)", response.Added, groupJID, response.Failed)
	return response, nil
}

// RemoveGroupParticipants removes participants from a group
func (s *Service) RemoveGroupParticipants(groupJID string, participants []string) (*GroupParticipantsResult, error) {
	if !s.client.IsConnected() {
		return nil, fmt.Errorf("WhatsApp not connected")
	}

	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return nil, fmt.Errorf("invalid group JID: %w", err)
	}

	// Convert phone numbers/JIDs to types.JID slice
	participantJIDs := make([]types.JID, 0, len(participants))
	for _, p := range participants {
		pJID, err := s.normalizeToJID(p)
		if err != nil {
			s.logger.Warnf("Invalid participant %s: %v", p, err)
			continue
		}
		participantJIDs = append(participantJIDs, pJID)
	}

	if len(participantJIDs) == 0 {
		return nil, fmt.Errorf("no valid participants provided")
	}

	ctx := context.Background()
	results, err := s.client.UpdateGroupParticipants(ctx, jid, participantJIDs, whatsmeow.ParticipantChangeRemove)
	if err != nil {
		s.logger.Errorf("Failed to remove participants from %s: %v", groupJID, err)
		return nil, fmt.Errorf("failed to remove participants: %w", err)
	}

	// Build result
	response := &GroupParticipantsResult{
		GroupID: groupJID,
		Action:  "remove",
		Results: make([]GroupParticipantChangeResult, 0, len(results)),
	}

	for _, r := range results {
		result := GroupParticipantChangeResult{
			JID:     r.JID.String(),
			Phone:   r.JID.User,
			Success: r.Error == 0,
		}
		if r.Error != 0 {
			result.Error = fmt.Sprintf("error code: %d", r.Error)
			response.Failed++
		} else {
			response.Removed++
		}
		response.Results = append(response.Results, result)
	}

	s.logger.Infof("Removed %d participants from group %s (failed: %d)", response.Removed, groupJID, response.Failed)
	return response, nil
}

// GetGroupInviteLink gets the invite link for a group
func (s *Service) GetGroupInviteLink(groupJID string) (*GroupInviteLinkResult, error) {
	if !s.client.IsConnected() {
		return nil, fmt.Errorf("WhatsApp not connected")
	}

	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return nil, fmt.Errorf("invalid group JID: %w", err)
	}

	ctx := context.Background()
	link, err := s.client.GetGroupInviteLink(ctx, jid, false)
	if err != nil {
		s.logger.Errorf("Failed to get invite link for %s: %v", groupJID, err)
		return nil, fmt.Errorf("failed to get invite link: %w", err)
	}

	s.logger.Infof("Retrieved invite link for group %s", groupJID)
	return &GroupInviteLinkResult{
		GroupID:    groupJID,
		InviteLink: link,
		Revoked:    false,
	}, nil
}

// RevokeGroupInviteLink revokes the current invite link and generates a new one
func (s *Service) RevokeGroupInviteLink(groupJID string) (*GroupInviteLinkResult, error) {
	if !s.client.IsConnected() {
		return nil, fmt.Errorf("WhatsApp not connected")
	}

	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return nil, fmt.Errorf("invalid group JID: %w", err)
	}

	ctx := context.Background()
	// reset=true revokes the old link and generates new one
	link, err := s.client.GetGroupInviteLink(ctx, jid, true)
	if err != nil {
		s.logger.Errorf("Failed to revoke invite link for %s: %v", groupJID, err)
		return nil, fmt.Errorf("failed to revoke invite link: %w", err)
	}

	s.logger.Infof("Revoked and regenerated invite link for group %s", groupJID)
	return &GroupInviteLinkResult{
		GroupID:    groupJID,
		InviteLink: link,
		Revoked:    true,
	}, nil
}

// GetCurrentQRCode returns the last generated QR code if recent
func (s *Service) GetCurrentQRCode() *QRCodeData {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastQRCode == nil {
		return nil
	}

	// Return QR if it was generated within last 60 seconds
	if time.Since(s.lastQRCodeTime) > 60*time.Second {
		return nil
	}

	return s.lastQRCode
}

// ============================================================================
// Rate Limiting Methods
// ============================================================================

// GetRateLimitConfig returns the current rate limit configuration
func (s *Service) GetRateLimitConfig() *RateLimitConfig {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	return s.rateLimitConfig
}

// SetRateLimitConfig updates the rate limit configuration
func (s *Service) SetRateLimitConfig(config *RateLimitConfig) error {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()

	// Validate config
	if config.MinDelayMs < 0 {
		return fmt.Errorf("min_delay_ms cannot be negative")
	}
	if config.MaxDelayMs < config.MinDelayMs {
		config.MaxDelayMs = config.MinDelayMs
	}
	if config.MaxMessagesPerMinute < 0 {
		config.MaxMessagesPerMinute = 0
	}
	if config.MaxMessagesPerHour < 0 {
		config.MaxMessagesPerHour = 0
	}
	if config.MaxNewContactsPerDay < 0 {
		config.MaxNewContactsPerDay = 0
	}

	s.rateLimitConfig = config
	s.logger.Infof("Rate limit config updated: enabled=%v, delay=%d-%dms, typing=%v",
		config.Enabled, config.MinDelayMs, config.MaxDelayMs, config.SimulateTyping)
	return nil
}

// GetRateLimitStats returns current rate limiting statistics
func (s *Service) GetRateLimitStats() *RateLimitStats {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()

	// Reset daily counters if needed
	s.resetDailyCountersIfNeeded()

	// Calculate stats from sliding window
	now := time.Now()
	minuteAgo := now.Add(-1 * time.Minute)
	hourAgo := now.Add(-1 * time.Hour)

	minuteCount := 0
	hourCount := 0
	for _, t := range s.messageTimes {
		if t.After(minuteAgo) {
			minuteCount++
		}
		if t.After(hourAgo) {
			hourCount++
		}
	}

	// Count new contacts today
	newContactsCount := 0
	for _, t := range s.newContactsToday {
		if t.After(s.dailyResetTime) {
			newContactsCount++
		}
	}

	// Calculate response rate (simplified - based on messages received vs sent)
	responseRate := 0.0
	if s.rateLimitStats.MessagesSentToday > 0 {
		responseRate = float64(s.rateLimitStats.ResponsesReceived) / float64(s.rateLimitStats.MessagesSentToday)
	}

	// Calculate next allowed time
	nextAllowed := s.rateLimitStats.LastMessageTime.Add(time.Duration(s.rateLimitConfig.MinDelayMs) * time.Millisecond)

	return &RateLimitStats{
		MessagesSentLastMinute: minuteCount,
		MessagesSentLastHour:   hourCount,
		MessagesSentToday:      s.rateLimitStats.MessagesSentToday,
		NewContactsToday:       newContactsCount,
		ResponsesReceived:      s.rateLimitStats.ResponsesReceived,
		ResponseRate:           responseRate,
		IsPaused:               s.rateLimitStats.IsPaused,
		PauseReason:            s.rateLimitStats.PauseReason,
		LastMessageTime:        s.rateLimitStats.LastMessageTime,
		NextAllowedTime:        nextAllowed,
	}
}

// resetDailyCountersIfNeeded resets daily counters at midnight
func (s *Service) resetDailyCountersIfNeeded() {
	now := time.Now()
	// Check if we've crossed midnight since last reset
	if now.Day() != s.dailyResetTime.Day() || now.Month() != s.dailyResetTime.Month() {
		s.rateLimitStats.MessagesSentToday = 0
		s.rateLimitStats.ResponsesReceived = 0
		s.newContactsToday = make(map[string]time.Time)
		s.dailyResetTime = now
		s.logger.Info("Daily rate limit counters reset")
	}
}

// CheckRateLimit checks rate limits and waits if necessary. Returns error only for hard limits.
func (s *Service) CheckRateLimit(recipient string) error {
	s.rateMu.Lock()
	config := s.rateLimitConfig
	s.rateMu.Unlock()

	if !config.Enabled {
		return nil
	}

	// Keep checking and waiting until we're within limits
	for {
		waitDuration, hardError := s.checkRateLimitInternal(recipient)
		if hardError != nil {
			return hardError // New contacts limit or paused - can't wait
		}
		if waitDuration <= 0 {
			return nil // Within limits, proceed
		}

		// Wait until we're within rate limits
		s.logger.Infof("Rate limit reached, waiting %v before sending...", waitDuration)
		time.Sleep(waitDuration)
	}
}

// checkRateLimitInternal checks limits and returns wait duration or hard error
func (s *Service) checkRateLimitInternal(recipient string) (time.Duration, error) {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()

	// Reset daily counters if needed
	s.resetDailyCountersIfNeeded()

	// Check if paused - this is a hard error, can't wait
	if s.rateLimitStats.IsPaused {
		return 0, fmt.Errorf("rate limiting paused: %s", s.rateLimitStats.PauseReason)
	}

	now := time.Now()

	// Clean up old message times (keep only last hour)
	hourAgo := now.Add(-1 * time.Hour)
	newTimes := make([]time.Time, 0, len(s.messageTimes))
	for _, t := range s.messageTimes {
		if t.After(hourAgo) {
			newTimes = append(newTimes, t)
		}
	}
	s.messageTimes = newTimes

	// Count messages in last minute and hour
	minuteAgo := now.Add(-1 * time.Minute)
	minuteCount := 0
	hourCount := len(s.messageTimes)

	// Find oldest message in last minute (to calculate wait time)
	var oldestInMinute time.Time
	var oldestInHour time.Time
	for _, t := range s.messageTimes {
		if t.After(minuteAgo) {
			minuteCount++
			if oldestInMinute.IsZero() || t.Before(oldestInMinute) {
				oldestInMinute = t
			}
		}
		if oldestInHour.IsZero() || t.Before(oldestInHour) {
			oldestInHour = t
		}
	}

	// Check per-minute limit - calculate wait time if exceeded
	if s.rateLimitConfig.MaxMessagesPerMinute > 0 && minuteCount >= s.rateLimitConfig.MaxMessagesPerMinute {
		// Wait until oldest message in minute window expires
		waitUntil := oldestInMinute.Add(1 * time.Minute)
		waitDuration := waitUntil.Sub(now) + 100*time.Millisecond // Add small buffer
		if waitDuration > 0 {
			s.logger.Debugf("Per-minute limit reached (%d/%d), need to wait %v",
				minuteCount, s.rateLimitConfig.MaxMessagesPerMinute, waitDuration)
			return waitDuration, nil
		}
	}

	// Check per-hour limit - calculate wait time if exceeded
	if s.rateLimitConfig.MaxMessagesPerHour > 0 && hourCount >= s.rateLimitConfig.MaxMessagesPerHour {
		// Wait until oldest message in hour window expires
		waitUntil := oldestInHour.Add(1 * time.Hour)
		waitDuration := waitUntil.Sub(now) + 100*time.Millisecond // Add small buffer
		if waitDuration > 0 {
			s.logger.Debugf("Per-hour limit reached (%d/%d), need to wait %v",
				hourCount, s.rateLimitConfig.MaxMessagesPerHour, waitDuration)
			return waitDuration, nil
		}
	}

	// Check new contacts limit - this is a HARD limit, return error (can't wait for tomorrow)
	if s.rateLimitConfig.MaxNewContactsPerDay > 0 {
		if _, seen := s.contactsSeen[recipient]; !seen {
			newContactsCount := 0
			for _, t := range s.newContactsToday {
				if t.After(s.dailyResetTime) {
					newContactsCount++
				}
			}
			if newContactsCount >= s.rateLimitConfig.MaxNewContactsPerDay {
				return 0, fmt.Errorf("new contacts limit exceeded: %d new contacts/day (max: %d) - try again tomorrow",
					newContactsCount, s.rateLimitConfig.MaxNewContactsPerDay)
			}
		}
	}

	// Check response rate threshold (if enabled) - hard error
	if s.rateLimitConfig.PauseOnLowResponse && s.rateLimitStats.MessagesSentToday >= 10 {
		responseRate := float64(s.rateLimitStats.ResponsesReceived) / float64(s.rateLimitStats.MessagesSentToday)
		if responseRate < s.rateLimitConfig.ResponseRateThreshold {
			s.rateLimitStats.IsPaused = true
			s.rateLimitStats.PauseReason = fmt.Sprintf("low response rate: %.1f%% (threshold: %.1f%%)",
				responseRate*100, s.rateLimitConfig.ResponseRateThreshold*100)
			return 0, fmt.Errorf("rate limiting paused: %s", s.rateLimitStats.PauseReason)
		}
	}

	return 0, nil // Within all limits
}

// ApplyMessageDelay applies the configured delay before sending a message
func (s *Service) ApplyMessageDelay(hasLinks bool) {
	s.rateMu.Lock()
	config := s.rateLimitConfig
	s.rateMu.Unlock()

	if !config.Enabled || config.MinDelayMs <= 0 {
		return
	}

	delay := config.MinDelayMs

	// Add randomization if enabled
	if config.RandomizeDelays && config.MaxDelayMs > config.MinDelayMs {
		delay = config.MinDelayMs + rand.Intn(config.MaxDelayMs-config.MinDelayMs)
	}

	// Add extra delay for messages with links
	if hasLinks && config.LinkExtraDelayMs > 0 {
		delay += config.LinkExtraDelayMs
	}

	s.logger.Debugf("Applying message delay: %dms (links: %v)", delay, hasLinks)
	time.Sleep(time.Duration(delay) * time.Millisecond)
}

// SendTypingIfEnabled sends typing indicator if configured
// The typing indicator stays active until the message is sent
func (s *Service) SendTypingIfEnabled(jidStr string) {
	s.rateMu.Lock()
	config := s.rateLimitConfig
	s.rateMu.Unlock()

	if !config.Enabled || !config.SimulateTyping || config.TypingDelayMs <= 0 {
		return
	}

	// Parse JID
	jid, err := types.ParseJID(jidStr)
	if err != nil {
		s.logger.Debugf("Could not parse JID for typing indicator: %v", err)
		return
	}

	// Send composing indicator - stays active until message is sent
	ctx := context.Background()
	err = s.client.SendChatPresence(ctx, jid, types.ChatPresenceComposing, types.ChatPresenceMediaText)
	if err != nil {
		s.logger.Debugf("Failed to send typing indicator: %v", err)
		return
	}

	s.logger.Debugf("Sent typing indicator to %s, waiting %dms", jidStr, config.TypingDelayMs)

	// Wait for typing duration (indicator stays active)
	time.Sleep(time.Duration(config.TypingDelayMs) * time.Millisecond)

	// No paused indicator - typing stays active until message is sent
	// WhatsApp clears the typing indicator when the message arrives
}

// RecordMessageSent updates rate limiting stats after a message is sent
func (s *Service) RecordMessageSent(recipient string) {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()

	now := time.Now()

	// Record message time
	s.messageTimes = append(s.messageTimes, now)
	s.rateLimitStats.MessagesSentToday++
	s.rateLimitStats.LastMessageTime = now

	// Track new contacts
	if _, seen := s.contactsSeen[recipient]; !seen {
		s.contactsSeen[recipient] = true
		s.newContactsToday[recipient] = now
		s.logger.Debugf("New contact recorded: %s (total today: %d)", recipient, len(s.newContactsToday))
	}
}

// RecordResponseReceived increments the response counter (call when receiving a message)
func (s *Service) RecordResponseReceived() {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	s.rateLimitStats.ResponsesReceived++
}

// UnpauseRateLimiting resumes rate limiting after it was paused
func (s *Service) UnpauseRateLimiting() {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	s.rateLimitStats.IsPaused = false
	s.rateLimitStats.PauseReason = ""
	s.logger.Info("Rate limiting unpaused")
}

func (s *Service) Shutdown() {
	if s.shutdown {
		return // Already shutdown
	}

	s.logger.Info("Shutting down WhatsApp service...")
	s.shutdown = true
	s.running = false
	s.pairing = false

	if s.client != nil {
		s.client.Disconnect()
	}

	// Give goroutines more time to exit cleanly
	time.Sleep(500 * time.Millisecond)

	// Drain any remaining events but DON'T close the channel
	// This allows ForwardEvents goroutines to continue working after restart
	for {
		select {
		case <-s.events:
			// Keep draining
		default:
			goto done
		}
	}
	done:

	// DO NOT close(s.events) - keep channel open for restart
	s.logger.Info("WhatsApp service shutdown complete")
}

func (s *Service) Reset() error {
	s.logger.Info("Starting full reset with logout...")

	// Logout from WhatsApp servers first (invalidates session on server side)
	if s.client != nil && s.client.IsLoggedIn() {
		s.logger.Info("Logging out from WhatsApp...")
		if err := s.client.Logout(context.Background()); err != nil {
			s.logger.Warnf("Logout failed (may already be logged out): %v", err)
		} else {
			s.logger.Info("Logged out from WhatsApp successfully")
		}
	}

	// Shutdown the service (disconnect, close channels)
	s.Shutdown()

	// Delete the device session from store
	if s.client != nil && s.client.Store != nil {
		s.logger.Info("Deleting device session from store...")
		if err := s.client.Store.Delete(context.Background()); err != nil {
			s.logger.Warnf("Failed to delete device store: %v", err)
		}
	}

	// Clear message history (belongs to old instance)
	if s.historyStore != nil {
		s.logger.Info("Clearing message history...")
		if err := s.historyStore.ClearHistory(); err != nil {
			s.logger.Warnf("Failed to clear history: %v", err)
		}
	}

	// Clear client reference to allow GC and release DB locks
	s.client = nil
	s.container = nil

	// Give time for database to release locks
	time.Sleep(500 * time.Millisecond)

	// Reinitialize client with fresh database
	if err := s.reinitClient(); err != nil {
		s.logger.Errorf("Failed to reinitialize client: %v", err)
		return fmt.Errorf("failed to reinitialize client: %w", err)
	}

	s.logger.Info("WhatsApp reset complete")
	return nil
}

// reinitClient creates a new database and client after reset
func (s *Service) reinitClient() error {
	s.logger.Info("Reinitializing WhatsApp client...")

	// Create new database
	dbLog := waLog.Stdout("Database", "INFO", true)
	ctx := context.Background()
	container, err := sqlstore.New(ctx, "sqlite", "file:"+s.dbPath+"?_pragma=foreign_keys(1)&_journal_mode=WAL&_busy_timeout=30000&cache=shared", dbLog)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	// Get device store
	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("failed to get device: %w", err)
	}

	// Create new client
	clientLog := waLog.Stdout("WhatsApp", "INFO", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	// Update service references
	s.container = container
	s.client = client

	// Add event handler to new client
	client.AddEventHandler(s.eventHandler)

	s.logger.Info("WhatsApp client reinitialized successfully")
	return nil
}

func (s *Service) cleanupOldQRCodes() {
	s.CleanupQRCodes()
}

// CleanupQRCodes deletes all QR code PNG files
func (s *Service) CleanupQRCodes() {
	// Delete all old QR code PNG files
	files, err := filepath.Glob("data/qr/qr_*.png")
	if err != nil {
		s.logger.Warnf("Failed to find old QR code files: %v", err)
		return
	}

	if len(files) > 0 {
		s.logger.Infof("Cleaning up %d old QR code file(s)", len(files))
		for _, file := range files {
			if err := os.Remove(file); err != nil {
				s.logger.Warnf("Failed to delete old QR code file %s: %v", file, err)
			} else {
				s.logger.Debugf("Deleted old QR code file: %s", file)
			}
		}
	}

	// Clear cached QR code
	s.lastQRCode = nil
}