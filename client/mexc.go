package client

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	pb "order-book-aggregator/proto"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

// ============================================================================
// MEXC-Specific Implementation
// ============================================================================

// MEXCWebSocketClient wraps the base client with MEXC-specific functionality
type MEXCWebSocketClient struct {
	*BaseWebSocketClient
	updateSpeed string
}

// MEXCImplementation implements the ExchangeImplementation interface for MEXC
type MEXCImplementation struct {
	client *MEXCWebSocketClient
}

// NewMEXCWebSocketClient creates a new MEXC WebSocket client
func NewMEXCWebSocketClient(callback OrderBookUpdateCallback) WebSocketClient {
	config := &ExchangeConfig{
		Name:           "MEXC",
		WebSocketURL:   "wss://wbs.mexc.com/ws",
		PingInterval:   30 * time.Second,
		ReconnectDelay: 5 * time.Second,
		MaxRetries:     10,
	}

	mexcClient := &MEXCWebSocketClient{
		updateSpeed: "100ms",
	}

	impl := &MEXCImplementation{
		client: mexcClient,
	}

	baseClient := NewBaseWebSocketClient(config, impl, callback)
	mexcClient.BaseWebSocketClient = baseClient

	return mexcClient
}

// SetUpdateSpeed sets the update speed for MEXC subscriptions
func (c *MEXCWebSocketClient) SetUpdateSpeed(speed string) {
	c.updateSpeed = speed
}

// SubscribeMultipleOrderBooks subscribes to multiple order books (MEXC-specific method)
func (c *MEXCWebSocketClient) SubscribeMultipleOrderBooks(symbols []string, updateSpeed string) error {
	c.updateSpeed = updateSpeed
	return c.Subscribe(symbols)
}

// CreateSubscriptionMessage creates MEXC subscription message
func (impl *MEXCImplementation) CreateSubscriptionMessage(symbols []string) (interface{}, error) {
	var channels []string

	for _, symbol := range symbols {
		normalizedSymbol := impl.NormalizeSymbol(symbol)
		updateSpeed := impl.client.updateSpeed

		channels = append(channels,
			fmt.Sprintf("spot@public.aggre.depth.v3.api.pb@%s@%s", updateSpeed, normalizedSymbol),
			fmt.Sprintf("spot@public.limit.depth.v3.api.pb@%s@20", normalizedSymbol),
			fmt.Sprintf("spot@public.aggre.bookTicker.v3.api.pb@%s@%s", updateSpeed, normalizedSymbol),
		)
	}

	return WSRequest{
		Method: "SUBSCRIPTION",
		Params: channels,
	}, nil
}

// CreateUnsubscriptionMessage creates MEXC unsubscription message
func (impl *MEXCImplementation) CreateUnsubscriptionMessage(symbols []string) (interface{}, error) {
	var channels []string

	for _, symbol := range symbols {
		normalizedSymbol := impl.NormalizeSymbol(symbol)
		updateSpeed := impl.client.updateSpeed

		channels = append(channels,
			fmt.Sprintf("spot@public.aggre.depth.v3.api.pb@%s@%s", updateSpeed, normalizedSymbol),
			fmt.Sprintf("spot@public.limit.depth.v3.api.pb@%s@20", normalizedSymbol),
			fmt.Sprintf("spot@public.aggre.bookTicker.v3.api.pb@%s@%s", updateSpeed, normalizedSymbol),
		)
	}

	return WSRequest{
		Method: "UNSUBSCRIPTION",
		Params: channels,
	}, nil
}

// CreatePingMessage creates MEXC ping message
func (impl *MEXCImplementation) CreatePingMessage() (interface{}, error) {
	return WSRequest{
		Method: "PING",
	}, nil
}

// NormalizeSymbol normalizes symbol for MEXC (uppercase)
func (impl *MEXCImplementation) NormalizeSymbol(symbol string) string {
	return strings.ToUpper(symbol)
}

// DenormalizeSymbol converts MEXC symbol back to standard format
func (impl *MEXCImplementation) DenormalizeSymbol(exchangeSymbol string) string {
	return strings.ToUpper(exchangeSymbol)
}

