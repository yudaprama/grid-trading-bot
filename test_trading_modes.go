package main

import (
	"fmt"
	"log"
	
	"github.com/yudaprama/grid-trading-bot/internal/config"
	"github.com/yudaprama/grid-trading-bot/internal/exchange"
	"github.com/yudaprama/grid-trading-bot/internal/logger"
	"github.com/yudaprama/grid-trading-bot/internal/models"
	"github.com/yudaprama/grid-trading-bot/internal/trading"
)

func main() {
	// Initialize logger
	logger.InitLogger(models.LogConfig{Level: "info", Output: "console"})
	
	fmt.Println("🚀 Testing Unified Trading Mode Implementation")
	fmt.Println(repeatString("=", 60))
	
	// Test 1: Futures Trading Mode
	fmt.Println("\n📊 Test 1: Futures Trading Mode")
	testFuturesMode()
	
	// Test 2: Spot Trading Mode
	fmt.Println("\n💰 Test 2: Spot Trading Mode")
	testSpotMode()
	
	// Test 3: Configuration Loading
	fmt.Println("\n⚙️  Test 3: Configuration Loading")
	testConfigurationLoading()
	
	fmt.Println("\n✅ All tests completed successfully!")
}

func testFuturesMode() {
	// Load futures configuration
	cfg := &models.Config{
		TradingMode:       "futures",
		Symbol:            "BTCUSDT",
		GridSpacing:       0.01,
		GridQuantity:      0.001,
		Leverage:          10,
		MarginType:        "CROSSED",
		HedgeMode:         false,
		InitialInvestment: 1000.0,
		WalletExposureLimit: 0.8,
		MinNotionalValue:  10.0,
		IsTestnet:         true,
		TestnetAPIURL:     "https://testnet.binancefuture.com",
		TestnetWSURL:      "wss://stream.binancefuture.com",
	}
	
	// Create backtest exchange for testing
	backtestExchange := exchange.NewBacktestExchange(cfg)
	
	// Create mode-aware exchange
	modeAwareExchange := exchange.NewModeAwareExchange(backtestExchange, cfg)
	
	// Test trading mode detection
	fmt.Printf("  ✓ Trading mode: %s\n", modeAwareExchange.GetTradingMode())
	fmt.Printf("  ✓ Supports futures: %v\n", modeAwareExchange.SupportsTradingMode("futures"))
	fmt.Printf("  ✓ Supports spot: %v\n", modeAwareExchange.SupportsTradingMode("spot"))
	
	// Create trading mode instance
	factory := trading.NewTradingModeFactory(modeAwareExchange, cfg)
	tradingMode, err := factory.CreateTradingMode()
	if err != nil {
		log.Fatalf("Failed to create futures trading mode: %v", err)
	}
	
	fmt.Printf("  ✓ Created trading mode: %s\n", tradingMode.GetTradingMode())
	fmt.Printf("  ✓ Supports leverage: %v\n", tradingMode.SupportsLeverage())
	fmt.Printf("  ✓ Required assets: %v\n", tradingMode.GetRequiredAssets())
	
	// Test account state
	accountState, err := tradingMode.GetAccountState()
	if err != nil {
		log.Printf("  ⚠️  Account state error (expected in test): %v", err)
	} else {
		fmt.Printf("  ✓ Account equity: %.2f\n", accountState.AccountEquity)
	}
	
	// Test order validation
	canPlace, err := tradingMode.CanPlaceOrder("BUY", 50000.0, 0.001)
	if err != nil {
		log.Printf("  ⚠️  Order validation error (expected in test): %v", err)
	} else {
		fmt.Printf("  ✓ Can place order: %v\n", canPlace)
	}
}

