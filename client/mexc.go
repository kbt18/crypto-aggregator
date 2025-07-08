package client

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "order-book-aggregator/proto"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

// WebSocket message types for subscription/control (still JSON)
type WSRequest struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
}

type WSResponse struct {
	ID   int    `json:"id"`
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// OrderBookData represents raw order book data from the exchange
type OrderBookData struct {
	Symbol     string
	Bids       map[string]string // price -> quantity
	Asks       map[string]string // price -> quantity
	LastUpdate int64
	Version    string
	Exchange   string // "MEXC"
}

// OrderBookUpdateCallback is a function type for handling order book updates
type OrderBookUpdateCallback func(data *OrderBookData)

// MEXCWebSocketClient represents the WebSocket client
type MEXCWebSocketClient struct {
	conn           *websocket.Conn
	orderBooks     map[string]*OrderBookData
	mutex          sync.RWMutex
	pingTicker     *time.Ticker
	reconnectCh    chan bool
	isConnected    bool
	subscriptions  []string
	updateCallback OrderBookUpdateCallback
}

// NewMEXCWebSocketClient creates a new WebSocket client
func NewMEXCWebSocketClient(callback OrderBookUpdateCallback) *MEXCWebSocketClient {
	return &MEXCWebSocketClient{
		orderBooks:     make(map[string]*OrderBookData),
		reconnectCh:    make(chan bool, 1),
		subscriptions:  make([]string, 0),
		updateCallback: callback,
	}
}

// Connect establishes WebSocket connection
func (c *MEXCWebSocketClient) Connect() error {
	u := url.URL{Scheme: "wss", Host: "wbs.mexc.com", Path: "/ws"}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	c.conn = conn
	c.isConnected = true

	// Start ping mechanism
	c.startPing()

	// Start message handler
	go c.handleMessages()

	log.Printf("Connected to MEXC WebSocket: %s", u.String())
	return nil
}

// startPing starts the ping mechanism to keep connection alive
func (c *MEXCWebSocketClient) startPing() {
	c.pingTicker = time.NewTicker(30 * time.Second)
	go func() {
		for range c.pingTicker.C {
			if !c.isConnected {
				return
			}

			ping := WSRequest{
				Method: "PING",
			}

			if err := c.conn.WriteJSON(ping); err != nil {
				log.Printf("Failed to send ping: %v", err)
				c.reconnectCh <- true
				return
			}
		}
	}()
}

// Subscribe subscribes to WebSocket channels
func (c *MEXCWebSocketClient) Subscribe(channels []string) error {
	if !c.isConnected {
		return fmt.Errorf("not connected to WebSocket")
	}

	req := WSRequest{
		Method: "SUBSCRIPTION",
		Params: channels,
	}

	if err := c.conn.WriteJSON(req); err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	c.subscriptions = append(c.subscriptions, channels...)
	log.Printf("Subscribed to channels: %v", channels)
	return nil
}

// SubscribeOrderBook subscribes to order book updates for a symbol
func (c *MEXCWebSocketClient) SubscribeOrderBook(symbol string, updateSpeed string) error {
	// Available update speeds: "100ms" or "10ms"
	if updateSpeed == "" {
		updateSpeed = "100ms"
	}

	// Subscribe to protobuf channels
	channels := []string{
		fmt.Sprintf("spot@public.aggre.depth.v3.api.pb@%s@%s", updateSpeed, strings.ToUpper(symbol)),
		fmt.Sprintf("spot@public.limit.depth.v3.api.pb@%s@20", strings.ToUpper(symbol)),
		fmt.Sprintf("spot@public.aggre.bookTicker.v3.api.pb@%s@%s", updateSpeed, strings.ToUpper(symbol)),
	}

	// Initialize order book
	c.mutex.Lock()
	c.orderBooks[symbol] = &OrderBookData{
		Symbol:   symbol,
		Bids:     make(map[string]string),
		Asks:     make(map[string]string),
		Exchange: "MEXC",
	}
	c.mutex.Unlock()

	return c.Subscribe(channels)
}

// SubscribeMultipleOrderBooks subscribes to multiple symbols with delays
func (c *MEXCWebSocketClient) SubscribeMultipleOrderBooks(symbols []string, updateSpeed string) error {
	for i, symbol := range symbols {
		if err := c.SubscribeOrderBook(symbol, updateSpeed); err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", symbol, err)
		}

		// Add delay between subscriptions to avoid rate limiting
		if i < len(symbols)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	return nil
}

// handleMessages handles incoming WebSocket messages
func (c *MEXCWebSocketClient) handleMessages() {
	defer func() {
		c.isConnected = false
		if c.pingTicker != nil {
			c.pingTicker.Stop()
		}
	}()

	for {
		messageType, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("Read error: %v", err)
			c.reconnectCh <- true
			return
		}

		// Handle different message types
		switch messageType {
		case websocket.TextMessage:
			c.handleControlMessage(message)
		case websocket.BinaryMessage:
			c.handleProtobufMessage(message)
		default:
			log.Printf("Unknown message type: %d", messageType)
		}
	}
}

