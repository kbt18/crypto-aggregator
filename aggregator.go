package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"order-book-aggregator/client"
	"order-book-aggregator/orderbook"
	"order-book-aggregator/webapi"
)

func main() {
	log.Println("Starting Order Book Aggregator...")

	// Create order book aggregator
	aggregator := orderbook.NewOrderBookAggregator()

	// Add callback to handle order book updates
	aggregator.AddCallback(func(symbol string, orderBook *orderbook.OrderBook) {
		// This callback is called whenever an order book is updated
		log.Printf("Order book updated for %s from exchanges: %v", symbol, orderBook.Sources)

		// You can add custom logic here, such as:
		// - Sending alerts when spread is too wide
		// - Logging order book snapshots
		// - Triggering trading strategies
	})

	// Symbols to track (note: BNB might not be available on Kraken)
	symbols := []string{"BTCUSDT", "ETHUSDT", "ADAUSDT", "SOLUSDT", "DOTUSDT"}
	log.Printf("Will subscribe to symbols: %v", symbols)

	mexcCallback := clientCallbackFactory("MEXC", aggregator)
	krakenCallback := clientCallbackFactory("Kraken", aggregator)

	mexcClient := client.NewMEXCWebSocketClient(mexcCallback)
	krakenClient := client.NewKrakenWebSocketClient(krakenCallback)

	time.Sleep(2 * time.Second)
	log.Println("Connecting to Kraken WebSocket...")
	if err := krakenClient.Connect(); err != nil {
		log.Printf("Failed to connect to Kraken: %v", err)
	} else {
		krakenClient.StartReconnectHandler()

		// Subscribe to Kraken order books
		if err := krakenClient.SubscribeOrderBook(symbols); err != nil {
			log.Printf("Failed to subscribe to Kraken order books: %v", err)
		} else {
			log.Println("Successfully subscribed to Kraken order books")
		}
	}
	defer krakenClient.Close()

	// Connect to MEXC
	log.Println("Connecting to MEXC WebSocket...")
	if err := mexcClient.Connect(); err != nil {
		log.Printf("Failed to connect to MEXC: %v", err)
	} else {
		mexcClient.StartReconnectHandler()

		// Subscribe to MEXC order books
		if err := mexcClient.SubscribeMultipleOrderBooks(symbols, "100ms"); err != nil {
			log.Printf("Failed to subscribe to MEXC order books: %v", err)
		} else {
			log.Println("Successfully subscribed to MEXC order books")
		}
	}
	defer mexcClient.Close()

	if err := mexcClient.SubscribeMultipleOrderBooks(symbols, "100ms"); err != nil {
		log.Fatalf("Failed to subscribe to order books: %v", err)
	}

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create a ticker for periodic order book display
	displayTicker := time.NewTicker(10 * time.Second)
	defer displayTicker.Stop()

	// Create a ticker for statistics
	statsTicker := time.NewTicker(30 * time.Second)
	defer statsTicker.Stop()

	log.Println("Order Book Aggregator is running. Press Ctrl+C to exit.")
	fmt.Println(strings.Repeat("=", 60))

	// Add this to your main() function
	go webapi.SetupHTTPServer(aggregator)

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

// Helper function to get keys from a map
func getKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func clientCallbackFactory(clientName string, aggregator *orderbook.OrderBookAggregator) func(*client.OrderBookData) {
	return func(data *client.OrderBookData) {
		// Convert MEXC data to the aggregator format
		exchangeBook := &orderbook.ExchangeOrderBook{
			Symbol:     data.Symbol,
			Exchange:   data.Exchange,
			Bids:       data.Bids,
			Asks:       data.Asks,
			LastUpdate: data.LastUpdate,
			Version:    data.Version,
		}

		// Update the aggregator
		aggregator.UpdateOrderBook(clientName, exchangeBook)

		// Get the updated aggregated order book
		if updatedOrderBook := aggregator.GetOrderBook(data.Symbol); updatedOrderBook != nil {
			// Broadcast to all WebSocket clients
			webapi.BroadcastOrderBookUpdate(updatedOrderBook)
		}
	}
}

// Global variable to track start time
var startTime = time.Now()
