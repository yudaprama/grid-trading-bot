package exchange

import (
	"context"
	"fmt"
	"strconv"
	"time"
	
	"github.com/adshao/go-binance/v2"
	"github.com/yudaprama/grid-trading-bot/internal/models"
	"github.com/gorilla/websocket"
)

// EnhancedSpotExchange implements the Exchange interface using the go-binance SDK
type EnhancedSpotExchange struct {
	client    *binance.Client
	isTestnet bool
	config    *models.Config
}

// NewEnhancedSpotExchange creates a new enhanced spot exchange using go-binance SDK
func NewEnhancedSpotExchange(apiKey, secretKey string, config *models.Config) *EnhancedSpotExchange {
	// Configure client for testnet or production
	if config.IsTestnet {
		binance.UseTestnet = true
	}
	
	client := binance.NewClient(apiKey, secretKey)
	
	return &EnhancedSpotExchange{
		client:    client,
		isTestnet: config.IsTestnet,
		config:    config,
	}
}

// GetPrice gets the current price for a symbol using go-binance SDK
func (e *EnhancedSpotExchange) GetPrice(symbol string) (float64, error) {
	prices, err := e.client.NewListPricesService().Symbol(symbol).Do(context.Background())
	if err != nil {
		return 0, fmt.Errorf("failed to get price: %v", err)
	}
	
	if len(prices) == 0 {
		return 0, fmt.Errorf("no price data for symbol %s", symbol)
	}
	
	price, err := strconv.ParseFloat(prices[0].Price, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse price: %v", err)
	}
	
	return price, nil
}

// PlaceOrder places a new order using go-binance SDK
func (e *EnhancedSpotExchange) PlaceOrder(symbol, side, orderType string, quantity, price float64, clientOrderID string) (*models.Order, error) {
	service := e.client.NewCreateOrderService().
		Symbol(symbol).
		Side(binance.SideType(side)).
		Type(binance.OrderType(orderType)).
		Quantity(fmt.Sprintf("%.8f", quantity)).
		NewClientOrderID(clientOrderID)
	
	if orderType == "LIMIT" {
		service = service.Price(fmt.Sprintf("%.8f", price)).TimeInForce(binance.TimeInForceTypeGTC)
	}
	
	result, err := service.Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to place order: %v", err)
	}
	
	// Convert binance.CreateOrderResponse to models.Order
	order := &models.Order{
		OrderId:       result.OrderID,
		Symbol:        result.Symbol,
		ClientOrderId: result.ClientOrderID,
		Side:          string(result.Side),
		Type:          string(result.Type),
		Status:        string(result.Status),
		Price:         result.Price,
		OrigQty:       result.OrigQuantity,
		ExecutedQty:   result.ExecutedQuantity,
		TimeInForce:   string(result.TimeInForce),
		TransactTime:  result.TransactTime,
	}
	
	return order, nil
}

// CancelOrder cancels an existing order using go-binance SDK
func (e *EnhancedSpotExchange) CancelOrder(symbol string, orderID int64) error {
	_, err := e.client.NewCancelOrderService().
		Symbol(symbol).
		OrderID(orderID).
		Do(context.Background())
	
	if err != nil {
		return fmt.Errorf("failed to cancel order: %v", err)
	}
	
	return nil
}

// GetOrderStatus gets the status of an order using go-binance SDK
func (e *EnhancedSpotExchange) GetOrderStatus(symbol string, orderID int64) (*models.Order, error) {
	result, err := e.client.NewGetOrderService().
		Symbol(symbol).
		OrderID(orderID).
		Do(context.Background())
	
	if err != nil {
		return nil, fmt.Errorf("failed to get order status: %v", err)
	}
	
	// Convert binance.Order to models.Order
	order := &models.Order{
		OrderId:       result.OrderID,
		Symbol:        result.Symbol,
		ClientOrderId: result.ClientOrderID,
		Side:          string(result.Side),
		Type:          string(result.Type),
		Status:        string(result.Status),
		Price:         result.Price,
		OrigQty:       result.OrigQuantity,
		ExecutedQty:   result.ExecutedQuantity,
		TimeInForce:   string(result.TimeInForce),
		Time:          result.Time,
		UpdateTime:    result.UpdateTime,
	}
	
	return order, nil
}

