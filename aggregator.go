package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"order-book-aggregator/client"
	"order-book-aggregator/orderbook"
	"order-book-aggregator/restapi"
)

// Global variables for monitoring
var (
	startTime        = time.Now()
	updateCounter    int64
	performanceStats = make(map[string]*int64)
	statsMutex       sync.RWMutex
)

// ClientConfig holds configuration for exchange clients
type ClientConfig struct {
	Name         string
	CreateClient func(callback client.OrderBookUpdateCallback) client.WebSocketClient
	ConnectDelay time.Duration
	Symbols      []string
	FailOnError  bool
	Enabled      bool
}

func main() {
	log.Println("Starting Order Book Aggregator...")

	// Create order book aggregator
	aggregator := orderbook.NewOrderBookAggregator()

	// Add main callback to handle order book updates
	aggregator.AddCallback(func(symbol string, orderBook *orderbook.OrderBook) {
		atomic.AddInt64(&updateCounter, 1)
		log.Printf("Order book updated for %s from exchanges: %v", symbol, orderBook.Sources)

		// Broadcast to all WebSocket clients
		restapi.BroadcastOrderBookUpdate(orderBook)
	})

	// Set up advanced monitoring
	setupAdvancedMonitoring(aggregator)

	// Symbols to track
	symbols := []string{"BTCUSDT", "ETHUSDT", "ADAUSDT", "SOLUSDT", "DOTUSDT"}
	log.Printf("Will subscribe to symbols: %v", symbols)

	// Create exchange callback factory
	createExchangeCallback := func(exchangeName string) client.OrderBookUpdateCallback {
		return func(data *client.OrderBookData) {
			// Convert client data to aggregator format
			exchangeBook := &orderbook.ExchangeOrderBook{
				Symbol:     data.Symbol,
				Exchange:   data.Exchange,
				Bids:       data.Bids,
				Asks:       data.Asks,
				LastUpdate: data.LastUpdate,
				Version:    data.Version,
			}

			// Update the aggregator
			aggregator.UpdateOrderBook(exchangeName, exchangeBook)

			// Update performance stats safely
			updatePerformanceStats(exchangeName)
		}
	}

	// Configure exchange clients
	exchangeConfigs := []ClientConfig{
		{
			Name:    "Kraken",
			Enabled: true,
			Symbols: symbols,
			CreateClient: func(callback client.OrderBookUpdateCallback) client.WebSocketClient {
				return client.NewKrakenWebSocketClient(callback)
			},
			ConnectDelay: 2 * time.Second,
			FailOnError:  false,
		},
		{
			Name:    "MEXC",
			Enabled: true,
			Symbols: symbols,
			CreateClient: func(callback client.OrderBookUpdateCallback) client.WebSocketClient {
				return client.NewMEXCWebSocketClient(callback)
			},
			ConnectDelay: 0,
			FailOnError:  false,
		},
		// Future exchanges can be added here
		// {
		//     Name:     "Binance",
		//     Enabled:  false, // Enable when implemented
		//     Symbols:  symbols,
		//     CreateClient: func(callback client.OrderBookUpdateCallback) client.WebSocketClient {
		//         return client.NewBinanceWebSocketClient(callback)
		//     },
		//     ConnectDelay: 1 * time.Second,
		//     FailOnError:  false,
		// },
	}

	// Connect and subscribe to all exchanges
	connectedClients := connectToExchanges(exchangeConfigs, createExchangeCallback)
	defer closeAllClients(connectedClients)

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Set up monitoring tickers
	displayTicker := time.NewTicker(15 * time.Second)
	defer displayTicker.Stop()

	statsTicker := time.NewTicker(60 * time.Second)
	defer statsTicker.Stop()

	healthTicker := time.NewTicker(30 * time.Second)
	defer healthTicker.Stop()

	log.Println("Order Book Aggregator is running. Press Ctrl+C to exit.")
	fmt.Println(strings.Repeat("=", 80))

	// Start HTTP server in goroutine
	go func() {
		log.Println("Starting HTTP/WebSocket server...")
		restapi.SetupHTTPServer(aggregator)
	}()

	// Start performance monitoring
	startPerformanceMonitoring()

	// Main event loop
	for {
		select {
		case <-displayTicker.C:
			displayOrderBooks(aggregator, symbols)

		case <-statsTicker.C:
			displayDetailedStatistics(aggregator, symbols, connectedClients)

		case <-healthTicker.C:
			performHealthCheck(connectedClients)

		case sig := <-sigChan:
			log.Printf("Received signal %v, shutting down gracefully...", sig)
			return
		}
	}
}

