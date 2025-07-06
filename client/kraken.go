package client

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Kraken WebSocket message types
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
	TimeIn  string                `json:"time_in,omitempty"`
	TimeOut string                `json:"time_out,omitempty"`
}

type KrakenSubscribeResult struct {
	Channel  string `json:"channel"`
	Depth    int    `json:"depth"`
	Snapshot bool   `json:"snapshot"`
	Symbol   string `json:"symbol"`
}

// Kraken order book update message
type KrakenBookUpdate struct {
	Channel string           `json:"channel"`
	Type    string           `json:"type"`
	Data    []KrakenBookData `json:"data"`
}

type KrakenBookData struct {
	Symbol    string             `json:"symbol"`
	Bids      []KrakenPriceLevel `json:"bids"`
	Asks      []KrakenPriceLevel `json:"asks"`
	Checksum  uint32             `json:"checksum"`
	Timestamp string             `json:"timestamp"`
}

type KrakenPriceLevel struct {
	Price    float64 `json:"price"`
	Quantity float64 `json:"qty"`
}

// KrakenWebSocketClient represents the Kraken WebSocket client
type KrakenWebSocketClient struct {
	conn           *websocket.Conn
	orderBooks     map[string]*OrderBookData
	mutex          sync.RWMutex
	pingTicker     *time.Ticker
	reconnectCh    chan bool
	isConnected    bool
	subscriptions  []string
	updateCallback OrderBookUpdateCallback
}

// NewKrakenWebSocketClient creates a new Kraken WebSocket client
func NewKrakenWebSocketClient(callback OrderBookUpdateCallback) *KrakenWebSocketClient {
	return &KrakenWebSocketClient{
		orderBooks:     make(map[string]*OrderBookData),
		reconnectCh:    make(chan bool, 1),
		subscriptions:  make([]string, 0),
		updateCallback: callback,
	}
}

// Connect establishes WebSocket connection to Kraken
func (c *KrakenWebSocketClient) Connect() error {
	u := url.URL{Scheme: "wss", Host: "ws.kraken.com", Path: "/v2"}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to connect to Kraken WebSocket: %w", err)
	}

	c.conn = conn
	c.isConnected = true

	// Start ping mechanism (Kraken expects ping every 30 seconds)
	c.startPing()

	// Start message handler
	go c.handleMessages()

	log.Printf("Connected to Kraken WebSocket: %s", u.String())
	return nil
}

// startPing starts the ping mechanism to keep connection alive
func (c *KrakenWebSocketClient) startPing() {
	c.pingTicker = time.NewTicker(30 * time.Second)
	go func() {
		for range c.pingTicker.C {
			if !c.isConnected {
				return
			}

			ping := map[string]interface{}{
				"method": "ping",
			}

			if err := c.conn.WriteJSON(ping); err != nil {
				log.Printf("Failed to send ping to Kraken: %v", err)
				c.reconnectCh <- true
				return
			}
		}
	}()
}

// SubscribeOrderBook subscribes to order book updates for symbols
func (c *KrakenWebSocketClient) SubscribeOrderBook(symbols []string) error {
	if !c.isConnected {
		return fmt.Errorf("not connected to Kraken WebSocket")
	}

	// Normalize symbol names for Kraken (e.g., BTCUSDT -> BTC/USDT)
	krakenSymbols := make([]string, len(symbols))
	for i, symbol := range symbols {
		krakenSymbols[i] = c.normalizeSymbol(symbol)

		// Initialize order book
		c.mutex.Lock()
		c.orderBooks[symbol] = &OrderBookData{
			Symbol:   symbol,
			Bids:     make(map[string]string),
			Asks:     make(map[string]string),
			Exchange: "Kraken",
		}
		c.mutex.Unlock()
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
	log.Printf("Subscribed to Kraken order books for symbols: %v", krakenSymbols)
	return nil
}

// normalizeSymbol converts symbol format (BTCUSDT -> BTC/USDT)
func (c *KrakenWebSocketClient) normalizeSymbol(symbol string) string {
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

// denormalizeSymbol converts back from Kraken format (BTC/USDT -> BTCUSDT)
func (c *KrakenWebSocketClient) denormalizeSymbol(krakenSymbol string) string {
	// Remove the "/" to get the format used in our aggregator
	return strings.ReplaceAll(krakenSymbol, "/", "")
}

// handleMessages handles incoming WebSocket messages
func (c *KrakenWebSocketClient) handleMessages() {
	defer func() {
		c.isConnected = false
		if c.pingTicker != nil {
			c.pingTicker.Stop()
		}
	}()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("Kraken WebSocket read error: %v", err)
			c.reconnectCh <- true
			return
		}

		c.handleMessage(message)
	}
}

// handleMessage processes different types of messages
func (c *KrakenWebSocketClient) handleMessage(message []byte) {
	// Try to parse as subscription response first
	var subResponse KrakenSubscribeResponse
	if err := json.Unmarshal(message, &subResponse); err == nil && subResponse.Method == "subscribe" {
		if subResponse.Success {
			log.Printf("Kraken subscription successful for %s", subResponse.Result.Symbol)
		} else {
			log.Printf("Kraken subscription failed: %s", subResponse.Error)
		}
		return
	}

	// Try to parse as pong response
	var pongResponse map[string]interface{}
	if err := json.Unmarshal(message, &pongResponse); err == nil {
		if method, exists := pongResponse["method"]; exists && method == "pong" {
			return // Ignore pong responses
		}
	}

	// Try to parse as order book update
	var bookUpdate KrakenBookUpdate
	if err := json.Unmarshal(message, &bookUpdate); err == nil && bookUpdate.Channel == "book" {
		c.handleOrderBookUpdate(&bookUpdate)
		return
	}

	log.Printf("Unknown Kraken message: %s", string(message))
}

