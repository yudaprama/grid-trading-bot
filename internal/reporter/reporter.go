package reporter

import (
	"github.com/yudaprama/grid-trading-bot/internal/exchange"
	"github.com/yudaprama/grid-trading-bot/internal/logger"
	"github.com/yudaprama/grid-trading-bot/internal/models"
	"math"
	"sort"
	"time"
)

// Metrics stores all the backtest performance metrics
type Metrics struct {
	InitialBalance      float64
	FinalEquity         float64 // Renamed: final balance -> final total assets
	TotalPnL            float64 // Renamed: total profit -> total profit and loss (including floating)
	RealizedProfit      float64 // New: realized profit
	UnrealizedPnL       float64 // New: unrealized profit and loss
	ProfitPercentage    float64
	TotalTrades         int
	WinningTrades       int
	LosingTrades        int
	WinRate             float64
	AvgProfitLoss       float64
	MaxDrawdown         float64
	SharpeRatio         float64
	AnnualizedReturn    float64       // New: annualized return
	EndingCash          float64       // New: ending cash
	EndingAssetValue    float64       // New: ending asset value
	AvgHoldDuration     time.Duration // New: average holding duration
	AvgWinHoldDuration  time.Duration // New: average holding duration of winning trades
	AvgLossHoldDuration time.Duration // New: average holding duration of losing trades
	TotalSlippage       float64       // New: total slippage cost
	TotalAssetQty       float64       // New: total quantity of held assets
	TotalFees           float64       // New: total fees
	StartTime           time.Time
	EndTime             time.Time
	MaxWalletExposure   float64 // New: maximum wallet exposure
}

// GenerateReport calculates and prints the performance report based on the backtest exchange state
func GenerateReport(backtestExchange *exchange.BacktestExchange, dataPath string, startTime, endTime time.Time) {
	logger.S().Info("Starting to generate backtest report...")
	logger.S().Infof("Number of completed trades to process: %d", len(backtestExchange.TradeLog))

	metrics, symbol := calculateMetrics(backtestExchange, startTime, endTime)

	logger.S().Info("========== Backtest Result Report ==========")
	logger.S().Infof("Data file:         %s", dataPath)
	logger.S().Infof("Symbol:           %s", symbol)
	logger.S().Infof("Backtest period:         %s 到 %s", metrics.StartTime.Format("2006-01-02 15:04"), metrics.EndTime.Format("2006-01-02 15:04"))
	logger.S().Info("------------------------------------")
	logger.S().Infof("Initial balance:         %.2f USDT", metrics.InitialBalance)
	logger.S().Infof("Ending total assets (Equity): %.2f USDT", metrics.FinalEquity)
	logger.S().Infof("Total profit and loss (Total PnL): %.2f USDT", metrics.TotalPnL)
	logger.S().Infof("  - Realized profit:   %.2f USDT", metrics.RealizedProfit)
	logger.S().Infof("  - Unrealized profit and loss:     %.2f USDT", metrics.UnrealizedPnL)
	logger.S().Infof("Total return rate:         %.2f%%", metrics.ProfitPercentage)
	logger.S().Infof("Total fees:         %.4f USDT", metrics.TotalFees)
	logger.S().Infof("Total slippage cost:       %.4f USDT", metrics.TotalSlippage)
	logger.S().Info("------------------------------------")
	logger.S().Infof("Total trades:       %d", metrics.TotalTrades)
	logger.S().Infof("Winning trades:         %d", metrics.WinningTrades)
	logger.S().Infof("Losing trades:         %d", metrics.LosingTrades)
	logger.S().Infof("Win rate:             %.2f%%", metrics.WinRate)
	logger.S().Infof("Average profit and loss ratio:       %.2f", metrics.AvgProfitLoss)
	logger.S().Infof("Maximum drawdown:         %.2f%%", metrics.MaxDrawdown)
	logger.S().Infof("Maximum wallet exposure: %.2f%%", metrics.MaxWalletExposure*100)
	logger.S().Info("------------------------------------")
	logger.S().Infof("Average holding duration:     %s", metrics.AvgHoldDuration)
	logger.S().Infof("Average holding duration of winning trades: %s", metrics.AvgWinHoldDuration)
	logger.S().Infof("Average holding duration of losing trades: %s", metrics.AvgLossHoldDuration)
	logger.S().Info("------------------------------------")
	logger.S().Infof("Sharpe ratio:         %.2f", metrics.SharpeRatio)
	if metrics.AnnualizedReturn == 0 && metrics.ProfitPercentage > 1000 {
		logger.S().Info("Annualized return:       N/A (abnormal total return rate)")
	} else if metrics.AnnualizedReturn == 0 {
		logger.S().Info("Annualized return:       N/A (short backtest period)")
	} else {
		logger.S().Infof("Annualized return:       %.2f%%", metrics.AnnualizedReturn)
	}
	logger.S().Info("--- Ending asset analysis ---")
	logger.S().Infof("Ending cash:         %.2f USDT", metrics.EndingCash)
	logger.S().Infof("Ending asset value:     %.2f USDT (共 %.4f %s)", metrics.EndingAssetValue, metrics.TotalAssetQty, symbol)
	logger.S().Info("===================================")

	// New: print trade distribution analysis
	logger.S().Info("Preparing to print trade distribution analysis...")
	printTradeDistributionAnalysis(backtestExchange.TradeLog)
	logger.S().Info("Trade distribution analysis printed.")
	logger.S().Info("Backtest report generated.")
}