// handleControlMessage processes JSON control messages (subscriptions, pings, etc.)
func (c *MEXCWebSocketClient) handleControlMessage(message []byte) {
	var response WSResponse
	if err := json.Unmarshal(message, &response); err == nil && response.Msg != "" {
		if response.Msg == "PONG" {
			return // Ignore pong responses
		}
		// Log subscription responses
		if strings.Contains(response.Msg, "successful") || strings.Contains(response.Msg, "Blocked") {
			log.Printf("WebSocket subscription status: %s", response.Msg)
		} else {
			log.Printf("WebSocket response: %+v", response)
		}
		return
	}

	log.Printf("Unknown control message: %s", string(message))
}

// handleProtobufMessage processes protobuf market data messages
func (c *MEXCWebSocketClient) handleProtobufMessage(message []byte) {
	// Parse the main wrapper
	var wrapper pb.PushDataV3ApiWrapper
	if err := proto.Unmarshal(message, &wrapper); err != nil {
		log.Printf("Failed to unmarshal protobuf wrapper: %v", err)
		return
	}

	// Extract basic info
	channel := wrapper.GetChannel()
	symbol := wrapper.GetSymbol()     // This might be empty for some messages
	sendTime := wrapper.GetSendTime() // This might be 0 for some messages

	// Use current time if sendTime is not provided
	if sendTime == 0 {
		sendTime = time.Now().UnixMilli()
	}

	log.Printf("Received protobuf message on channel: %s, symbol: %s", channel, symbol)

	// Handle different message types based on what's available in the wrapper
	if increaseDepths := wrapper.GetPublicIncreaseDepths(); increaseDepths != nil {
		c.handleIncrementalDepth(increaseDepths, symbol, sendTime)
	} else if limitDepths := wrapper.GetPublicLimitDepths(); limitDepths != nil {
		c.handleLimitDepth(limitDepths, symbol, sendTime)
	} else if bookTicker := wrapper.GetPublicBookTicker(); bookTicker != nil {
		c.handleBookTicker(bookTicker, symbol, sendTime)
	} else if aggreDepths := wrapper.GetPublicAggreDepths(); aggreDepths != nil {
		c.handleAggreDepths(aggreDepths, symbol, sendTime)
	} else if aggreBookTicker := wrapper.GetPublicAggreBookTicker(); aggreBookTicker != nil {
		c.handleAggreBookTicker(aggreBookTicker, symbol, sendTime)
	} else {
		log.Printf("Unhandled protobuf message type for channel: %s", channel)
	}
}

// handleIncrementalDepth processes incremental depth updates
func (c *MEXCWebSocketClient) handleIncrementalDepth(depthData *pb.PublicIncreaseDepthsV3Api, symbol string, sendTime int64) {
	if depthData == nil {
		return
	}

	bids := make(map[string]string)
	asks := make(map[string]string)

	// Process bids
	for _, bid := range depthData.GetBids() {
		bids[bid.GetPrice()] = bid.GetQuantity()
	}

	// Process asks
	for _, ask := range depthData.GetAsks() {
		asks[ask.GetPrice()] = ask.GetQuantity()
	}

	version := depthData.GetVersion()
	log.Printf("Incremental depth update for %s: %d bids, %d asks, version: %s", symbol, len(bids), len(asks), version)

	c.processOrderBookUpdate(symbol, bids, asks, version, sendTime)
}

// handleLimitDepth processes snapshot depth updates
func (c *MEXCWebSocketClient) handleLimitDepth(depthData *pb.PublicLimitDepthsV3Api, symbol string, sendTime int64) {
	if depthData == nil {
		return
	}

	bids := make(map[string]string)
	asks := make(map[string]string)

	// Process bids (replace all)
	for _, bid := range depthData.GetBids() {
		bids[bid.GetPrice()] = bid.GetQuantity()
	}

	// Process asks (replace all)
	for _, ask := range depthData.GetAsks() {
		asks[ask.GetPrice()] = ask.GetQuantity()
	}

	version := depthData.GetVersion()
	log.Printf("Limit depth snapshot for %s: %d bids, %d asks, version: %s", symbol, len(bids), len(asks), version)

	// For limit depth, we replace the entire order book
	c.processOrderBookSnapshot(symbol, bids, asks, version, sendTime)
}