// connectToExchanges handles connection and subscription for all exchange clients
func connectToExchanges(configs []ClientConfig, callbackFactory func(string) client.OrderBookUpdateCallback) []client.WebSocketClient {
	var connectedClients []client.WebSocketClient

	for _, config := range configs {
		if !config.Enabled {
			log.Printf("Skipping disabled exchange: %s", config.Name)
			continue
		}

		// Add connection delay if specified
		if config.ConnectDelay > 0 {
			log.Printf("Waiting %v before connecting to %s...", config.ConnectDelay, config.Name)
			time.Sleep(config.ConnectDelay)
		}

		log.Printf("Connecting to %s WebSocket...", config.Name)

		// Create client with callback
		callback := callbackFactory(config.Name)
		exchangeClient := config.CreateClient(callback)

		// Connect to exchange
		if err := exchangeClient.Connect(); err != nil {
			log.Printf("Failed to connect to %s: %v", config.Name, err)
			if config.FailOnError {
				log.Fatalf("Critical connection failure for %s", config.Name)
			}
			continue
		}

		// Start reconnect handler
		exchangeClient.StartReconnectHandler()

		// Subscribe to order books
		if err := exchangeClient.Subscribe(config.Symbols); err != nil {
			log.Printf("Failed to subscribe to %s order books: %v", config.Name, err)
			if config.FailOnError {
				log.Fatalf("Critical subscription failure for %s", config.Name)
			}
			// Close the client if subscription fails
			exchangeClient.Close()
			continue
		}

		log.Printf("Successfully connected and subscribed to %s for symbols: %v",
			config.Name, config.Symbols)
		connectedClients = append(connectedClients, exchangeClient)

		// Initialize performance stats
		initPerformanceStats(config.Name)
	}

	log.Printf("Connected to %d exchanges", len(connectedClients))
	return connectedClients
}

// closeAllClients closes all connected exchange clients
func closeAllClients(clients []client.WebSocketClient) {
	log.Println("Closing all exchange connections...")
	for _, exchangeClient := range clients {
		exchangeName := exchangeClient.GetExchangeName()
		log.Printf("Closing connection to %s...", exchangeName)
		if err := exchangeClient.Close(); err != nil {
			log.Printf("Error closing %s client: %v", exchangeName, err)
		}
	}
}

// displayOrderBooks shows the current state of all order books
func displayOrderBooks(aggregator *orderbook.OrderBookAggregator, symbols []string) {
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Printf("ORDER BOOKS - %s\n", time.Now().Format("15:04:05"))
	fmt.Println(strings.Repeat("=", 80))

	for _, symbol := range symbols {
		orderBook := aggregator.GetOrderBook(symbol)
		if orderBook == nil {
			fmt.Printf("%-10s: No data available\n", symbol)
			continue
		}

		bestBid, bestAsk, spread, err := orderBook.GetBestPrice()
		if err != nil {
			fmt.Printf("%-10s: Error getting best prices: %v\n", symbol, err)
			continue
		}

		midPrice := (bestBid + bestAsk) / 2
		spreadPercent := (spread / midPrice) * 100

		// Get order book depth
		bids, asks := orderBook.GetDepth(5)
		bidDepth := len(bids)
		askDepth := len(asks)

		fmt.Printf("%-10s | Bid: $%10.2f | Ask: $%10.2f | Spread: $%8.2f (%.4f%%) | Depth: %d/%d | Sources: %v | Age: %s\n",
			symbol, bestBid, bestAsk, spread, spreadPercent, bidDepth, askDepth,
			orderBook.Sources, time.Since(orderBook.LastUpdate).Round(time.Second))
	}
}

