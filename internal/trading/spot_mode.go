package trading

import (
	"fmt"
	"strconv"
	"time"
	
	"github.com/yudaprama/grid-trading-bot/internal/models"
)

// SpotBalance represents a spot trading balance
type SpotBalance struct {
	Asset  string `json:"asset"`
	Free   string `json:"free"`
	Locked string `json:"locked"`
}

// SpotMode implements TradingMode for Binance Spot trading
type SpotMode struct {
	exchange ModeAwareExchange
	config   *models.Config
	
	// Cached data
	lastAccountState *AccountState
	lastUpdateTime   time.Time
}

// NewSpotMode creates a new spot trading mode instance
func NewSpotMode(exchange ModeAwareExchange, config *models.Config) (*SpotMode, error) {
	if config.TradingMode != "spot" {
		return nil, fmt.Errorf("invalid trading mode for spot: %s", config.TradingMode)
	}
	
	// Validate spot-specific configuration
	if config.BaseAsset == "" || config.QuoteAsset == "" {
		return nil, fmt.Errorf("base_asset and quote_asset must be specified for spot trading")
	}
	
	return &SpotMode{
		exchange: exchange,
		config:   config,
	}, nil
}

// GetAccountState returns the current account state for spot trading
func (s *SpotMode) GetAccountState() (*AccountState, error) {
	// Use cached data if recent (within 5 seconds)
	if s.lastAccountState != nil && time.Since(s.lastUpdateTime) < 5*time.Second {
		return s.lastAccountState, nil
	}
	
	// Get balances for spot trading
	balances, err := s.exchange.GetBalancesForMode("spot")
	if err != nil {
		return nil, fmt.Errorf("failed to get balances: %v", err)
	}
	
	spotBalances, ok := balances.([]SpotBalance)
	if !ok {
		return nil, fmt.Errorf("invalid balance data type")
	}
	
	// Extract base and quote balances
	var baseBalance, quoteBalance, baseInOrders, quoteInOrders float64
	
	for _, balance := range spotBalances {
		free, _ := strconv.ParseFloat(balance.Free, 64)
		locked, _ := strconv.ParseFloat(balance.Locked, 64)
		
		if balance.Asset == s.config.BaseAsset {
			baseBalance = free
			baseInOrders = locked
		} else if balance.Asset == s.config.QuoteAsset {
			quoteBalance = free
			quoteInOrders = locked
		}
	}
	
	// Get current price to calculate total value
	currentPrice, err := s.exchange.GetPrice(s.config.Symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to get current price: %v", err)
	}
	
	// Calculate total value in quote asset terms
	totalBaseValue := (baseBalance + baseInOrders) * currentPrice
	totalQuoteValue := quoteBalance + quoteInOrders
	totalValue := totalBaseValue + totalQuoteValue
	
	// Available value is free balances only
	availableValue := (baseBalance * currentPrice) + quoteBalance
	
	accountState := &AccountState{
		TradingMode:    "spot",
		TotalValue:     totalValue,
		AvailableValue: availableValue,
		BaseBalance:    baseBalance,
		QuoteBalance:   quoteBalance,
		BaseInOrders:   baseInOrders,
		QuoteInOrders:  quoteInOrders,
		BaseAsset:      s.config.BaseAsset,
		QuoteAsset:     s.config.QuoteAsset,
		LastUpdated:    time.Now().Unix(),
	}
	
	// Cache the result
	s.lastAccountState = accountState
	s.lastUpdateTime = time.Now()
	
	return accountState, nil
}

// GetAvailableBalance returns available balance for a specific asset
func (s *SpotMode) GetAvailableBalance(asset string) (float64, error) {
	accountState, err := s.GetAccountState()
	if err != nil {
		return 0, err
	}
	
	if asset == s.config.BaseAsset {
		return accountState.BaseBalance, nil
	} else if asset == s.config.QuoteAsset {
		return accountState.QuoteBalance, nil
	}
	
	return 0, fmt.Errorf("unsupported asset: %s", asset)
}

// CanPlaceOrder checks if an order can be placed
func (s *SpotMode) CanPlaceOrder(side string, price, quantity float64) (bool, error) {
	if side == "BUY" {
		// Check quote asset balance
		requiredQuote := quantity * price
		availableQuote, err := s.GetAvailableBalance(s.config.QuoteAsset)
		if err != nil {
			return false, err
		}
		
		return availableQuote >= requiredQuote, nil
	} else if side == "SELL" {
		// Check base asset balance
		availableBase, err := s.GetAvailableBalance(s.config.BaseAsset)
		if err != nil {
			return false, err
		}
		
		return availableBase >= quantity, nil
	}
	
	return false, fmt.Errorf("invalid order side: %s", side)
}

