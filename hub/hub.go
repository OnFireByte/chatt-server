package hub

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

var H = &Hub{
	broadcast:    make(chan message),
	register:     make(chan subscription),
	unregister:   make(chan subscription),
	Rooms:        make(map[string]map[*connection]struct{}),
	Users:        make(map[string]struct{}),
	RoomPassword: make(map[string]string),
}

// Hub maintains the set of active clients and broadcasts messages to the
type Hub struct {
	Users map[string]struct{}

	// put registered clients into the room.
	Rooms        map[string]map[*connection]struct{}
	RoomPassword map[string]string

	// Inbound messages from the clients.
	broadcast chan message

	// Register requests from the clients.
	register chan subscription

	// Unregister requests from clients.
	unregister chan subscription
}

type message struct {
	// Room and User are mutually exclusive
	Room      string    `json:"-"` // room that will receive the message
	User      string    `json:"-"` // user that will receive the message
	Sender    string    `json:"user"`
	Data      string    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

func (h *Hub) Run() {
	for {
		select {
		case s := <-h.register:
			var roomKey string
			if s.room != "" {
				roomKey = fmt.Sprintf("room:%s", s.room)
			} else if s.recvUserName != "" {
				roomKey = createUserChatKey(s.senderUserName, s.recvUserName)
			}
			connections := h.Rooms[roomKey]
			if connections == nil {
				connections = make(map[*connection]struct{})
				h.Rooms[roomKey] = connections
				h.RoomPassword[roomKey] = s.password
			} else if pw := h.RoomPassword[roomKey]; pw != "" && pw != s.password {
				s.conn.ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseUnsupportedData, "Invalid password"))
				close(s.conn.send)

				continue
			}

			h.Rooms[roomKey][s.conn] = struct{}{}
			h.Users[s.senderUserName] = struct{}{}
		case s := <-h.unregister:
			var roomKey string
			if s.room != "" {
				roomKey = fmt.Sprintf("room:%s", s.room)
			} else if s.recvUserName != "" {
				roomKey = createUserChatKey(s.senderUserName, s.recvUserName)
			}
			connections := h.Rooms[roomKey]
			if connections != nil {
				if _, ok := connections[s.conn]; ok {
					delete(connections, s.conn)
					close(s.conn.send)
					if len(connections) == 0 {
						delete(h.Rooms, s.room)
						delete(h.RoomPassword, s.room)
					}
				}
			}

			delete(h.Users, s.senderUserName)
		case m := <-h.broadcast:
			var connections map[*connection]struct{}
			if m.Room != "" {
				connections = h.Rooms[fmt.Sprintf("room:%s", m.Room)]
			} else if m.User != "" {
				connections = h.Rooms[createUserChatKey(m.Sender, m.User)]
			}
			jsonStr, err := json.Marshal(m)
			if err != nil {
				log.Println("error:", err)
				continue
			}
			for c := range connections {
				select {
				case c.send <- jsonStr:
				default:
					close(c.send)
					delete(connections, c)
					if len(connections) == 0 {
						delete(h.Rooms, m.Room)
					}
				}
			}
		}
	}
}
