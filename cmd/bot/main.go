package main

import (
	"github.com/yudaprama/grid-trading-bot/internal/bot"
	"github.com/yudaprama/grid-trading-bot/internal/config"
	"github.com/yudaprama/grid-trading-bot/internal/downloader"
	"github.com/yudaprama/grid-trading-bot/internal/exchange"
	"github.com/yudaprama/grid-trading-bot/internal/logger" // Added logger package
	"github.com/yudaprama/grid-trading-bot/internal/models"
	"github.com/yudaprama/grid-trading-bot/internal/reporter"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

// extractSymbolFromPath extracts trading pair name from data file path
// Example: "data/BNBUSDT-2025-03-15-2025-06-15.csv" -> "BNBUSDT"
func extractSymbolFromPath(path string) string {
	// Remove directory and .csv suffix
	name := strings.TrimSuffix(path, ".csv")
	parts := strings.Split(name, "/")
	fileName := parts[len(parts)-1]

	// Split by "-" and take first part
	symbolParts := strings.Split(fileName, "-")
	if len(symbolParts) > 0 {
		return symbolParts[0]
	}
	return ""
}

func main() {
	// --- Command line parameter definitions ---
	configPath := flag.String("config", "config.json", "path to the config file")
	mode := flag.String("mode", "live", "running mode: live or backtest")
	dataPath := flag.String("data", "", "path to historical data file for backtesting")
	symbol := flag.String("symbol", "", "symbol to backtest (e.g., BNBUSDT)")
	startDate := flag.String("start", "", "start date for backtesting (YYYY-MM-DD)")
	endDate := flag.String("end", "", "end date for backtesting (YYYY-MM-DD)")
	flag.Parse()

	// --- Initialize logging (early) ---
	// To enable logging during .env or config loading, we need to initialize a temporary or default logger
	// before other logic. Here we assume InitLogger can be safely called early
	logger.InitLogger(models.LogConfig{Level: "info", Output: "console"}) // Use a default configuration

	// --- Load .env file ---
	err := godotenv.Load()
	if err != nil {
		logger.S().Info("No .env file found, will read from system environment variables.")
	} else {
		logger.S().Info("Successfully loaded configuration from .env file.")
	}

	// --- Load JSON configuration ---
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		logger.S().Fatalf("Unable to load configuration file: %v", err)
	}

	// --- Re-initialize logging with configuration from file ---
	logger.InitLogger(cfg.LogConfig)
	defer logger.S().Sync() // Ensure all buffered logs are flushed when main function exits

	switch *mode {
	case "live":
		runLiveMode(cfg)
	case "backtest":
		finalDataPath, err := handleBacktestMode(*symbol, *startDate, *endDate, *dataPath)
		if err != nil {
			logger.S().Fatal(err)
		}
		runBacktestMode(cfg, finalDataPath)
	default:
		logger.S().Fatalf("Unknown running mode: %s. Please choose 'live' or 'backtest'.", *mode)
	}
}

// handleBacktestMode handles backtest mode startup logic, including data download.
// Returns data file path on success, error on failure.
func handleBacktestMode(symbol, startDate, endDate, dataPath string) (string, error) {
	// Check if data download is needed
	shouldDownload := symbol != "" && startDate != "" && endDate != ""

	if shouldDownload {
		startTime, err1 := time.Parse("2006-01-02", startDate)
		endTime, err2 := time.Parse("2006-01-02", endDate)
		if err1 != nil || err2 != nil {
			return "", fmt.Errorf("date format error, please use YYYY-MM-DD format. start: %v, end: %v", err1, err2)
		}

		// Ensure data directory exists
		if _, err := os.Stat("data"); os.IsNotExist(err) {
			if err := os.Mkdir("data", 0755); err != nil {
				return "", fmt.Errorf("failed to create data directory: %v", err)
			}
		}

		downloader := downloader.NewKlineDownloader()
		fileName := fmt.Sprintf("data/%s-%s-%s.csv", symbol, startDate, endDate)
		logger.S().Infof("Starting to download K-line data for %s from %s to %s...", symbol, startDate, endDate)

		if err := downloader.DownloadKlines(symbol, fileName, startTime, endTime); err != nil {
			return "", fmt.Errorf("failed to download data: %v", err)
		}
		return fileName, nil // Return downloaded file path
	}

	// If not downloading, data path must be provided
	if dataPath == "" {
		return "", fmt.Errorf("backtest mode requires data source specified via --data or --symbol/start/end parameters")
	}
	return dataPath, nil
}

