package exchange

import (
	"fmt"
	"strconv"
	"time"
	
	"github.com/yudaprama/grid-trading-bot/internal/models"
	"github.com/yudaprama/grid-trading-bot/internal/trading"
	"github.com/gorilla/websocket"
)

// ModeAwareExchangeImpl implements the ModeAwareExchange interface
type ModeAwareExchangeImpl struct {
	futuresExchange Exchange // Original futures exchange
	spotExchange    Exchange // Spot exchange (to be implemented)
	currentMode     string   // "futures" or "spot"
	config          *models.Config
}

// NewModeAwareExchange creates a new mode-aware exchange wrapper
func NewModeAwareExchange(futuresExchange Exchange, config *models.Config) *ModeAwareExchangeImpl {
	return &ModeAwareExchangeImpl{
		futuresExchange: futuresExchange,
		currentMode:     config.TradingMode,
		config:          config,
	}
}

// SetSpotExchange sets the spot exchange implementation
func (m *ModeAwareExchangeImpl) SetSpotExchange(spotExchange Exchange) {
	m.spotExchange = spotExchange
}

// GetTradingMode returns the current trading mode
func (m *ModeAwareExchangeImpl) GetTradingMode() string {
	return m.currentMode
}

// SupportsTradingMode checks if a trading mode is supported
func (m *ModeAwareExchangeImpl) SupportsTradingMode(mode string) bool {
	switch mode {
	case "futures":
		return m.futuresExchange != nil
	case "spot":
		return m.spotExchange != nil
	default:
		return false
	}
}

// SetTradingMode sets the current trading mode
func (m *ModeAwareExchangeImpl) SetTradingMode(mode string) error {
	if !m.SupportsTradingMode(mode) {
		return fmt.Errorf("trading mode %s is not supported", mode)
	}
	m.currentMode = mode
	return nil
}

// getActiveExchange returns the exchange for the current mode
func (m *ModeAwareExchangeImpl) getActiveExchange() Exchange {
	switch m.currentMode {
	case "futures":
		return m.futuresExchange
	case "spot":
		return m.spotExchange
	default:
		return m.futuresExchange // fallback
	}
}

// Common methods that delegate to the active exchange
func (m *ModeAwareExchangeImpl) GetPrice(symbol string) (float64, error) {
	return m.getActiveExchange().GetPrice(symbol)
}

func (m *ModeAwareExchangeImpl) PlaceOrder(symbol, side, orderType string, quantity, price float64, clientOrderID string) (*models.Order, error) {
	return m.getActiveExchange().PlaceOrder(symbol, side, orderType, quantity, price, clientOrderID)
}

func (m *ModeAwareExchangeImpl) CancelOrder(symbol string, orderID int64) error {
	return m.getActiveExchange().CancelOrder(symbol, orderID)
}

func (m *ModeAwareExchangeImpl) GetAccountInfo() (*models.AccountInfo, error) {
	return m.getActiveExchange().GetAccountInfo()
}

func (m *ModeAwareExchangeImpl) CancelAllOpenOrders(symbol string) error {
	return m.getActiveExchange().CancelAllOpenOrders(symbol)
}

func (m *ModeAwareExchangeImpl) GetOrderStatus(symbol string, orderID int64) (*models.Order, error) {
	return m.getActiveExchange().GetOrderStatus(symbol, orderID)
}

func (m *ModeAwareExchangeImpl) GetCurrentTime() time.Time {
	return m.getActiveExchange().GetCurrentTime()
}

func (m *ModeAwareExchangeImpl) GetSymbolInfo(symbol string) (*models.SymbolInfo, error) {
	return m.getActiveExchange().GetSymbolInfo(symbol)
}

func (m *ModeAwareExchangeImpl) GetOpenOrders(symbol string) ([]models.Order, error) {
	return m.getActiveExchange().GetOpenOrders(symbol)
}

func (m *ModeAwareExchangeImpl) GetServerTime() (int64, error) {
	return m.getActiveExchange().GetServerTime()
}

func (m *ModeAwareExchangeImpl) GetLastTrade(symbol string, orderID int64) (*models.Trade, error) {
	return m.getActiveExchange().GetLastTrade(symbol, orderID)
}

func (m *ModeAwareExchangeImpl) CreateListenKey() (string, error) {
	return m.getActiveExchange().CreateListenKey()
}

func (m *ModeAwareExchangeImpl) KeepAliveListenKey(listenKey string) error {
	return m.getActiveExchange().KeepAliveListenKey(listenKey)
}

func (m *ModeAwareExchangeImpl) ConnectWebSocket(listenKey string) (*websocket.Conn, error) {
	return m.getActiveExchange().ConnectWebSocket(listenKey)
}

// Futures-specific methods
func (m *ModeAwareExchangeImpl) GetPositions(symbol string) ([]models.Position, error) {
	if m.currentMode != "futures" {
		return nil, fmt.Errorf("GetPositions is only available in futures mode")
	}
	return m.futuresExchange.GetPositions(symbol)
}

func (m *ModeAwareExchangeImpl) SetLeverage(symbol string, leverage int) error {
	if m.currentMode != "futures" {
		return fmt.Errorf("SetLeverage is only available in futures mode")
	}
	return m.futuresExchange.SetLeverage(symbol, leverage)
}

