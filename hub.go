package main

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

type SessionState struct {
	File    string  `json:"file"`
	Quality string  `json:"quality"`
	Pos     float64 `json:"pos"`
	Paused  bool    `json:"paused"`
}

type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan broadcastMsg
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	state      SessionState
	mu         sync.Mutex
}

type broadcastMsg struct {
	sender *websocket.Conn
	data   []byte
}

func NewHub(initialState SessionState) *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan broadcastMsg, 256),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		state:      initialState,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case conn := <-h.register:
			h.clients[conn] = true
			h.mu.Lock()
			initMsg, _ := json.Marshal(map[string]interface{}{
				"event":   "init",
				"file":    h.state.File,
				"quality": h.state.Quality,
				"pos":     h.state.Pos,
				"paused":  h.state.Paused,
			})
			h.mu.Unlock()
			if err := conn.WriteMessage(websocket.TextMessage, initMsg); err != nil {
				log.Printf("error sending init: %v", err)
			}

		case conn := <-h.unregister:
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
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
				}
			}
		}
	}
}

func (h *Hub) Register(c *websocket.Conn) {
	h.register <- c
}

func (h *Hub) Unregister(c *websocket.Conn) {
	h.unregister <- c
}

func (h *Hub) Broadcast(sender *websocket.Conn, msg []byte) {
	h.broadcast <- broadcastMsg{sender: sender, data: msg}
}

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
	}
}