// HandleMessage handles incoming messages from MEXC
func (impl *MEXCImplementation) HandleMessage(messageType int, message []byte) error {
	switch messageType {
	case websocket.TextMessage:
		return impl.handleControlMessage(message)
	case websocket.BinaryMessage:
		return impl.handleProtobufMessage(message)
	default:
		log.Printf("Unknown MEXC message type: %d", messageType)
		return nil
	}
}

// handleControlMessage processes JSON control messages
func (impl *MEXCImplementation) handleControlMessage(message []byte) error {
	var response WSResponse
	if err := json.Unmarshal(message, &response); err == nil && response.Msg != "" {
		if response.Msg == "PONG" {
			return nil // Ignore pong responses
		}
		// Log subscription responses
		if strings.Contains(response.Msg, "successful") || strings.Contains(response.Msg, "Blocked") {
			log.Printf("MEXC WebSocket subscription status: %s", response.Msg)
		} else {
			log.Printf("MEXC WebSocket response: %+v", response)
		}
		return nil
	}

	log.Printf("Unknown MEXC control message: %s", string(message))
	return nil
}

// handleProtobufMessage processes protobuf market data messages
func (impl *MEXCImplementation) handleProtobufMessage(message []byte) error {
	var wrapper pb.PushDataV3ApiWrapper
	if err := proto.Unmarshal(message, &wrapper); err != nil {
		return fmt.Errorf("failed to unmarshal MEXC protobuf wrapper: %w", err)
	}

	symbol := wrapper.GetSymbol()
	sendTime := wrapper.GetSendTime()

	if sendTime == 0 {
		sendTime = time.Now().UnixMilli()
	}

	log.Printf("Received MEXC protobuf message for symbol: %s", symbol)

	// Handle different message types
	if increaseDepths := wrapper.GetPublicIncreaseDepths(); increaseDepths != nil {
		return impl.handleIncrementalDepth(increaseDepths, symbol, sendTime)
	} else if limitDepths := wrapper.GetPublicLimitDepths(); limitDepths != nil {
		return impl.handleLimitDepth(limitDepths, symbol, sendTime)
	} else if bookTicker := wrapper.GetPublicBookTicker(); bookTicker != nil {
		return impl.handleBookTicker(bookTicker, symbol, sendTime)
	} else if aggreDepths := wrapper.GetPublicAggreDepths(); aggreDepths != nil {
		return impl.handleAggreDepths(aggreDepths, symbol, sendTime)
	} else if aggreBookTicker := wrapper.GetPublicAggreBookTicker(); aggreBookTicker != nil {
		return impl.handleAggreBookTicker(aggreBookTicker, symbol, sendTime)
	}

	return nil
}

// handleIncrementalDepth processes incremental depth updates
func (impl *MEXCImplementation) handleIncrementalDepth(depthData *pb.PublicIncreaseDepthsV3Api, symbol string, sendTime int64) error {
	if depthData == nil {
		return nil
	}

	bids := make(map[string]string)
	asks := make(map[string]string)

	for _, bid := range depthData.GetBids() {
		bids[bid.GetPrice()] = bid.GetQuantity()
	}

	for _, ask := range depthData.GetAsks() {
		asks[ask.GetPrice()] = ask.GetQuantity()
	}

	version := depthData.GetVersion()
	log.Printf("MEXC incremental depth update for %s: %d bids, %d asks, version: %s", symbol, len(bids), len(asks), version)

	impl.client.UpdateOrderBook(symbol, bids, asks, version, sendTime, false)
	return nil
}

// handleLimitDepth processes snapshot depth updates
func (impl *MEXCImplementation) handleLimitDepth(depthData *pb.PublicLimitDepthsV3Api, symbol string, sendTime int64) error {
	if depthData == nil {
		return nil
	}

	bids := make(map[string]string)
	asks := make(map[string]string)

	for _, bid := range depthData.GetBids() {
		bids[bid.GetPrice()] = bid.GetQuantity()
	}

	for _, ask := range depthData.GetAsks() {
		asks[ask.GetPrice()] = ask.GetQuantity()
	}

	version := depthData.GetVersion()
	log.Printf("MEXC limit depth snapshot for %s: %d bids, %d asks, version: %s", symbol, len(bids), len(asks), version)

	impl.client.UpdateOrderBook(symbol, bids, asks, version, sendTime, true)
	return nil
}