// runLiveMode runs the live trading bot
func runLiveMode(cfg *models.Config) {
	logger.S().Info("--- Starting Live Trading Mode ---")

	// Load API keys from environment variables
	apiKey := os.Getenv("BINANCE_API_KEY")
	secretKey := os.Getenv("BINANCE_SECRET_KEY")
	if apiKey == "" || secretKey == "" {
		logger.S().Fatal("Error: BINANCE_API_KEY and BINANCE_SECRET_KEY environment variables must be set.")
	}

	// Set API URLs based on configuration
	var baseURL, wsBaseURL string
	if cfg.IsTestnet {
		baseURL = cfg.TestnetAPIURL
		wsBaseURL = cfg.TestnetWSURL
		logger.S().Info("Using Binance testnet...")
	} else {
		baseURL = cfg.LiveAPIURL
		wsBaseURL = cfg.LiveWSURL
		logger.S().Info("Using Binance production network...")
	}
	cfg.BaseURL = baseURL
	cfg.WSBaseURL = wsBaseURL

	// Initialize exchange
	liveExchange, err := exchange.NewLiveExchange(apiKey, secretKey, cfg.BaseURL, cfg.WSBaseURL, logger.L())
	if err != nil {
		logger.S().Fatalf("Failed to initialize exchange: %v", err)
	}

	// --- Initialize exchange settings ---
	logger.S().Info("Initializing exchange settings...")

	// 1. Set position mode (one-way/hedge)
	if _, err := liveExchange.GetAccountInfo(); err != nil {
		logger.S().Fatalf("Failed to call GetAccountInfo: %v", err)
		return
	}

	// 1. Set position mode (one-way/hedge)
	currentHedgeMode, err := liveExchange.GetPositionMode()
	if err != nil {
		logger.S().Fatalf("Failed to get current position mode: %v", err)
		return
	}

	if currentHedgeMode != cfg.HedgeMode {
		logger.S().Infof("Current position mode (HedgeMode=%v) doesn't match configuration (HedgeMode=%v), attempting to update...", currentHedgeMode, cfg.HedgeMode)
		if err := liveExchange.SetPositionMode(cfg.HedgeMode); err != nil {
			logger.S().Fatalf("Failed to set position mode: %v", err)
			return
		}
		logger.S().Infof("Position mode successfully updated to: HedgeMode=%v", cfg.HedgeMode)
	} else {
		logger.S().Infof("Current position mode is already target mode (HedgeMode=%v), no change needed.", cfg.HedgeMode)
	}

	// 2. Set margin mode (cross/isolated)
	currentMarginType, err := liveExchange.GetMarginType(cfg.Symbol)
	if err != nil {
		logger.S().Fatalf("Failed to get current margin mode: %v", err)
		return
	}

	// Compare ignoring case
	if !strings.EqualFold(currentMarginType, cfg.MarginType) {
		logger.S().Infof("Current margin mode (%s) doesn't match configuration (%s), attempting to update...", currentMarginType, cfg.MarginType)
		if err := liveExchange.SetMarginType(cfg.Symbol, cfg.MarginType); err != nil {
			logger.S().Fatalf("Failed to set margin mode: %v", err)
			return
		}
		logger.S().Infof("Margin mode successfully updated to: %s", cfg.MarginType)
	} else {
		logger.S().Infof("Current margin mode is already target mode (%s), no change needed.", cfg.MarginType)
	}

	// Initialize bot
	gridBot := bot.NewGridTradingBot(cfg, liveExchange, false)

	// Start bot
	if err := gridBot.Start(); err != nil {
		logger.S().Fatalf("Failed to start bot: %v", err)
		return
	}

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// Stop bot, state saving logic has been moved to bot.Stop() internally
	gridBot.Stop()
	logger.S().Info("Bot has been successfully stopped.")
}