// GetOpenOrders gets all open orders for a symbol using go-binance SDK
func (e *EnhancedSpotExchange) GetOpenOrders(symbol string) ([]models.Order, error) {
	results, err := e.client.NewListOpenOrdersService().
		Symbol(symbol).
		Do(context.Background())
	
	if err != nil {
		return nil, fmt.Errorf("failed to get open orders: %v", err)
	}
	
	orders := make([]models.Order, len(results))
	for i, result := range results {
		orders[i] = models.Order{
			OrderId:       result.OrderID,
			Symbol:        result.Symbol,
			ClientOrderId: result.ClientOrderID,
			Side:          string(result.Side),
			Type:          string(result.Type),
			Status:        string(result.Status),
			Price:         result.Price,
			OrigQty:       result.OrigQuantity,
			ExecutedQty:   result.ExecutedQuantity,
			TimeInForce:   string(result.TimeInForce),
			Time:          result.Time,
			UpdateTime:    result.UpdateTime,
		}
	}
	
	return orders, nil
}

// CancelAllOpenOrders cancels all open orders for a symbol using go-binance SDK
func (e *EnhancedSpotExchange) CancelAllOpenOrders(symbol string) error {
	_, err := e.client.NewCancelOpenOrdersService().
		Symbol(symbol).
		Do(context.Background())
	
	if err != nil {
		return fmt.Errorf("failed to cancel all open orders: %v", err)
	}
	
	return nil
}

// GetSpotBalances gets account balances using go-binance SDK
func (e *EnhancedSpotExchange) GetSpotBalances() ([]models.SpotBalance, error) {
	account, err := e.client.NewGetAccountService().Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get account info: %v", err)
	}
	
	balances := make([]models.SpotBalance, len(account.Balances))
	for i, balance := range account.Balances {
		balances[i] = models.SpotBalance{
			Asset:  balance.Asset,
			Free:   balance.Free,
			Locked: balance.Locked,
		}
	}
	
	return balances, nil
}

// GetSpotBalance gets balance for a specific asset using go-binance SDK
func (e *EnhancedSpotExchange) GetSpotBalance(asset string) (float64, error) {
	balances, err := e.GetSpotBalances()
	if err != nil {
		return 0, err
	}
	
	for _, balance := range balances {
		if balance.Asset == asset {
			free, err := strconv.ParseFloat(balance.Free, 64)
			if err != nil {
				return 0, fmt.Errorf("failed to parse balance: %v", err)
			}
			return free, nil
		}
	}
	
	return 0, nil // Asset not found, return 0 balance
}

// GetSymbolInfo gets symbol information using go-binance SDK
func (e *EnhancedSpotExchange) GetSymbolInfo(symbol string) (*models.SymbolInfo, error) {
	exchangeInfo, err := e.client.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get exchange info: %v", err)
	}
	
	for _, symbolInfo := range exchangeInfo.Symbols {
		if symbolInfo.Symbol == symbol {
			// Convert binance.Symbol to models.SymbolInfo
			filters := make([]models.SymbolFilter, len(symbolInfo.Filters))
			for i, filter := range symbolInfo.Filters {
				filters[i] = models.SymbolFilter{
					FilterType:  string(filter["filterType"].(string)),
					MinPrice:    getStringFromMap(filter, "minPrice"),
					MaxPrice:    getStringFromMap(filter, "maxPrice"),
					TickSize:    getStringFromMap(filter, "tickSize"),
					MinQty:      getStringFromMap(filter, "minQty"),
					MaxQty:      getStringFromMap(filter, "maxQty"),
					StepSize:    getStringFromMap(filter, "stepSize"),
					MinNotional: getStringFromMap(filter, "minNotional"),
				}
			}
			
			return &models.SymbolInfo{
				Symbol:  symbolInfo.Symbol,
				Status:  string(symbolInfo.Status),
				Filters: filters,
			}, nil
		}
	}
	
	return nil, fmt.Errorf("symbol %s not found", symbol)
}

// GetServerTime gets server time using go-binance SDK
func (e *EnhancedSpotExchange) GetServerTime() (int64, error) {
	serverTime, err := e.client.NewServerTimeService().Do(context.Background())
	if err != nil {
		return 0, fmt.Errorf("failed to get server time: %v", err)
	}
	
	return serverTime, nil
}