// displayDetailedStatistics shows comprehensive statistics
func displayDetailedStatistics(aggregator *orderbook.OrderBookAggregator, symbols []string, clients []client.WebSocketClient) {
	fmt.Println("\n" + strings.Repeat("-", 80))
	fmt.Printf("DETAILED STATISTICS - %s\n", time.Now().Format("15:04:05"))
	fmt.Println(strings.Repeat("-", 80))

	// Basic stats
	totalSymbols := len(symbols)
	activeSymbols := 0
	totalSources := make(map[string]bool)
	stalestData := time.Time{}
	freshestData := time.Now()

	for _, symbol := range symbols {
		orderBook := aggregator.GetOrderBook(symbol)
		if orderBook != nil {
			activeSymbols++
			for _, source := range orderBook.Sources {
				totalSources[source] = true
			}

			if orderBook.LastUpdate.After(stalestData) {
				stalestData = orderBook.LastUpdate
			}
			if orderBook.LastUpdate.Before(freshestData) {
				freshestData = orderBook.LastUpdate
			}
		}
	}

	fmt.Printf("System Status:\n")
	fmt.Printf("  Total Symbols: %d\n", totalSymbols)
	fmt.Printf("  Active Symbols: %d\n", activeSymbols)
	fmt.Printf("  Connected Exchanges: %d (%v)\n", len(totalSources), getKeys(totalSources))
	fmt.Printf("  Total Updates: %d\n", atomic.LoadInt64(&updateCounter))
	fmt.Printf("  Uptime: %s\n", time.Since(startTime).Round(time.Second))

	if !stalestData.IsZero() {
		fmt.Printf("  Data Freshness: %s (oldest) to %s (newest)\n",
			time.Since(freshestData).Round(time.Second),
			time.Since(stalestData).Round(time.Second))
	}

	// Exchange-specific performance
	fmt.Printf("\nExchange Performance:\n")
	for _, exchangeClient := range clients {
		exchangeName := exchangeClient.GetExchangeName()
		updates := getPerformanceStats(exchangeName)
		status := "CONNECTED"
		if !exchangeClient.IsConnected() {
			status = "DISCONNECTED"
		}

		subscriptions := exchangeClient.GetSubscriptions()
		fmt.Printf("  %-10s: %s | Updates: %6d | Subscriptions: %d\n",
			exchangeName, status, updates, len(subscriptions))
	}

	// Market overview
	fmt.Printf("\nMarket Overview:\n")
	var totalVolume float64
	var avgSpreadPercent float64
	validSpreads := 0

	for _, symbol := range symbols {
		orderBook := aggregator.GetOrderBook(symbol)
		if orderBook != nil {
			bestBid, bestAsk, spread, err := orderBook.GetBestPrice()
			if err == nil {
				midPrice := (bestBid + bestAsk) / 2
				spreadPercent := (spread / midPrice) * 100
				avgSpreadPercent += spreadPercent
				validSpreads++

				// Calculate volume (top 5 levels)
				bids, asks := orderBook.GetDepth(5)
				var bidVolume, askVolume float64
				for _, bid := range bids {
					bidVolume += bid.Quantity * bid.Price
				}
				for _, ask := range asks {
					askVolume += ask.Quantity * ask.Price
				}
				totalVolume += (bidVolume + askVolume) / 2
			}
		}
	}

	if validSpreads > 0 {
		avgSpreadPercent /= float64(validSpreads)
		fmt.Printf("  Average Spread: %.4f%%\n", avgSpreadPercent)
		fmt.Printf("  Total Book Value: $%.2f\n", totalVolume)
	}
}

// performHealthCheck checks the health of all connections
func performHealthCheck(clients []client.WebSocketClient) {
	disconnectedCount := 0

	for _, exchangeClient := range clients {
		if !exchangeClient.IsConnected() {
			disconnectedCount++
			log.Printf("HEALTH: %s is disconnected", exchangeClient.GetExchangeName())
		}
	}

	if disconnectedCount > 0 {
		log.Printf("HEALTH WARNING: %d/%d exchanges disconnected", disconnectedCount, len(clients))
	} else if len(clients) > 0 {
		log.Printf("HEALTH OK: All %d exchanges connected", len(clients))
	}
}

