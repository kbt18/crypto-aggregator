## Features

- Real-time order book aggregation from MEXC and Kraken exchanges. Can be extended to add more exchanges.
- WebSocket API for live market data streaming
- Automatic reconnection handling

## Quick Start

### Prerequisites
- Go 1.19+

## Installation & Running

### Install dependencies
```bash
go mod tidy
```

### Run the aggregator
```bash
go run aggregator.go
```

## WebSocket API
Connect to ws://localhost:8080/ws for real-time order book updates.
Message Format:

```json
{
  "symbol": "BTCUSDT",
  "bids": [{"price": 45000.0, "quantity": 1.5}],
  "asks": [{"price": 45100.0, "quantity": 2.0}],
  "sources": ["MEXC", "Kraken"],
  "lastUpdate": "2025-01-01T12:00:00Z"
}
```