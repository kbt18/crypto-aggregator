package orderbook

import (
	"testing"
	"time"
)

// Helper function to create test exchange order book data
func createTestExchangeOrderBook(exchange, symbol string, bids, asks map[string]string) *ExchangeOrderBook {
	return &ExchangeOrderBook{
		Symbol:     symbol,
		Exchange:   exchange,
		Bids:       bids,
		Asks:       asks,
		LastUpdate: time.Now().UnixMilli(),
		Version:    "1.0",
	}
}

// Helper function to create test aggregator with sample data
func createTestAggregator() *OrderBookAggregator {
	oba := NewOrderBookAggregator()

	// Add MEXC order book
	mexcBids := map[string]string{
		"50000.00": "1.0",
		"49999.00": "2.0",
		"49998.00": "1.5",
	}
	mexcAsks := map[string]string{
		"50001.00": "1.2",
		"50002.00": "2.1",
		"50003.00": "1.8",
	}
	mexcBook := createTestExchangeOrderBook("MEXC", "BTCUSDT", mexcBids, mexcAsks)

	// Add Kraken order book
	krakenBids := map[string]string{
		"49999.50": "0.8",
		"49998.50": "1.2",
		"49997.50": "2.0",
	}
	krakenAsks := map[string]string{
		"50001.50": "0.9",
		"50002.50": "1.5",
		"50003.50": "2.2",
	}
	krakenBook := createTestExchangeOrderBook("Kraken", "BTCUSDT", krakenBids, krakenAsks)

	oba.UpdateOrderBook("MEXC", mexcBook)
	oba.UpdateOrderBook("Kraken", krakenBook)

	return oba
}

func TestNewOrderBookAggregator(t *testing.T) {
	oba := NewOrderBookAggregator()

	if oba == nil {
		t.Fatal("NewOrderBookAggregator returned nil")
	}

	if oba.orderBooks == nil {
		t.Error("orderBooks map not initialized")
	}

	if oba.exchanges == nil {
		t.Error("exchanges map not initialized")
	}

	if oba.callbacks == nil {
		t.Error("callbacks slice not initialized")
	}

	if len(oba.callbacks) != 0 {
		t.Error("callbacks slice should be empty initially")
	}
}

func TestAddCallback(t *testing.T) {
	oba := NewOrderBookAggregator()

	callbackCount := 0
	callback := func(symbol string, orderBook *OrderBook) {
		callbackCount++
	}

	oba.AddCallback(callback)

	if len(oba.callbacks) != 1 {
		t.Errorf("Expected 1 callback, got %d", len(oba.callbacks))
	}

	// Add second callback
	oba.AddCallback(callback)

	if len(oba.callbacks) != 2 {
		t.Errorf("Expected 2 callbacks, got %d", len(oba.callbacks))
	}
}

func TestUpdateOrderBook(t *testing.T) {
	oba := NewOrderBookAggregator()

	bids := map[string]string{
		"50000.00": "1.0",
		"49999.00": "2.0",
	}
	asks := map[string]string{
		"50001.00": "1.5",
		"50002.00": "2.5",
	}

	exchangeBook := createTestExchangeOrderBook("TestExchange", "BTCUSDT", bids, asks)

	// Test callback execution
	callbackExecuted := false
	var callbackSymbol string
	var callbackOrderBook *OrderBook

	oba.AddCallback(func(symbol string, orderBook *OrderBook) {
		callbackExecuted = true
		callbackSymbol = symbol
		callbackOrderBook = orderBook
	})

	oba.UpdateOrderBook("TestExchange", exchangeBook)

	// Give callback time to execute (it runs in goroutine)
	time.Sleep(10 * time.Millisecond)

	if !callbackExecuted {
		t.Error("Callback was not executed")
	}

	if callbackSymbol != "BTCUSDT" {
		t.Errorf("Expected callback symbol BTCUSDT, got %s", callbackSymbol)
	}

	if callbackOrderBook == nil {
		t.Error("Callback order book is nil")
	}

	// Check if exchange order book was stored
	if oba.exchanges["TestExchange"] == nil {
		t.Error("Exchange map not created for TestExchange")
	}

	if oba.exchanges["TestExchange"]["BTCUSDT"] == nil {
		t.Error("Order book not stored for BTCUSDT")
	}

	// Check if aggregated order book was created
	aggregated := oba.GetOrderBook("BTCUSDT")
	if aggregated == nil {
		t.Error("Aggregated order book not created")
	}

	if len(aggregated.Sources) != 1 || aggregated.Sources[0] != "TestExchange" {
		t.Errorf("Expected sources [TestExchange], got %v", aggregated.Sources)
	}
}

