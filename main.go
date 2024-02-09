package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/onfirebyte/bubble-chat-server/hub"
)

func serveDefault(w http.ResponseWriter, r *http.Request) {
	log.Println(r.URL)
	if r.URL.Path != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.ServeFile(w, r, "home.html")
}

func main() {
	H := hub.H
	go H.Run()
	http.HandleFunc("/", serveDefault)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		hub.ServeWs(w, r)
	})
	http.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			users := make([]string, 0, len(H.Users))
			for user := range H.Users {
				users = append(users, user)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(users)
		case http.MethodPost:
			user := r.URL.Query().Get("user")
			if user == "" {
				http.Error(w, "User is required", http.StatusBadRequest)
				return
			}
			// if _, ok := H.Users[user]; ok {
			// 	http.Error(w, "User already exists", http.StatusConflict)
			// 	return
			// }
			H.Users[user] = struct{}{}
			w.WriteHeader(http.StatusCreated)
			w.Write(nil)
		}
	})

	http.HandleFunc("/rooms", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			rooms := make([]string, 0, len(H.Rooms))
			for room := range H.Rooms {
				if room[:5] == "room:" {
					rooms = append(rooms, room[5:])
				}
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(rooms)
		}
	})
	// Listerning on port :8080...
	log.Println("Listening on port :42069")
	log.Fatal(http.ListenAndServe(":42069", nil))
}
