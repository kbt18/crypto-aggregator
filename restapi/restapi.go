package restapi

import (
	"encoding/json"
	"log"
	"net/http"
	"order-book-aggregator/orderbook"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

func SetupHTTPServer(aggregator *orderbook.OrderBookAggregator) {
	r := mux.NewRouter()

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
	}).Methods("GET")

	// WebSocket endpoint for real-time updates
	r.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade error: %v", err)
			return
		}
		defer conn.Close()

		// Send updates to this WebSocket connection
		// Implementation depends on your needs
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
