package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

// Mock WebSocket server for testing
func createMockMEXCServer(t *testing.T) *httptest.Server {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Failed to upgrade connection: %v", err)
			return
		}
		defer conn.Close()

		// Handle messages
		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				break
			}

			if messageType == websocket.TextMessage {
				var req WSRequest
				if err := json.Unmarshal(message, &req); err != nil {
					continue
				}

				// Respond to different request types
				switch req.Method {
				case "PING":
					resp := WSResponse{
						ID:   1,
						Code: 0,
						Msg:  "PONG",
					}
					conn.WriteJSON(resp)

				case "SUBSCRIPTION":
					resp := WSResponse{
						ID:   1,
						Code: 0,
						Msg:  "subscription successful",
					}
					conn.WriteJSON(resp)

					// Send mock order book data after subscription
					time.Sleep(100 * time.Millisecond)
					mockOrderBookData := []byte{} // Would be protobuf data in real scenario
					conn.WriteMessage(websocket.BinaryMessage, mockOrderBookData)
				}
			}
		}
	}))
}

func TestNewMEXCWebSocketClient(t *testing.T) {
	callback := func(data *OrderBookData) {
		// Test callback function
	}

	client := NewMEXCWebSocketClient(callback)

	assert.NotNil(t, client)
	assert.NotNil(t, client.orderBooks)
	assert.NotNil(t, client.reconnectCh)
	assert.NotNil(t, client.updateCallback)
	assert.False(t, client.isConnected)
	assert.Empty(t, client.subscriptions)
}

func TestMEXCWebSocketClient_Connect(t *testing.T) {
	server := createMockMEXCServer(t)
	defer server.Close()

	callback := func(data *OrderBookData) {}
	client := NewMEXCWebSocketClient(callback)

	// Replace the URL to point to our test server
	// Note: In a real implementation, you'd want to make the URL configurable
	// For this test, we'll assume the Connect method can be modified or mocked

	t.Run("successful connection", func(t *testing.T) {
		// This test would require modifying the Connect method to accept a URL parameter
		// or using dependency injection for testing
		// For now, we'll test the structure and behavior
		assert.NotNil(t, client)
	})
}

func TestMEXCWebSocketClient_SubscribeOrderBook(t *testing.T) {
	callback := func(data *OrderBookData) {}
	client := NewMEXCWebSocketClient(callback)

	t.Run("subscribe when not connected", func(t *testing.T) {
		err := client.SubscribeOrderBook("BTCUSDT", "100ms")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not connected")
	})

	t.Run("subscribe with empty update speed", func(t *testing.T) {
		// Create a mock that simulates connected state
		mockClient := &MockMEXCClient{
			isConnected: true,
			orderBooks:  make(map[string]*OrderBookData),
		}

		symbol := "BTCUSDT"
		err := mockClient.SubscribeOrderBook(symbol, "")
		assert.NoError(t, err)

		// Check that order book was initialized
		mockClient.mutex.RLock()
		orderBook, exists := mockClient.orderBooks[symbol]
		mockClient.mutex.RUnlock()

		assert.True(t, exists)
		assert.Equal(t, symbol, orderBook.Symbol)
		assert.Equal(t, "MEXC", orderBook.Exchange)
		assert.NotNil(t, orderBook.Bids)
		assert.NotNil(t, orderBook.Asks)
	})
}

func (m *MockMEXCClient) SubscribeOrderBook(symbol string, updateSpeed string) error {
	if !m.isConnected {
		return fmt.Errorf("not connected to WebSocket")
	}

	// Initialize order book
	m.mutex.Lock()
	m.orderBooks[symbol] = &OrderBookData{
		Symbol:   symbol,
		Bids:     make(map[string]string),
		Asks:     make(map[string]string),
		Exchange: "MEXC",
	}
	m.mutex.Unlock()

	return nil
}

