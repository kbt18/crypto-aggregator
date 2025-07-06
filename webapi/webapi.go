package webapi

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"order-book-aggregator/orderbook"
	"sync"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// Connection management
var (
	connections      = make(map[string]chan *orderbook.OrderBook)
	connectionsMutex sync.RWMutex
	nextConnectionID int
)

func addWebSocketConnection(updateChan chan *orderbook.OrderBook) string {
	connectionsMutex.Lock()
	defer connectionsMutex.Unlock()

	nextConnectionID++
	connectionID := fmt.Sprintf("conn_%d", nextConnectionID)
	connections[connectionID] = updateChan

	log.Printf("Added WebSocket connection: %s", connectionID)
	return connectionID
}

func removeWebSocketConnection(connectionID string) {
	connectionsMutex.Lock()
	defer connectionsMutex.Unlock()

	if updateChan, exists := connections[connectionID]; exists {
		close(updateChan)
		delete(connections, connectionID)
		log.Printf("Removed WebSocket connection: %s", connectionID)
	}
}

// Function to broadcast order book updates to all connected clients
func BroadcastOrderBookUpdate(orderBook *orderbook.OrderBook) {
	connectionsMutex.RLock()
	defer connectionsMutex.RUnlock()

	for connectionID, updateChan := range connections {
		select {
		case updateChan <- orderBook:
			// Update sent successfully
		default:
			// Channel is full or closed, remove this connection
			log.Printf("Removing unresponsive connection: %s", connectionID)
			go removeWebSocketConnection(connectionID)
		}
	}
}

func SetupHTTPServer(aggregator *orderbook.OrderBookAggregator) {
	r := mux.NewRouter()

	r.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	}).Methods(http.MethodGet)

	// REST API endpoints
	r.HandleFunc("/api/orderbook/{symbol}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		symbol := vars["symbol"]

		orderBook := aggregator.GetOrderBook(symbol)
		if orderBook == nil {
			http.Error(w, "Order book not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(orderBook)
	}).Methods(http.MethodGet)

	// WebSocket endpoint for real-time updates
	r.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade error: %v", err)
			return
		}
		defer conn.Close()

		log.Printf("WebSocket client connected from %s", r.RemoteAddr)

		// Create a channel to handle order book updates for this connection
		updateChan := make(chan *orderbook.OrderBook, 100)

		// Subscribe this connection to order book updates
		// (You'll need to implement this in your aggregator)
		connectionID := addWebSocketConnection(updateChan)
		defer removeWebSocketConnection(connectionID)

		// Start goroutine to send updates to client
		go func() {
			for orderBookUpdate := range updateChan {
				message := map[string]interface{}{
					"type": "orderbook_update",
					"data": orderBookUpdate,
				}

				if err := conn.WriteJSON(message); err != nil {
					log.Printf("WebSocket write error: %v", err)
					return
				}
			}
		}()

		// Message handling loop
		for {
			var message map[string]interface{}
			err := conn.ReadJSON(&message)
			if err != nil {
				log.Printf("WebSocket read error: %v", err)
				break
			}

			log.Printf("Received WebSocket message: %+v", message)

			// Handle different message types
			switch message["type"] {
			case "ping":
				// Respond to ping with pong
				pong := map[string]interface{}{
					"type":      "pong",
					"timestamp": message["requestTime"],
				}
				conn.WriteJSON(pong)

			case "subscribe":
				// Handle subscription requests
				if symbol, ok := message["symbol"].(string); ok {
					log.Printf("Client subscribed to %s", symbol)
					// Add logic to track subscriptions per connection
				}

			case "unsubscribe":
				// Handle unsubscription requests
				if symbol, ok := message["symbol"].(string); ok {
					log.Printf("Client unsubscribed from %s", symbol)
				}
			}
		}

		log.Printf("WebSocket client disconnected")
	})

	// Enable CORS
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			if r.Method == "OPTIONS" {
				return
			}

			next.ServeHTTP(w, r)
		})
	})

	log.Println("HTTP server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