// setupAdvancedMonitoring sets up monitoring for specific conditions
func setupAdvancedMonitoring(aggregator *orderbook.OrderBookAggregator) {
	// Alert when spread is unusually wide
	aggregator.AddCallback(func(symbol string, orderBook *orderbook.OrderBook) {
		bestBid, bestAsk, spread, err := orderBook.GetBestPrice()
		if err != nil {
			return
		}

		midPrice := (bestBid + bestAsk) / 2
		spreadPercent := (spread / midPrice) * 100

		// Alert if spread is greater than 0.1%
		if spreadPercent > 0.1 {
			log.Printf("ALERT: Wide spread detected for %s: %.4f%% ($%.2f) from %v",
				symbol, spreadPercent, spread, orderBook.Sources)
		}
	})

	// Monitor for arbitrage opportunities
	aggregator.AddCallback(func(symbol string, orderBook *orderbook.OrderBook) {
		if len(orderBook.Sources) > 1 {
			// Could implement cross-exchange arbitrage detection here
			// log.Printf("ARBITRAGE: Multiple sources available for %s: %v", symbol, orderBook.Sources)
		}
	})

	// Monitor data staleness
	aggregator.AddCallback(func(symbol string, orderBook *orderbook.OrderBook) {
		age := time.Since(orderBook.LastUpdate)
		if age > 30*time.Second {
			log.Printf("STALE DATA: %s data is %s old from %v",
				symbol, age.Round(time.Second), orderBook.Sources)
		}
	})
}

// startPerformanceMonitoring monitors system performance
func startPerformanceMonitoring() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		var lastTotalUpdates int64

		for range ticker.C {
			currentTotal := atomic.LoadInt64(&updateCounter)
			updatesPerMinute := currentTotal - lastTotalUpdates
			lastTotalUpdates = currentTotal

			if updatesPerMinute > 0 {
				log.Printf("PERFORMANCE: %d total updates (%d/min)", currentTotal, updatesPerMinute)
			}
		}
	}()
}

// getKeys returns keys from a map as a slice
func getKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// Performance stats helper functions
func initPerformanceStats(exchangeName string) {
	statsMutex.Lock()
	defer statsMutex.Unlock()
	counter := int64(0)
	performanceStats[exchangeName] = &counter
}

func updatePerformanceStats(exchangeName string) {
	statsMutex.RLock()
	counter, exists := performanceStats[exchangeName]
	statsMutex.RUnlock()

	if exists && counter != nil {
		atomic.AddInt64(counter, 1)
	}
}

func getPerformanceStats(exchangeName string) int64 {
	statsMutex.RLock()
	counter, exists := performanceStats[exchangeName]
	statsMutex.RUnlock()

	if exists && counter != nil {
		return atomic.LoadInt64(counter)
	}
	return 0
}

// addExchange demonstrates how to add a new exchange at runtime
func addExchange(aggregator *orderbook.OrderBookAggregator, exchangeName string, symbols []string) {
	log.Printf("Adding new exchange: %s", exchangeName)

	// This is an example of how you could add exchanges dynamically
	// In practice, you'd need to implement the specific exchange client

	/*
		callback := func(data *client.OrderBookData) {
			exchangeBook := &orderbook.ExchangeOrderBook{
				Symbol:     data.Symbol,
				Exchange:   data.Exchange,
				Bids:       data.Bids,
				Asks:       data.Asks,
				LastUpdate: data.LastUpdate,
				Version:    data.Version,
			}
			aggregator.UpdateOrderBook(exchangeName, exchangeBook)
		}

		// Create and connect new client
		newClient := client.NewExchangeWebSocketClient(exchangeName, callback)
		if err := newClient.Connect(); err != nil {
			log.Printf("Failed to connect to %s: %v", exchangeName, err)
			return
		}

		newClient.StartReconnectHandler()

		if err := newClient.Subscribe(symbols); err != nil {
			log.Printf("Failed to subscribe to %s: %v", exchangeName, err)
			newClient.Close()
			return
		}

		log.Printf("Successfully added %s exchange", exchangeName)
	*/
}