// runBacktestMode runs backtest mode
func runBacktestMode(cfg *models.Config, dataPath string) {
	logger.S().Info("--- Starting Backtest Mode ---")
	cfg.WSBaseURL = "ws://localhost" // In backtest, we don't need real ws connection

	// Extract symbol from data path and use it to override config value
	backtestSymbol := extractSymbolFromPath(dataPath)
	if backtestSymbol == "" {
		logger.S().Fatalf("Unable to extract trading pair from data file path %s", dataPath)
	}
	cfg.Symbol = backtestSymbol // Ensure bot logic also uses correct symbol

	// Use new constructor and pass complete config
	backtestExchange := exchange.NewBacktestExchange(cfg)
	gridBot := bot.NewGridTradingBot(cfg, backtestExchange, true)

	// Load and process historical data
	file, err := os.Open(dataPath)
	if err != nil {
		logger.S().Fatalf("Unable to open historical data file: %v", err)
	}
	defer file.Close()

	// --- Refactor data reading to capture time ---
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		logger.S().Fatalf("Unable to read all CSV records: %v", err)
	}
	if len(records) <= 1 { // Need at least header and one data row
		logger.S().Fatal("Historical data file is empty or contains only header.")
	}

	// Remove header
	records = records[1:]

	// Parse start and end times
	startTimeMs, _ := strconv.ParseInt(records[0][0], 10, 64)
	endTimeMs, _ := strconv.ParseInt(records[len(records)-1][0], 10, 64)
	startTime := time.UnixMilli(startTimeMs)
	endTime := time.UnixMilli(endTimeMs)

	// --- Use first row data for initialization ---
	initialRecord := records[0]
	initialTimeMs, _ := strconv.ParseInt(initialRecord[0], 10, 64)
	initialTime := time.UnixMilli(initialTimeMs)
	initialOpen, errO := strconv.ParseFloat(initialRecord[1], 64)
	initialHigh, errH := strconv.ParseFloat(initialRecord[2], 64)
	initialLow, errL := strconv.ParseFloat(initialRecord[3], 64)
	initialClose, errC := strconv.ParseFloat(initialRecord[4], 64)
	if errO != nil || errH != nil || errL != nil || errC != nil {
		logger.S().With(
			zap.Error(errO),
			zap.Error(errH),
			zap.Error(errL),
			zap.Error(errC),
		).Fatal("Unable to parse initial prices")
	}

	backtestExchange.SetPrice(initialOpen, initialHigh, initialLow, initialClose, initialTime)
	gridBot.SetCurrentPrice(initialClose)
	if err := gridBot.StartForBacktest(); err != nil {
		logger.S().Fatalf("Failed to initialize backtest bot: %v", err)
	}
	logger.S().Infof("Bot initialization completed using initial price %.2f.\n", initialClose)

	// --- Process all data points in loop ---
	logger.S().Info("Starting backtest...")
	for _, record := range records {
		// Check for liquidation or halt status
		if backtestExchange.IsLiquidated() {
			logger.S().Warn("Liquidation detected, terminating backtest loop early.")
			break
		}
		if gridBot.IsHalted() {
			logger.S().Info("Bot is halted, terminating backtest loop early.")
			break
		}

		timestampMs, errT := strconv.ParseInt(record[0], 10, 64)
		openPrice, errO := strconv.ParseFloat(record[1], 64)
		high, errH := strconv.ParseFloat(record[2], 64)
		low, errL := strconv.ParseFloat(record[3], 64)
		closePrice, errC := strconv.ParseFloat(record[4], 64)
		if errT != nil || errO != nil || errH != nil || errL != nil || errC != nil {
			logger.S().Warnf("Unable to parse K-line data, skipping this record: %v", record)
			continue
		}
		timestamp := time.UnixMilli(timestampMs)
		backtestExchange.SetPrice(openPrice, high, low, closePrice, timestamp)
		gridBot.ProcessBacktestTick()
	}

	logger.S().Info("Backtest completed.")

	// --- Generate and print backtest report ---
	reporter.GenerateReport(backtestExchange, dataPath, startTime, endTime)
}