// handleBookTicker processes best bid/ask updates
func (impl *MEXCImplementation) handleBookTicker(tickerData *pb.PublicBookTickerV3Api, symbol string, sendTime int64) error {
	if tickerData == nil {
		return nil
	}

	log.Printf("MEXC best prices for %s - Bid: %s@%s, Ask: %s@%s",
		symbol,
		tickerData.GetBidPrice(), tickerData.GetBidQuantity(),
		tickerData.GetAskPrice(), tickerData.GetAskQuantity())

	return nil
}

// handleAggreDepths processes aggregate depth updates
func (impl *MEXCImplementation) handleAggreDepths(depthData *pb.PublicAggreDepthsV3Api, symbol string, sendTime int64) error {
	log.Printf("Received MEXC aggregate depths for %s", symbol)
	return nil
}

// handleAggreBookTicker processes aggregate book ticker updates
func (impl *MEXCImplementation) handleAggreBookTicker(tickerData *pb.PublicAggreBookTickerV3Api, symbol string, sendTime int64) error {
	if tickerData == nil {
		return nil
	}

	log.Printf("MEXC aggregate best prices for %s - Bid: %s@%s, Ask: %s@%s",
		symbol,
		tickerData.GetBidPrice(), tickerData.GetBidQuantity(),
		tickerData.GetAskPrice(), tickerData.GetAskQuantity())

	return nil
}

// ProcessOrderBookUpdate processes order book update data
func (impl *MEXCImplementation) ProcessOrderBookUpdate(symbol string, data interface{}) (*OrderBookData, error) {
	// This method can be used for additional processing if needed
	// For now, the processing is handled in the specific message handlers
	return nil, nil
}

// ============================================================================
// Additional Interface Methods for MEXC
// ============================================================================

// Unsubscribe implements the WebSocketClient interface
func (c *MEXCWebSocketClient) Unsubscribe(symbols []string) error {
	return c.BaseWebSocketClient.Unsubscribe(symbols)
}

// GetSubscriptions implements the WebSocketClient interface
func (c *MEXCWebSocketClient) GetSubscriptions() []string {
	return c.BaseWebSocketClient.GetSubscriptions()
}

// GetOrderBookData implements the WebSocketClient interface
func (c *MEXCWebSocketClient) GetOrderBookData(symbol string) (*OrderBookData, error) {
	return c.BaseWebSocketClient.GetOrderBookData(symbol)
}

// GetBestPrice implements the WebSocketClient interface
func (c *MEXCWebSocketClient) GetBestPrice(symbol string) (bestBid, bestAsk float64, err error) {
	return c.BaseWebSocketClient.GetBestPrice(symbol)
}

// GetExchangeName implements the WebSocketClient interface
func (c *MEXCWebSocketClient) GetExchangeName() string {
	return c.BaseWebSocketClient.GetExchangeName()
}

// IsConnected implements the WebSocketClient interface
func (c *MEXCWebSocketClient) IsConnected() bool {
	return c.BaseWebSocketClient.IsConnected()
}

// SetUpdateCallback implements the WebSocketClient interface
func (c *MEXCWebSocketClient) SetUpdateCallback(callback OrderBookUpdateCallback) {
	c.BaseWebSocketClient.SetUpdateCallback(callback)
}

// Connect implements the WebSocketClient interface
func (c *MEXCWebSocketClient) Connect() error {
	return c.BaseWebSocketClient.Connect()
}

// Close implements the WebSocketClient interface
func (c *MEXCWebSocketClient) Close() error {
	return c.BaseWebSocketClient.Close()
}

// Reconnect implements the WebSocketClient interface
func (c *MEXCWebSocketClient) Reconnect() error {
	return c.BaseWebSocketClient.Reconnect()
}

// StartReconnectHandler implements the WebSocketClient interface
func (c *MEXCWebSocketClient) StartReconnectHandler() {
	c.BaseWebSocketClient.StartReconnectHandler()
}

// Subscribe implements the WebSocketClient interface
func (c *MEXCWebSocketClient) Subscribe(symbols []string) error {
	return c.BaseWebSocketClient.Subscribe(symbols)
}

// ============================================================================
// Legacy types for backward compatibility
// ============================================================================

// WSRequest represents WebSocket subscription/control messages (still JSON)
type WSRequest struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
}

// WSResponse represents WebSocket response messages
type WSResponse struct {
	ID   int    `json:"id"`
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}
