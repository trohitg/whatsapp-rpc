package server

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"whatsapp-rpc/src/go/whatsapp"
)

// Server handles WebSocket RPC connections
type Server struct {
	whatsapp *whatsapp.Service
	logger   *logrus.Logger
	upgrader websocket.Upgrader
}

// New creates a new WebSocket RPC server
func New(whatsappService *whatsapp.Service, logger *logrus.Logger) *Server {
	return &Server{
		whatsapp: whatsappService,
		logger:   logger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
			ReadBufferSize:  1024 * 1024,       // 1 MB read buffer
			WriteBufferSize: 100 * 1024 * 1024, // 100 MB write buffer for large media
		},
	}
}

// SetupRoutes configures WebSocket RPC routes only
func (s *Server) SetupRoutes() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	// CORS middleware
	router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(200)
			return
		}
		c.Next()
	})

	// WebSocket RPC endpoint - the only endpoint
	router.GET("/ws/rpc", s.handleWebSocketRPC)

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "type": "websocket-rpc"})
	})

	return router
}

// handleWebSocketRPC handles bidirectional JSON-RPC over WebSocket
func (s *Server) handleWebSocketRPC(c *gin.Context) {
	conn, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		s.logger.Errorf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	s.logger.Info("RPC client connected")

	var writeMu sync.Mutex
	done := make(chan struct{})
	handler := NewRPCHandler(s.whatsapp, s.logger)

	// Forward events as JSON-RPC notifications
	go handler.ForwardEvents(conn, &writeMu, done)

	// Send initial status
	status := s.whatsapp.GetStatus()
	writeMu.Lock()
	conn.WriteJSON(RPCRequest{
		JSONRPC: "2.0",
		Method:  "event.status",
		Params:  mustMarshal(status),
	})
	writeMu.Unlock()

	// Read and handle incoming requests
	for {
		var req RPCRequest
		if err := conn.ReadJSON(&req); err != nil {
			s.logger.Debugf("RPC client disconnected: %v", err)
			close(done)
			return
		}

		s.logger.Debugf("RPC request: %s", req.Method)

		// Process request and send response
		resp := handler.HandleRequest(&req)
		if req.ID != nil {
			writeMu.Lock()
			conn.WriteJSON(resp)
			writeMu.Unlock()
		}
	}
}
