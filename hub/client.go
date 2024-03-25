package hub

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (

	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type subscription struct {
	conn           *connection
	room           string
	password       string // empty string means no password
	recvUserName   string
	senderUserName string
}

type connection struct {
	// The websocket connection.
	ws *websocket.Conn
	// Buffered channel of outbound messages.
	send chan []byte
}

type msgReq struct {
	Sender  string `json:"user"`
	Message string `json:"message"`
}

func (s *subscription) readPump() {
	c := s.conn
	defer func() {
		// Unregister
		fmt.Printf("Unregistering user: %v\n", s.senderUserName)
		H.unregister <- *s
		c.ws.Close()
	}()
	c.ws.SetReadLimit(maxMessageSize)
	c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error { c.ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		// Reading incoming message...
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			if !websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				log.Printf("error: %v", err)
			}
			break
		}

		m := message{
			Room:   s.room,
			User:   s.recvUserName,
			Sender: s.senderUserName,
			Data:   string(msg),
		}
		H.broadcast <- m
	}
}

func (s *subscription) writePump() {
	c := s.conn
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.ws.Close()
	}()
	for {
		select {
		// Listerning message when it comes will write it into writer and then send it to the client
		case message, ok := <-c.send:
			if !ok {
				c.write(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.write(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.write(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

func (c *connection) write(mt int, payload []byte) error {
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(mt, payload)
}

func ServeWs(w http.ResponseWriter, r *http.Request, senderUserName string) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	// Get room's id from client...
	var password string
	queryValues := r.URL.Query()
	roomQuery := queryValues.Get("roomName")
	// decode escape characters
	roomQuery, err = url.QueryUnescape(roomQuery)
	if err != nil {
		log.Println(err)

		ws.Close()
		return
	}

	roomSplit := strings.Split(roomQuery, ":")
	roomName := roomSplit[0]
	if len(roomSplit) > 1 {
		password = roomSplit[1]
	}
	recvUserName := queryValues.Get("recvUserName")

	c := &connection{send: make(chan []byte, 256), ws: ws}
	log.Println("User connected", senderUserName, roomName, password)
	s := subscription{
		conn:           c,
		room:           roomName,
		password:       password,
		recvUserName:   recvUserName,
		senderUserName: senderUserName,
	}
	H.register <- s
	go s.writePump()
	go s.readPump()
}
