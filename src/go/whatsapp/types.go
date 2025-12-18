package whatsapp

import "time"

// Event represents a WhatsApp service event
type Event struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data"`
	Time time.Time              `json:"timestamp"`
}

// QRCodeData represents QR code information
type QRCodeData struct {
	Code      string    `json:"code"`
	Filename  string    `json:"filename"`
	ImageData string    `json:"image_data,omitempty"` // Base64 PNG data
	Time      time.Time `json:"timestamp"`
}

// MessageRequest represents various message types for sending
type MessageRequest struct {
	Phone     string            `json:"phone,omitempty"`
	GroupID   string            `json:"group_id,omitempty"`
	Message   string            `json:"message,omitempty"`
	Type      string            `json:"type"` // text, image, document, audio, video, location, sticker, contact
	MediaData *MediaData        `json:"media_data,omitempty"`
	Location  *LocationData     `json:"location,omitempty"`
	Contact   *ContactCard      `json:"contact,omitempty"`
	Reply     *ReplyData        `json:"reply,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// MediaData represents media content for messages
type MediaData struct {
	Data      string `json:"data"`      // base64 encoded data
	MimeType  string `json:"mime_type"` // image/jpeg, audio/mp3, etc.
	Filename  string `json:"filename"`  // optional filename
	Caption   string `json:"caption"`   // optional caption for media
	Thumbnail string `json:"thumbnail"` // base64 encoded thumbnail (optional)
}

// LocationData represents location information
type LocationData struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Name      string  `json:"name,omitempty"`    // optional location name
	Address   string  `json:"address,omitempty"` // optional address
}

// ReplyData represents reply context for messages
type ReplyData struct {
	MessageID string `json:"message_id"`
	Sender    string `json:"sender"`
	Content   string `json:"content"`
}

// ContactCard represents a contact vCard for sharing
type ContactCard struct {
	DisplayName string `json:"display_name"`
	VCard       string `json:"vcard"` // vCard format string
}

// ContactInfo represents comprehensive contact information
type ContactInfo struct {
	JID          string    `json:"jid"`
	Phone        string    `json:"phone"`
	Name         string    `json:"name,omitempty"`
	BusinessName string    `json:"business_name,omitempty"`
	PushName     string    `json:"push_name,omitempty"`
	IsContact    bool      `json:"is_contact"`
	IsBusiness   bool      `json:"is_business"`
	LastSeen     time.Time `json:"last_seen,omitempty"`
	ProfilePic   string    `json:"profile_pic,omitempty"` // base64 encoded
}

// GroupInfo represents comprehensive group information
type GroupInfo struct {
	JID          string             `json:"jid"`
	Name         string             `json:"name"`
	Topic        string             `json:"topic,omitempty"`
	Owner        string             `json:"owner"`
	Participants []GroupParticipant `json:"participants"`
	CreatedAt    time.Time          `json:"created_at"`
	Size         int                `json:"size"`
	IsAnnounce   bool               `json:"is_announce"`  // Only admins can send messages
	IsLocked     bool               `json:"is_locked"`    // Only admins can edit group info
}

// GroupUpdateRequest represents parameters for updating a group
type GroupUpdateRequest struct {
	GroupID string `json:"group_id"`
	Name    string `json:"name,omitempty"`
	Topic   string `json:"topic,omitempty"`
}

// GroupParticipant represents a group member
type GroupParticipant struct {
	JID          string `json:"jid"`
	Phone        string `json:"phone"`
	Name         string `json:"name,omitempty"`
	IsAdmin      bool   `json:"is_admin"`
	IsSuperAdmin bool   `json:"is_super_admin"`
}

// MessageInfo represents received message information
type MessageInfo struct {
	ID        string             `json:"id"`
	From      string             `json:"from"`
	To        string             `json:"to"`
	Type      string             `json:"type"`
	Content   string             `json:"content,omitempty"`
	MediaInfo *ReceivedMediaInfo `json:"media_info,omitempty"`
	Location  *LocationData      `json:"location,omitempty"`
	GroupInfo *GroupMessageInfo  `json:"group_info,omitempty"`
	Reply     *ReplyData         `json:"reply,omitempty"`
	Timestamp time.Time          `json:"timestamp"`
	IsFromMe  bool               `json:"is_from_me"`
	Status    string             `json:"status"` // sent, delivered, read
}

// ReceivedMediaInfo represents received media message information
type ReceivedMediaInfo struct {
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
	Filename string `json:"filename,omitempty"`
	Caption  string `json:"caption,omitempty"`
	URL      string `json:"url"`    // download URL
	SHA256   string `json:"sha256"` // file hash
}

// GroupMessageInfo represents group message context
type GroupMessageInfo struct {
	GroupJID   string `json:"group_jid"`
	GroupName  string `json:"group_name,omitempty"`
	SenderJID  string `json:"sender_jid"`
	SenderName string `json:"sender_name,omitempty"`
}

// PresenceInfo represents presence information
type PresenceInfo struct {
	JID      string    `json:"jid"`
	Status   string    `json:"status"` // available, unavailable, composing, paused
	LastSeen time.Time `json:"last_seen,omitempty"`
}

// WebhookConfig represents webhook configuration
type WebhookConfig struct {
	URL     string            `json:"url"`
	Secret  string            `json:"secret,omitempty"`
	Events  []string          `json:"events"`  // message, status, presence, etc.
	Headers map[string]string `json:"headers"` // custom headers
	Enabled bool              `json:"enabled"`
}

// MessageStatus represents delivery status
type MessageStatus struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"` // sent, delivered, read
	Timestamp time.Time `json:"timestamp"`
	From      string    `json:"from"`
	To        string    `json:"to"`
}