// GetSpotTradingFees gets trading fees for spot trading using go-binance SDK
func (e *EnhancedSpotExchange) GetSpotTradingFees(symbol string) (*models.TradingFees, error) {
	tradeFee, err := e.client.NewTradeFeeService().Symbol(symbol).Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get trading fees: %v", err)
	}
	
	if len(tradeFee) == 0 {
		return nil, fmt.Errorf("no trading fee data for symbol %s", symbol)
	}
	
	fee := tradeFee[0]
	makerFee, _ := strconv.ParseFloat(fee.MakerCommission, 64)
	takerFee, _ := strconv.ParseFloat(fee.TakerCommission, 64)
	
	return &models.TradingFees{
		MakerFee:    makerFee,
		TakerFee:    takerFee,
		Symbol:      symbol,
		TradingMode: "spot",
	}, nil
}

// Helper function to safely get string values from map
func getStringFromMap(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// Futures-specific methods (not supported in spot mode)
func (e *EnhancedSpotExchange) GetPositions(symbol string) ([]models.Position, error) {
	return nil, fmt.Errorf("GetPositions is not supported in spot trading mode")
}

func (e *EnhancedSpotExchange) SetLeverage(symbol string, leverage int) error {
	return fmt.Errorf("SetLeverage is not supported in spot trading mode")
}

func (e *EnhancedSpotExchange) SetPositionMode(isHedgeMode bool) error {
	return fmt.Errorf("SetPositionMode is not supported in spot trading mode")
}

func (e *EnhancedSpotExchange) GetPositionMode() (bool, error) {
	return false, fmt.Errorf("GetPositionMode is not supported in spot trading mode")
}

func (e *EnhancedSpotExchange) SetMarginType(symbol string, marginType string) error {
	return fmt.Errorf("SetMarginType is not supported in spot trading mode")
}

func (e *EnhancedSpotExchange) GetMarginType(symbol string) (string, error) {
	return "", fmt.Errorf("GetMarginType is not supported in spot trading mode")
}

func (e *EnhancedSpotExchange) GetAccountState(symbol string) (positionValue float64, accountEquity float64, err error) {
	return 0, 0, fmt.Errorf("GetAccountState is not supported in spot trading mode")
}

func (e *EnhancedSpotExchange) GetMaxWalletExposure() float64 {
	return 0 // Not applicable for spot trading
}

func (e *EnhancedSpotExchange) GetBalance() (float64, error) {
	return 0, fmt.Errorf("GetBalance is not supported in spot trading mode (use GetSpotBalance instead)")
}

// Common methods
func (e *EnhancedSpotExchange) GetAccountInfo() (*models.AccountInfo, error) {
	account, err := e.client.NewGetAccountService().Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get account info: %v", err)
	}
	
	return &models.AccountInfo{
		MakerCommission:  int(account.MakerCommission),
		TakerCommission:  int(account.TakerCommission),
		BuyerCommission:  int(account.BuyerCommission),
		SellerCommission: int(account.SellerCommission),
		CanTrade:         account.CanTrade,
		CanWithdraw:      account.CanWithdraw,
		CanDeposit:       account.CanDeposit,
		UpdateTime:       int64(account.UpdateTime),
		AccountType:      string(account.AccountType),
	}, nil
}

func (e *EnhancedSpotExchange) GetCurrentTime() time.Time {
	return time.Now()
}

func (e *EnhancedSpotExchange) GetLastTrade(symbol string, orderID int64) (*models.Trade, error) {
	// This would require additional implementation
	return &models.Trade{}, nil
}

func (e *EnhancedSpotExchange) CreateListenKey() (string, error) {
	listenKey, err := e.client.NewStartUserStreamService().Do(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to create listen key: %v", err)
	}
	
	return listenKey, nil
}

func (e *EnhancedSpotExchange) KeepAliveListenKey(listenKey string) error {
	err := e.client.NewKeepaliveUserStreamService().ListenKey(listenKey).Do(context.Background())
	if err != nil {
		return fmt.Errorf("failed to keep alive listen key: %v", err)
	}
	
	return nil
}

func (e *EnhancedSpotExchange) ConnectWebSocket(listenKey string) (*websocket.Conn, error) {
	// This would require WebSocket implementation using the SDK
	// For now, return a placeholder
	return nil, fmt.Errorf("WebSocket connection not implemented yet")
}

// Trading mode methods
func (e *EnhancedSpotExchange) GetTradingMode() string {
	return "spot"
}

func (e *EnhancedSpotExchange) SupportsTradingMode(mode string) bool {
	return mode == "spot"
}

func (e *EnhancedSpotExchange) SetTradingMode(mode string) error {
	if mode != "spot" {
		return fmt.Errorf("enhanced spot exchange only supports spot trading mode")
	}
	return nil
}
