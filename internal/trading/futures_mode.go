package trading

import (
	"fmt"
	"strconv"
	"time"
	
	"github.com/yudaprama/grid-trading-bot/internal/models"
)

// FuturesMode implements TradingMode for Binance Futures trading
type FuturesMode struct {
	exchange ModeAwareExchange
	config   *models.Config
	
	// Cached data
	lastAccountState *AccountState
	lastUpdateTime   time.Time
}

// NewFuturesMode creates a new futures trading mode instance
func NewFuturesMode(exchange ModeAwareExchange, config *models.Config) (*FuturesMode, error) {
	if config.TradingMode != "futures" {
		return nil, fmt.Errorf("invalid trading mode for futures: %s", config.TradingMode)
	}
	
	return &FuturesMode{
		exchange: exchange,
		config:   config,
	}, nil
}

// GetAccountState returns the current account state for futures trading
func (f *FuturesMode) GetAccountState() (*AccountState, error) {
	// Use cached data if recent (within 5 seconds)
	if f.lastAccountState != nil && time.Since(f.lastUpdateTime) < 5*time.Second {
		return f.lastAccountState, nil
	}
	
	// Get positions for the symbol
	positions, err := f.exchange.GetBalancesForMode("futures")
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %v", err)
	}
	
	futuresPositions, ok := positions.([]models.Position)
	if !ok {
		return nil, fmt.Errorf("invalid position data type")
	}
	
	// Calculate account state from positions
	var positionValue, unrealizedPNL float64
	for _, pos := range futuresPositions {
		if pos.Symbol == f.config.Symbol {
			posAmt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
			unrealizedProfit, _ := strconv.ParseFloat(pos.UnrealizedProfit, 64)

			currentPrice, err := f.exchange.GetPrice(f.config.Symbol)
			if err != nil {
				return nil, fmt.Errorf("failed to get current price: %v", err)
			}

			positionValue = posAmt * currentPrice
			unrealizedPNL = unrealizedProfit
			break
		}
	}
	
	// Get account equity (this would need to be implemented in the exchange)
	accountEquity := f.config.InitialInvestment + unrealizedPNL // Simplified calculation
	marginUsed := positionValue / float64(f.config.Leverage)
	marginAvailable := accountEquity - marginUsed
	
	accountState := &AccountState{
		TradingMode:     "futures",
		TotalValue:      accountEquity,
		AvailableValue:  marginAvailable,
		AccountEquity:   accountEquity,
		PositionValue:   positionValue,
		UnrealizedPNL:   unrealizedPNL,
		MarginUsed:      marginUsed,
		MarginAvailable: marginAvailable,
		LastUpdated:     time.Now().Unix(),
	}
	
	// Cache the result
	f.lastAccountState = accountState
	f.lastUpdateTime = time.Now()
	
	return accountState, nil
}

// GetAvailableBalance returns available balance for futures trading
func (f *FuturesMode) GetAvailableBalance(asset string) (float64, error) {
	accountState, err := f.GetAccountState()
	if err != nil {
		return 0, err
	}
	
	// For futures, available balance is margin available
	return accountState.MarginAvailable, nil
}

// CanPlaceOrder checks if an order can be placed
func (f *FuturesMode) CanPlaceOrder(side string, price, quantity float64) (bool, error) {
	orderValue := quantity * price
	requiredMargin := orderValue / float64(f.config.Leverage)
	
	availableBalance, err := f.GetAvailableBalance("USDT")
	if err != nil {
		return false, err
	}
	
	// Check if we have enough margin
	if requiredMargin > availableBalance {
		return false, nil
	}
	
	// Check risk limits
	if !f.IsWithinRiskLimits(side, quantity, price) {
		return false, nil
	}
	
	return true, nil
}

// CalculateOrderQuantity calculates the appropriate order quantity
func (f *FuturesMode) CalculateOrderQuantity(side string, price float64, availableFunds float64) (float64, error) {
	// For futures, use the configured grid quantity
	return f.config.GridQuantity, nil
}