func TestAggregateOrderBook(t *testing.T) {
	oba := createTestAggregator()

	orderBook := oba.GetOrderBook("BTCUSDT")
	if orderBook == nil {
		t.Fatal("Aggregated order book is nil")
	}

	// Check sources
	expectedSources := map[string]bool{"MEXC": true, "Kraken": true}
	if len(orderBook.Sources) != 2 {
		t.Errorf("Expected 2 sources, got %d", len(orderBook.Sources))
	}

	for _, source := range orderBook.Sources {
		if !expectedSources[source] {
			t.Errorf("Unexpected source: %s", source)
		}
	}

	// Check that bids are sorted highest to lowest
	if len(orderBook.Bids) < 2 {
		t.Fatal("Not enough bids to test sorting")
	}

	for i := 1; i < len(orderBook.Bids); i++ {
		if orderBook.Bids[i-1].Price < orderBook.Bids[i].Price {
			t.Error("Bids are not sorted highest to lowest")
		}
	}

	// Check that asks are sorted lowest to highest
	if len(orderBook.Asks) < 2 {
		t.Fatal("Not enough asks to test sorting")
	}

	for i := 1; i < len(orderBook.Asks); i++ {
		if orderBook.Asks[i-1].Price > orderBook.Asks[i].Price {
			t.Error("Asks are not sorted lowest to highest")
		}
	}

	// Test quantity aggregation - prices that exist in both exchanges should have combined quantities
	// MEXC has 50000.00: 1.0, Kraken doesn't have this price, so quantity should be 1.0
	found := false
	for _, bid := range orderBook.Bids {
		if bid.Price == 50000.00 {
			found = true
			if bid.Quantity != 1.0 {
				t.Errorf("Expected quantity 1.0 for price 50000.00, got %f", bid.Quantity)
			}
			break
		}
	}
	if !found {
		t.Error("Bid price 50000.00 not found in aggregated order book")
	}
}

func TestGetOrderBook(t *testing.T) {
	oba := createTestAggregator()

	// Test existing symbol
	orderBook := oba.GetOrderBook("BTCUSDT")
	if orderBook == nil {
		t.Error("GetOrderBook returned nil for existing symbol")
	}

	if orderBook.Symbol != "BTCUSDT" {
		t.Errorf("Expected symbol BTCUSDT, got %s", orderBook.Symbol)
	}

	// Test non-existing symbol
	nonExistentBook := oba.GetOrderBook("ETHUSDT")
	if nonExistentBook != nil {
		t.Error("GetOrderBook should return nil for non-existent symbol")
	}

	// Test that returned order book is a copy (mutations shouldn't affect original)
	originalBidsCount := len(orderBook.Bids)
	orderBook.Bids = append(orderBook.Bids, OrderBookEntry{Price: 1000, Quantity: 1})

	newOrderBook := oba.GetOrderBook("BTCUSDT")
	if len(newOrderBook.Bids) != originalBidsCount {
		t.Error("GetOrderBook doesn't return a proper copy")
	}
}

func TestOrderBook_GetBestPrice(t *testing.T) {
	oba := createTestAggregator()
	orderBook := oba.GetOrderBook("BTCUSDT")

	bestBid, bestAsk, spread, err := orderBook.GetBestPrice()
	if err != nil {
		t.Fatalf("GetBestPrice returned error: %v", err)
	}

	// Best bid should be the highest bid price
	expectedBestBid := orderBook.Bids[0].Price
	if bestBid != expectedBestBid {
		t.Errorf("Expected best bid %f, got %f", expectedBestBid, bestBid)
	}

	// Best ask should be the lowest ask price
	expectedBestAsk := orderBook.Asks[0].Price
	if bestAsk != expectedBestAsk {
		t.Errorf("Expected best ask %f, got %f", expectedBestAsk, bestAsk)
	}

	// Spread should be ask - bid
	expectedSpread := bestAsk - bestBid
	if spread != expectedSpread {
		t.Errorf("Expected spread %f, got %f", expectedSpread, spread)
	}

	// Test empty order book
	emptyBook := &OrderBook{
		Symbol: "EMPTY",
		Bids:   []OrderBookEntry{},
		Asks:   []OrderBookEntry{},
	}

	_, _, _, err = emptyBook.GetBestPrice()
	if err == nil {
		t.Error("GetBestPrice should return error for empty order book")
	}
}

