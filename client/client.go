// base_client.go
package client

import (
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// OrderBookData represents raw order book data from the exchange
type OrderBookData struct {
	Symbol     string
	Bids       map[string]string // price -> quantity
	Asks       map[string]string // price -> quantity
	LastUpdate int64
	Version    string
	Exchange   string
}

// OrderBookUpdateCallback is a function type for handling order book updates
type OrderBookUpdateCallback func(data *OrderBookData)

// WebSocketClient interface defines common WebSocket client operations
type WebSocketClient interface {
	Connect() error
	Close() error
	SubscribeOrderBook(symbols []string) error
	GetOrderBookData(symbol string) (*OrderBookData, error)
	GetBestPrice(symbol string) (bestBid, bestAsk float64, err error)
	GetExchangeName() string
	StartReconnectHandler()
}

// ExchangeConfig holds exchange-specific configuration
type ExchangeConfig struct {
	WSHost       string
	WSPath       string
	PingMethod   string
	PingInterval time.Duration
}

// BaseWebSocketClient provides common functionality for all exchange clients
type BaseWebSocketClient struct {
	conn           *websocket.Conn
	orderBooks     map[string]*OrderBookData
	mutex          sync.RWMutex
	pingTicker     *time.Ticker
	reconnectCh    chan bool
	isConnected    bool
	subscriptions  []string
	updateCallback OrderBookUpdateCallback
	config         ExchangeConfig
	exchangeName   string

	// Exchange-specific handlers (to be implemented by each exchange)
	messageHandler   func([]byte)
	subscribeHandler func([]string) error
	reconnectHandler func() error
}

// NewBaseWebSocketClient creates a new base WebSocket client
func NewBaseWebSocketClient(exchangeName string, config ExchangeConfig, callback OrderBookUpdateCallback) *BaseWebSocketClient {
	return &BaseWebSocketClient{
		orderBooks:     make(map[string]*OrderBookData),
		reconnectCh:    make(chan bool, 1),
		subscriptions:  make([]string, 0),
		updateCallback: callback,
		config:         config,
		exchangeName:   exchangeName,
	}
}

// Connect establishes WebSocket connection
func (c *BaseWebSocketClient) Connect() error {
	url := fmt.Sprintf("wss://%s%s", c.config.WSHost, c.config.WSPath)

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to %s WebSocket: %w", c.exchangeName, err)
	}

	c.conn = conn
	c.isConnected = true

	c.startPing()

	go c.handleMessages()

	log.Printf("Connected to %s WebSocket: %s", c.exchangeName, url)
	return nil
}

func (c *BaseWebSocketClient) startPing() {
	c.pingTicker = time.NewTicker(c.config.PingInterval)
	go func() {
		for range c.pingTicker.C {
			if !c.isConnected {
				return
			}

			if err := c.sendPing(); err != nil {
				log.Printf("Failed to send ping to %s: %v", c.exchangeName, err)
				c.reconnectCh <- true
				return
			}
		}
	}()
}

func (c *BaseWebSocketClient) sendPing() error {
	if c.config.PingMethod == "PING" {
		ping := map[string]interface{}{"method": "PING"}
		return c.conn.WriteJSON(ping)
	} else {
		ping := map[string]interface{}{"method": "ping"}
		return c.conn.WriteJSON(ping)
	}
}

// handleMessages handles incoming WebSocket messages
func (c *BaseWebSocketClient) handleMessages() {
	defer func() {
		c.isConnected = false
		if c.pingTicker != nil {
			c.pingTicker.Stop()
		}
	}()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("%s WebSocket read error: %v", c.exchangeName, err)
			c.reconnectCh <- true
			return
		}

		// Delegate to exchange-specific message handler
		if c.messageHandler != nil {
			c.messageHandler(message)
		}
	}
}

// Close closes the WebSocket connection
func (c *BaseWebSocketClient) Close() error {
	c.isConnected = false

	if c.pingTicker != nil {
		c.pingTicker.Stop()
	}

	if c.conn != nil {
		return c.conn.Close()
	}

	return nil
}

// GetExchangeName returns the exchange name
func (c *BaseWebSocketClient) GetExchangeName() string {
	return c.exchangeName
}

// StartReconnectHandler starts the reconnection handler
func (c *BaseWebSocketClient) StartReconnectHandler() {
	go func() {
		for range c.reconnectCh {
			for !c.isConnected {
				if err := c.reconnect(); err != nil {
					log.Printf("%s reconnection failed: %v, retrying in 10 seconds...", c.exchangeName, err)
					time.Sleep(10 * time.Second)
					continue
				}
				break
			}
		}
	}()
}