func (m *ModeAwareExchangeImpl) SetPositionMode(isHedgeMode bool) error {
	if m.currentMode != "futures" {
		return fmt.Errorf("SetPositionMode is only available in futures mode")
	}
	return m.futuresExchange.SetPositionMode(isHedgeMode)
}

func (m *ModeAwareExchangeImpl) GetPositionMode() (bool, error) {
	if m.currentMode != "futures" {
		return false, fmt.Errorf("GetPositionMode is only available in futures mode")
	}
	return m.futuresExchange.GetPositionMode()
}

func (m *ModeAwareExchangeImpl) SetMarginType(symbol string, marginType string) error {
	if m.currentMode != "futures" {
		return fmt.Errorf("SetMarginType is only available in futures mode")
	}
	return m.futuresExchange.SetMarginType(symbol, marginType)
}

func (m *ModeAwareExchangeImpl) GetMarginType(symbol string) (string, error) {
	if m.currentMode != "futures" {
		return "", fmt.Errorf("GetMarginType is only available in futures mode")
	}
	return m.futuresExchange.GetMarginType(symbol)
}

func (m *ModeAwareExchangeImpl) GetAccountState(symbol string) (positionValue float64, accountEquity float64, err error) {
	if m.currentMode != "futures" {
		return 0, 0, fmt.Errorf("GetAccountState is only available in futures mode")
	}
	return m.futuresExchange.GetAccountState(symbol)
}

func (m *ModeAwareExchangeImpl) GetMaxWalletExposure() float64 {
	if m.currentMode != "futures" {
		return 0
	}
	return m.futuresExchange.GetMaxWalletExposure()
}

func (m *ModeAwareExchangeImpl) GetBalance() (float64, error) {
	if m.currentMode != "futures" {
		return 0, fmt.Errorf("GetBalance is only available in futures mode")
	}
	return m.futuresExchange.GetBalance()
}

// Spot-specific methods
func (m *ModeAwareExchangeImpl) GetSpotBalances() ([]models.SpotBalance, error) {
	if m.currentMode != "spot" {
		return nil, fmt.Errorf("GetSpotBalances is only available in spot mode")
	}
	if m.spotExchange == nil {
		return nil, fmt.Errorf("spot exchange not configured")
	}
	
	// This would need to be implemented in the spot exchange
	// For now, return a placeholder implementation
	return []models.SpotBalance{}, fmt.Errorf("spot exchange implementation needed")
}

func (m *ModeAwareExchangeImpl) GetSpotBalance(asset string) (float64, error) {
	if m.currentMode != "spot" {
		return 0, fmt.Errorf("GetSpotBalance is only available in spot mode")
	}
	
	balances, err := m.GetSpotBalances()
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

func (m *ModeAwareExchangeImpl) GetSpotTradingFees(symbol string) (*models.TradingFees, error) {
	if m.currentMode != "spot" {
		return nil, fmt.Errorf("GetSpotTradingFees is only available in spot mode")
	}
	
	// Default spot trading fees (would be fetched from API in real implementation)
	return &models.TradingFees{
		MakerFee:    0.001, // 0.1%
		TakerFee:    0.001, // 0.1%
		Symbol:      symbol,
		TradingMode: "spot",
	}, nil
}

// Mode-aware methods for the trading interface
func (m *ModeAwareExchangeImpl) GetAccountStateForMode(mode string) (*trading.AccountState, error) {
	if mode != m.currentMode {
		return nil, fmt.Errorf("requested mode %s does not match current mode %s", mode, m.currentMode)
	}
	
	// This would be implemented based on the mode
	// For now, return a placeholder
	return &trading.AccountState{
		TradingMode: mode,
		LastUpdated: time.Now().Unix(),
	}, nil
}

func (m *ModeAwareExchangeImpl) GetBalancesForMode(mode string) (interface{}, error) {
	if mode != m.currentMode {
		return nil, fmt.Errorf("requested mode %s does not match current mode %s", mode, m.currentMode)
	}
	
	switch mode {
	case "futures":
		return m.GetPositions(m.config.Symbol)
	case "spot":
		return m.GetSpotBalances()
	default:
		return nil, fmt.Errorf("unsupported mode: %s", mode)
	}
}

func (m *ModeAwareExchangeImpl) GetTradingFeesForMode(mode, symbol string) (*trading.TradingFees, error) {
	if mode != m.currentMode {
		return nil, fmt.Errorf("requested mode %s does not match current mode %s", mode, m.currentMode)
	}
	
	switch mode {
	case "futures":
		// Default futures trading fees
		return &trading.TradingFees{
			MakerFee:    0.0002, // 0.02%
			TakerFee:    0.0004, // 0.04%
			Symbol:      symbol,
			TradingMode: "futures",
		}, nil
	case "spot":
		fees, err := m.GetSpotTradingFees(symbol)
		if err != nil {
			return nil, err
		}
		return &trading.TradingFees{
			MakerFee:    fees.MakerFee,
			TakerFee:    fees.TakerFee,
			Symbol:      fees.Symbol,
			TradingMode: fees.TradingMode,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported mode: %s", mode)
	}
}

func (m *ModeAwareExchangeImpl) GetSupportedModes() []string {
	var modes []string
	if m.futuresExchange != nil {
		modes = append(modes, "futures")
	}
	if m.spotExchange != nil {
		modes = append(modes, "spot")
	}
	return modes
}
