package client

import (
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ============================================================================
// Common Types and Data Structures
// ============================================================================

// OrderBookData represents raw order book data from an exchange
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

// ============================================================================
// Common Interface and Base Structs
// ============================================================================

// WebSocketClient defines the standard interface for all exchange WebSocket clients
type WebSocketClient interface {
	// Connection management
	Connect() error
	Close() error
	Reconnect() error
	IsConnected() bool

	// Subscription management
	Subscribe(symbols []string) error
	Unsubscribe(symbols []string) error
	GetSubscriptions() []string

	// Data access
	GetOrderBookData(symbol string) (*OrderBookData, error)
	GetBestPrice(symbol string) (bestBid, bestAsk float64, err error)

	// Exchange identification
	GetExchangeName() string

	// Lifecycle management
	StartReconnectHandler()
	SetUpdateCallback(callback OrderBookUpdateCallback)
}

// ExchangeConfig holds exchange-specific configuration
type ExchangeConfig struct {
	Name           string
	WebSocketURL   string
	PingInterval   time.Duration
	ReconnectDelay time.Duration
	MaxRetries     int
}

// ExchangeImplementation defines methods that each exchange must implement
type ExchangeImplementation interface {
	// Message handling
	HandleMessage(messageType int, message []byte) error

	// Subscription message creation
	CreateSubscriptionMessage(symbols []string) (interface{}, error)
	CreateUnsubscriptionMessage(symbols []string) (interface{}, error)
	CreatePingMessage() (interface{}, error)

	// Symbol normalization
	NormalizeSymbol(symbol string) string
	DenormalizeSymbol(exchangeSymbol string) string

	// Order book processing
	ProcessOrderBookUpdate(symbol string, data interface{}) (*OrderBookData, error)
}

// BaseWebSocketClient contains common functionality for all exchange clients
type BaseWebSocketClient struct {
	// Connection management
	conn        *websocket.Conn
	isConnected bool
	mutex       sync.RWMutex

	// Reconnection handling
	reconnectCh chan bool

	// Order book storage
	orderBooks map[string]*OrderBookData

	// Subscription tracking
	subscriptions []string

	// Callback handling
	updateCallback OrderBookUpdateCallback

	// Keep-alive mechanism
	pingTicker *time.Ticker

	// Exchange-specific configuration
	config *ExchangeConfig

	// Abstract methods that must be implemented by specific exchanges
	exchangeImpl ExchangeImplementation
}

// ============================================================================
// Utility Functions
// ============================================================================

// GetBestPriceFromData returns the best bid and ask prices from order book data
func GetBestPriceFromData(orderBook *OrderBookData) (bestBid, bestAsk float64, err error) {
	var highestBid, lowestAsk float64 = 0, 1e10 // Use a large number for initial lowestAsk

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
	// Simple string to float conversion for price strings
	var result float64
	_, err := fmt.Sscanf(s, "%f", &result)
	return result, err
}

// ============================================================================
// Base Client Implementation
// ============================================================================

// NewBaseWebSocketClient creates a new base WebSocket client
func NewBaseWebSocketClient(config *ExchangeConfig, impl ExchangeImplementation, callback OrderBookUpdateCallback) *BaseWebSocketClient {
	return &BaseWebSocketClient{
		orderBooks:     make(map[string]*OrderBookData),
		reconnectCh:    make(chan bool, 1),
		subscriptions:  make([]string, 0),
		updateCallback: callback,
		config:         config,
		exchangeImpl:   impl,
	}
}

// Connect establishes WebSocket connection
func (c *BaseWebSocketClient) Connect() error {
	u, err := url.Parse(c.config.WebSocketURL)
	if err != nil {
		return fmt.Errorf("invalid WebSocket URL: %w", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to connect to %s WebSocket: %w", c.config.Name, err)
	}

	c.mutex.Lock()
	c.conn = conn
	c.isConnected = true
	c.mutex.Unlock()

	// Start ping mechanism
	c.startPing()

	// Start message handler
	go c.handleMessages()

	log.Printf("Connected to %s WebSocket: %s", c.config.Name, u.String())
	return nil
}

// startPing starts the ping mechanism to keep connection alive
func (c *BaseWebSocketClient) startPing() {
	c.pingTicker = time.NewTicker(c.config.PingInterval)
	go func() {
		for range c.pingTicker.C {
			if !c.IsConnected() {
				return
			}

			pingMsg, err := c.exchangeImpl.CreatePingMessage()
			if err != nil {
				log.Printf("Failed to create ping message for %s: %v", c.config.Name, err)
				continue
			}

			c.mutex.RLock()
			conn := c.conn
			c.mutex.RUnlock()

			if conn != nil {
				if err := conn.WriteJSON(pingMsg); err != nil {
					log.Printf("Failed to send ping to %s: %v", c.config.Name, err)
					c.reconnectCh <- true
					return
				}
			}
		}
	}()
}

// Subscribe subscribes to WebSocket channels for given symbols
func (c *BaseWebSocketClient) Subscribe(symbols []string) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected to %s WebSocket", c.config.Name)
	}

	// Initialize order books for new symbols
	c.mutex.Lock()
	for _, symbol := range symbols {
		if _, exists := c.orderBooks[symbol]; !exists {
			c.orderBooks[symbol] = &OrderBookData{
				Symbol:   symbol,
				Bids:     make(map[string]string),
				Asks:     make(map[string]string),
				Exchange: c.config.Name,
			}
		}
	}
	c.mutex.Unlock()

	// Create subscription message
	subMsg, err := c.exchangeImpl.CreateSubscriptionMessage(symbols)
	if err != nil {
		return fmt.Errorf("failed to create subscription message: %w", err)
	}

	// Send subscription
	c.mutex.RLock()
	conn := c.conn
	c.mutex.RUnlock()

	if conn != nil {
		if err := conn.WriteJSON(subMsg); err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", c.config.Name, err)
		}
	}

	// Track subscriptions
	c.mutex.Lock()
	c.subscriptions = append(c.subscriptions, symbols...)
	c.mutex.Unlock()

	log.Printf("Subscribed to %s symbols: %v", c.config.Name, symbols)
	return nil
}