// reconnect handles reconnection logic
func (c *BaseWebSocketClient) reconnect() error {
	log.Printf("Attempting to reconnect to %s...", c.exchangeName)

	c.Close()
	time.Sleep(5 * time.Second)

	if err := c.Connect(); err != nil {
		return err
	}

	// Use exchange-specific reconnect handler if available
	if c.reconnectHandler != nil {
		return c.reconnectHandler()
	}

	// Default: resubscribe to previous subscriptions
	if len(c.subscriptions) > 0 {
		return c.SubscribeOrderBook(c.subscriptions)
	}

	log.Printf("Reconnected to %s successfully", c.exchangeName)
	return nil
}

// UpdateOrderBook updates the order book with new data (common logic)
func (c *BaseWebSocketClient) UpdateOrderBook(symbol string, bids, asks map[string]string, version string, timestamp int64, isSnapshot bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	orderBook, exists := c.orderBooks[symbol]
	if !exists {
		return
	}

	orderBook.LastUpdate = timestamp
	orderBook.Version = version

	if isSnapshot {
		// Replace entire order book
		orderBook.Bids = make(map[string]string)
		orderBook.Asks = make(map[string]string)
	}

	// Update bids
	for price, qty := range bids {
		if qty == "0" || qty == "0.00000000" {
			delete(orderBook.Bids, price)
		} else {
			orderBook.Bids[price] = qty
		}
	}

	// Update asks
	for price, qty := range asks {
		if qty == "0" || qty == "0.00000000" {
			delete(orderBook.Asks, price)
		} else {
			orderBook.Asks[price] = qty
		}
	}

	// Notify callback
	if c.updateCallback != nil {
		copy := c.createOrderBookCopy(orderBook)
		c.updateCallback(copy)
	}
}

// createOrderBookCopy creates a copy of order book data for callbacks
func (c *BaseWebSocketClient) createOrderBookCopy(orderBook *OrderBookData) *OrderBookData {
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
func (c *BaseWebSocketClient) GetOrderBookData(symbol string) (*OrderBookData, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	orderBook, exists := c.orderBooks[symbol]
	if !exists {
		return nil, fmt.Errorf("order book not found for symbol: %s", symbol)
	}

	return c.createOrderBookCopy(orderBook), nil
}

// GetBestPrice returns the best bid and ask prices
func (c *BaseWebSocketClient) GetBestPrice(symbol string) (bestBid, bestAsk float64, err error) {
	orderBook, err := c.GetOrderBookData(symbol)
	if err != nil {
		return 0, 0, err
	}

	return GetBestPriceFromData(orderBook)
}

// InitializeOrderBook initializes an order book for a symbol
func (c *BaseWebSocketClient) InitializeOrderBook(symbol string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.orderBooks[symbol] = &OrderBookData{
		Symbol:   symbol,
		Bids:     make(map[string]string),
		Asks:     make(map[string]string),
		Exchange: c.exchangeName,
	}
}

// SubscribeOrderBook is implemented by each exchange
func (c *BaseWebSocketClient) SubscribeOrderBook(symbols []string) error {
	if c.subscribeHandler != nil {
		return c.subscribeHandler(symbols)
	}
	return fmt.Errorf("subscribe handler not implemented for %s", c.exchangeName)
}

// Helper function to get best prices from order book data (moved from mexc.go)
func GetBestPriceFromData(orderBook *OrderBookData) (bestBid, bestAsk float64, err error) {
	var highestBid, lowestAsk float64 = 0, 1e10

	// Find highest bid
	for priceStr := range orderBook.Bids {
		if price, parseErr := parseFloat(priceStr); parseErr == nil {
			if price > highestBid {
				highestBid = price
			}
		}
	}

	// Find lowest ask
	for priceStr := range orderBook.Asks {
		if price, parseErr := parseFloat(priceStr); parseErr == nil {
			if price < lowestAsk {
				lowestAsk = price
			}
		}
	}

	if highestBid == 0 || lowestAsk == 1e10 {
		return 0, 0, fmt.Errorf("insufficient order book data")
	}

	return highestBid, lowestAsk, nil
}

// parseFloat is a helper function to parse float64 from string
func parseFloat(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}

	return strconv.ParseFloat(s, 64)
}