// handleOrderBookUpdate processes order book updates
func (c *KrakenWebSocketClient) handleOrderBookUpdate(update *KrakenBookUpdate) {
	for _, data := range update.Data {
		symbol := c.denormalizeSymbol(data.Symbol)

		c.mutex.Lock()
		orderBook, exists := c.orderBooks[symbol]
		if !exists {
			c.mutex.Unlock()
			continue
		}

		// Parse timestamp
		timestamp := time.Now().UnixMilli()
		if data.Timestamp != "" {
			if parsedTime, err := time.Parse(time.RFC3339Nano, data.Timestamp); err == nil {
				timestamp = parsedTime.UnixMilli()
			}
		}

		orderBook.LastUpdate = timestamp

		// Handle snapshot vs update
		if update.Type == "snapshot" {
			// Clear existing data for snapshot
			orderBook.Bids = make(map[string]string)
			orderBook.Asks = make(map[string]string)
		}

		// Update bids
		for _, bid := range data.Bids {
			priceStr := fmt.Sprintf("%.8f", bid.Price)
			qtyStr := fmt.Sprintf("%.8f", bid.Quantity)

			if bid.Quantity == 0 {
				delete(orderBook.Bids, priceStr)
			} else {
				orderBook.Bids[priceStr] = qtyStr
			}
		}

		// Update asks
		for _, ask := range data.Asks {
			priceStr := fmt.Sprintf("%.8f", ask.Price)
			qtyStr := fmt.Sprintf("%.8f", ask.Quantity)

			if ask.Quantity == 0 {
				delete(orderBook.Asks, priceStr)
			} else {
				orderBook.Asks[priceStr] = qtyStr
			}
		}

		c.mutex.Unlock()

		// Create a copy for the callback
		orderBookCopy := c.createOrderBookCopy(orderBook)
		log.Printf("Kraken order book %s for %s: %d bids, %d asks",
			update.Type, symbol, len(orderBookCopy.Bids), len(orderBookCopy.Asks))

		// Notify callback
		if c.updateCallback != nil {
			c.updateCallback(orderBookCopy)
		}
	}
}

// createOrderBookCopy creates a copy of order book data for callbacks
func (c *KrakenWebSocketClient) createOrderBookCopy(orderBook *OrderBookData) *OrderBookData {
	copy := &OrderBookData{
		Symbol:     orderBook.Symbol,
		Bids:       make(map[string]string),
		Asks:       make(map[string]string),
		LastUpdate: orderBook.LastUpdate,
		Version:    orderBook.Version,
		Exchange:   orderBook.Exchange,
	}

	for price, qty := range orderBook.Bids {
		copy.Bids[price] = qty
	}

	for price, qty := range orderBook.Asks {
		copy.Asks[price] = qty
	}

	return copy
}

// GetOrderBookData returns the current order book data for a symbol
func (c *KrakenWebSocketClient) GetOrderBookData(symbol string) (*OrderBookData, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	orderBook, exists := c.orderBooks[symbol]
	if !exists {
		return nil, fmt.Errorf("order book not found for symbol: %s", symbol)
	}

	return c.createOrderBookCopy(orderBook), nil
}

// GetBestPrice returns the best bid and ask prices
func (c *KrakenWebSocketClient) GetBestPrice(symbol string) (bestBid, bestAsk float64, err error) {
	orderBook, err := c.GetOrderBookData(symbol)
	if err != nil {
		return 0, 0, err
	}

	return GetBestPriceFromData(orderBook)
}

// Close closes the WebSocket connection
func (c *KrakenWebSocketClient) Close() error {
	c.isConnected = false

	if c.pingTicker != nil {
		c.pingTicker.Stop()
	}

	if c.conn != nil {
		return c.conn.Close()
	}

	return nil
}

// Reconnect handles reconnection logic
func (c *KrakenWebSocketClient) Reconnect() error {
	log.Println("Attempting to reconnect to Kraken...")

	c.Close()
	time.Sleep(5 * time.Second)

	if err := c.Connect(); err != nil {
		return err
	}

	// Resubscribe to previous symbols
	if len(c.subscriptions) > 0 {
		if err := c.SubscribeOrderBook(c.subscriptions); err != nil {
			return err
		}
	}

	log.Println("Reconnected to Kraken successfully")
	return nil
}

// StartReconnectHandler starts the reconnection handler
func (c *KrakenWebSocketClient) StartReconnectHandler() {
	go func() {
		for range c.reconnectCh {
			for !c.isConnected {
				if err := c.Reconnect(); err != nil {
					log.Printf("Kraken reconnection failed: %v, retrying in 10 seconds...", err)
					time.Sleep(10 * time.Second)
					continue
				}
				break
			}
		}
	}()
}

// GetExchangeName returns the exchange name
func (c *KrakenWebSocketClient) GetExchangeName() string {
	return "Kraken"
}
