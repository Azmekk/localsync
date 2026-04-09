package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"

	"localsync/localsync/internal/service"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// NewWSHandler returns an HTTP handler that upgrades connections to WebSocket
// and bridges them through the hub for sync message broadcasting.
func NewWSHandler(hub *service.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !hub.CanRegister() {
			http.Error(w, "max clients reached", http.StatusServiceUnavailable)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("websocket upgrade failed: %v", err)
			return
		}

		ip := r.RemoteAddr
		hub.Register(conn, ip)
		defer hub.Unregister(conn)

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}
			var raw map[string]interface{}
			if err := json.Unmarshal(msg, &raw); err == nil {
				source, _ := raw["source"].(string)
				event, _ := raw["event"].(string)

				// Stats events are stored but never broadcast
				if event == "stats" {
					hub.UpdateClientStats(conn, msg)
					continue
				}

				// Only forward host messages, ready, and buffering events
				if source != "host" && event != "ready" && event != "buffering" {
					continue
				}
			}
			hub.UpdateState(msg)
			hub.Broadcast(conn, msg)
		}
	}
}
