package service

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"localsync/localsync/internal/model"
)

// Hub manages WebSocket clients, broadcasts sync messages, and tracks client stats.
type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan broadcastMsg
	register   chan registerMsg
	unregister chan *websocket.Conn
	state      model.SessionState
	maxClients int
	mu         sync.Mutex

	clientInfo   map[*websocket.Conn]*model.ClientInfo
	clientInfoMu sync.RWMutex
}

type broadcastMsg struct {
	sender *websocket.Conn
	data   []byte
}

type registerMsg struct {
	conn *websocket.Conn
	ip   string
}

func NewHub(initialState model.SessionState, maxClients int) *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan broadcastMsg, 256),
		register:   make(chan registerMsg),
		unregister: make(chan *websocket.Conn),
		state:      initialState,
		maxClients: maxClients,
		clientInfo: make(map[*websocket.Conn]*model.ClientInfo),
	}
}

// CanRegister reports whether a new remote client can connect.
// The host's syncclient also connects via WS, so total connections = remote clients + 1.
// maxClients of 0 means unlimited.
func (h *Hub) CanRegister() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.maxClients == 0 {
		return true
	}
	return len(h.clients) < h.maxClients+1
}

func (h *Hub) Run() {
	for {
		select {
		case msg := <-h.register:
			h.clients[msg.conn] = true
			h.clientInfoMu.Lock()
			h.clientInfo[msg.conn] = &model.ClientInfo{IP: msg.ip}
			h.clientInfoMu.Unlock()

			h.mu.Lock()
			initMsg, _ := json.Marshal(map[string]interface{}{
				"event":    "init",
				"file":     h.state.File,
				"quality":  h.state.Quality,
				"pos":      h.state.Pos,
				"paused":   h.state.Paused,
				"variants": h.state.Variants,
			})
			h.mu.Unlock()
			if err := msg.conn.WriteMessage(websocket.TextMessage, initMsg); err != nil {
				log.Printf("error sending init: %v", err)
			}

		case conn := <-h.unregister:
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
				h.clientInfoMu.Lock()
				delete(h.clientInfo, conn)
				h.clientInfoMu.Unlock()
			}

		case msg := <-h.broadcast:
			for conn := range h.clients {
				if conn == msg.sender {
					continue
				}
				if err := conn.WriteMessage(websocket.TextMessage, msg.data); err != nil {
					log.Printf("error broadcasting: %v", err)
					delete(h.clients, conn)
					conn.Close()
					h.clientInfoMu.Lock()
					delete(h.clientInfo, conn)
					h.clientInfoMu.Unlock()
				}
			}
		}
	}
}

func (h *Hub) Register(conn *websocket.Conn, ip string) {
	h.register <- registerMsg{conn: conn, ip: ip}
}

func (h *Hub) Unregister(c *websocket.Conn) {
	h.unregister <- c
}

func (h *Hub) Broadcast(sender *websocket.Conn, msg []byte) {
	h.broadcast <- broadcastMsg{sender: sender, data: msg}
}

// UpdateState updates the session state from an incoming sync message.
func (h *Hub) UpdateState(msg []byte) {
	var raw map[string]interface{}
	if err := json.Unmarshal(msg, &raw); err != nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	event, _ := raw["event"].(string)
	switch event {
	case "seek":
		if pos, ok := raw["pos"].(float64); ok {
			h.state.Pos = pos
		}
	case "pause":
		if state, ok := raw["state"].(bool); ok {
			h.state.Paused = state
		}
		if pos, ok := raw["pos"].(float64); ok {
			h.state.Pos = pos
		}
	case "sync":
		if pos, ok := raw["pos"].(float64); ok {
			h.state.Pos = pos
		}
	}
}

// UpdateClientStats parses a stats message and updates the client info map.
func (h *Hub) UpdateClientStats(conn *websocket.Conn, raw []byte) {
	var stats model.StatsMessage
	if err := json.Unmarshal(raw, &stats); err != nil {
		return
	}

	h.clientInfoMu.Lock()
	defer h.clientInfoMu.Unlock()

	info, ok := h.clientInfo[conn]
	if !ok {
		return
	}
	info.Name = stats.Source
	info.SpeedKbps = stats.SpeedKbps
	info.BufferSecs = stats.BufferSecs
	info.Pos = stats.Pos
	info.LastUpdate = time.Now()
}

// GetClientStats returns a snapshot of all connected client stats.
func (h *Hub) GetClientStats() []model.ClientInfo {
	h.clientInfoMu.RLock()
	defer h.clientInfoMu.RUnlock()

	stats := make([]model.ClientInfo, 0, len(h.clientInfo))
	for _, info := range h.clientInfo {
		stats = append(stats, *info)
	}
	return stats
}
