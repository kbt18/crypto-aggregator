package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"order-book-aggregator/client"
	"order-book-aggregator/orderbook"
	"order-book-aggregator/webapi"
)

// Global variable to track start time
var startTime = time.Now()

// ExchangeClient interface for common client operations
type ExchangeClient interface {
	Connect() error
	Close() error
	StartReconnectHandler()
	GetExchangeName() string
}

// ClientConfig holds configuration for exchange clients
type ClientConfig struct {
	Name          string
	Client        ExchangeClient
	ConnectDelay  time.Duration
	SubscribeFunc func() error
	FailOnError   bool
}

func main() {
	log.Println("Starting Order Book Aggregator...")

	// Create order book aggregator
	aggregator := orderbook.NewOrderBookAggregator()

	// Add main callback to handle order book updates
	aggregator.AddCallback(func(symbol string, orderBook *orderbook.OrderBook) {
		log.Printf("Order book updated for %s from exchanges: %v", symbol, orderBook.Sources)
		// Broadcast to all WebSocket clients
		webapi.BroadcastOrderBookUpdate(orderBook)
	})

	// Set up advanced monitoring
	setupAdvancedMonitoring(aggregator)

	// Symbols to track
	symbols := []string{"BTCUSDT", "ETHUSDT", "ADAUSDT", "SOLUSDT", "DOTUSDT"}
	log.Printf("Will subscribe to symbols: %v", symbols)

	// Create exchange callback factory
	createExchangeCallback := func(exchangeName string) func(*client.OrderBookData) {
		return func(data *client.OrderBookData) {
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
	}

	// Create clients
	mexcClient := client.NewMEXCWebSocketClient(createExchangeCallback("MEXC"))
	krakenClient := client.NewKrakenWebSocketClient(createExchangeCallback("Kraken"))

	// Configure exchange clients
	exchangeConfigs := []ClientConfig{
		{
			Name:         "Kraken",
			Client:       krakenClient,
			ConnectDelay: 2 * time.Second,
			SubscribeFunc: func() error {
				return krakenClient.SubscribeOrderBook(symbols)
			},
			FailOnError: false,
		},
		{
			Name:         "MEXC",
			Client:       mexcClient,
			ConnectDelay: 0,
			SubscribeFunc: func() error {
				return mexcClient.SubscribeMultipleOrderBooks(symbols, "100ms")
			},
			FailOnError: false,
		},
	}

	// Connect and subscribe to all exchanges
	connectedClients := connectToExchanges(exchangeConfigs)
	defer closeAllClients(connectedClients)

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Set up tickers
	displayTicker := time.NewTicker(10 * time.Second)
	defer displayTicker.Stop()

	statsTicker := time.NewTicker(30 * time.Second)
	defer statsTicker.Stop()

	log.Println("Order Book Aggregator is running. Press Ctrl+C to exit.")
	fmt.Println(strings.Repeat("=", 60))

	// Start HTTP server
	go webapi.SetupHTTPServer(aggregator)

	// Start performance monitoring
	startPerformanceMonitoring()

	// Main event loop
	for {
		select {
		case <-displayTicker.C:
			displayOrderBooks(aggregator, symbols)

		case <-statsTicker.C:
			displayStatistics(aggregator, symbols)

		case sig := <-sigChan:
			log.Printf("Received signal %v, shutting down gracefully...", sig)
			return
		}
	}
}

// connectToExchanges handles connection and subscription for all exchange clients
func connectToExchanges(configs []ClientConfig) []ExchangeClient {
	var connectedClients []ExchangeClient

	for _, config := range configs {
		// Add connection delay if specified
		if config.ConnectDelay > 0 {
			time.Sleep(config.ConnectDelay)
		}

		log.Printf("Connecting to %s WebSocket...", config.Name)

		// Connect to exchange
		if err := config.Client.Connect(); err != nil {
			log.Printf("Failed to connect to %s: %v", config.Name, err)
			if config.FailOnError {
				log.Fatalf("Critical connection failure for %s", config.Name)
			}
			continue
		}

		// Start reconnect handler
		config.Client.StartReconnectHandler()

		// Subscribe to order books
		if err := config.SubscribeFunc(); err != nil {
			log.Printf("Failed to subscribe to %s order books: %v", config.Name, err)
			if config.FailOnError {
				log.Fatalf("Critical subscription failure for %s", config.Name)
			}
			continue
		}

		log.Printf("Successfully connected and subscribed to %s", config.Name)
		connectedClients = append(connectedClients, config.Client)
	}

	return connectedClients
}

// closeAllClients closes all connected exchange clients
func closeAllClients(clients []ExchangeClient) {
	for _, client := range clients {
		if err := client.Close(); err != nil {
			log.Printf("Error closing client: %v", err)
		}
	}
}

// displayOrderBooks shows the current state of all order books
func displayOrderBooks(aggregator *orderbook.OrderBookAggregator, symbols []string) {
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("CURRENT ORDER BOOKS")
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

		fmt.Printf("%-10s | Bid: $%8.2f | Ask: $%8.2f | Spread: $%6.2f (%.3f%%) | Sources: %v\n",
			symbol, bestBid, bestAsk, spread, spreadPercent, orderBook.Sources)
	}
}

