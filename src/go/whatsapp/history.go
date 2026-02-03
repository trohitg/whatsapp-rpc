package whatsapp

import (
	"database/sql"
	"time"

	"github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

// HistoryStore handles message persistence for chat history
type HistoryStore struct {
	db     *sql.DB
	logger *logrus.Logger
}

// MessageRecord represents a stored message
type MessageRecord struct {
	MessageID   string    `json:"message_id"`
	ChatID      string    `json:"chat_id"`
	Sender      string    `json:"sender"`
	SenderPhone string    `json:"sender_phone"`
	MessageType string    `json:"message_type"`
	Text        string    `json:"text"`
	Timestamp   time.Time `json:"timestamp"`
	IsGroup     bool      `json:"is_group"`
	IsFromMe    bool      `json:"is_from_me"`
}

// ChatHistoryResult is the response for chat_history queries
type ChatHistoryResult struct {
	Messages []MessageRecord `json:"messages"`
	Total    int             `json:"total"`
	HasMore  bool            `json:"has_more"`
}

// NewHistoryStore creates a new history store with SQLite persistence
func NewHistoryStore(dbPath string, logger *logrus.Logger) (*HistoryStore, error) {
	// Use separate file for history to avoid conflicts with whatsmeow's session store
	historyPath := dbPath + "_history"
	db, err := sql.Open("sqlite", "file:"+historyPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	store := &HistoryStore{db: db, logger: logger}
	if err := store.initTable(); err != nil {
		return nil, err
	}
	return store, nil
}

func (h *HistoryStore) initTable() error {
	_, err := h.db.Exec(`
		CREATE TABLE IF NOT EXISTS message_history (
			message_id TEXT PRIMARY KEY,
			chat_id TEXT NOT NULL,
			sender TEXT NOT NULL,
			sender_phone TEXT,
			message_type TEXT NOT NULL,
			text TEXT,
			timestamp INTEGER NOT NULL,
			is_group INTEGER NOT NULL DEFAULT 0,
			is_from_me INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
		);
		CREATE INDEX IF NOT EXISTS idx_chat_id ON message_history(chat_id);
		CREATE INDEX IF NOT EXISTS idx_timestamp ON message_history(timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_sender_phone ON message_history(sender_phone);
	`)
	return err
}

// StoreMessage persists a message to the history store
func (h *HistoryStore) StoreMessage(msg MessageRecord) error {
	_, err := h.db.Exec(`
		INSERT OR IGNORE INTO message_history
		(message_id, chat_id, sender, sender_phone, message_type, text, timestamp, is_group, is_from_me)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, msg.MessageID, msg.ChatID, msg.Sender, msg.SenderPhone, msg.MessageType,
		msg.Text, msg.Timestamp.Unix(), boolToInt(msg.IsGroup), boolToInt(msg.IsFromMe))
	return err
}

// GetChatHistory retrieves messages for a chat with optional filters
func (h *HistoryStore) GetChatHistory(chatID string, limit, offset int, senderPhone string, textOnly bool) (*ChatHistoryResult, error) {
	// Build query with filters
	query := `SELECT message_id, chat_id, sender, sender_phone, message_type, text, timestamp, is_group, is_from_me
			  FROM message_history WHERE chat_id = ?`
	args := []interface{}{chatID}

	if senderPhone != "" {
		query += " AND sender_phone = ?"
		args = append(args, senderPhone)
	}
	if textOnly {
		query += " AND message_type = 'text'"
	}

	// Count total matching
	countQuery := "SELECT COUNT(*) FROM message_history WHERE chat_id = ?"
	countArgs := []interface{}{chatID}
	if senderPhone != "" {
		countQuery += " AND sender_phone = ?"
		countArgs = append(countArgs, senderPhone)
	}
	if textOnly {
		countQuery += " AND message_type = 'text'"
	}

	var total int
	h.db.QueryRow(countQuery, countArgs...).Scan(&total)

	// Get messages ordered by timestamp descending (newest first)
	query += " ORDER BY timestamp DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := []MessageRecord{}
	for rows.Next() {
		var msg MessageRecord
		var ts int64
		var isGroup, isFromMe int
		err := rows.Scan(&msg.MessageID, &msg.ChatID, &msg.Sender, &msg.SenderPhone,
			&msg.MessageType, &msg.Text, &ts, &isGroup, &isFromMe)
		if err != nil {
			continue
		}
		msg.Timestamp = time.Unix(ts, 0)
		msg.IsGroup = isGroup == 1
		msg.IsFromMe = isFromMe == 1
		messages = append(messages, msg)
	}

	return &ChatHistoryResult{
		Messages: messages,
		Total:    total,
		HasMore:  offset+len(messages) < total,
	}, nil
}

// Close closes the database connection
func (h *HistoryStore) Close() error {
	if h.db != nil {
		return h.db.Close()
	}
	return nil
}

// ClearHistory deletes all messages from the history store
func (h *HistoryStore) ClearHistory() error {
	_, err := h.db.Exec("DELETE FROM message_history")
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