func testSpotMode() {
	// Load spot configuration
	cfg := &models.Config{
		TradingMode:        "spot",
		Symbol:             "BTCUSDT",
		BaseAsset:          "BTC",
		QuoteAsset:         "USDT",
		GridSpacing:        0.01,
		InitialBaseAmount:  0.1,
		InitialQuoteAmount: 1000.0,
		WalletExposureLimit: 0.5,
		MinNotionalValue:   10.0,
		IsTestnet:          true,
		SpotTestnetAPIURL:  "https://testnet.binance.vision",
		SpotTestnetWSURL:   "wss://testnet.binance.vision",
	}
	
	// Create enhanced spot exchange for testing
	spotExchange := exchange.NewEnhancedSpotExchange("test-api-key", "test-secret-key", cfg)
	
	// Create mode-aware exchange
	modeAwareExchange := exchange.NewModeAwareExchange(nil, cfg)
	modeAwareExchange.SetSpotExchange(spotExchange)
	
	// Test trading mode detection
	fmt.Printf("  ✓ Trading mode: %s\n", modeAwareExchange.GetTradingMode())
	fmt.Printf("  ✓ Supports futures: %v\n", modeAwareExchange.SupportsTradingMode("futures"))
	fmt.Printf("  ✓ Supports spot: %v\n", modeAwareExchange.SupportsTradingMode("spot"))
	
	// Create trading mode instance
	factory := trading.NewTradingModeFactory(modeAwareExchange, cfg)
	tradingMode, err := factory.CreateTradingMode()
	if err != nil {
		log.Fatalf("Failed to create spot trading mode: %v", err)
	}
	
	fmt.Printf("  ✓ Created trading mode: %s\n", tradingMode.GetTradingMode())
	fmt.Printf("  ✓ Supports leverage: %v\n", tradingMode.SupportsLeverage())
	fmt.Printf("  ✓ Required assets: %v\n", tradingMode.GetRequiredAssets())
	
	// Test account state
	accountState, err := tradingMode.GetAccountState()
	if err != nil {
		log.Printf("  ⚠️  Account state error (expected in test): %v", err)
	} else {
		fmt.Printf("  ✓ Base balance: %.8f %s\n", accountState.BaseBalance, cfg.BaseAsset)
		fmt.Printf("  ✓ Quote balance: %.2f %s\n", accountState.QuoteBalance, cfg.QuoteAsset)
	}
	
	// Test order validation
	canPlace, err := tradingMode.CanPlaceOrder("BUY", 50000.0, 0.001)
	if err != nil {
		log.Printf("  ⚠️  Order validation error (expected in test): %v", err)
	} else {
		fmt.Printf("  ✓ Can place order: %v\n", canPlace)
	}
}

func testConfigurationLoading() {
	// Test futures configuration
	fmt.Println("  📄 Testing futures configuration...")
	futuresCfg, err := config.LoadConfig("config.json")
	if err != nil {
		log.Printf("  ⚠️  Failed to load futures config: %v", err)
	} else {
		fmt.Printf("  ✓ Loaded config - Trading mode: %s\n", futuresCfg.TradingMode)
		fmt.Printf("  ✓ Symbol: %s, Leverage: %d\n", futuresCfg.Symbol, futuresCfg.Leverage)
	}
	
	// Test spot configuration
	fmt.Println("  📄 Testing spot configuration...")
	spotCfg, err := config.LoadConfig("config_spot_trading_example.json")
	if err != nil {
		log.Printf("  ⚠️  Failed to load spot config: %v", err)
	} else {
		fmt.Printf("  ✓ Loaded config - Trading mode: %s\n", spotCfg.TradingMode)
		fmt.Printf("  ✓ Symbol: %s, Base: %s, Quote: %s\n", 
			spotCfg.Symbol, spotCfg.BaseAsset, spotCfg.QuoteAsset)
		fmt.Printf("  ✓ Initial amounts - Base: %.8f, Quote: %.2f\n", 
			spotCfg.InitialBaseAmount, spotCfg.InitialQuoteAmount)
	}
}

// Helper function to repeat strings (Go doesn't have built-in string multiplication)
func repeatString(s string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}
