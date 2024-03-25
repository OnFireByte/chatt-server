package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/onfirebyte/bubble-chat-server/dto"
	"github.com/onfirebyte/bubble-chat-server/hub"
	"go.etcd.io/bbolt"
	"golang.org/x/crypto/bcrypt"
)

type TokenInfo struct {
	Name   string
	Expire string
}

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

var DB *bbolt.DB

var InvalidPassword = errors.New("invalid password")

var tokenSecret []byte

func main() {
	secret := os.Args[1]
	tokenSecret = []byte(secret)
	db, err := bbolt.Open("my.db", 0o600, nil)
	if err != nil {
		log.Fatal(err)
	}
	DB = db
	defer db.Close()

	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("users"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	H := hub.H
	go H.Run()
	http.HandleFunc("/", serveDefault)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		log.Println("New connection")
		authorization := r.Header.Get("Authorization")
		if len(authorization) < len("Bearer ") {
			log.Println("Token required", authorization)
			http.Error(w, "Token required", http.StatusUnauthorized)
			return
		}
		token := authorization[len("Bearer "):]
		splited := strings.Split(token, ".")
		if len(splited) != 2 {
			log.Panicln("Invalid token", token)
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		b64Info := splited[0]
		b64Hash := splited[1]

		var infoBytes []byte
		infoBytes, err := base64.URLEncoding.DecodeString(b64Info)
		if err != nil {
			log.Println("Invalid token", err)
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		var hash []byte
		hash, err = base64.URLEncoding.DecodeString(b64Hash)
		if err != nil {
			log.Println("Invalid token", err)
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		hasher := hmac.New(sha256.New, tokenSecret)

		_, err = hasher.Write(infoBytes)
		if err != nil {
			log.Println("Invalid token", err)
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		if !hmac.Equal(hash, hasher.Sum(nil)) {
			log.Println("Invalid token")
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		var info TokenInfo
		err = json.Unmarshal(infoBytes, &info)
		if err != nil {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		expire, err := time.Parse(time.RFC3339, info.Expire)
		if err != nil {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		if time.Now().After(expire) {
			http.Error(w, "Token expired", http.StatusUnauthorized)
			return
		}

		log.Println("User connected", info)

		hub.ServeWs(w, r, info.Name)
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
			password := r.URL.Query().Get("password")
			if user == "" {
				http.Error(w, "User is required", http.StatusBadRequest)
				return
			}

			err := DB.Update(func(tx *bbolt.Tx) error {
				b := tx.Bucket([]byte("users"))
				if b == nil {
					return errors.New("users bucket not found")
				}

				val := b.Get([]byte(user))
				if val == nil {
					hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
					if err != nil {
						return err
					}
					err = b.Put([]byte(user), hashed)
					if err != nil {
						return err
					}

					return nil
				} else {
					err = bcrypt.CompareHashAndPassword(val, []byte(password))
					if err != nil {
						return InvalidPassword
					}
				}

				return nil
			})
			if err != nil {
				if errors.Is(err, InvalidPassword) {
					http.Error(w, "Invalid password", http.StatusUnauthorized)
					return
				}

				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// create auth token
			hasher := hmac.New(sha256.New, []byte(secret))
			expires := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
			info := TokenInfo{
				Name:   user,
				Expire: expires,
			}
			var infoBytes []byte
			infoBytes, err = json.Marshal(info)
			if err != nil {
				log.Println(err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			b64Info := base64.URLEncoding.EncodeToString([]byte(infoBytes))
			_, err = hasher.Write([]byte(infoBytes))
			if err != nil {
				log.Println(err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			hashedToken := hasher.Sum(nil)
			b64Token := base64.URLEncoding.EncodeToString(hashedToken)

			token := b64Info + "." + b64Token

			H.Users[user] = struct{}{}
			w.WriteHeader(http.StatusOK)

			w.Write([]byte(token))
		}
	})

	http.HandleFunc("/rooms", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			rooms := make([]dto.Room, 0, len(H.Rooms))
			for room := range H.Rooms {
				if room[:5] == "room:" {
					rooms = append(rooms, dto.Room{
						Name: room[5:],
						Lock: H.RoomPassword[room] != "",
					})
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
