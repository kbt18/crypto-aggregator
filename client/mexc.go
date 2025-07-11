// mexc_client.go
package client

import (
	"encoding/json"
	"fmt"
	"log"
	pb "order-book-aggregator/proto"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
)

// MEXC-specific message types
type WSRequest struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
}

type WSResponse struct {
	ID   int    `json:"id"`
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// MEXCWebSocketClient wraps the base client with MEXC-specific functionality
type MEXCWebSocketClient struct {
	*BaseWebSocketClient
}

// NewMEXCWebSocketClient creates a new MEXC WebSocket client
func NewMEXCWebSocketClient(callback OrderBookUpdateCallback) *MEXCWebSocketClient {
	config := ExchangeConfig{
		WSHost:       "wbs.mexc.com",
		WSPath:       "/ws",
		PingMethod:   "PING",
		PingInterval: 30 * time.Second,
	}

	base := NewBaseWebSocketClient("MEXC", config, callback)

	mexc := &MEXCWebSocketClient{
		BaseWebSocketClient: base,
	}

	// Set MEXC-specific handlers
	base.messageHandler = mexc.handleMessage
	base.subscribeHandler = mexc.subscribeOrderBook
	base.reconnectHandler = mexc.reconnectHandler

	return mexc
}

// handleMessage processes MEXC-specific messages
func (c *MEXCWebSocketClient) handleMessage(message []byte) {
	// Check if it's a binary (protobuf) message
	if c.isProtobufMessage(message) {
		c.handleProtobufMessage(message)
	} else {
		c.handleControlMessage(message)
	}
}

// isProtobufMessage checks if the message is likely protobuf
func (c *MEXCWebSocketClient) isProtobufMessage(message []byte) bool {
	// Simple heuristic: if it's not valid JSON, assume it's protobuf
	var temp interface{}
	return json.Unmarshal(message, &temp) != nil
}

// handleControlMessage processes JSON control messages
func (c *MEXCWebSocketClient) handleControlMessage(message []byte) {
	var response WSResponse
	if err := json.Unmarshal(message, &response); err == nil && response.Msg != "" {
		if response.Msg == "PONG" {
			return
		}
		if strings.Contains(response.Msg, "successful") || strings.Contains(response.Msg, "Blocked") {
			log.Printf("MEXC subscription status: %s", response.Msg)
		} else {
			log.Printf("MEXC response: %+v", response)
		}
		return
	}
	log.Printf("Unknown MEXC control message: %s", string(message))
}

// handleProtobufMessage processes protobuf market data messages
func (c *MEXCWebSocketClient) handleProtobufMessage(message []byte) {
	var wrapper pb.PushDataV3ApiWrapper
	if err := proto.Unmarshal(message, &wrapper); err != nil {
		log.Printf("Failed to unmarshal MEXC protobuf: %v", err)
		return
	}

	symbol := wrapper.GetSymbol()
	sendTime := wrapper.GetSendTime()

	if sendTime == 0 {
		sendTime = time.Now().UnixMilli()
	}

	// Handle different message types
	if increaseDepths := wrapper.GetPublicIncreaseDepths(); increaseDepths != nil {
		c.handleIncrementalDepth(increaseDepths, symbol, sendTime)
	} else if limitDepths := wrapper.GetPublicLimitDepths(); limitDepths != nil {
		c.handleLimitDepth(limitDepths, symbol, sendTime)
	}
	// Add other handlers as needed...
}

// handleIncrementalDepth processes incremental depth updates
func (c *MEXCWebSocketClient) handleIncrementalDepth(depthData *pb.PublicIncreaseDepthsV3Api, symbol string, sendTime int64) {
	bids := make(map[string]string)
	asks := make(map[string]string)

	for _, bid := range depthData.GetBids() {
		bids[bid.GetPrice()] = bid.GetQuantity()
	}

	for _, ask := range depthData.GetAsks() {
		asks[ask.GetPrice()] = ask.GetQuantity()
	}

	version := depthData.GetVersion()
	c.UpdateOrderBook(symbol, bids, asks, version, sendTime, false)
}

// handleLimitDepth processes snapshot depth updates
func (c *MEXCWebSocketClient) handleLimitDepth(depthData *pb.PublicLimitDepthsV3Api, symbol string, sendTime int64) {
	bids := make(map[string]string)
	asks := make(map[string]string)

	for _, bid := range depthData.GetBids() {
		bids[bid.GetPrice()] = bid.GetQuantity()
	}

	for _, ask := range depthData.GetAsks() {
		asks[ask.GetPrice()] = ask.GetQuantity()
	}

	version := depthData.GetVersion()
	c.UpdateOrderBook(symbol, bids, asks, version, sendTime, true) // true = snapshot
}

// subscribeOrderBook implements MEXC-specific subscription logic
func (c *MEXCWebSocketClient) subscribeOrderBook(symbols []string) error {
	for i, symbol := range symbols {
		if err := c.subscribeToSymbol(symbol, "100ms"); err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", symbol, err)
		}

		c.InitializeOrderBook(symbol)

		if i < len(symbols)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	c.subscriptions = append(c.subscriptions, symbols...)
	return nil
}

// subscribeToSymbol subscribes to a single symbol
func (c *MEXCWebSocketClient) subscribeToSymbol(symbol string, updateSpeed string) error {
	channels := []string{
		fmt.Sprintf("spot@public.aggre.depth.v3.api.pb@%s@%s", updateSpeed, strings.ToUpper(symbol)),
		fmt.Sprintf("spot@public.limit.depth.v3.api.pb@%s@20", strings.ToUpper(symbol)),
	}

	req := WSRequest{
		Method: "SUBSCRIPTION",
		Params: channels,
	}

	return c.conn.WriteJSON(req)
}

// reconnectHandler handles MEXC-specific reconnection
func (c *MEXCWebSocketClient) reconnectHandler() error {
	if len(c.subscriptions) > 0 {
		return c.subscribeOrderBook(c.subscriptions)
	}
	return nil
}

// SubscribeMultipleOrderBooks provides the original interface
func (c *MEXCWebSocketClient) SubscribeMultipleOrderBooks(symbols []string, updateSpeed string) error {
	return c.SubscribeOrderBook(symbols)
}
