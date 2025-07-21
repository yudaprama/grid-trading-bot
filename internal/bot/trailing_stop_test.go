package bot

import (
	"github.com/yudaprama/grid-trading-bot/internal/models"
	"fmt"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// MockExchange is a simple mock for testing trailing stops
type MockExchange struct {
	currentPrice float64
	orders       map[int64]*models.Order
	nextOrderID  int64
}

func NewMockExchange() *MockExchange {
	return &MockExchange{
		currentPrice: 100.0,
		orders:       make(map[int64]*models.Order),
		nextOrderID:  1,
	}
}

func (m *MockExchange) GetPrice(symbol string) (float64, error) {
	return m.currentPrice, nil
}

func (m *MockExchange) PlaceOrder(symbol, side, orderType string, quantity, price float64, clientOrderID string) (*models.Order, error) {
	order := &models.Order{
		OrderId:  m.nextOrderID,
		Symbol:   symbol,
		Side:     side,
		Type:     orderType,
		Price:    formatFloat(price),
		OrigQty:  formatFloat(quantity),
		Status:   "NEW",
	}
	m.orders[m.nextOrderID] = order
	m.nextOrderID++
	return order, nil
}

func (m *MockExchange) CancelOrder(symbol string, orderID int64) error {
	delete(m.orders, orderID)
	return nil
}

func (m *MockExchange) SetPrice(price float64) {
	m.currentPrice = price
}

// Implement other required Exchange interface methods with minimal functionality
func (m *MockExchange) GetPositions(symbol string) ([]models.Position, error) { return nil, nil }
func (m *MockExchange) SetLeverage(symbol string, leverage int) error { return nil }
func (m *MockExchange) SetPositionMode(isHedgeMode bool) error { return nil }
func (m *MockExchange) GetPositionMode() (bool, error) { return false, nil }
func (m *MockExchange) SetMarginType(symbol string, marginType string) error { return nil }
func (m *MockExchange) GetMarginType(symbol string) (string, error) { return "CROSSED", nil }
func (m *MockExchange) GetAccountInfo() (*models.AccountInfo, error) { return nil, nil }
func (m *MockExchange) CancelAllOpenOrders(symbol string) error { return nil }
func (m *MockExchange) GetOrderStatus(symbol string, orderID int64) (*models.Order, error) { return nil, nil }
func (m *MockExchange) GetCurrentTime() time.Time { return time.Now() }
func (m *MockExchange) GetAccountState(symbol string) (float64, float64, error) { return 0, 0, nil }
func (m *MockExchange) GetSymbolInfo(symbol string) (*models.SymbolInfo, error) { return nil, nil }
func (m *MockExchange) GetOpenOrders(symbol string) ([]models.Order, error) { return nil, nil }
func (m *MockExchange) GetServerTime() (int64, error) { return time.Now().UnixMilli(), nil }
func (m *MockExchange) GetLastTrade(symbol string, orderID int64) (*models.Trade, error) { return nil, nil }
func (m *MockExchange) GetMaxWalletExposure() float64 { return 1000.0 }
func (m *MockExchange) CreateListenKey() (string, error) { return "", nil }
func (m *MockExchange) KeepAliveListenKey(listenKey string) error { return nil }
func (m *MockExchange) GetBalance() (float64, error) { return 1000.0, nil }
func (m *MockExchange) ConnectWebSocket(listenKey string) (*websocket.Conn, error) { return nil, nil }

func formatFloat(f float64) string {
	return fmt.Sprintf("%.4f", f)
}

func TestTrailingStopInitialization(t *testing.T) {
	config := &models.Config{
		Symbol:               "BTCUSDT",
		EnableTrailingUp:     true,
		EnableTrailingDown:   true,
		TrailingUpDistance:   0.02,   // 2%
		TrailingDownDistance: 0.015,  // 1.5%
		TrailingType:         "percentage",
	}

	mockExchange := NewMockExchange()
	bot := NewGridTradingBot(config, mockExchange, true)

	// Test initialization
	entryPrice := 100.0
	bot.initializeTrailingStop(entryPrice, "LONG")

	if bot.trailingStopState == nil {
		t.Fatal("Trailing stop state should be initialized")
	}

	if !bot.trailingStopState.IsActive {
		t.Error("Trailing stop should be active")
	}

	if bot.trailingStopState.EntryPrice != entryPrice {
		t.Errorf("Expected entry price %f, got %f", entryPrice, bot.trailingStopState.EntryPrice)
	}

	expectedTrailingUp := entryPrice * (1 - config.TrailingUpDistance)
	if bot.trailingStopState.TrailingUpLevel != expectedTrailingUp {
		t.Errorf("Expected trailing up level %f, got %f", expectedTrailingUp, bot.trailingStopState.TrailingUpLevel)
	}

	// For LONG positions, trailing down (stop loss) should be below entry price
	expectedTrailingDown := entryPrice * (1 - config.TrailingDownDistance)
	if bot.trailingStopState.TrailingDownLevel != expectedTrailingDown {
		t.Errorf("Expected trailing down level %f, got %f", expectedTrailingDown, bot.trailingStopState.TrailingDownLevel)
	}
}

func TestTrailingUpAdjustment(t *testing.T) {
	config := &models.Config{
		Symbol:               "BTCUSDT",
		EnableTrailingUp:     true,
		EnableTrailingDown:   false,
		TrailingUpDistance:   0.02, // 2%
		TrailingType:         "percentage",
	}

	mockExchange := NewMockExchange()
	bot := NewGridTradingBot(config, mockExchange, true)

	// Initialize trailing stop
	entryPrice := 100.0
	bot.initializeTrailingStop(entryPrice, "LONG")
	initialTrailingUp := bot.trailingStopState.TrailingUpLevel

	// Simulate price increase
	newPrice := 110.0
	bot.updateTrailingStop(newPrice)

	// Check that trailing up level was adjusted
	expectedNewTrailingUp := newPrice * (1 - config.TrailingUpDistance)
	if bot.trailingStopState.TrailingUpLevel <= initialTrailingUp {
		t.Error("Trailing up level should have been adjusted upward")
	}

	if bot.trailingStopState.TrailingUpLevel != expectedNewTrailingUp {
		t.Errorf("Expected new trailing up level %f, got %f", expectedNewTrailingUp, bot.trailingStopState.TrailingUpLevel)
	}

	if bot.trailingStopState.TotalAdjustments == 0 {
		t.Error("Total adjustments should be greater than 0")
	}
}

func TestTrailingDownAdjustment(t *testing.T) {
	config := &models.Config{
		Symbol:               "BTCUSDT",
		EnableTrailingUp:     false,
		EnableTrailingDown:   true,
		TrailingDownDistance: 0.015, // 1.5%
		TrailingType:         "percentage",
	}

	mockExchange := NewMockExchange()
	bot := NewGridTradingBot(config, mockExchange, true)

	// Initialize trailing stop
	entryPrice := 100.0
	bot.initializeTrailingStop(entryPrice, "LONG")

	// Simulate price decrease from entry point
	newPrice := 95.0 // Price going down from entry (100)
	bot.updateTrailingStop(newPrice)

	// For trailing down, we expect it to follow the lowest price
	// Since price went from 100 to 95, lowest should be 95
	if bot.trailingStopState.LowestPrice != newPrice {
		t.Errorf("Expected lowest price %f, got %f", newPrice, bot.trailingStopState.LowestPrice)
	}

	// Check that trailing down level was calculated correctly
	// For LONG positions, trailing down should be below the lowest price
	expectedTrailingDown := newPrice * (1 - config.TrailingDownDistance)
	if bot.trailingStopState.TrailingDownLevel != expectedTrailingDown {
		t.Errorf("Expected trailing down level %f, got %f", expectedTrailingDown, bot.trailingStopState.TrailingDownLevel)
	}
}

func TestAbsoluteTrailingType(t *testing.T) {
	config := &models.Config{
		Symbol:               "BTCUSDT",
		EnableTrailingUp:     true,
		EnableTrailingDown:   true,
		TrailingUpDistance:   2.0, // $2
		TrailingDownDistance: 1.5, // $1.5
		TrailingType:         "absolute",
	}

	mockExchange := NewMockExchange()
	bot := NewGridTradingBot(config, mockExchange, true)

	// Test calculation
	price := 100.0
	upLevel := bot.calculateTrailingLevel(price, config.TrailingUpDistance, "up")
	downLevel := bot.calculateTrailingLevel(price, config.TrailingDownDistance, "down")

	expectedUp := price - config.TrailingUpDistance
	expectedDown := price + config.TrailingDownDistance

	if upLevel != expectedUp {
		t.Errorf("Expected absolute trailing up level %f, got %f", expectedUp, upLevel)
	}

	if downLevel != expectedDown {
		t.Errorf("Expected absolute trailing down level %f, got %f", expectedDown, downLevel)
	}
}