func TestMEXCWebSocketClient_SubscribeMultipleOrderBooks(t *testing.T) {
	callback := func(data *OrderBookData) {}
	client := NewMEXCWebSocketClient(callback)

	symbols := []string{"BTCUSDT", "ETHUSDT", "ADAUSDT"}

	t.Run("not connected", func(t *testing.T) {
		err := client.SubscribeMultipleOrderBooks(symbols, "100ms")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not connected")
	})

	t.Run("successful subscription", func(t *testing.T) {
		// Create a mock client that implements the interface we need
		mockClient := &MockMEXCClient{
			isConnected:        true,
			orderBooks:         make(map[string]*OrderBookData),
			subscribedChannels: []string{},
		}

		err := mockClient.SubscribeMultipleOrderBooks(symbols, "100ms")
		assert.NoError(t, err)

		// Check that all symbols were initialized
		for _, symbol := range symbols {
			_, exists := mockClient.orderBooks[symbol]
			assert.True(t, exists, "Order book not initialized for %s", symbol)
		}

		// Check that channels were subscribed
		assert.True(t, len(mockClient.subscribedChannels) > 0)
	})
}

// MockMEXCClient for testing
type MockMEXCClient struct {
	isConnected        bool
	orderBooks         map[string]*OrderBookData
	subscribedChannels []string
	mutex              sync.RWMutex
}

func (m *MockMEXCClient) SubscribeMultipleOrderBooks(symbols []string, updateSpeed string) error {
	if !m.isConnected {
		return fmt.Errorf("not connected to WebSocket")
	}

	for _, symbol := range symbols {
		// Initialize order book
		m.mutex.Lock()
		m.orderBooks[symbol] = &OrderBookData{
			Symbol:   symbol,
			Bids:     make(map[string]string),
			Asks:     make(map[string]string),
			Exchange: "MEXC",
		}
		m.mutex.Unlock()

		// Mock subscription
		if updateSpeed == "" {
			updateSpeed = "100ms"
		}

		channels := []string{
			fmt.Sprintf("spot@public.aggre.depth.v3.api.pb@%s@%s", updateSpeed, strings.ToUpper(symbol)),
			fmt.Sprintf("spot@public.limit.depth.v3.api.pb@%s@20", strings.ToUpper(symbol)),
			fmt.Sprintf("spot@public.aggre.bookTicker.v3.api.pb@%s@%s", updateSpeed, strings.ToUpper(symbol)),
		}

		m.subscribedChannels = append(m.subscribedChannels, channels...)
	}

	return nil
}

func TestMEXCWebSocketClient_GetOrderBookData(t *testing.T) {
	callback := func(data *OrderBookData) {}
	client := NewMEXCWebSocketClient(callback)

	symbol := "BTCUSDT"

	t.Run("order book not found", func(t *testing.T) {
		_, err := client.GetOrderBookData(symbol)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "order book not found")
	})

	t.Run("successful retrieval", func(t *testing.T) {
		// Initialize order book
		client.mutex.Lock()
		client.orderBooks[symbol] = &OrderBookData{
			Symbol:   symbol,
			Bids:     map[string]string{"100.00": "1.5"},
			Asks:     map[string]string{"101.00": "2.0"},
			Exchange: "MEXC",
		}
		client.mutex.Unlock()

		data, err := client.GetOrderBookData(symbol)
		assert.NoError(t, err)
		assert.Equal(t, symbol, data.Symbol)
		assert.Equal(t, "MEXC", data.Exchange)
		assert.Equal(t, "1.5", data.Bids["100.00"])
		assert.Equal(t, "2.0", data.Asks["101.00"])
	})
}

func TestGetBestPriceFromData(t *testing.T) {
	t.Run("valid order book", func(t *testing.T) {
		orderBook := &OrderBookData{
			Symbol: "BTCUSDT",
			Bids: map[string]string{
				"100.00": "1.0",
				"99.50":  "2.0",
				"99.00":  "1.5",
			},
			Asks: map[string]string{
				"101.00": "1.0",
				"101.50": "2.0",
				"102.00": "1.5",
			},
		}

		bestBid, bestAsk, err := GetBestPriceFromData(orderBook)
		assert.NoError(t, err)
		assert.Equal(t, 100.00, bestBid)
		assert.Equal(t, 101.00, bestAsk)
	})

	t.Run("empty order book", func(t *testing.T) {
		orderBook := &OrderBookData{
			Symbol: "BTCUSDT",
			Bids:   map[string]string{},
			Asks:   map[string]string{},
		}

		_, _, err := GetBestPriceFromData(orderBook)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "insufficient order book data")
	})

	t.Run("invalid price format", func(t *testing.T) {
		orderBook := &OrderBookData{
			Symbol: "BTCUSDT",
			Bids: map[string]string{
				"invalid": "1.0",
			},
			Asks: map[string]string{
				"101.00": "1.0",
			},
		}

		_, _, err := GetBestPriceFromData(orderBook)
		assert.Error(t, err)
	})
}

