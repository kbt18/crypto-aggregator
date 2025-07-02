package orderbook

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OrderBookEntry represents a single order book entry
type OrderBookEntry struct {
	Price    float64
	Quantity float64
}

// OrderBook represents an aggregated order book from multiple exchanges
type OrderBook struct {
	Symbol     string
	Bids       []OrderBookEntry // Sorted highest to lowest
	Asks       []OrderBookEntry // Sorted lowest to highest
	LastUpdate time.Time
	Sources    []string // List of exchanges contributing to this order book
	mutex      sync.RWMutex
}

// ExchangeOrderBook represents order book data from a specific exchange
type ExchangeOrderBook struct {
	Symbol     string
	Exchange   string
	Bids       map[string]string // price -> quantity
	Asks       map[string]string // price -> quantity
	LastUpdate int64
	Version    string
}

// OrderBookAggregator aggregates order books from multiple exchanges
type OrderBookAggregator struct {
	orderBooks map[string]*OrderBook                    // symbol -> aggregated order book
	exchanges  map[string]map[string]*ExchangeOrderBook // exchange -> symbol -> order book
	mutex      sync.RWMutex
	callbacks  []OrderBookUpdateCallback
}

// OrderBookUpdateCallback is called when an order book is updated
type OrderBookUpdateCallback func(symbol string, orderBook *OrderBook)

// NewOrderBookAggregator creates a new order book aggregator
func NewOrderBookAggregator() *OrderBookAggregator {
	return &OrderBookAggregator{
		orderBooks: make(map[string]*OrderBook),
		exchanges:  make(map[string]map[string]*ExchangeOrderBook),
		callbacks:  make([]OrderBookUpdateCallback, 0),
	}
}

// AddCallback adds a callback function for order book updates
func (oba *OrderBookAggregator) AddCallback(callback OrderBookUpdateCallback) {
	oba.mutex.Lock()
	defer oba.mutex.Unlock()
	oba.callbacks = append(oba.callbacks, callback)
}

// UpdateOrderBook updates the order book for a specific exchange and symbol
func (oba *OrderBookAggregator) UpdateOrderBook(exchange string, data *ExchangeOrderBook) {
	oba.mutex.Lock()
	defer oba.mutex.Unlock()

	// Initialize exchange map if it doesn't exist
	if oba.exchanges[exchange] == nil {
		oba.exchanges[exchange] = make(map[string]*ExchangeOrderBook)
	}

	// Store the exchange-specific order book
	oba.exchanges[exchange][data.Symbol] = data

	// Aggregate order books from all exchanges for this symbol
	oba.aggregateOrderBook(data.Symbol)

	// Notify callbacks
	if aggregatedBook, exists := oba.orderBooks[data.Symbol]; exists {
		for _, callback := range oba.callbacks {
			go callback(data.Symbol, aggregatedBook)
		}
	}
}

// aggregateOrderBook combines order books from all exchanges for a symbol
func (oba *OrderBookAggregator) aggregateOrderBook(symbol string) {
	allBids := make(map[float64]float64) // price -> total quantity
	allAsks := make(map[float64]float64) // price -> total quantity
	var sources []string
	latestUpdate := time.Time{}

	// Collect order book data from all exchanges
	for exchange, books := range oba.exchanges {
		if book, exists := books[symbol]; exists {
			sources = append(sources, exchange)

			updateTime := time.Unix(book.LastUpdate/1000, 0)
			if updateTime.After(latestUpdate) {
				latestUpdate = updateTime
			}

			// Aggregate bids
			for priceStr, qtyStr := range book.Bids {
				price, err1 := strconv.ParseFloat(priceStr, 64)
				qty, err2 := strconv.ParseFloat(qtyStr, 64)
				if err1 == nil && err2 == nil && qty > 0 {
					allBids[price] += qty
				}
			}

			// Aggregate asks
			for priceStr, qtyStr := range book.Asks {
				price, err1 := strconv.ParseFloat(priceStr, 64)
				qty, err2 := strconv.ParseFloat(qtyStr, 64)
				if err1 == nil && err2 == nil && qty > 0 {
					allAsks[price] += qty
				}
			}
		}
	}

	// Convert maps to sorted slices
	var bids, asks []OrderBookEntry

	for price, qty := range allBids {
		bids = append(bids, OrderBookEntry{Price: price, Quantity: qty})
	}

	for price, qty := range allAsks {
		asks = append(asks, OrderBookEntry{Price: price, Quantity: qty})
	}

	// Sort bids (highest to lowest) and asks (lowest to highest)
	sort.Slice(bids, func(i, j int) bool {
		return bids[i].Price > bids[j].Price
	})

	sort.Slice(asks, func(i, j int) bool {
		return asks[i].Price < asks[j].Price
	})

	// Update or create the aggregated order book
	if oba.orderBooks[symbol] == nil {
		oba.orderBooks[symbol] = &OrderBook{}
	}

	book := oba.orderBooks[symbol]
	book.mutex.Lock()
	book.Symbol = symbol
	book.Bids = bids
	book.Asks = asks
	book.LastUpdate = latestUpdate
	book.Sources = sources
	book.mutex.Unlock()
}

