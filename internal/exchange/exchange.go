package exchange

import (
	"time"

	"github.com/yudaprama/grid-trading-bot/internal/models"

	"github.com/gorilla/websocket"
)

// NewOrderRequest defines all parameters required to create a new order.
// This is a standardized structure for passing information between the bot and exchange implementations.
type NewOrderRequest struct {
	Symbol   string
	Side     string
	Type     string
	Price    float64
	Quantity float64
}

// Exchange defines the common methods that all exchange implementations must provide.
// This allows the trading bot to seamlessly switch between live trading and backtesting.
type Exchange interface {
	// Common methods for both futures and spot
	GetPrice(symbol string) (float64, error)
	PlaceOrder(symbol, side, orderType string, quantity, price float64, clientOrderID string) (*models.Order, error)
	CancelOrder(symbol string, orderID int64) error
	GetAccountInfo() (*models.AccountInfo, error)
	CancelAllOpenOrders(symbol string) error
	GetOrderStatus(symbol string, orderID int64) (*models.Order, error)
	GetCurrentTime() time.Time
	GetSymbolInfo(symbol string) (*models.SymbolInfo, error)
	GetOpenOrders(symbol string) ([]models.Order, error)
	GetServerTime() (int64, error)
	GetLastTrade(symbol string, orderID int64) (*models.Trade, error)
	CreateListenKey() (string, error)
	KeepAliveListenKey(listenKey string) error
	ConnectWebSocket(listenKey string) (*websocket.Conn, error)

	// Futures-specific methods (will return error for spot mode)
	GetPositions(symbol string) ([]models.Position, error)
	SetLeverage(symbol string, leverage int) error
	SetPositionMode(isHedgeMode bool) error
	GetPositionMode() (bool, error)
	SetMarginType(symbol string, marginType string) error
	GetMarginType(symbol string) (string, error)
	GetAccountState(symbol string) (positionValue float64, accountEquity float64, err error)
	GetMaxWalletExposure() float64
	GetBalance() (float64, error) // For futures, returns USDT balance

	// Spot-specific methods (will return error for futures mode)
	GetSpotBalances() ([]models.SpotBalance, error)
	GetSpotBalance(asset string) (float64, error)
	GetSpotTradingFees(symbol string) (*models.TradingFees, error)

	// Mode detection and configuration
	GetTradingMode() string
	SupportsTradingMode(mode string) bool
	SetTradingMode(mode string) error
}