// Unsubscribe unsubscribes from WebSocket channels for given symbols
func (c *BaseWebSocketClient) Unsubscribe(symbols []string) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected to %s WebSocket", c.config.Name)
	}

	unsubMsg, err := c.exchangeImpl.CreateUnsubscriptionMessage(symbols)
	if err != nil {
		return fmt.Errorf("failed to create unsubscription message: %w", err)
	}

	c.mutex.RLock()
	conn := c.conn
	c.mutex.RUnlock()

	if conn != nil {
		if err := conn.WriteJSON(unsubMsg); err != nil {
			return fmt.Errorf("failed to unsubscribe from %s: %w", c.config.Name, err)
		}
	}

	// Remove from subscriptions
	c.mutex.Lock()
	newSubs := make([]string, 0)
	for _, existing := range c.subscriptions {
		found := false
		for _, toRemove := range symbols {
			if existing == toRemove {
				found = true
				break
			}
		}
		if !found {
			newSubs = append(newSubs, existing)
		}
	}
	c.subscriptions = newSubs
	c.mutex.Unlock()

	log.Printf("Unsubscribed from %s symbols: %v", c.config.Name, symbols)
	return nil
}

// handleMessages handles incoming WebSocket messages
func (c *BaseWebSocketClient) handleMessages() {
	defer func() {
		c.mutex.Lock()
		c.isConnected = false
		c.mutex.Unlock()

		if c.pingTicker != nil {
			c.pingTicker.Stop()
		}
	}()

	for {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			break
		}

		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("%s WebSocket read error: %v", c.config.Name, err)
			c.reconnectCh <- true
			return
		}

		// Delegate message handling to exchange implementation
		if err := c.exchangeImpl.HandleMessage(messageType, message); err != nil {
			log.Printf("Error handling %s message: %v", c.config.Name, err)
		}
	}
}

// GetOrderBookData returns the current order book data for a symbol
func (c *BaseWebSocketClient) GetOrderBookData(symbol string) (*OrderBookData, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	orderBook, exists := c.orderBooks[symbol]
	if !exists {
		return nil, fmt.Errorf("order book not found for symbol: %s", symbol)
	}

	// Return a copy to avoid race conditions
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

// IsConnected returns the connection status
func (c *BaseWebSocketClient) IsConnected() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.isConnected
}

// GetSubscriptions returns the current subscriptions
func (c *BaseWebSocketClient) GetSubscriptions() []string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return append([]string(nil), c.subscriptions...)
}

// GetExchangeName returns the exchange name
func (c *BaseWebSocketClient) GetExchangeName() string {
	return c.config.Name
}

// SetUpdateCallback sets the order book update callback
func (c *BaseWebSocketClient) SetUpdateCallback(callback OrderBookUpdateCallback) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.updateCallback = callback
}

// Close closes the WebSocket connection
func (c *BaseWebSocketClient) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

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
func (c *BaseWebSocketClient) Reconnect() error {
	log.Printf("Attempting to reconnect to %s...", c.config.Name)

	c.Close()
	time.Sleep(c.config.ReconnectDelay)

	if err := c.Connect(); err != nil {
		return err
	}

	// Resubscribe to previous symbols
	subscriptions := c.GetSubscriptions()
	if len(subscriptions) > 0 {
		if err := c.Subscribe(subscriptions); err != nil {
			return err
		}
	}

	log.Printf("Reconnected to %s successfully", c.config.Name)
	return nil
}

// StartReconnectHandler starts the reconnection handler
func (c *BaseWebSocketClient) StartReconnectHandler() {
	go func() {
		for range c.reconnectCh {
			retries := 0
			for !c.IsConnected() && retries < c.config.MaxRetries {
				if err := c.Reconnect(); err != nil {
					retries++
					delay := time.Duration(retries) * c.config.ReconnectDelay
					log.Printf("%s reconnection failed (attempt %d/%d): %v, retrying in %v...",
						c.config.Name, retries, c.config.MaxRetries, err, delay)
					time.Sleep(delay)
					continue
				}
				retries = 0
				break
			}
		}
	}()
}

// NotifyOrderBookUpdate notifies the callback of an order book update
func (c *BaseWebSocketClient) NotifyOrderBookUpdate(symbol string, orderBook *OrderBookData) {
	c.mutex.RLock()
	callback := c.updateCallback
	c.mutex.RUnlock()

	if callback != nil {
		copy := c.createOrderBookCopy(orderBook)
		callback(copy)
	}
}

// UpdateOrderBook updates the order book data for a symbol
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
		// Replace entire order book for snapshots
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
	c.NotifyOrderBookUpdate(symbol, orderBook)
}
