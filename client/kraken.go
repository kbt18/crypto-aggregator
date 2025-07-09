package client

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// ============================================================================
// Kraken-Specific Implementation
// ============================================================================

// KrakenWebSocketClient wraps the base client with Kraken-specific functionality
type KrakenWebSocketClient struct {
	*BaseWebSocketClient
}

// KrakenImplementation implements the ExchangeImplementation interface for Kraken
type KrakenImplementation struct {
	client *KrakenWebSocketClient
}

// NewKrakenWebSocketClient creates a new Kraken WebSocket client
func NewKrakenWebSocketClient(callback OrderBookUpdateCallback) WebSocketClient {
	config := &ExchangeConfig{
		Name:           "Kraken",
		WebSocketURL:   "wss://ws.kraken.com/v2",
		PingInterval:   30 * time.Second,
		ReconnectDelay: 5 * time.Second,
		MaxRetries:     10,
	}

	krakenClient := &KrakenWebSocketClient{}

	impl := &KrakenImplementation{
		client: krakenClient,
	}

	baseClient := NewBaseWebSocketClient(config, impl, callback)
	krakenClient.BaseWebSocketClient = baseClient

	return krakenClient
}

// SubscribeOrderBook subscribes to order book updates for symbols (Kraken-specific method)
func (c *KrakenWebSocketClient) SubscribeOrderBook(symbols []string) error {
	return c.Subscribe(symbols)
}

// CreateSubscriptionMessage creates Kraken subscription message
func (impl *KrakenImplementation) CreateSubscriptionMessage(symbols []string) (interface{}, error) {
	// Normalize symbols for Kraken format
	krakenSymbols := make([]string, len(symbols))
	for i, symbol := range symbols {
		krakenSymbols[i] = impl.NormalizeSymbol(symbol)
	}

	return KrakenSubscribeRequest{
		Method: "subscribe",
		Params: KrakenSubscribeParams{
			Channel: "book",
			Symbol:  krakenSymbols,
		},
	}, nil
}

// CreateUnsubscriptionMessage creates Kraken unsubscription message
func (impl *KrakenImplementation) CreateUnsubscriptionMessage(symbols []string) (interface{}, error) {
	// Normalize symbols for Kraken format
	krakenSymbols := make([]string, len(symbols))
	for i, symbol := range symbols {
		krakenSymbols[i] = impl.NormalizeSymbol(symbol)
	}

	return KrakenSubscribeRequest{
		Method: "unsubscribe",
		Params: KrakenSubscribeParams{
			Channel: "book",
			Symbol:  krakenSymbols,
		},
	}, nil
}

// CreatePingMessage creates Kraken ping message
func (impl *KrakenImplementation) CreatePingMessage() (interface{}, error) {
	return map[string]interface{}{
		"method": "ping",
	}, nil
}

// NormalizeSymbol converts symbol format (BTCUSDT -> BTC/USDT)
func (impl *KrakenImplementation) NormalizeSymbol(symbol string) string {
	// Common symbol mappings for Kraken
	symbolMappings := map[string]string{
		"BTCUSDT":   "BTC/USDT",
		"ETHUSDT":   "ETH/USDT",
		"ADAUSDT":   "ADA/USDT",
		"BNBUSDT":   "BNB/USDT", // Note: BNB might not be available on Kraken
		"SOLUSDT":   "SOL/USDT",
		"DOTUSDT":   "DOT/USDT",
		"LINKUSDT":  "LINK/USDT",
		"UNIUSDT":   "UNI/USDT",
		"LTCUSDT":   "LTC/USDT",
		"BCHUSDT":   "BCH/USDT",
		"XLMUSDT":   "XLM/USDT",
		"ATOMUSDT":  "ATOM/USDT",
		"ALGOUSDT":  "ALGO/USDT",
		"MATICUSDT": "MATIC/USDT",
	}

	if krakenSymbol, exists := symbolMappings[strings.ToUpper(symbol)]; exists {
		return krakenSymbol
	}

	// If no mapping found, try to insert "/" before "USDT"
	if strings.HasSuffix(strings.ToUpper(symbol), "USDT") {
		base := symbol[:len(symbol)-4]
		return strings.ToUpper(base) + "/USDT"
	}

	return strings.ToUpper(symbol)
}

// DenormalizeSymbol converts back from Kraken format (BTC/USDT -> BTCUSDT)
func (impl *KrakenImplementation) DenormalizeSymbol(exchangeSymbol string) string {
	// Remove the "/" to get the format used in our aggregator
	return strings.ReplaceAll(exchangeSymbol, "/", "")
}

// HandleMessage handles incoming messages from Kraken
func (impl *KrakenImplementation) HandleMessage(messageType int, message []byte) error {
	if messageType != websocket.TextMessage {
		return nil // Kraken only sends text messages
	}

	// Try to parse as subscription response first
	var subResponse KrakenSubscribeResponse
	if err := json.Unmarshal(message, &subResponse); err == nil && subResponse.Method != "" {
		return impl.handleSubscriptionResponse(&subResponse)
	}

	// Try to parse as pong response
	var pongResponse map[string]interface{}
	if err := json.Unmarshal(message, &pongResponse); err == nil {
		if method, exists := pongResponse["method"]; exists && method == "pong" {
			return nil // Ignore pong responses
		}
	}

	// Try to parse as order book update
	var bookUpdate KrakenBookUpdate
	if err := json.Unmarshal(message, &bookUpdate); err == nil && bookUpdate.Channel == "book" {
		return impl.handleOrderBookUpdate(&bookUpdate)
	}

	log.Printf("Unknown Kraken message: %s", string(message))
	return nil
}