// handleBookTicker processes best bid/ask updates
func (c *MEXCWebSocketClient) handleBookTicker(tickerData *pb.PublicBookTickerV3Api, symbol string, sendTime int64) {
	if tickerData == nil {
		return
	}

	// Update best bid/ask (you could store this separately if needed)
	log.Printf("Best prices for %s - Bid: %s@%s, Ask: %s@%s",
		symbol,
		tickerData.GetBidPrice(), tickerData.GetBidQuantity(),
		tickerData.GetAskPrice(), tickerData.GetAskQuantity())
}

// handleAggreDepths processes aggregate depth updates
func (c *MEXCWebSocketClient) handleAggreDepths(depthData *pb.PublicAggreDepthsV3Api, symbol string, sendTime int64) {
	// This would be similar to handleIncrementalDepth but for aggregated data
	// Implementation depends on the structure of PublicAggreDepthsV3Api
	log.Printf("Received aggregate depths for %s", symbol)
}

// handleAggreBookTicker processes aggregate book ticker updates
func (c *MEXCWebSocketClient) handleAggreBookTicker(tickerData *pb.PublicAggreBookTickerV3Api, symbol string, sendTime int64) {
	if tickerData == nil {
		return
	}

	// Update best bid/ask from aggregate book ticker
	log.Printf("Aggregate best prices for %s - Bid: %s@%s, Ask: %s@%s",
		symbol,
		tickerData.GetBidPrice(), tickerData.GetBidQuantity(),
		tickerData.GetAskPrice(), tickerData.GetAskQuantity())
}

// processOrderBookSnapshot replaces the entire order book (for limit depth)
func (c *MEXCWebSocketClient) processOrderBookSnapshot(symbol string, bids, asks map[string]string, version string, timestamp int64) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	orderBook, exists := c.orderBooks[symbol]
	if !exists {
		return
	}

	orderBook.LastUpdate = timestamp
	orderBook.Version = version

	// Replace entire order book
	orderBook.Bids = make(map[string]string)
	orderBook.Asks = make(map[string]string)

	// Set new bids
	for price, qty := range bids {
		if qty != "0" && qty != "0.00000000" {
			orderBook.Bids[price] = qty
		}
	}

	// Set new asks
	for price, qty := range asks {
		if qty != "0" && qty != "0.00000000" {
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
func (c *MEXCWebSocketClient) createOrderBookCopy(orderBook *OrderBookData) *OrderBookData {
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
func (c *MEXCWebSocketClient) GetOrderBookData(symbol string) (*OrderBookData, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	orderBook, exists := c.orderBooks[symbol]
	if !exists {
		return nil, fmt.Errorf("order book not found for symbol: %s", symbol)
	}

	// Return a copy to avoid race conditions
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

	return copy, nil
}

// GetBestPrice returns the best bid and ask prices from raw order book data
func (c *MEXCWebSocketClient) GetBestPrice(symbol string) (bestBid, bestAsk float64, err error) {
	orderBook, err := c.GetOrderBookData(symbol)
	if err != nil {
		return 0, 0, err
	}

	return GetBestPriceFromData(orderBook)
}

// Helper function to get best prices from order book data
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
	return strconv.ParseFloat(s, 64)
}

// Close closes the WebSocket connection
func (c *MEXCWebSocketClient) Close() error {
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
func (c *MEXCWebSocketClient) Reconnect() error {
	log.Println("Attempting to reconnect...")

	c.Close()
	time.Sleep(5 * time.Second)

	if err := c.Connect(); err != nil {
		return err
	}

	// Resubscribe to previous channels
	if len(c.subscriptions) > 0 {
		if err := c.Subscribe(c.subscriptions); err != nil {
			return err
		}
	}

	log.Println("Reconnected successfully")
	return nil
}

// StartReconnectHandler starts the reconnection handler
func (c *MEXCWebSocketClient) StartReconnectHandler() {
	go func() {
		for range c.reconnectCh {
			for !c.isConnected {
				if err := c.Reconnect(); err != nil {
					log.Printf("Reconnection failed: %v, retrying in 10 seconds...", err)
					time.Sleep(10 * time.Second)
					continue
				}
				break
			}
		}
	}()
}

// GetExchangeName returns the exchange name
func (c *MEXCWebSocketClient) GetExchangeName() string {
	return "MEXC"
}

// processOrderBookUpdate processes incremental order book updates
func (c *MEXCWebSocketClient) processOrderBookUpdate(symbol string, bids, asks map[string]string, version string, timestamp int64) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	orderBook, exists := c.orderBooks[symbol]
	if !exists {
		return
	}

	orderBook.LastUpdate = timestamp
	orderBook.Version = version

	// Update bids (incremental)
	for price, qty := range bids {
		if qty == "0" || qty == "0.00000000" {
			delete(orderBook.Bids, price)
		} else {
			orderBook.Bids[price] = qty
		}
	}

	// Update asks (incremental)
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
