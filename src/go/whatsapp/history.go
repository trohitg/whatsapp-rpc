package whatsapp

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/sirupsen/logrus"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
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
	db, err := sql.Open("sqlite3", "file:"+historyPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &HistoryStore{db: db, logger: logger}
	if err := store.initTable(); err != nil {
		return nil, err
	}
	return store, nil
}

func (h *HistoryStore) initTable() error {
	// Message history table
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
	if err != nil {
		return err
	}

	// Groups cache table
	_, err = h.db.Exec(`
		CREATE TABLE IF NOT EXISTS groups (
			jid TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			topic TEXT,
			owner TEXT,
			created_at INTEGER,
			size INTEGER,
			is_announce INTEGER DEFAULT 0,
			is_locked INTEGER DEFAULT 0,
			is_community INTEGER DEFAULT 0,
			updated_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_groups_name ON groups(name);
	`)
	if err != nil {
		return err
	}

	// Group participants cache table
	_, err = h.db.Exec(`
		CREATE TABLE IF NOT EXISTS group_participants (
			group_jid TEXT NOT NULL,
			participant_jid TEXT NOT NULL,
			phone TEXT,
			name TEXT,
			is_admin INTEGER DEFAULT 0,
			is_super_admin INTEGER DEFAULT 0,
			PRIMARY KEY (group_jid, participant_jid)
		);
		CREATE INDEX IF NOT EXISTS idx_participants_group ON group_participants(group_jid);
		CREATE INDEX IF NOT EXISTS idx_participants_phone ON group_participants(phone);
	`)
	if err != nil {
		return err
	}

	// Contact check cache (WhatsApp registration status)
	_, err = h.db.Exec(`
		CREATE TABLE IF NOT EXISTS contact_check_cache (
			phone TEXT PRIMARY KEY,
			jid TEXT,
			is_registered INTEGER NOT NULL,
			is_business INTEGER DEFAULT 0,
			business_name TEXT,
			checked_at INTEGER NOT NULL
		);
	`)
	if err != nil {
		return err
	}

	// Profile picture cache
	_, err = h.db.Exec(`
		CREATE TABLE IF NOT EXISTS profile_pic_cache (
			jid TEXT PRIMARY KEY,
			url TEXT,
			pic_id TEXT,
			pic_exists INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);
	`)
	if err != nil {
		return err
	}

	// Group invite link cache
	_, err = h.db.Exec(`
		CREATE TABLE IF NOT EXISTS group_invite_cache (
			group_jid TEXT PRIMARY KEY,
			invite_link TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		);
	`)
	if err != nil {
		return err
	}

	// Newsletter (channel) cache
	_, err = h.db.Exec(`
		CREATE TABLE IF NOT EXISTS newsletter_cache (
			jid TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			subscriber_count INTEGER DEFAULT 0,
			verification_state TEXT,
			state TEXT,
			invite_code TEXT,
			picture_url TEXT,
			picture_id TEXT,
			preview_url TEXT,
			role TEXT,
			mute TEXT,
			created_at INTEGER,
			updated_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_newsletter_name ON newsletter_cache(name);
	`)
	if err != nil {
		return err
	}

	// Newsletter message cache
	_, err = h.db.Exec(`
		CREATE TABLE IF NOT EXISTS newsletter_message_cache (
			message_id TEXT PRIMARY KEY,
			newsletter_jid TEXT NOT NULL,
			message_server_id INTEGER NOT NULL,
			message_type TEXT,
			timestamp INTEGER NOT NULL,
			views_count INTEGER DEFAULT 0,
			reaction_counts TEXT,
			text TEXT,
			media_type TEXT,
			mime_type TEXT,
			file_length INTEGER DEFAULT 0,
			filename TEXT,
			fetched_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
		);
		CREATE INDEX IF NOT EXISTS idx_nl_msg_jid ON newsletter_message_cache(newsletter_jid);
		CREATE INDEX IF NOT EXISTS idx_nl_msg_ts ON newsletter_message_cache(timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_nl_msg_server_id ON newsletter_message_cache(message_server_id DESC);
		CREATE INDEX IF NOT EXISTS idx_nl_msg_media ON newsletter_message_cache(media_type);
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

// ClearAllData deletes all data from all cache tables (for full reset)
func (h *HistoryStore) ClearAllData() error {
	tables := []string{
		"message_history",
		"groups",
		"group_participants",
		"contact_check_cache",
		"profile_pic_cache",
		"group_invite_cache",
		"newsletter_cache",
		"newsletter_message_cache",
	}
	for _, table := range tables {
		if _, err := h.db.Exec("DELETE FROM " + table); err != nil {
			h.logger.Warnf("Failed to clear table %s: %v", table, err)
		}
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ============================================================================
// Groups Cache Methods
// ============================================================================

// StoreGroups stores multiple groups in a transaction (replaces all existing)
func (h *HistoryStore) StoreGroups(groups []GroupInfo) error {
	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Clear existing groups and participants
	if _, err := tx.Exec("DELETE FROM group_participants"); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM groups"); err != nil {
		return err
	}

	now := time.Now().Unix()

	// Insert groups
	groupStmt, err := tx.Prepare(`
		INSERT INTO groups (jid, name, topic, owner, created_at, size, is_announce, is_locked, is_community, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer groupStmt.Close()

	// Insert participants
	partStmt, err := tx.Prepare(`
		INSERT INTO group_participants (group_jid, participant_jid, phone, name, is_admin, is_super_admin)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer partStmt.Close()

	for _, g := range groups {
		_, err := groupStmt.Exec(g.JID, g.Name, g.Topic, g.Owner, g.CreatedAt.Unix(), g.Size,
			boolToInt(g.IsAnnounce), boolToInt(g.IsLocked), boolToInt(g.IsCommunity), now)
		if err != nil {
			h.logger.Warnf("Failed to store group %s: %v", g.JID, err)
			continue
		}

		for _, p := range g.Participants {
			_, err := partStmt.Exec(g.JID, p.JID, p.Phone, p.Name, boolToInt(p.IsAdmin), boolToInt(p.IsSuperAdmin))
			if err != nil {
				h.logger.Warnf("Failed to store participant %s in group %s: %v", p.JID, g.JID, err)
			}
		}
	}

	return tx.Commit()
}

// GetCachedGroups retrieves all cached groups with their participants
func (h *HistoryStore) GetCachedGroups() ([]GroupInfo, error) {
	rows, err := h.db.Query(`
		SELECT jid, name, topic, owner, created_at, size, is_announce, is_locked, is_community
		FROM groups ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := []GroupInfo{}
	for rows.Next() {
		var g GroupInfo
		var createdAt int64
		var isAnnounce, isLocked, isCommunity int
		err := rows.Scan(&g.JID, &g.Name, &g.Topic, &g.Owner, &createdAt, &g.Size,
			&isAnnounce, &isLocked, &isCommunity)
		if err != nil {
			continue
		}
		g.CreatedAt = time.Unix(createdAt, 0)
		g.IsAnnounce = isAnnounce == 1
		g.IsLocked = isLocked == 1
		g.IsCommunity = isCommunity == 1

		// Get participants for this group
		g.Participants, _ = h.getGroupParticipants(g.JID)
		groups = append(groups, g)
	}

	return groups, nil
}

// GetCachedGroupByJID retrieves a single cached group by JID
func (h *HistoryStore) GetCachedGroupByJID(jid string) (*GroupInfo, error) {
	var g GroupInfo
	var createdAt int64
	var isAnnounce, isLocked, isCommunity int

	err := h.db.QueryRow(`
		SELECT jid, name, topic, owner, created_at, size, is_announce, is_locked, is_community
		FROM groups WHERE jid = ?
	`, jid).Scan(&g.JID, &g.Name, &g.Topic, &g.Owner, &createdAt, &g.Size,
		&isAnnounce, &isLocked, &isCommunity)
	if err != nil {
		return nil, err
	}

	g.CreatedAt = time.Unix(createdAt, 0)
	g.IsAnnounce = isAnnounce == 1
	g.IsLocked = isLocked == 1
	g.IsCommunity = isCommunity == 1
	g.Participants, _ = h.getGroupParticipants(g.JID)

	return &g, nil
}

// getGroupParticipants retrieves participants for a group
func (h *HistoryStore) getGroupParticipants(groupJID string) ([]GroupParticipant, error) {
	rows, err := h.db.Query(`
		SELECT participant_jid, phone, name, is_admin, is_super_admin
		FROM group_participants WHERE group_jid = ?
	`, groupJID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	participants := []GroupParticipant{}
	for rows.Next() {
		var p GroupParticipant
		var isAdmin, isSuperAdmin int
		err := rows.Scan(&p.JID, &p.Phone, &p.Name, &isAdmin, &isSuperAdmin)
		if err != nil {
			continue
		}
		p.IsAdmin = isAdmin == 1
		p.IsSuperAdmin = isSuperAdmin == 1
		participants = append(participants, p)
	}

	return participants, nil
}

// ClearGroups removes all cached groups
func (h *HistoryStore) ClearGroups() error {
	if _, err := h.db.Exec("DELETE FROM group_participants"); err != nil {
		return err
	}
	_, err := h.db.Exec("DELETE FROM groups")
	return err
}

// HasCachedGroups returns true if there are any cached groups
func (h *HistoryStore) HasCachedGroups() bool {
	var count int
	h.db.QueryRow("SELECT COUNT(*) FROM groups").Scan(&count)
	return count > 0
}

// ============================================================================
// Contact Check Cache Methods
// ============================================================================

// StoreContactCheck stores a contact registration check result
func (h *HistoryStore) StoreContactCheck(result ContactCheckResult) error {
	_, err := h.db.Exec(`
		INSERT OR REPLACE INTO contact_check_cache (phone, jid, is_registered, is_business, business_name, checked_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, result.Query, result.JID, boolToInt(result.IsRegistered), boolToInt(result.IsBusiness),
		result.BusinessName, time.Now().Unix())
	return err
}

// StoreContactChecks stores multiple contact check results
func (h *HistoryStore) StoreContactChecks(results []ContactCheckResult) error {
	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO contact_check_cache (phone, jid, is_registered, is_business, business_name, checked_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, r := range results {
		_, err := stmt.Exec(r.Query, r.JID, boolToInt(r.IsRegistered), boolToInt(r.IsBusiness), r.BusinessName, now)
		if err != nil {
			h.logger.Warnf("Failed to store contact check for %s: %v", r.Query, err)
		}
	}

	return tx.Commit()
}

// GetCachedContactCheck retrieves a cached contact check if not expired
func (h *HistoryStore) GetCachedContactCheck(phone string, maxAgeHours int) (*ContactCheckResult, error) {
	var result ContactCheckResult
	var isRegistered, isBusiness int
	var checkedAt int64

	err := h.db.QueryRow(`
		SELECT phone, jid, is_registered, is_business, business_name, checked_at
		FROM contact_check_cache WHERE phone = ?
	`, phone).Scan(&result.Query, &result.JID, &isRegistered, &isBusiness, &result.BusinessName, &checkedAt)
	if err != nil {
		return nil, err
	}

	// Check TTL
	if maxAgeHours > 0 {
		age := time.Since(time.Unix(checkedAt, 0))
		if age > time.Duration(maxAgeHours)*time.Hour {
			return nil, sql.ErrNoRows // Expired
		}
	}

	result.IsRegistered = isRegistered == 1
	result.IsBusiness = isBusiness == 1
	return &result, nil
}

// GetCachedContactChecks retrieves multiple cached contact checks
func (h *HistoryStore) GetCachedContactChecks(phones []string, maxAgeHours int) (map[string]*ContactCheckResult, error) {
	results := make(map[string]*ContactCheckResult)
	for _, phone := range phones {
		if result, err := h.GetCachedContactCheck(phone, maxAgeHours); err == nil {
			results[phone] = result
		}
	}
	return results, nil
}

// ============================================================================
// Profile Picture Cache Methods
// ============================================================================

// StoreProfilePic stores a profile picture result
func (h *HistoryStore) StoreProfilePic(jid string, result ProfilePicResult) error {
	_, err := h.db.Exec(`
		INSERT OR REPLACE INTO profile_pic_cache (jid, url, pic_id, pic_exists, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, jid, result.URL, result.ID, boolToInt(result.Exists), time.Now().Unix())
	return err
}

// GetCachedProfilePic retrieves a cached profile picture if not expired
func (h *HistoryStore) GetCachedProfilePic(jid string, maxAgeHours int) (*ProfilePicResult, error) {
	var result ProfilePicResult
	var picExists int
	var updatedAt int64

	err := h.db.QueryRow(`
		SELECT url, pic_id, pic_exists, updated_at
		FROM profile_pic_cache WHERE jid = ?
	`, jid).Scan(&result.URL, &result.ID, &picExists, &updatedAt)
	if err != nil {
		return nil, err
	}

	// Check TTL
	if maxAgeHours > 0 {
		age := time.Since(time.Unix(updatedAt, 0))
		if age > time.Duration(maxAgeHours)*time.Hour {
			return nil, sql.ErrNoRows // Expired
		}
	}

	result.Exists = picExists == 1
	return &result, nil
}

// ============================================================================
// Group Invite Link Cache Methods
// ============================================================================

// StoreGroupInviteLink stores a group invite link
func (h *HistoryStore) StoreGroupInviteLink(groupJID, link string) error {
	_, err := h.db.Exec(`
		INSERT OR REPLACE INTO group_invite_cache (group_jid, invite_link, updated_at)
		VALUES (?, ?, ?)
	`, groupJID, link, time.Now().Unix())
	return err
}

// GetCachedGroupInviteLink retrieves a cached invite link if not expired
func (h *HistoryStore) GetCachedGroupInviteLink(groupJID string, maxAgeHours int) (string, error) {
	var link string
	var updatedAt int64

	err := h.db.QueryRow(`
		SELECT invite_link, updated_at FROM group_invite_cache WHERE group_jid = ?
	`, groupJID).Scan(&link, &updatedAt)
	if err != nil {
		return "", err
	}

	// Check TTL
	if maxAgeHours > 0 {
		age := time.Since(time.Unix(updatedAt, 0))
		if age > time.Duration(maxAgeHours)*time.Hour {
			return "", sql.ErrNoRows // Expired
		}
	}

	return link, nil
}

// DeleteGroupInviteLink removes a cached invite link (call after revoke)
func (h *HistoryStore) DeleteGroupInviteLink(groupJID string) error {
	_, err := h.db.Exec("DELETE FROM group_invite_cache WHERE group_jid = ?", groupJID)
	return err
}

// ============================================================================
// Newsletter (Channel) Cache Methods
// ============================================================================

// StoreNewsletters stores multiple newsletters in a transaction (replaces all existing)
func (h *HistoryStore) StoreNewsletters(newsletters []NewsletterInfo) error {
	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM newsletter_cache"); err != nil {
		return err
	}

	now := time.Now().Unix()
	stmt, err := tx.Prepare(`
		INSERT INTO newsletter_cache (jid, name, description, subscriber_count, verification_state, state,
			invite_code, picture_url, picture_id, preview_url, role, mute, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, n := range newsletters {
		_, err := stmt.Exec(n.JID, n.Name, n.Description, n.SubscriberCount, n.VerificationState, n.State,
			n.InviteCode, n.PictureURL, n.PictureID, n.PreviewURL, n.Role, n.Mute, n.CreatedAt, now)
		if err != nil {
			h.logger.Warnf("Failed to store newsletter %s: %v", n.JID, err)
		}
	}

	return tx.Commit()
}

// GetCachedNewsletters retrieves all cached newsletters if not expired
func (h *HistoryStore) GetCachedNewsletters() ([]NewsletterInfo, error) {
	// Check if cache is still valid (24h TTL)
	var updatedAt int64
	err := h.db.QueryRow("SELECT updated_at FROM newsletter_cache LIMIT 1").Scan(&updatedAt)
	if err != nil {
		return nil, err
	}
	if time.Since(time.Unix(updatedAt, 0)) > 24*time.Hour {
		return nil, sql.ErrNoRows
	}

	rows, err := h.db.Query(`
		SELECT jid, name, description, subscriber_count, verification_state, state,
			invite_code, picture_url, picture_id, preview_url, role, mute, created_at
		FROM newsletter_cache ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	newsletters := []NewsletterInfo{}
	for rows.Next() {
		var n NewsletterInfo
		err := rows.Scan(&n.JID, &n.Name, &n.Description, &n.SubscriberCount, &n.VerificationState, &n.State,
			&n.InviteCode, &n.PictureURL, &n.PictureID, &n.PreviewURL, &n.Role, &n.Mute, &n.CreatedAt)
		if err != nil {
			continue
		}
		if n.InviteCode != "" {
			n.InviteLink = "https://whatsapp.com/channel/" + n.InviteCode
		}
		newsletters = append(newsletters, n)
	}

	return newsletters, nil
}

// GetCachedNewsletter retrieves a single cached newsletter by JID if not expired
func (h *HistoryStore) GetCachedNewsletter(jid string) (*NewsletterInfo, error) {
	var n NewsletterInfo
	var updatedAt int64

	err := h.db.QueryRow(`
		SELECT jid, name, description, subscriber_count, verification_state, state,
			invite_code, picture_url, picture_id, preview_url, role, mute, created_at, updated_at
		FROM newsletter_cache WHERE jid = ?
	`, jid).Scan(&n.JID, &n.Name, &n.Description, &n.SubscriberCount, &n.VerificationState, &n.State,
		&n.InviteCode, &n.PictureURL, &n.PictureID, &n.PreviewURL, &n.Role, &n.Mute, &n.CreatedAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	// Check TTL (24 hours)
	if time.Since(time.Unix(updatedAt, 0)) > 24*time.Hour {
		return nil, sql.ErrNoRows
	}

	if n.InviteCode != "" {
		n.InviteLink = "https://whatsapp.com/channel/" + n.InviteCode
	}
	return &n, nil
}

// StoreNewsletter stores or updates a single newsletter
func (h *HistoryStore) StoreNewsletter(newsletter NewsletterInfo) error {
	_, err := h.db.Exec(`
		INSERT OR REPLACE INTO newsletter_cache (jid, name, description, subscriber_count, verification_state, state,
			invite_code, picture_url, picture_id, preview_url, role, mute, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, newsletter.JID, newsletter.Name, newsletter.Description, newsletter.SubscriberCount,
		newsletter.VerificationState, newsletter.State, newsletter.InviteCode, newsletter.PictureURL,
		newsletter.PictureID, newsletter.PreviewURL, newsletter.Role, newsletter.Mute,
		newsletter.CreatedAt, time.Now().Unix())
	return err
}

// DeleteNewsletter removes a cached newsletter (for unfollow)
func (h *HistoryStore) DeleteNewsletter(jid string) error {
	_, err := h.db.Exec("DELETE FROM newsletter_cache WHERE jid = ?", jid)
	return err
}

// HasCachedNewsletters returns true if there are any cached newsletters
func (h *HistoryStore) HasCachedNewsletters() bool {
	var count int
	h.db.QueryRow("SELECT COUNT(*) FROM newsletter_cache").Scan(&count)
	return count > 0
}

// ============================================================================
// Newsletter Message Cache Methods
// ============================================================================

func (h *HistoryStore) StoreNewsletterMessages(newsletterJID string, messages []NewsletterMessageInfo) error {
	if len(messages) == 0 {
		return nil
	}
	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO newsletter_message_cache
		(message_id, newsletter_jid, message_server_id, message_type, timestamp,
		 views_count, reaction_counts, text, media_type, mime_type, file_length, filename, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, m := range messages {
		reactions := ""
		if len(m.ReactionCounts) > 0 {
			b, _ := json.Marshal(m.ReactionCounts)
			reactions = string(b)
		}
		stmt.Exec(m.MessageID, newsletterJID, m.MessageServerID, m.Type, m.Timestamp,
			m.ViewsCount, reactions, m.Text, m.MediaType, m.MimeType, m.FileLength, m.Filename, now)
	}
	return tx.Commit()
}

func (h *HistoryStore) GetCachedNewsletterMessages(query *NewsletterMessageQuery, limit, offset int) ([]NewsletterMessageInfo, int, error) {
	where := "newsletter_jid = ?"
	args := []interface{}{query.JID}

	if query.Before > 0 {
		where += " AND message_server_id < ?"
		args = append(args, query.Before)
	}
	if query.Since > 0 {
		where += " AND timestamp >= ?"
		args = append(args, query.Since)
	}
	if query.Until > 0 {
		where += " AND timestamp <= ?"
		args = append(args, query.Until)
	}
	if query.MediaType != "" {
		where += " AND media_type = ?"
		args = append(args, query.MediaType)
	}
	if query.Search != "" {
		where += " AND text LIKE ?"
		args = append(args, "%"+query.Search+"%")
	}

	var total int
	h.db.QueryRow("SELECT COUNT(*) FROM newsletter_message_cache WHERE "+where, args...).Scan(&total)

	queryArgs := append(args, limit, offset)
	rows, err := h.db.Query(`
		SELECT message_id, message_server_id, message_type, timestamp,
			views_count, reaction_counts, text, media_type, mime_type, file_length, filename
		FROM newsletter_message_cache WHERE `+where+`
		ORDER BY timestamp DESC LIMIT ? OFFSET ?
	`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var messages []NewsletterMessageInfo
	for rows.Next() {
		var m NewsletterMessageInfo
		var reactions string
		err := rows.Scan(&m.MessageID, &m.MessageServerID, &m.Type, &m.Timestamp,
			&m.ViewsCount, &reactions, &m.Text, &m.MediaType, &m.MimeType, &m.FileLength, &m.Filename)
		if err != nil {
			continue
		}
		if reactions != "" {
			json.Unmarshal([]byte(reactions), &m.ReactionCounts)
		}
		messages = append(messages, m)
	}
	return messages, total, nil
}

func (h *HistoryStore) HasCachedNewsletterMessages(newsletterJID string) bool {
	var count int
	h.db.QueryRow("SELECT COUNT(*) FROM newsletter_message_cache WHERE newsletter_jid = ?", newsletterJID).Scan(&count)
	return count > 0
}

func (h *HistoryStore) DeleteNewsletterMessages(newsletterJID string) error {
	_, err := h.db.Exec("DELETE FROM newsletter_message_cache WHERE newsletter_jid = ?", newsletterJID)
	return err
}