// ValidateOrderSize validates if the order size meets requirements
func (f *FuturesMode) ValidateOrderSize(side string, quantity, price float64) error {
	orderValue := quantity * price
	
	// Check minimum notional value
	if orderValue < f.config.MinNotionalValue {
		return fmt.Errorf("order value %.4f is below minimum notional %.4f", 
			orderValue, f.config.MinNotionalValue)
	}
	
	// Check symbol info for lot size and price filters
	symbolInfo, err := f.exchange.GetSymbolInfo(f.config.Symbol)
	if err != nil {
		return fmt.Errorf("failed to get symbol info: %v", err)
	}
	
	// Validate against filters
	for _, filter := range symbolInfo.Filters {
		switch filter.FilterType {
		case "LOT_SIZE":
			minQty, _ := strconv.ParseFloat(filter.MinQty, 64)
			maxQty, _ := strconv.ParseFloat(filter.MaxQty, 64)
			if quantity < minQty || quantity > maxQty {
				return fmt.Errorf("quantity %.8f is outside allowed range [%.8f, %.8f]", 
					quantity, minQty, maxQty)
			}
		case "MIN_NOTIONAL":
			minNotional, _ := strconv.ParseFloat(filter.MinNotional, 64)
			if orderValue < minNotional {
				return fmt.Errorf("order value %.4f is below minimum notional %.4f", 
					orderValue, minNotional)
			}
		}
	}
	
	return nil
}

// CalculateMaxOrderSize calculates the maximum order size
func (f *FuturesMode) CalculateMaxOrderSize(side string, price float64) (float64, error) {
	availableBalance, err := f.GetAvailableBalance("USDT")
	if err != nil {
		return 0, err
	}
	
	// Calculate max order value based on available margin
	maxOrderValue := availableBalance * float64(f.config.Leverage)
	
	// Apply exposure limit
	exposureLimit := f.GetExposureLimit()
	accountState, err := f.GetAccountState()
	if err != nil {
		return 0, err
	}
	
	maxExposureValue := accountState.AccountEquity * exposureLimit
	if maxOrderValue > maxExposureValue {
		maxOrderValue = maxExposureValue
	}
	
	return maxOrderValue / price, nil
}

// IsWithinRiskLimits checks if the order is within risk limits
func (f *FuturesMode) IsWithinRiskLimits(side string, quantity, price float64) bool {
	orderValue := quantity * price
	
	accountState, err := f.GetAccountState()
	if err != nil {
		return false
	}
	
	// Check exposure limit
	newExposure := (accountState.PositionValue + orderValue) / accountState.AccountEquity

	return newExposure <= f.GetExposureLimit()
}

// GetExposureLimit returns the exposure limit for futures trading
func (f *FuturesMode) GetExposureLimit() float64 {
	return f.config.WalletExposureLimit
}

// CalculateUnrealizedPnL calculates unrealized P&L
func (f *FuturesMode) CalculateUnrealizedPnL() (float64, error) {
	accountState, err := f.GetAccountState()
	if err != nil {
		return 0, err
	}
	
	return accountState.UnrealizedPNL, nil
}

// GetTradingFees returns trading fees for futures
func (f *FuturesMode) GetTradingFees() (*TradingFees, error) {
	return f.exchange.GetTradingFeesForMode("futures", f.config.Symbol)
}

// GetTradingMode returns the trading mode
func (f *FuturesMode) GetTradingMode() string {
	return "futures"
}

// GetRequiredAssets returns required assets for futures trading
func (f *FuturesMode) GetRequiredAssets() []string {
	return []string{"USDT"} // Futures typically uses USDT as margin
}

// SupportsLeverage returns true for futures trading
func (f *FuturesMode) SupportsLeverage() bool {
	return true
}