// CalculateOrderQuantity calculates the appropriate order quantity for spot trading
func (s *SpotMode) CalculateOrderQuantity(side string, price float64, availableFunds float64) (float64, error) {
	// For spot trading, calculate quantity based on available balance and risk management
	accountState, err := s.GetAccountState()
	if err != nil {
		return 0, err
	}
	
	// Use a percentage of available balance per order (e.g., 10%)
	orderSizePercentage := 0.1
	
	if side == "BUY" {
		// Calculate quantity based on available quote balance
		maxQuoteAmount := accountState.QuoteBalance * orderSizePercentage
		if availableFunds > 0 && availableFunds < maxQuoteAmount {
			maxQuoteAmount = availableFunds
		}
		return maxQuoteAmount / price, nil
	} else if side == "SELL" {
		// Calculate quantity based on available base balance
		maxBaseAmount := accountState.BaseBalance * orderSizePercentage
		return maxBaseAmount, nil
	}
	
	return 0, fmt.Errorf("invalid order side: %s", side)
}

// ValidateOrderSize validates if the order size meets requirements
func (s *SpotMode) ValidateOrderSize(side string, quantity, price float64) error {
	orderValue := quantity * price
	
	// Check minimum notional value
	if orderValue < s.config.MinNotionalValue {
		return fmt.Errorf("order value %.4f is below minimum notional %.4f", 
			orderValue, s.config.MinNotionalValue)
	}
	
	// Check symbol info for lot size and price filters
	symbolInfo, err := s.exchange.GetSymbolInfo(s.config.Symbol)
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

// CalculateMaxOrderSize calculates the maximum order size for spot trading
func (s *SpotMode) CalculateMaxOrderSize(side string, price float64) (float64, error) {
	if side == "BUY" {
		availableQuote, err := s.GetAvailableBalance(s.config.QuoteAsset)
		if err != nil {
			return 0, err
		}
		
		// Apply exposure limit
		exposureLimit := s.GetExposureLimit()
		accountState, err := s.GetAccountState()
		if err != nil {
			return 0, err
		}
		
		maxOrderValue := accountState.QuoteBalance * exposureLimit
		if maxOrderValue > availableQuote {
			maxOrderValue = availableQuote
		}
		
		return maxOrderValue / price, nil
	} else if side == "SELL" {
		availableBase, err := s.GetAvailableBalance(s.config.BaseAsset)
		if err != nil {
			return 0, err
		}
		
		// Apply exposure limit
		exposureLimit := s.GetExposureLimit()
		maxQuantity := availableBase * exposureLimit
		
		return maxQuantity, nil
	}
	
	return 0, fmt.Errorf("invalid order side: %s", side)
}

// IsWithinRiskLimits checks if the order is within risk limits for spot trading
func (s *SpotMode) IsWithinRiskLimits(side string, quantity, price float64) bool {
	orderValue := quantity * price
	
	accountState, err := s.GetAccountState()
	if err != nil {
		return false
	}
	
	// For spot trading, check if order value is within exposure limit
	exposureLimit := s.GetExposureLimit()
	maxOrderValue := accountState.TotalValue * exposureLimit
	
	return orderValue <= maxOrderValue
}

// GetExposureLimit returns the exposure limit for spot trading
func (s *SpotMode) GetExposureLimit() float64 {
	// For spot trading, use a more conservative exposure limit
	return s.config.WalletExposureLimit * 0.5 // 50% of configured limit
}

// CalculateUnrealizedPnL calculates unrealized P&L for spot trading
func (s *SpotMode) CalculateUnrealizedPnL() (float64, error) {
	accountState, err := s.GetAccountState()
	if err != nil {
		return 0, err
	}
	
	// For spot trading, calculate P&L based on asset appreciation
	currentPrice, err := s.exchange.GetPrice(s.config.Symbol)
	if err != nil {
		return 0, err
	}
	
	// Calculate initial value
	initialValue := (s.config.InitialBaseAmount * currentPrice) + s.config.InitialQuoteAmount
	
	// Current value vs initial value
	unrealizedPnL := accountState.TotalValue - initialValue
	
	return unrealizedPnL, nil
}

// GetTradingFees returns trading fees for spot trading
func (s *SpotMode) GetTradingFees() (*TradingFees, error) {
	return s.exchange.GetTradingFeesForMode("spot", s.config.Symbol)
}

// GetTradingMode returns the trading mode
func (s *SpotMode) GetTradingMode() string {
	return "spot"
}

// GetRequiredAssets returns required assets for spot trading
func (s *SpotMode) GetRequiredAssets() []string {
	return []string{s.config.BaseAsset, s.config.QuoteAsset}
}

// SupportsLeverage returns false for spot trading
func (s *SpotMode) SupportsLeverage() bool {
	return false
}