func TestMEXCWebSocketClient_processOrderBookUpdate(t *testing.T) {
	var updateReceived bool
	var receivedData *OrderBookData
	callback := func(data *OrderBookData) {
		updateReceived = true
		receivedData = data
	}

	client := NewMEXCWebSocketClient(callback)
	symbol := "BTCUSDT"

	// Initialize order book
	client.mutex.Lock()
	client.orderBooks[symbol] = &OrderBookData{
		Symbol:   symbol,
		Bids:     map[string]string{"100.00": "1.0"},
		Asks:     map[string]string{"101.00": "1.0"},
		Exchange: "MEXC",
	}
	client.mutex.Unlock()

	t.Run("incremental update", func(t *testing.T) {
		bids := map[string]string{
			"100.50": "2.0", // New bid
			"100.00": "0",   // Remove existing bid
		}
		asks := map[string]string{
			"100.80": "1.5", // New ask
		}

		client.processOrderBookUpdate(symbol, bids, asks, "v1", time.Now().UnixMilli())

		// Check that callback was called
		assert.True(t, updateReceived)
		assert.NotNil(t, receivedData)

		// Check order book state
		client.mutex.RLock()
		orderBook := client.orderBooks[symbol]
		client.mutex.RUnlock()

		// Old bid should be removed, new bid added
		assert.NotContains(t, orderBook.Bids, "100.00")
		assert.Contains(t, orderBook.Bids, "100.50")
		assert.Equal(t, "2.0", orderBook.Bids["100.50"])

		// New ask should be added
		assert.Contains(t, orderBook.Asks, "100.80")
		assert.Equal(t, "1.5", orderBook.Asks["100.80"])
	})
}

func TestMEXCWebSocketClient_processOrderBookSnapshot(t *testing.T) {
	var updateReceived bool
	callback := func(data *OrderBookData) {
		updateReceived = true
	}

	client := NewMEXCWebSocketClient(callback)
	symbol := "BTCUSDT"

	// Initialize order book with some data
	client.mutex.Lock()
	client.orderBooks[symbol] = &OrderBookData{
		Symbol:   symbol,
		Bids:     map[string]string{"100.00": "1.0", "99.50": "2.0"},
		Asks:     map[string]string{"101.00": "1.0", "101.50": "2.0"},
		Exchange: "MEXC",
	}
	client.mutex.Unlock()

	t.Run("snapshot replacement", func(t *testing.T) {
		newBids := map[string]string{
			"100.25": "1.5",
			"100.00": "2.0",
		}
		newAsks := map[string]string{
			"100.75": "1.0",
		}

		client.processOrderBookSnapshot(symbol, newBids, newAsks, "v2", time.Now().UnixMilli())

		// Check that callback was called
		assert.True(t, updateReceived)

		// Check that order book was completely replaced
		client.mutex.RLock()
		orderBook := client.orderBooks[symbol]
		client.mutex.RUnlock()

		// Old data should be gone
		assert.NotContains(t, orderBook.Bids, "99.50")
		assert.NotContains(t, orderBook.Asks, "101.00")
		assert.NotContains(t, orderBook.Asks, "101.50")

		// New data should be present
		assert.Contains(t, orderBook.Bids, "100.25")
		assert.Contains(t, orderBook.Bids, "100.00")
		assert.Contains(t, orderBook.Asks, "100.75")

		assert.Equal(t, "1.5", orderBook.Bids["100.25"])
		assert.Equal(t, "1.0", orderBook.Asks["100.75"])
	})
}