func calculateMetrics(be *exchange.BacktestExchange, startTime, endTime time.Time) (*Metrics, string) {
	m := &Metrics{
		StartTime: startTime,
		EndTime:   endTime,
	}

	m.InitialBalance = be.InitialBalance

	// 1. Calculate realized profit and related trade statistics
	m.TotalTrades = len(be.TradeLog)
	var totalProfitOnWins, totalLossOnLosses float64
	var totalHoldDuration, winHoldDuration, lossHoldDuration time.Duration
	for _, trade := range be.TradeLog {
		totalHoldDuration += trade.HoldDuration
		m.TotalSlippage += trade.Slippage
		m.RealizedProfit += trade.Profit // Accumulate the net profit of each trade

		if trade.Profit > 0 {
			m.WinningTrades++
			totalProfitOnWins += trade.Profit
			winHoldDuration += trade.HoldDuration
		} else {
			m.LosingTrades++
			totalLossOnLosses += trade.Profit
			lossHoldDuration += trade.HoldDuration
		}
	}
	m.TotalFees = be.TotalFees // Get accurate total fees directly from exchange

	// 2. Calculate final asset details (ending cash and ending asset value)
	m.EndingCash = be.Cash
	for _, posQty := range be.Positions {
		m.TotalAssetQty += posQty
	}
	m.EndingAssetValue = m.TotalAssetQty * be.CurrentPrice
	m.FinalEquity = m.EndingCash + m.EndingAssetValue

	// 3. Calculate total PnL and floating PnL (total profit and loss and unrealized profit and loss)
	m.TotalPnL = m.FinalEquity - m.InitialBalance
	m.UnrealizedPnL = m.TotalPnL - m.RealizedProfit // Total PnL = Realized + Unrealized

	// 4. Calculate return rate and other ratios (profit percentage, win rate, average profit and loss ratio, maximum drawdown, maximum wallet exposure, annualized return, and sharpe ratio)
	if m.InitialBalance != 0 {
		m.ProfitPercentage = (m.TotalPnL / m.InitialBalance) * 100
	}

	if m.TotalTrades > 0 {
		m.WinRate = float64(m.WinningTrades) / float64(m.TotalTrades) * 100
		m.AvgHoldDuration = totalHoldDuration / time.Duration(m.TotalTrades)
	}

	if m.WinningTrades > 0 {
		m.AvgWinHoldDuration = winHoldDuration / time.Duration(m.WinningTrades)
	}

	if m.LosingTrades > 0 {
		m.AvgLossHoldDuration = lossHoldDuration / time.Duration(m.LosingTrades)
	}

	if m.WinningTrades > 0 && m.LosingTrades > 0 {
		avgWin := totalProfitOnWins / float64(m.WinningTrades)
		avgLoss := math.Abs(totalLossOnLosses / float64(m.LosingTrades))
		if avgLoss > 0 {
			m.AvgProfitLoss = avgWin / avgLoss
		}
	}

	// Calculate maximum drawdown from equity curve
	m.MaxDrawdown = calculateMaxDrawdown(be.EquityCurve) * 100

	// Calculate annualized return and sharpe ratio
	days := endTime.Sub(startTime).Hours() / 24
	// Add robustness: When the period is too short (less than 30 days) or the total return rate is too high (greater than 1000%), it is considered that the annualized metrics are meaningless.
	isProfitAbnormal := m.ProfitPercentage > 1000
	if days >= 30 && m.InitialBalance > 0 && !isProfitAbnormal {
		m.AnnualizedReturn = (math.Pow(1+m.TotalPnL/m.InitialBalance, 365.0/days) - 1) * 100
	} else {
		m.AnnualizedReturn = 0 // The period is too short or the profit is abnormal, so it is not calculated
	}
	m.SharpeRatio = calculateSharpeRatio(be.GetDailyEquity(), 0.0)
	m.MaxWalletExposure = be.GetMaxWalletExposure()

	return m, be.Symbol
}