// handleSubscriptionResponse processes subscription/unsubscription responses
func (impl *KrakenImplementation) handleSubscriptionResponse(response *KrakenSubscribeResponse) error {
	if response.Method == "subscribe" {
		if response.Success {
			log.Printf("Kraken subscription successful for %s", response.Result.Symbol)
		} else {
			log.Printf("Kraken subscription failed: %s", response.Error)
		}
	} else if response.Method == "unsubscribe" {
		if response.Success {
			log.Printf("Kraken unsubscription successful for %s", response.Result.Symbol)
		} else {
			log.Printf("Kraken unsubscription failed: %s", response.Error)
		}
	}
	return nil
}

// handleOrderBookUpdate processes order book updates
func (impl *KrakenImplementation) handleOrderBookUpdate(update *KrakenBookUpdate) error {
	for _, data := range update.Data {
		symbol := impl.DenormalizeSymbol(data.Symbol)

		// Parse timestamp
		timestamp := time.Now().UnixMilli()
		if data.Timestamp != "" {
			if parsedTime, err := time.Parse(time.RFC3339Nano, data.Timestamp); err == nil {
				timestamp = parsedTime.UnixMilli()
			}
		}

		// Convert price levels to string maps
		bids := make(map[string]string)
		asks := make(map[string]string)

		// Convert bids
		for _, bid := range data.Bids {
			priceStr := strconv.FormatFloat(bid.Price, 'f', -1, 64)
			qtyStr := strconv.FormatFloat(bid.Quantity, 'f', -1, 64)

			if bid.Quantity == 0 {
				bids[priceStr] = "0" // Mark for removal
			} else {
				bids[priceStr] = qtyStr
			}
		}

		// Convert asks
		for _, ask := range data.Asks {
			priceStr := strconv.FormatFloat(ask.Price, 'f', -1, 64)
			qtyStr := strconv.FormatFloat(ask.Quantity, 'f', -1, 64)

			if ask.Quantity == 0 {
				asks[priceStr] = "0" // Mark for removal
			} else {
				asks[priceStr] = qtyStr
			}
		}

		// Determine if this is a snapshot
		isSnapshot := update.Type == "snapshot"

		// Use checksum as version if available
		version := fmt.Sprintf("%d", data.Checksum)

		log.Printf("Kraken order book %s for %s: %d bids, %d asks, checksum: %d",
			update.Type, symbol, len(bids), len(asks), data.Checksum)

		// Update the order book using the base client method
		impl.client.UpdateOrderBook(symbol, bids, asks, version, timestamp, isSnapshot)
	}

	return nil
}

// ProcessOrderBookUpdate processes order book update data (implementation required by interface)
func (impl *KrakenImplementation) ProcessOrderBookUpdate(symbol string, data interface{}) (*OrderBookData, error) {
	// This method can be used for additional processing if needed
	// For now, the processing is handled in the handleOrderBookUpdate method
	return nil, nil
}

// ============================================================================
// Kraken Message Types
// ============================================================================

// KrakenSubscribeRequest represents a subscription/unsubscription request
type KrakenSubscribeRequest struct {
	Method string                `json:"method"`
	Params KrakenSubscribeParams `json:"params"`
}

// KrakenSubscribeParams represents subscription parameters
type KrakenSubscribeParams struct {
	Channel string   `json:"channel"`
	Symbol  []string `json:"symbol"`
}

// KrakenSubscribeResponse represents a subscription response
type KrakenSubscribeResponse struct {
	Method  string                `json:"method"`
	Result  KrakenSubscribeResult `json:"result,omitempty"`
	Success bool                  `json:"success"`
	Error   string                `json:"error,omitempty"`
	TimeIn  string                `json:"time_in,omitempty"`
	TimeOut string                `json:"time_out,omitempty"`
}

// KrakenSubscribeResult represents subscription result details
type KrakenSubscribeResult struct {
	Channel  string `json:"channel"`
	Depth    int    `json:"depth"`
	Snapshot bool   `json:"snapshot"`
	Symbol   string `json:"symbol"`
}

// KrakenBookUpdate represents an order book update message
type KrakenBookUpdate struct {
	Channel string           `json:"channel"`
	Type    string           `json:"type"`
	Data    []KrakenBookData `json:"data"`
}

// KrakenBookData represents order book data for a symbol
type KrakenBookData struct {
	Symbol    string             `json:"symbol"`
	Bids      []KrakenPriceLevel `json:"bids"`
	Asks      []KrakenPriceLevel `json:"asks"`
	Checksum  uint32             `json:"checksum"`
	Timestamp string             `json:"timestamp"`
}

// KrakenPriceLevel represents a price level in the order book
type KrakenPriceLevel struct {
	Price    float64 `json:"price"`
	Quantity float64 `json:"qty"`
}