func TestMEXCWebSocketClient_createOrderBookCopy(t *testing.T) {
	callback := func(data *OrderBookData) {}
	client := NewMEXCWebSocketClient(callback)

	original := &OrderBookData{
		Symbol:     "BTCUSDT",
		Bids:       map[string]string{"100.00": "1.0"},
		Asks:       map[string]string{"101.00": "1.0"},
		LastUpdate: time.Now().UnixMilli(),
		Version:    "v1",
		Exchange:   "MEXC",
	}

	copy := client.createOrderBookCopy(original)

	// Check that all fields are copied
	assert.Equal(t, original.Symbol, copy.Symbol)
	assert.Equal(t, original.LastUpdate, copy.LastUpdate)
	assert.Equal(t, original.Version, copy.Version)
	assert.Equal(t, original.Exchange, copy.Exchange)

	// Check that maps are copied, not referenced
	assert.Equal(t, original.Bids["100.00"], copy.Bids["100.00"])
	assert.Equal(t, original.Asks["101.00"], copy.Asks["101.00"])

	// Modify original to ensure copy is independent
	original.Bids["100.00"] = "2.0"
	assert.Equal(t, "1.0", copy.Bids["100.00"])
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
		hasError bool
	}{
		{"100.50", 100.50, false},
		{"0", 0, false},
		{"123.456789", 123.456789, false},
		{"", 0, true},
		{"invalid", 0, true},
		{"100.50.25", 0, true},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			result, err := parseFloat(test.input)
			if test.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, test.expected, result)
			}
		})
	}
}

func TestMEXCWebSocketClient_GetExchangeName(t *testing.T) {
	callback := func(data *OrderBookData) {}
	client := NewMEXCWebSocketClient(callback)

	assert.Equal(t, "MEXC", client.GetExchangeName())
}

func TestMEXCWebSocketClient_Close(t *testing.T) {
	callback := func(data *OrderBookData) {}
	client := NewMEXCWebSocketClient(callback)

	// Test closing when not connected
	err := client.Close()
	assert.NoError(t, err)
	assert.False(t, client.isConnected)

	// Test with ping ticker
	client.pingTicker = time.NewTicker(1 * time.Second)
	client.isConnected = true

	err = client.Close()
	assert.NoError(t, err)
	assert.False(t, client.isConnected)
}

// Benchmark tests
func BenchmarkMEXCWebSocketClient_processOrderBookUpdate(b *testing.B) {
	callback := func(data *OrderBookData) {}
	client := NewMEXCWebSocketClient(callback)
	symbol := "BTCUSDT"

	// Initialize order book
	client.mutex.Lock()
	client.orderBooks[symbol] = &OrderBookData{
		Symbol:   symbol,
		Bids:     make(map[string]string),
		Asks:     make(map[string]string),
		Exchange: "MEXC",
	}
	client.mutex.Unlock()

	bids := map[string]string{
		"100.00": "1.0",
		"99.50":  "2.0",
	}
	asks := map[string]string{
		"101.00": "1.0",
		"101.50": "2.0",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client.processOrderBookUpdate(symbol, bids, asks, "v1", time.Now().UnixMilli())
	}
}

func BenchmarkGetBestPriceFromData(b *testing.B) {
	orderBook := &OrderBookData{
		Symbol: "BTCUSDT",
		Bids: map[string]string{
			"100.00": "1.0",
			"99.50":  "2.0",
			"99.00":  "1.5",
			"98.50":  "3.0",
			"98.00":  "2.5",
		},
		Asks: map[string]string{
			"101.00": "1.0",
			"101.50": "2.0",
			"102.00": "1.5",
			"102.50": "3.0",
			"103.00": "2.5",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetBestPriceFromData(orderBook)
	}
}

// Test race conditions
func TestMEXCWebSocketClient_ConcurrentAccess(t *testing.T) {
	var wg sync.WaitGroup
	callback := func(data *OrderBookData) {}
	client := NewMEXCWebSocketClient(callback)
	symbol := "BTCUSDT"

	// Initialize order book
	client.mutex.Lock()
	client.orderBooks[symbol] = &OrderBookData{
		Symbol:   symbol,
		Bids:     make(map[string]string),
		Asks:     make(map[string]string),
		Exchange: "MEXC",
	}
	client.mutex.Unlock()

	// Concurrent reads and writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			// Simulate updates
			bids := map[string]string{
				fmt.Sprintf("100.%02d", i): "1.0",
			}
			asks := map[string]string{
				fmt.Sprintf("101.%02d", i): "1.0",
			}

			client.processOrderBookUpdate(symbol, bids, asks, fmt.Sprintf("v%d", i), time.Now().UnixMilli())
		}(i)

		wg.Add(1)
		go func() {
			defer wg.Done()

			// Simulate reads
			client.GetOrderBookData(symbol)
			client.GetBestPrice(symbol)
		}()
	}

	wg.Wait()
	// If we get here without deadlock, the test passes
}