func TestOrderBook_GetSpread(t *testing.T) {
	oba := createTestAggregator()
	orderBook := oba.GetOrderBook("BTCUSDT")

	spread := orderBook.GetSpread()

	bestBid, bestAsk, _, err := orderBook.GetBestPrice()
	if err != nil {
		t.Fatalf("GetBestPrice failed: %v", err)
	}

	expectedSpread := bestAsk - bestBid
	if spread != expectedSpread {
		t.Errorf("Expected spread %f, got %f", expectedSpread, spread)
	}

	// Test empty order book
	emptyBook := &OrderBook{
		Symbol: "EMPTY",
		Bids:   []OrderBookEntry{},
		Asks:   []OrderBookEntry{},
	}

	spread = emptyBook.GetSpread()
	if spread != 0 {
		t.Errorf("Expected spread 0 for empty book, got %f", spread)
	}
}

func TestOrderBook_GetDepth(t *testing.T) {
	oba := createTestAggregator()
	orderBook := oba.GetOrderBook("BTCUSDT")

	// Test getting depth of 2
	bids, asks := orderBook.GetDepth(2)

	if len(bids) > 2 {
		t.Errorf("Expected max 2 bids, got %d", len(bids))
	}

	if len(asks) > 2 {
		t.Errorf("Expected max 2 asks, got %d", len(asks))
	}

	// Test getting depth larger than available
	totalBids := len(orderBook.Bids)
	totalAsks := len(orderBook.Asks)

	largeBids, largeAsks := orderBook.GetDepth(1000)

	if len(largeBids) != totalBids {
		t.Errorf("Expected %d bids, got %d", totalBids, len(largeBids))
	}

	if len(largeAsks) != totalAsks {
		t.Errorf("Expected %d asks, got %d", totalAsks, len(largeAsks))
	}

	// Test zero depth
	zeroBids, zeroAsks := orderBook.GetDepth(0)

	if len(zeroBids) != 0 {
		t.Errorf("Expected 0 bids for zero depth, got %d", len(zeroBids))
	}

	if len(zeroAsks) != 0 {
		t.Errorf("Expected 0 asks for zero depth, got %d", len(zeroAsks))
	}
}

func TestOrderBook_GetVolumeAtPrice(t *testing.T) {
	oba := createTestAggregator()
	orderBook := oba.GetOrderBook("BTCUSDT")

	// Test buy side - should look at asks at or below the price
	buyVolume := orderBook.GetVolumeAtPrice(50002.00, "BUY")

	// Should include asks at 50001.00, 50001.50, and 50002.00
	expectedVolume := 0.0
	for _, ask := range orderBook.Asks {
		if ask.Price <= 50002.00 {
			expectedVolume += ask.Quantity
		}
	}

	if buyVolume != expectedVolume {
		t.Errorf("Expected buy volume %f, got %f", expectedVolume, buyVolume)
	}

	// Test sell side - should look at bids at or above the price
	sellVolume := orderBook.GetVolumeAtPrice(49999.00, "SELL")

	expectedVolume = 0.0
	for _, bid := range orderBook.Bids {
		if bid.Price >= 49999.00 {
			expectedVolume += bid.Quantity
		}
	}

	if sellVolume != expectedVolume {
		t.Errorf("Expected sell volume %f, got %f", expectedVolume, sellVolume)
	}

	// Test with case insensitive side parameter
	bidVolume := orderBook.GetVolumeAtPrice(49999.00, "ask")
	askVolume := orderBook.GetVolumeAtPrice(50002.00, "bid")

	if bidVolume != sellVolume {
		t.Errorf("Expected bid volume %f, got %f", sellVolume, bidVolume)
	}

	if askVolume != buyVolume {
		t.Errorf("Expected ask volume %f, got %f", buyVolume, askVolume)
	}
}