// displayStatistics shows aggregated statistics
func displayStatistics(aggregator *orderbook.OrderBookAggregator, symbols []string) {
	fmt.Println("\n" + strings.Repeat("-", 80))
	fmt.Println("STATISTICS")
	fmt.Println(strings.Repeat("-", 80))

	totalSymbols := 0
	activeSymbols := 0
	totalSources := make(map[string]bool)

	for _, symbol := range symbols {
		totalSymbols++
		orderBook := aggregator.GetOrderBook(symbol)
		if orderBook != nil {
			activeSymbols++
			for _, source := range orderBook.Sources {
				totalSources[source] = true
			}
		}
	}

	fmt.Printf("Total Symbols: %d\n", totalSymbols)
	fmt.Printf("Active Symbols: %d\n", activeSymbols)
	fmt.Printf("Connected Exchanges: %d (%v)\n", len(totalSources), getKeys(totalSources))
	fmt.Printf("Uptime: %s\n", time.Since(startTime).Round(time.Second))
}

// getKeys returns keys from a map as a slice
func getKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
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
			log.Printf("ALERT: Wide spread detected for %s: %.3f%% ($%.2f)",
				symbol, spreadPercent, spread)
		}
	})

	// Monitor for arbitrage opportunities
	aggregator.AddCallback(func(symbol string, orderBook *orderbook.OrderBook) {
		if len(orderBook.Sources) > 1 {
			log.Printf("Multiple sources available for %s: %v", symbol, orderBook.Sources)
		}
	})
}

// startPerformanceMonitoring monitors system performance
func startPerformanceMonitoring() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		var lastUpdateCount int64

		for range ticker.C {
			currentCount := atomic.LoadInt64(&lastUpdateCount)
			// Reset counter for next interval
			atomic.StoreInt64(&lastUpdateCount, 0)

			log.Printf("Performance: %d updates/min", currentCount)
		}
	}()
}

// addMoreExchanges shows how to extend for additional exchanges
func addMoreExchanges(aggregator *orderbook.OrderBookAggregator) {
	// Example structure for adding Binance or other exchanges:
	/*
		binanceCallback := createExchangeCallback("Binance")
		binanceClient := client.NewBinanceWebSocketClient(binanceCallback)

		newConfig := ClientConfig{
			Name:         "Binance",
			Client:       binanceClient,
			ConnectDelay: 1 * time.Second,
			SubscribeFunc: func() error {
				return binanceClient.SubscribeOrderBook(symbols)
			},
			FailOnError: false,
		}

		// Add to exchangeConfigs slice and it will be handled automatically
	*/

	log.Println("Ready to add more exchanges...")
}