func calculateMaxDrawdown(equityCurve []float64) float64 {
	if len(equityCurve) < 2 {
		return 0.0
	}
	peak := equityCurve[0]
	maxDrawdown := 0.0

	for _, equity := range equityCurve {
		if equity > peak {
			peak = equity
		}
		drawdown := (peak - equity) / peak
		if drawdown > maxDrawdown {
			maxDrawdown = drawdown
		}
	}
	return maxDrawdown
}

func calculateSharpeRatio(dailyEquity map[string]float64, riskFreeRate float64) float64 {
	var dailyReturns []float64
	// To calculate the return rate, we need to sort the dates
	days := make([]string, 0, len(dailyEquity))
	for day := range dailyEquity {
		days = append(days, day)
	}
	sort.Strings(days)

	var lastEquity float64 = -1
	for _, day := range days {
		equity := dailyEquity[day]
		if lastEquity != -1 {
			dailyReturn := (equity - lastEquity) / lastEquity
			dailyReturns = append(dailyReturns, dailyReturn)
		}
		lastEquity = equity
	}

	if len(dailyReturns) < 2 {
		return 0.0
	}

	// Calculate the average daily return and standard deviation
	var sumReturns float64
	for _, r := range dailyReturns {
		sumReturns += r
	}
	meanReturn := sumReturns / float64(len(dailyReturns))

	var sumSqDiff float64
	for _, r := range dailyReturns {
		sumSqDiff += math.Pow(r-meanReturn, 2)
	}
	stdDev := math.Sqrt(sumSqDiff / float64(len(dailyReturns)))

	if stdDev == 0 {
		return 0.0
	}

	// Annualized Sharpe ratio (assuming 252 trading days per year)
	sharpeRatio := (meanReturn - riskFreeRate/252) / stdDev * math.Sqrt(252)
	return sharpeRatio
}

// --- New trade distribution analysis functions ---

func printTradeDistributionAnalysis(trades []models.CompletedTrade) {
	if len(trades) == 0 {
		return
	}

	logger.S().Info("--- Trade Distribution Analysis ---")
	analyzeTradeDistributionByDay(trades)
	analyzeTradeDistributionByPrice(trades)
	logger.S().Info("===================================")
}

// analyzeTradeDistributionByDay analyzes daily trade count
func analyzeTradeDistributionByDay(trades []models.CompletedTrade) {
	tradesByDay := make(map[string]int)
	for _, trade := range trades {
		day := trade.ExitTime.Format("2006-01-02")
		tradesByDay[day]++
	}

	// Sort dates for ordered output
	days := make([]string, 0, len(tradesByDay))
	for day := range tradesByDay {
		days = append(days, day)
	}
	sort.Strings(days)

	logger.S().Info("\n[Daily Trade Count Distribution]")
	for _, day := range days {
		logger.S().Infof("%s: %d trades", day, tradesByDay[day])
	}
}

// analyzeTradeDistributionByPrice analyze the number of trades in the price range
func analyzeTradeDistributionByPrice(trades []models.CompletedTrade) {
	tradesByPrice := make(map[int]int)
	minPrice, maxPrice := trades[0].ExitPrice, trades[0].ExitPrice
	for _, trade := range trades {
		if trade.ExitPrice < minPrice {
			minPrice = trade.ExitPrice
		}
		if trade.ExitPrice > maxPrice {
			maxPrice = trade.ExitPrice
		}
	}

	// Dynamically determine the price step, the goal is to divide into approximately 20 intervals
	priceRange := maxPrice - minPrice
	if priceRange == 0 {
		logger.S().Info("\n[Price range trade count distribution]: All trades are completed at the same price.")
		return
	}
	step := math.Pow(10, math.Floor(math.Log10(priceRange/20)))
	step = math.Max(step, 0.0001) // Avoid step being 0

	for _, trade := range trades {
		bucket := int(math.Floor(trade.ExitPrice/step)) * int(step)
		tradesByPrice[bucket]++
	}

	buckets := make([]int, 0, len(tradesByPrice))
	for bucket := range tradesByPrice {
		buckets = append(buckets, bucket)
	}
	sort.Ints(buckets)

	logger.S().Info("\n[Price range trade count distribution]")
	for _, bucket := range buckets {
		logger.S().Infof("~%.4f USDT: %d 次", float64(bucket), tradesByPrice[bucket])
	}
}