func TestOrderBook_GetMidPrice(t *testing.T) {
	oba := createTestAggregator()
	orderBook := oba.GetOrderBook("BTCUSDT")

	midPrice, err := orderBook.GetMidPrice()
	if err != nil {
		t.Fatalf("GetMidPrice returned error: %v", err)
	}

	bestBid, bestAsk, _, err := orderBook.GetBestPrice()
	if err != nil {
		t.Fatalf("GetBestPrice failed: %v", err)
	}

	expectedMidPrice := (bestBid + bestAsk) / 2
	if midPrice != expectedMidPrice {
		t.Errorf("Expected mid price %f, got %f", expectedMidPrice, midPrice)
	}

	// Test empty order book
	emptyBook := &OrderBook{
		Symbol: "EMPTY",
		Bids:   []OrderBookEntry{},
		Asks:   []OrderBookEntry{},
	}

	_, err = emptyBook.GetMidPrice()
	if err == nil {
		t.Error("GetMidPrice should return error for empty order book")
	}
}

func TestOrderBook_IsStale(t *testing.T) {
	// Create order book with current time
	orderBook := &OrderBook{
		Symbol:     "BTCUSDT",
		LastUpdate: time.Now(),
	}

	// Should not be stale for 1 second max age
	if orderBook.IsStale(1 * time.Second) {
		t.Error("Order book should not be stale")
	}

	// Create order book with old timestamp
	oldOrderBook := &OrderBook{
		Symbol:     "BTCUSDT",
		LastUpdate: time.Now().Add(-2 * time.Hour),
	}

	// Should be stale for 1 hour max age
	if !oldOrderBook.IsStale(1 * time.Hour) {
		t.Error("Order book should be stale")
	}
}

func TestOrderBookEntry(t *testing.T) {
	entry := OrderBookEntry{
		Price:    50000.0,
		Quantity: 1.5,
	}

	if entry.Price != 50000.0 {
		t.Errorf("Expected price 50000.0, got %f", entry.Price)
	}

	if entry.Quantity != 1.5 {
		t.Errorf("Expected quantity 1.5, got %f", entry.Quantity)
	}
}

func TestExchangeOrderBook(t *testing.T) {
	bids := map[string]string{"50000": "1.0"}
	asks := map[string]string{"50001": "1.5"}
	timestamp := time.Now().UnixMilli()

	exchangeBook := &ExchangeOrderBook{
		Symbol:     "BTCUSDT",
		Exchange:   "TestExchange",
		Bids:       bids,
		Asks:       asks,
		LastUpdate: timestamp,
		Version:    "1.0",
	}

	if exchangeBook.Symbol != "BTCUSDT" {
		t.Errorf("Expected symbol BTCUSDT, got %s", exchangeBook.Symbol)
	}

	if exchangeBook.Exchange != "TestExchange" {
		t.Errorf("Expected exchange TestExchange, got %s", exchangeBook.Exchange)
	}

	if exchangeBook.LastUpdate != timestamp {
		t.Errorf("Expected timestamp %d, got %d", timestamp, exchangeBook.LastUpdate)
	}
}

// Benchmark tests
func BenchmarkUpdateOrderBook(b *testing.B) {
	oba := NewOrderBookAggregator()

	bids := map[string]string{
		"50000.00": "1.0",
		"49999.00": "2.0",
		"49998.00": "1.5",
	}
	asks := map[string]string{
		"50001.00": "1.2",
		"50002.00": "2.1",
		"50003.00": "1.8",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exchangeBook := createTestExchangeOrderBook("BenchExchange", "BTCUSDT", bids, asks)
		oba.UpdateOrderBook("BenchExchange", exchangeBook)
	}
}

func BenchmarkGetBestPrice(b *testing.B) {
	oba := createTestAggregator()
	orderBook := oba.GetOrderBook("BTCUSDT")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = orderBook.GetBestPrice()
	}
}

func BenchmarkGetDepth(b *testing.B) {
	oba := createTestAggregator()
	orderBook := oba.GetOrderBook("BTCUSDT")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = orderBook.GetDepth(10)
	}
}
