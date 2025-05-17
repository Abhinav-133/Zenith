
package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

type Message struct {
	SenderIP string  `json:"sender_ip"`
	Sender   string  `json:"sender,omitempty"`
	Content  string  `json:"content,omitempty"`
	Lat      float64 `json:"lat,omitempty"`
	Lng      float64 `json:"lng,omitempty"`
	Avatar   string  `json:"avatar,omitempty"`
	IsRescue     bool  `json:"is_rescue"` // New field for the unique flag
}

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	clients         = make(map[*websocket.Conn]string)
	broadcast       = make(chan Message)
	clientLocations = make(map[string]Message)
	messageHistory  = []Message{}

	mutex sync.Mutex

	messagesFile  = "messages.json"
	locationsFile = "locations.json"
	avatars       = []string{"/avatars/avatar1.png", "/avatars/avatar2.png", "/avatars/avatar3.png"}
)

func main() {
	loadData()

	http.Handle("/leaflet/", http.StripPrefix("/leaflet/", http.FileServer(http.Dir("./leaflet"))))
	http.Handle("/avatars/", http.StripPrefix("/avatars/", http.FileServer(http.Dir("./avatars"))))
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/users", handleUsers)
	http.HandleFunc("/ws", handleConnections)

	go handleMessages()

	addr := ":8443"
	fmt.Printf("✅ Server running at https://localhost%s\n", addr)
	log.Fatal(http.ListenAndServeTLS(addr, "cert.pem", "key.pem", nil))
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func handleUsers(w http.ResponseWriter, r *http.Request) {
	mutex.Lock()
	defer mutex.Unlock()

	var users []Message
	for _, msg := range clientLocations {
		users = append(users, msg)
	}
	w.Header().Set("content-type", "application/json")
	json.NewEncoder(w).Encode(users)
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	ip := getIPAddress(r)
	avatar := avatars[len(clients)%len(avatars)]

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade error: %v\n", err)
		return
	}
	defer ws.Close()
	ws.SetReadLimit(20 * 1024)

	clientID := fmt.Sprintf("%p", ws)
	log.Printf("New client connected: %s [%s]\n", ip, clientID)

	mutex.Lock()
	clients[ws] = ip
	mutex.Unlock()

	// Send previous messages and locations to the new client
	mutex.Lock()
	for _, msg := range messageHistory {
		if msg.Avatar == "" {
			msg.Avatar = avatar
		}
		ws.WriteJSON(msg)
	}
	for _, loc := range clientLocations {
		ws.WriteJSON(loc)
	}
	mutex.Unlock()

	for {
		var msg Message
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Printf("Client %s disconnected: %v\n", ip, err)
			mutex.Lock()
			delete(clients, ws)
			delete(clientLocations, clientID)
			saveLocations()
			mutex.Unlock()
			break
		}

		msg.SenderIP = ip
		msg.Avatar = avatar

		// ✅ Identify rescue personnel
		if msg.Sender == "1234554321" {
			msg.IsRescue = true
		}

		log.Printf("📨 Message from %s: %+v\n", ip, msg)

		mutex.Lock()
		if msg.Content != "" {
			messageHistory = append(messageHistory, msg)
			saveMessages()
		}
		if msg.Lat != 0 || msg.Lng != 0 {
			clientLocations[clientID] = msg
			saveLocations()
		}
		mutex.Unlock()

		broadcast <- msg
	}
}


// func handleConnections(w http.ResponseWriter, r *http.Request) {
// 	ip := getIPAddress(r)
// 	avatar := avatars[len(clients)%len(avatars)]
//
// 	ws, err := upgrader.Upgrade(w, r, nil)
// 	if err != nil {
// 		log.Printf("Upgrade error: %v\n", err)
// 		return
// 	}
// 	defer ws.Close()
// 	ws.SetReadLimit(20 * 1024)
//
// 	clientID := fmt.Sprintf("%p", ws)
// 	log.Printf("New client connected: %s [%s]\n", ip, clientID)
//
// 	mutex.Lock()
// 	clients[ws] = ip
// 	mutex.Unlock()
//
// 	mutex.Lock()
// 	for _, msg := range messageHistory {
// 		if msg.Avatar == "" {
// 			msg.Avatar = avatar
// 		}
// 		ws.WriteJSON(msg)
// 	}
// 	for _, loc := range clientLocations {
// 		ws.WriteJSON(loc)
// 	}
// 	mutex.Unlock()
//
// 	for {
// 		var msg Message
// 		err := ws.ReadJSON(&msg)
// 		if err != nil {
// 			log.Printf("Client %s disconnected: %v\n", ip, err)
// 			mutex.Lock()
// 			delete(clients, ws)
// 			delete(clientLocations, clientID)
// 			saveLocations()
// 			mutex.Unlock()
// 			break
// 		}
//
// 		msg.SenderIP = ip
// 		msg.Avatar = avatar
//
// 		// Check if the sender's name is a numeric string
// 		if isNumericString(msg.Sender) {
// 			msg.Flag = generateUniqueFlag(msg.Sender)
// 		}
//
// 		log.Printf("📨 Message from %s: %+v\n", ip, msg)
//
// 		mutex.Lock()
// 		if msg.Content != "" {
// 			messageHistory = append(messageHistory, msg)
// 			saveMessages()
// 		}
// 		if msg.Lat != 0 || msg.Lng != 0 {
// 			clientLocations[clientID] = msg
// 			saveLocations()
// 		}
// 		mutex.Unlock()
//
// 		broadcast <- msg
// 	}
// }

func handleMessages() {
	for {
		msg := <-broadcast
		mutex.Lock()
		for client := range clients {
			client.WriteJSON(msg)
		}
		mutex.Unlock()
	}
}

func saveMessages() {
	data, err := json.MarshalIndent(messageHistory, "", "  ")
	if err == nil {
		_ = ioutil.WriteFile(messagesFile, data, 0644)
	}
}

func saveLocations() {
	data, err := json.MarshalIndent(clientLocations, "", "  ")
	if err == nil {
		_ = ioutil.WriteFile(locationsFile, data, 0644)
	}
}

func loadData() {
	if data, err := ioutil.ReadFile(messagesFile); err == nil {
		_ = json.Unmarshal(data, &messageHistory)
	}
	if data, err := ioutil.ReadFile(locationsFile); err == nil {
		_ = json.Unmarshal(data, &clientLocations)
	}
}

func getIPAddress(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	ip := strings.Split(r.RemoteAddr, ":")[0]
	return ip
}

// Helper function to check if the string is numeric
func isNumericString(s string) bool {
	for _, char := range s {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// Helper function to generate a unique flag for numeric names
func generateUniqueFlag(name string) string {
	// Generate a simple unique identifier based on the name (can be extended as needed)
	return fmt.Sprintf("flag-%s", name)
}

