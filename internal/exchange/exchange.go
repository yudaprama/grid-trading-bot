package exchange

import (
	"github.com/yudaprama/grid-trading-bot/internal/models"
	"time"

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

// Exchange 定义了所有交易所实现必须提供的通用方法。
// 这使得交易机器人可以在真实交易和回测之间轻松切换。
type Exchange interface {
	GetPrice(symbol string) (float64, error)
	GetPositions(symbol string) ([]models.Position, error)
	PlaceOrder(symbol, side, orderType string, quantity, price float64, clientOrderID string) (*models.Order, error)
	CancelOrder(symbol string, orderID int64) error
	SetLeverage(symbol string, leverage int) error
	SetPositionMode(isHedgeMode bool) error
	GetPositionMode() (bool, error)
	SetMarginType(symbol string, marginType string) error
	GetMarginType(symbol string) (string, error)
	GetAccountInfo() (*models.AccountInfo, error)
	CancelAllOpenOrders(symbol string) error
	GetOrderStatus(symbol string, orderID int64) (*models.Order, error)
	GetCurrentTime() time.Time
	// GetAccountState 获取账户状态，包括总持仓价值和账户总权益
	GetAccountState(symbol string) (positionValue float64, accountEquity float64, err error)
	GetSymbolInfo(symbol string) (*models.SymbolInfo, error)
	GetOpenOrders(symbol string) ([]models.Order, error) // 新增：获取所有挂单
	GetServerTime() (int64, error)                       // 新增：获取服务器时间
	GetLastTrade(symbol string, orderID int64) (*models.Trade, error)
	GetMaxWalletExposure() float64
	CreateListenKey() (string, error)
	KeepAliveListenKey(listenKey string) error
	GetBalance() (float64, error)
	ConnectWebSocket(listenKey string) (*websocket.Conn, error)
}
