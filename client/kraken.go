// kraken_client.go
package client

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// Kraken-specific message types
type KrakenSubscribeRequest struct {
	Method string                `json:"method"`
	Params KrakenSubscribeParams `json:"params"`
}

type KrakenSubscribeParams struct {
	Channel string   `json:"channel"`
	Symbol  []string `json:"symbol"`
}

type KrakenSubscribeResponse struct {
	Method  string                `json:"method"`
	Result  KrakenSubscribeResult `json:"result,omitempty"`
	Success bool                  `json:"success"`
	Error   string                `json:"error,omitempty"`
}

type KrakenSubscribeResult struct {
	Channel string `json:"channel"`
	Symbol  string `json:"symbol"`
}

type KrakenBookUpdate struct {
	Channel string           `json:"channel"`
	Type    string           `json:"type"`
	Data    []KrakenBookData `json:"data"`
}

type KrakenBookData struct {
	Symbol    string             `json:"symbol"`
	Bids      []KrakenPriceLevel `json:"bids"`
	Asks      []KrakenPriceLevel `json:"asks"`
	Timestamp string             `json:"timestamp"`
}

type KrakenPriceLevel struct {
	Price    float64 `json:"price"`
	Quantity float64 `json:"qty"`
}

// KrakenWebSocketClient wraps the base client with Kraken-specific functionality
type KrakenWebSocketClient struct {
	*BaseWebSocketClient
}

// NewKrakenWebSocketClient creates a new Kraken WebSocket client
func NewKrakenWebSocketClient(callback OrderBookUpdateCallback) *KrakenWebSocketClient {
	config := ExchangeConfig{
		WSHost:       "ws.kraken.com",
		WSPath:       "/v2",
		PingMethod:   "ping",
		PingInterval: 30 * time.Second,
	}

	base := NewBaseWebSocketClient("Kraken", config, callback)

	kraken := &KrakenWebSocketClient{
		BaseWebSocketClient: base,
	}

	// Set Kraken-specific handlers
	base.messageHandler = kraken.handleMessage
	base.subscribeHandler = kraken.subscribeOrderBook

	return kraken
}

// handleMessage processes Kraken-specific messages
func (c *KrakenWebSocketClient) handleMessage(message []byte) {
	// Try subscription response
	var subResponse KrakenSubscribeResponse
	if err := json.Unmarshal(message, &subResponse); err == nil && subResponse.Method == "subscribe" {
		if subResponse.Success {
			log.Printf("Kraken subscription successful for %s", subResponse.Result.Symbol)
		} else {
			log.Printf("Kraken subscription failed: %s", subResponse.Error)
		}
		return
	}

	// Try pong response
	var pongResponse map[string]interface{}
	if err := json.Unmarshal(message, &pongResponse); err == nil {
		if method, exists := pongResponse["method"]; exists && method == "pong" {
			return
		}
	}

	// Try order book update
	var bookUpdate KrakenBookUpdate
	if err := json.Unmarshal(message, &bookUpdate); err == nil && bookUpdate.Channel == "book" {
		c.handleOrderBookUpdate(&bookUpdate)
		return
	}

	log.Printf("Unknown Kraken message: %s", string(message))
}

// subscribeOrderBook implements Kraken-specific subscription logic
func (c *KrakenWebSocketClient) subscribeOrderBook(symbols []string) error {
	krakenSymbols := make([]string, len(symbols))
	for i, symbol := range symbols {
		krakenSymbols[i] = c.normalizeSymbol(symbol)
		c.InitializeOrderBook(symbol)
	}

	req := KrakenSubscribeRequest{
		Method: "subscribe",
		Params: KrakenSubscribeParams{
			Channel: "book",
			Symbol:  krakenSymbols,
		},
	}

	if err := c.conn.WriteJSON(req); err != nil {
		return fmt.Errorf("failed to subscribe to Kraken order books: %w", err)
	}

	c.subscriptions = append(c.subscriptions, symbols...)
	log.Printf("Subscribed to Kraken order books: %v", krakenSymbols)
	return nil
}

// normalizeSymbol converts symbol format (BTCUSDT -> BTC/USDT)
func (c *KrakenWebSocketClient) normalizeSymbol(symbol string) string {
	symbolMappings := map[string]string{
		"BTCUSDT": "BTC/USDT",
		"ETHUSDT": "ETH/USDT",
		"ADAUSDT": "ADA/USDT",
		"SOLUSDT": "SOL/USDT",
		"DOTUSDT": "DOT/USDT",
	}

	if krakenSymbol, exists := symbolMappings[strings.ToUpper(symbol)]; exists {
		return krakenSymbol
	}

	if strings.HasSuffix(strings.ToUpper(symbol), "USDT") {
		base := symbol[:len(symbol)-4]
		return strings.ToUpper(base) + "/USDT"
	}

	return strings.ToUpper(symbol)
}

// denormalizeSymbol converts back from Kraken format
func (c *KrakenWebSocketClient) denormalizeSymbol(krakenSymbol string) string {
	return strings.ReplaceAll(krakenSymbol, "/", "")
}

// handleOrderBookUpdate processes Kraken order book updates
func (c *KrakenWebSocketClient) handleOrderBookUpdate(update *KrakenBookUpdate) {
	for _, data := range update.Data {
		symbol := c.denormalizeSymbol(data.Symbol)

		timestamp := time.Now().UnixMilli()
		if data.Timestamp != "" {
			if parsedTime, err := time.Parse(time.RFC3339Nano, data.Timestamp); err == nil {
				timestamp = parsedTime.UnixMilli()
			}
		}

		bids := make(map[string]string)
		asks := make(map[string]string)

		// Convert Kraken price levels to our format
		for _, bid := range data.Bids {
			priceStr := fmt.Sprintf("%.8f", bid.Price)
			qtyStr := fmt.Sprintf("%.8f", bid.Quantity)
			if bid.Quantity > 0 {
				bids[priceStr] = qtyStr
			}
		}

		for _, ask := range data.Asks {
			priceStr := fmt.Sprintf("%.8f", ask.Price)
			qtyStr := fmt.Sprintf("%.8f", ask.Quantity)
			if ask.Quantity > 0 {
				asks[priceStr] = qtyStr
			}
		}

		isSnapshot := update.Type == "snapshot"
		c.UpdateOrderBook(symbol, bids, asks, "", timestamp, isSnapshot)

		log.Printf("Kraken order book %s for %s: %d bids, %d asks",
			update.Type, symbol, len(bids), len(asks))
	}
}
