package trading

import (
	"fmt"
	"math"
	"strconv"

	"github.com/yudaprama/grid-trading-bot/internal/models"
)

// TradingMode defines the interface for different trading modes (futures, spot)
type TradingMode interface {
	// Account management
	GetAccountState() (*AccountState, error)
	GetAvailableBalance(asset string) (float64, error)
	
	// Order management
	CanPlaceOrder(side string, price, quantity float64) (bool, error)
	CalculateOrderQuantity(side string, price float64, availableFunds float64) (float64, error)
	ValidateOrderSize(side string, quantity, price float64) error
	
	// Risk management
	CalculateMaxOrderSize(side string, price float64) (float64, error)
	IsWithinRiskLimits(side string, quantity, price float64) bool
	GetExposureLimit() float64
	
	// Profit and performance
	CalculateUnrealizedPnL() (float64, error)
	GetTradingFees() (*TradingFees, error)
	
	// Mode-specific information
	GetTradingMode() string
	GetRequiredAssets() []string
	SupportsLeverage() bool
}

// AccountState represents the current account state for any trading mode
type AccountState struct {
	TradingMode string `json:"trading_mode"`
	
	// Common fields
	TotalValue    float64 `json:"total_value"`
	AvailableValue float64 `json:"available_value"`
	
	// Futures-specific fields
	AccountEquity    float64 `json:"account_equity,omitempty"`
	PositionValue    float64 `json:"position_value,omitempty"`
	UnrealizedPNL    float64 `json:"unrealized_pnl,omitempty"`
	MarginUsed       float64 `json:"margin_used,omitempty"`
	MarginAvailable  float64 `json:"margin_available,omitempty"`
	
	// Spot-specific fields
	BaseBalance      float64 `json:"base_balance,omitempty"`
	QuoteBalance     float64 `json:"quote_balance,omitempty"`
	BaseInOrders     float64 `json:"base_in_orders,omitempty"`
	QuoteInOrders    float64 `json:"quote_in_orders,omitempty"`
	BaseAsset        string  `json:"base_asset,omitempty"`
	QuoteAsset       string  `json:"quote_asset,omitempty"`
	
	// Additional metadata
	LastUpdated      int64   `json:"last_updated"`
}

// TradingFees represents the fee structure for a trading mode
type TradingFees struct {
	MakerFee    float64 `json:"maker_fee"`
	TakerFee    float64 `json:"taker_fee"`
	Symbol      string  `json:"symbol"`
	TradingMode string  `json:"trading_mode"`
}

// OrderValidation represents order validation result
type OrderValidation struct {
	IsValid      bool    `json:"is_valid"`
	Reason       string  `json:"reason,omitempty"`
	MaxQuantity  float64 `json:"max_quantity,omitempty"`
	MinQuantity  float64 `json:"min_quantity,omitempty"`
	AdjustedQty  float64 `json:"adjusted_qty,omitempty"`
}

// RiskLimits represents risk management limits for a trading mode
type RiskLimits struct {
	MaxPositionSize   float64 `json:"max_position_size"`
	MaxOrderSize      float64 `json:"max_order_size"`
	ExposureLimit     float64 `json:"exposure_limit"`
	MinOrderValue     float64 `json:"min_order_value"`
	MaxOrderValue     float64 `json:"max_order_value"`
}

// TradingModeConfig represents configuration specific to a trading mode
type TradingModeConfig struct {
	Mode           string     `json:"mode"`
	RiskLimits     RiskLimits `json:"risk_limits"`
	
	// Futures-specific
	Leverage       int        `json:"leverage,omitempty"`
	MarginType     string     `json:"margin_type,omitempty"`
	HedgeMode      bool       `json:"hedge_mode,omitempty"`
	
	// Spot-specific
	BaseAsset      string     `json:"base_asset,omitempty"`
	QuoteAsset     string     `json:"quote_asset,omitempty"`
	InitialBase    float64    `json:"initial_base,omitempty"`
	InitialQuote   float64    `json:"initial_quote,omitempty"`
}

// Exchange interface extension for mode-specific operations
type ModeAwareExchange interface {
	// Common exchange methods
	GetPrice(symbol string) (float64, error)
	PlaceOrder(symbol, side, orderType string, quantity, price float64, clientOrderID string) (*models.Order, error)
	CancelOrder(symbol string, orderID int64) error
	GetOrderStatus(symbol string, orderID int64) (*models.Order, error)
	GetOpenOrders(symbol string) ([]models.Order, error)
	GetSymbolInfo(symbol string) (*models.SymbolInfo, error)
	
	// Mode-specific methods
	GetAccountStateForMode(mode string) (*AccountState, error)
	GetBalancesForMode(mode string) (interface{}, error)
	GetTradingFeesForMode(mode, symbol string) (*TradingFees, error)
	
	// Mode detection
	SupportsTradingMode(mode string) bool
	GetSupportedModes() []string
}

// TradingModeFactory creates trading mode instances
type TradingModeFactory struct {
	exchange ModeAwareExchange
	config   *models.Config
}

// NewTradingModeFactory creates a new factory instance
func NewTradingModeFactory(exchange ModeAwareExchange, config *models.Config) *TradingModeFactory {
	return &TradingModeFactory{
		exchange: exchange,
		config:   config,
	}
}

// CreateTradingMode creates a trading mode instance based on configuration
func (f *TradingModeFactory) CreateTradingMode() (TradingMode, error) {
	switch f.config.TradingMode {
	case "futures":
		return NewFuturesMode(f.exchange, f.config)
	case "spot":
		return NewSpotMode(f.exchange, f.config)
	default:
		return nil, fmt.Errorf("unsupported trading mode: %s", f.config.TradingMode)
	}
}

// Helper functions for common operations
func CalculateOrderValue(quantity, price float64) float64 {
	return quantity * price
}

func CalculateRequiredMargin(orderValue float64, leverage int) float64 {
	if leverage <= 0 {
		return orderValue
	}
	return orderValue / float64(leverage)
}

func ValidateMinNotional(orderValue, minNotional float64) bool {
	return orderValue >= minNotional
}

func AdjustQuantityToStepSize(quantity float64, stepSize string) (float64, error) {
	step, err := strconv.ParseFloat(stepSize, 64)
	if err != nil {
		return quantity, err
	}
	
	if step <= 0 {
		return quantity, nil
	}
	
	return math.Floor(quantity/step) * step, nil
}

func AdjustPriceToTickSize(price float64, tickSize string) (float64, error) {
	tick, err := strconv.ParseFloat(tickSize, 64)
	if err != nil {
		return price, err
	}
	
	if tick <= 0 {
		return price, nil
	}
	
	return math.Round(price/tick) * tick, nil
}