// ContactCheckResult represents the result of checking if a phone is on WhatsApp
type ContactCheckResult struct {
	Query        string `json:"query"`                   // Original phone number queried
	JID          string `json:"jid,omitempty"`           // WhatsApp JID if registered
	IsRegistered bool   `json:"is_registered"`           // Whether registered on WhatsApp
	IsBusiness   bool   `json:"is_business"`             // Whether it's a business account
	BusinessName string `json:"business_name,omitempty"` // Business verified name
}

// ProfilePicResult represents profile picture information
type ProfilePicResult struct {
	URL    string `json:"url,omitempty"`  // URL to download image
	ID     string `json:"id,omitempty"`   // Image identifier
	Data   string `json:"data,omitempty"` // Base64-encoded image data
	Exists bool   `json:"exists"`         // Whether profile picture exists
}

// GroupParticipantChangeResult represents result for a single participant change
type GroupParticipantChangeResult struct {
	JID     string `json:"jid"`
	Phone   string `json:"phone,omitempty"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// GroupParticipantsResult represents batch participant change result
type GroupParticipantsResult struct {
	GroupID string                         `json:"group_id"`
	Action  string                         `json:"action"` // "add" or "remove"
	Results []GroupParticipantChangeResult `json:"results"`
	Added   int                            `json:"added,omitempty"`
	Removed int                            `json:"removed,omitempty"`
	Failed  int                            `json:"failed"`
}

// GroupInviteLinkResult represents invite link response
type GroupInviteLinkResult struct {
	GroupID    string `json:"group_id"`
	InviteLink string `json:"invite_link"`
	Revoked    bool   `json:"revoked,omitempty"`
}

// RateLimitConfig represents configurable rate limiting settings for anti-ban protection
type RateLimitConfig struct {
	Enabled bool `json:"enabled"`

	// Message delays (milliseconds)
	MinDelayMs       int `json:"min_delay_ms"`        // Minimum delay between messages (default: 3000)
	MaxDelayMs       int `json:"max_delay_ms"`        // Maximum delay for randomization (default: 8000)
	TypingDelayMs    int `json:"typing_delay_ms"`     // Typing indicator duration (default: 2000)
	LinkExtraDelayMs int `json:"link_extra_delay_ms"` // Extra delay for messages with links (default: 5000)

	// Rate limits
	MaxMessagesPerMinute int `json:"max_messages_per_minute"` // Per-minute limit (default: 10)
	MaxMessagesPerHour   int `json:"max_messages_per_hour"`   // Per-hour limit (default: 60)
	MaxNewContactsPerDay int `json:"max_new_contacts_per_day"` // New contacts limit (default: 20)

	// Human simulation
	SimulateTyping  bool `json:"simulate_typing"`  // Send typing indicator before message
	RandomizeDelays bool `json:"randomize_delays"` // Add random variance to delays

	// Safety
	PauseOnLowResponse    bool    `json:"pause_on_low_response"`    // Pause if response rate < threshold
	ResponseRateThreshold float64 `json:"response_rate_threshold"` // Min response rate (0.3 = 30%)
}

// RateLimitStats represents current rate limiting statistics
type RateLimitStats struct {
	MessagesSentLastMinute int       `json:"messages_sent_last_minute"`
	MessagesSentLastHour   int       `json:"messages_sent_last_hour"`
	MessagesSentToday      int       `json:"messages_sent_today"`
	NewContactsToday       int       `json:"new_contacts_today"`
	ResponsesReceived      int       `json:"responses_received"`
	ResponseRate           float64   `json:"response_rate"`
	IsPaused               bool      `json:"is_paused"`
	PauseReason            string    `json:"pause_reason,omitempty"`
	LastMessageTime        time.Time `json:"last_message_time"`
	NextAllowedTime        time.Time `json:"next_allowed_time"`
}

// DefaultRateLimitConfig returns conservative defaults for anti-ban protection
func DefaultRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		Enabled:               true,
		MinDelayMs:            3000,  // 3 seconds minimum
		MaxDelayMs:            8000,  // Up to 8 seconds with randomization
		TypingDelayMs:         2000,  // 2 seconds typing
		LinkExtraDelayMs:      5000,  // 5 extra seconds for links
		MaxMessagesPerMinute:  10,    // Conservative
		MaxMessagesPerHour:    60,    // 1 per minute average
		MaxNewContactsPerDay:  20,    // Critical for new accounts
		SimulateTyping:        true,  // Major anti-detection
		RandomizeDelays:       true,  // Avoid fixed patterns
		PauseOnLowResponse:    false, // Disabled by default
		ResponseRateThreshold: 0.3,   // 30% minimum
	}
}