// GetOrderBook returns the aggregated order book for a symbol
func (oba *OrderBookAggregator) GetOrderBook(symbol string) *OrderBook {
	oba.mutex.RLock()
	defer oba.mutex.RUnlock()

	if book, exists := oba.orderBooks[symbol]; exists {
		book.mutex.RLock()
		defer book.mutex.RUnlock()

		// Return a copy to avoid race conditions
		return &OrderBook{
			Symbol:     book.Symbol,
			Bids:       append([]OrderBookEntry(nil), book.Bids...),
			Asks:       append([]OrderBookEntry(nil), book.Asks...),
			LastUpdate: book.LastUpdate,
			Sources:    append([]string(nil), book.Sources...),
		}
	}

	return nil
}

// GetBestPrice returns the best bid and ask prices from the aggregated order book
func (ob *OrderBook) GetBestPrice() (bestBid, bestAsk, spread float64, err error) {
	ob.mutex.RLock()
	defer ob.mutex.RUnlock()

	if len(ob.Bids) == 0 || len(ob.Asks) == 0 {
		return 0, 0, 0, fmt.Errorf("insufficient order book data")
	}

	bestBid = ob.Bids[0].Price // Highest bid
	bestAsk = ob.Asks[0].Price // Lowest ask
	spread = bestAsk - bestBid

	return bestBid, bestAsk, spread, nil
}

// GetSpread returns the bid-ask spread
func (ob *OrderBook) GetSpread() float64 {
	_, _, spread, err := ob.GetBestPrice()
	if err != nil {
		return 0
	}
	return spread
}

// GetDepth returns the order book depth up to a specified number of levels
func (ob *OrderBook) GetDepth(levels int) ([]OrderBookEntry, []OrderBookEntry) {
	ob.mutex.RLock()
	defer ob.mutex.RUnlock()

	maxBids := levels
	if len(ob.Bids) < maxBids {
		maxBids = len(ob.Bids)
	}

	maxAsks := levels
	if len(ob.Asks) < maxAsks {
		maxAsks = len(ob.Asks)
	}

	return ob.Bids[:maxBids], ob.Asks[:maxAsks]
}

// PrintOrderBook prints the order book in a formatted way
func (ob *OrderBook) PrintOrderBook(depth int) {
	ob.mutex.RLock()
	defer ob.mutex.RUnlock()

	fmt.Printf("\n=== Aggregated Order Book for %s ===\n", ob.Symbol)
	fmt.Printf("Sources: %v | Last Update: %s\n", ob.Sources, ob.LastUpdate.Format("15:04:05"))
	fmt.Printf("%-15s %-15s | %-15s %-15s\n", "Bid Price", "Bid Qty", "Ask Price", "Ask Qty")
	fmt.Println(strings.Repeat("-", 65))

	maxDepth := depth
	if len(ob.Bids) < maxDepth {
		maxDepth = len(ob.Bids)
	}
	if len(ob.Asks) < maxDepth {
		maxDepth = len(ob.Asks)
	}

	for i := 0; i < maxDepth; i++ {
		bidPrice, bidQty := "", ""
		askPrice, askQty := "", ""

		if i < len(ob.Bids) {
			bidPrice = fmt.Sprintf("%.8f", ob.Bids[i].Price)
			bidQty = fmt.Sprintf("%.8f", ob.Bids[i].Quantity)
		}

		if i < len(ob.Asks) {
			askPrice = fmt.Sprintf("%.8f", ob.Asks[i].Price)
			askQty = fmt.Sprintf("%.8f", ob.Asks[i].Quantity)
		}

		fmt.Printf("%-15s %-15s | %-15s %-15s\n", bidPrice, bidQty, askPrice, askQty)
	}

	// Print spread information
	if spread := ob.GetSpread(); spread > 0 {
		fmt.Printf("\nSpread: %.8f\n", spread)
	}
}

// GetVolumeAtPrice returns the total volume available at or better than the given price
func (ob *OrderBook) GetVolumeAtPrice(price float64, side string) float64 {
	ob.mutex.RLock()
	defer ob.mutex.RUnlock()

	var totalVolume float64

	if strings.ToUpper(side) == "BUY" || strings.ToUpper(side) == "BID" {
		// For buy orders, look at asks at or below the price
		for _, ask := range ob.Asks {
			if ask.Price <= price {
				totalVolume += ask.Quantity
			} else {
				break // Asks are sorted lowest to highest
			}
		}
	} else if strings.ToUpper(side) == "SELL" || strings.ToUpper(side) == "ASK" {
		// For sell orders, look at bids at or above the price
		for _, bid := range ob.Bids {
			if bid.Price >= price {
				totalVolume += bid.Quantity
			} else {
				break // Bids are sorted highest to lowest
			}
		}
	}

	return totalVolume
}

// GetMidPrice returns the mid price (average of best bid and ask)
func (ob *OrderBook) GetMidPrice() (float64, error) {
	bestBid, bestAsk, _, err := ob.GetBestPrice()
	if err != nil {
		return 0, err
	}
	return (bestBid + bestAsk) / 2, nil
}

// IsStale checks if the order book is older than the specified duration
func (ob *OrderBook) IsStale(maxAge time.Duration) bool {
	ob.mutex.RLock()
	defer ob.mutex.RUnlock()
	return time.Since(ob.LastUpdate) > maxAge
}
