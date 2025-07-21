package exchange

import (
	"github.com/yudaprama/grid-trading-bot/internal/models"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jedib0t/go-pretty/v6/table"
)

// BacktestExchange implements the Exchange interface to simulate exchange behavior for backtesting.
type BacktestExchange struct {
	Symbol                string // Store the current backtesting trading pair
	InitialBalance        float64
	Cash                  float64
	CurrentPrice          float64   // Stores the close price of the current tick
	CurrentTime           time.Time // Stores the timestamp of the current tick
	Positions             map[string]float64
	AvgEntryPrice         map[string]float64
	buyQueue              map[string][]models.BuyTrade // FIFO buy queue, replaces positionEntryTime
	orders                map[int64]*models.Order
	TradeLog              []models.CompletedTrade
	EquityCurve           []float64
	dailyEquity           map[string]float64 // Used to record daily equity
	NextOrderID           int64
	Leverage              int
	mu                    sync.Mutex
	config                *models.Config // Store configuration
	TakerFeeRate          float64        // Taker fee rate
	MakerFeeRate          float64        // Maker fee rate
	SlippageRate          float64        // Slippage rate
	MaintenanceMarginRate float64        // Maintenance margin rate
	TotalFees             float64        // Cumulative total fees
	MinNotionalValue      float64        // Minimum notional value
	Margin                float64        // Position margin
	UnrealizedPNL         float64        // Unrealized PnL
	LiquidationPrice      float64        // Estimated liquidation price
	isLiquidated          bool           // Mark if liquidated (private)
	MaxWalletExposure     float64        // Record maximum wallet risk exposure during backtesting
}

// NewBacktestExchange create a new BacktestExchange instance.
func NewBacktestExchange(cfg *models.Config) *BacktestExchange {
	return &BacktestExchange{
		Symbol:                cfg.Symbol,
		InitialBalance:        cfg.InitialInvestment,
		Cash:                  cfg.InitialInvestment,
		Positions:             make(map[string]float64),
		AvgEntryPrice:         make(map[string]float64),
		buyQueue:              make(map[string][]models.BuyTrade),
		orders:                make(map[int64]*models.Order),
		TradeLog:              make([]models.CompletedTrade, 0),
		EquityCurve:           make([]float64, 0, 10000),
		dailyEquity:           make(map[string]float64),
		NextOrderID:           1,
		Leverage:              cfg.Leverage,
		Margin:                0,
		UnrealizedPNL:         0,
		LiquidationPrice:      0,
		isLiquidated:          false,
		TakerFeeRate:          cfg.TakerFeeRate,
		MakerFeeRate:          cfg.MakerFeeRate,
		SlippageRate:          cfg.SlippageRate,
		MaintenanceMarginRate: cfg.MaintenanceMarginRate,
		TotalFees:             0,
		MinNotionalValue:      cfg.MinNotionalValue,
		MaxWalletExposure:     0.0,
		config:                cfg,
	}
}

// SetPrice is the core of backtesting, simulating price changes and triggering order execution checks.
func (e *BacktestExchange) SetPrice(open, high, low, close float64, timestamp time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.CurrentTime = timestamp

	if e.isLiquidated {
		return
	}

	e.checkLimitOrdersAtPrice(open)
	e.checkLimitOrdersAtPrice(low)
	e.checkLimitOrdersAtPrice(high)
	e.checkLimitOrdersAtPrice(close)

	e.CurrentPrice = close
	e.updateMarginAndPNL()

	if e.Positions[e.Symbol] > 0 && e.LiquidationPrice > 0 && low <= e.LiquidationPrice {
		e.CurrentPrice = e.LiquidationPrice
		e.handleLiquidation()
		e.updateEquity()
		return
	}

	e.updateEquity()
}

func (e *BacktestExchange) checkLimitOrdersAtPrice(price float64) {
	var orderedIDs []int64
	for id := range e.orders {
		orderedIDs = append(orderedIDs, id)
	}
	sort.Slice(orderedIDs, func(i, j int) bool { return orderedIDs[i] < orderedIDs[j] })

	for _, orderID := range orderedIDs {
		order := e.orders[orderID]
		if order.Status == "NEW" && order.Type == "LIMIT" {
			limitPrice, _ := strconv.ParseFloat(order.Price, 64)
			shouldFill := false
			if order.Side == "BUY" && price <= limitPrice {
				shouldFill = true
			} else if order.Side == "SELL" && price >= limitPrice {
				shouldFill = true
			}

			if shouldFill {
				e.handleFilledOrder(order)
			}
		}
	}
}

func (e *BacktestExchange) handleFilledOrder(order *models.Order) {
	order.Status = "FILLED"

	limitPrice, _ := strconv.ParseFloat(order.Price, 64)
	quantity, _ := strconv.ParseFloat(order.OrigQty, 64)

	var executionPrice float64
	if order.Side == "BUY" {
		executionPrice = limitPrice * (1 + e.SlippageRate)
	} else { // SELL
		executionPrice = limitPrice * (1 - e.SlippageRate)
	}
	if order.Type == "MARKET" {
		if order.Side == "BUY" {
			executionPrice = e.CurrentPrice * (1 + e.SlippageRate)
		} else {
			executionPrice = e.CurrentPrice * (1 - e.SlippageRate)
		}
	}

	feeRate := e.MakerFeeRate
	if order.Type == "MARKET" {
		feeRate = e.TakerFeeRate
	}
	fee := executionPrice * quantity * feeRate
	e.TotalFees += fee
	e.Cash -= fee

	currentPosition := e.Positions[order.Symbol]
	avgEntry := e.AvgEntryPrice[order.Symbol]

	if order.Side == "BUY" {
		newBuy := models.BuyTrade{
			Timestamp: e.CurrentTime,
			Quantity:  quantity,
			Price:     executionPrice,
		}
		e.buyQueue[order.Symbol] = append(e.buyQueue[order.Symbol], newBuy)

		newTotalQty := currentPosition + quantity
		newTotalValue := (avgEntry * currentPosition) + (executionPrice * quantity)
		if newTotalQty > 0 {
			e.AvgEntryPrice[order.Symbol] = newTotalValue / newTotalQty
		}
		e.Positions[order.Symbol] = newTotalQty

	} else { // SELL (close long position) with FIFO logic
		if currentPosition > 1e-9 {
			sellQuantity := quantity
			if sellQuantity > currentPosition {
				sellQuantity = currentPosition
			}

			var weightedEntryPriceSum float64
			var weightedHoldDurationSum float64
			var quantityToMatch = sellQuantity
			var consumedCount = 0

			queue := e.buyQueue[order.Symbol]

			for i := range queue {
				if quantityToMatch < 1e-9 {
					break
				}
				oldestBuy := &queue[i]

				var consumedQty float64
				if oldestBuy.Quantity <= quantityToMatch {
					consumedQty = oldestBuy.Quantity
				} else {
					consumedQty = quantityToMatch
				}

				weightedEntryPriceSum += oldestBuy.Price * consumedQty
				durationNs := float64(e.CurrentTime.Sub(oldestBuy.Timestamp))
				weightedHoldDurationSum += durationNs * consumedQty

				oldestBuy.Quantity -= consumedQty
				quantityToMatch -= consumedQty

				if oldestBuy.Quantity < 1e-9 {
					consumedCount++
				}
			}

			if consumedCount > 0 {
				e.buyQueue[order.Symbol] = queue[consumedCount:]
			} else {
				e.buyQueue[order.Symbol] = queue
			}

			if sellQuantity > 1e-9 {
				avgEntryForThisSell := weightedEntryPriceSum / sellQuantity
				avgHoldDurationNs := weightedHoldDurationSum / sellQuantity
				avgHoldDuration := time.Duration(avgHoldDurationNs)
				entryTimeApproximation := e.CurrentTime.Add(-avgHoldDuration)

				realizedPNL := (executionPrice - avgEntryForThisSell) * sellQuantity
				e.Cash += realizedPNL
				e.Positions[order.Symbol] -= sellQuantity

				slippageCost := (executionPrice - limitPrice) * sellQuantity

				e.TradeLog = append(e.TradeLog, models.CompletedTrade{
					Symbol:       e.Symbol,
					Quantity:     sellQuantity,
					EntryTime:    entryTimeApproximation,
					ExitTime:     e.CurrentTime,
					HoldDuration: avgHoldDuration,
					EntryPrice:   avgEntryForThisSell,
					ExitPrice:    executionPrice,
					Profit:       realizedPNL - fee,
					Fee:          fee,
					Slippage:     slippageCost,
				})
			}

			if e.Positions[order.Symbol] < 1e-9 {
				e.buyQueue[order.Symbol] = nil
			}
		}
	}

	e.updateMarginAndPNL()
	if e.Positions[order.Symbol] > 1e-9 {
		e.calculateLiquidationPrice()
	} else {
		e.Positions[order.Symbol] = 0
		e.LiquidationPrice = 0
	}

	equity := e.Cash + e.Margin + e.UnrealizedPNL
	positionValue := e.Positions[order.Symbol] * e.CurrentPrice
	walletExposure := 0.0
	if equity > 0 {
		walletExposure = positionValue / equity
	}
	if walletExposure > e.MaxWalletExposure {
		e.MaxWalletExposure = walletExposure
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetTitle(fmt.Sprintf("[Backtest] Order execution snapshot (%s)", e.CurrentTime.Format("2006-01-02 15:04:05")))
	t.SetStyle(table.StyleLight)

	t.AppendRow(table.Row{"Transaction details", fmt.Sprintf("%s %s @ %.4f, quantity: %.5f", order.Side, order.Type, executionPrice, quantity)}) // Execution details
	t.AppendSeparator()
	t.AppendRow(table.Row{"Position status"})                                                                                                         // Position status
	t.AppendRow(table.Row{"  New Position Volume", fmt.Sprintf("%.5f %s", e.Positions[order.Symbol], strings.Replace(order.Symbol, "USDT", "", -1))}) // New position quantity
	t.AppendRow(table.Row{"  Average Position Cost", fmt.Sprintf("%.4f", e.AvgEntryPrice[order.Symbol])})                                             // Average entry price
	t.AppendRow(table.Row{"  Wallet Risk Exposure", fmt.Sprintf("%.2f%%", walletExposure*100)})                                                       // Wallet risk exposure
	t.AppendSeparator()
	t.AppendRow(table.Row{"Account status"})                                                     // Account status
	t.AppendRow(table.Row{"  Cash Balance", fmt.Sprintf("%.4f", e.Cash)})                        // Cash balance
	t.AppendRow(table.Row{"  Margin", fmt.Sprintf("%.4f", e.Margin)})                            // Margin
	t.AppendRow(table.Row{"  Unrealized Profit and Loss", fmt.Sprintf("%.4f", e.UnrealizedPNL)}) // Unrealized PnL
	t.AppendRow(table.Row{"  Total Account Equity", fmt.Sprintf("%.4f", equity)})

	t.Render()
}

func (e *BacktestExchange) updateMarginAndPNL() {
	positionSize := e.Positions[e.Symbol]
	if positionSize > 0 {
		avgEntryPrice := e.AvgEntryPrice[e.Symbol]
		e.Margin = (avgEntryPrice * positionSize) / float64(e.Leverage)
		e.UnrealizedPNL = (e.CurrentPrice - avgEntryPrice) * positionSize
	} else {
		e.Margin = 0
		e.UnrealizedPNL = 0
		e.AvgEntryPrice[e.Symbol] = 0
	}
}

func (e *BacktestExchange) calculateLiquidationPrice() {
	positionSize := e.Positions[e.Symbol]
	avgEntryPrice := e.AvgEntryPrice[e.Symbol]
	mmr := e.MaintenanceMarginRate
	walletBalance := e.Cash + e.Margin

	if positionSize > 1e-9 {
		numerator := avgEntryPrice*positionSize - walletBalance
		denominator := positionSize * (1 - mmr)

		if denominator != 0 {
			e.LiquidationPrice = numerator / denominator
		} else {
			e.LiquidationPrice = -1
		}
	} else {
		e.LiquidationPrice = 0
	}
}

func (e *BacktestExchange) handleLiquidation() {
	if !e.isLiquidated {
		e.isLiquidated = true

		originalCash := e.Cash
		originalMargin := e.Margin
		originalUnrealizedPNL := e.UnrealizedPNL
		finalEquity := originalCash + originalMargin + originalUnrealizedPNL
		originalPositionSize := e.Positions[e.Symbol]
		originalAvgEntryPrice := e.AvgEntryPrice[e.Symbol]

		e.Cash = 0
		e.Positions[e.Symbol] = 0
		e.UnrealizedPNL = 0
		e.Margin = 0

		fmt.Printf(`
!----- Liquidation Event Occurred -----!
Time: %s
Trigger Price (Execution Price): %.4f
Calculated Liquidation Price: %.4f
--- Pre-liquidation Status Snapshot ---
Available Cash: %.4f
Margin: %.4f
Unrealized PnL: %.4f
Total Account Equity: %.4f
Position Size: %.8f
Average Entry Price: %.4f
--- Post-liquidation ---
Remaining Account Equity: %.4f
!--------------------------!
`,
			e.CurrentTime.Format(time.RFC3339),
			e.CurrentPrice,
			e.LiquidationPrice,
			originalCash,
			originalMargin,
			originalUnrealizedPNL,
			finalEquity,
			originalPositionSize,
			originalAvgEntryPrice,
			e.Cash,
		)
		e.LiquidationPrice = 0
	}
}

func (e *BacktestExchange) updateEquity() {
	equity := e.Cash + e.Margin + e.UnrealizedPNL
	e.EquityCurve = append(e.EquityCurve, equity)

	dayKey := e.CurrentTime.Format("2006-01-02")
	e.dailyEquity[dayKey] = equity
}

// --- Exchange interface implementation ---

func (e *BacktestExchange) GetPrice(symbol string) (float64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.CurrentPrice, nil
}

func (e *BacktestExchange) PlaceOrder(symbol, side, orderType string, quantity, price float64, clientOrderID string) (*models.Order, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	order := &models.Order{
		OrderId:       e.NextOrderID,
		ClientOrderId: clientOrderID,
		Symbol:        e.Symbol,
		Side:          side,
		Type:          orderType,
		OrigQty:       fmt.Sprintf("%.8f", quantity),
		Price:         fmt.Sprintf("%.8f", price),
		Status:        "NEW",
	}
	e.orders[order.OrderId] = order
	e.NextOrderID++

	if orderType == "MARKET" {
		order.Price = fmt.Sprintf("%.8f", e.CurrentPrice)
		e.handleFilledOrder(order)
	}

	return order, nil
}

func (e *BacktestExchange) GetOrderStatus(symbol string, orderID int64) (*models.Order, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if order, ok := e.orders[orderID]; ok {
		orderCopy := *order
		return &orderCopy, nil
	}
	return nil, fmt.Errorf("order ID %d not found in backtesting", orderID)
}

func (e *BacktestExchange) CancelAllOpenOrders(symbol string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, order := range e.orders {
		if order.Symbol == symbol && order.Status == "NEW" {
			order.Status = "CANCELED"
		}
	}
	return nil
}

func (e *BacktestExchange) IsLiquidated() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.isLiquidated
}

func (e *BacktestExchange) GetPositions(symbol string) ([]models.Position, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if posSize, ok := e.Positions[symbol]; ok && posSize > 0 {
		avgPrice := e.AvgEntryPrice[symbol]
		return []models.Position{
			{
				Symbol:           symbol,
				PositionAmt:      fmt.Sprintf("%f", posSize),
				EntryPrice:       fmt.Sprintf("%f", avgPrice),
				UnrealizedProfit: fmt.Sprintf("%f", e.UnrealizedPNL),
				LiquidationPrice: fmt.Sprintf("%f", e.LiquidationPrice),
				Leverage:         strconv.Itoa(e.Leverage),
				MarginType:       strings.ToLower(e.config.MarginType),
			},
		}, nil
	}
	return []models.Position{}, nil
}

func (e *BacktestExchange) SetLeverage(symbol string, leverage int) error {
	e.Leverage = leverage
	return nil
}

func (e *BacktestExchange) SetMarginType(symbol string, marginType string) error {
	// In backtesting, we assume this operation always succeeds
	return nil
}

func (e *BacktestExchange) SetPositionMode(isHedgeMode bool) error {
	// In backtesting, we assume this operation always succeeds
	return nil
}

func (e *BacktestExchange) GetPositionMode() (bool, error) {
	return e.config.HedgeMode, nil
}

func (e *BacktestExchange) GetMarginType(symbol string) (string, error) {
	return e.config.MarginType, nil
}

func (e *BacktestExchange) GetAllOrders() []*models.Order {
	e.mu.Lock()
	defer e.mu.Unlock()
	var allOrders []*models.Order
	for _, order := range e.orders {
		allOrders = append(allOrders, order)
	}
	return allOrders
}

func (e *BacktestExchange) GetCurrentTime() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.CurrentTime
}

func (e *BacktestExchange) GetAccountState(symbol string) (positionValue float64, accountEquity float64, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	equity := e.Cash + e.Margin + e.UnrealizedPNL
	positionValue = e.Positions[symbol] * e.CurrentPrice
	return positionValue, equity, nil
}

func (e *BacktestExchange) GetSymbolInfo(symbol string) (*models.SymbolInfo, error) {
	return &models.SymbolInfo{
		Symbol: symbol,
		Filters: []models.Filter{
			{FilterType: "PRICE_FILTER", TickSize: "0.01"},
			{FilterType: "LOT_SIZE", StepSize: "0.001", MinQty: "0.001"},
			{FilterType: "MIN_NOTIONAL", MinNotional: "5.0"},
		},
	}, nil
}

func (e *BacktestExchange) GetOpenOrders(symbol string) ([]models.Order, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var openOrders []models.Order
	for _, order := range e.orders {
		if order.Symbol == symbol && order.Status == "NEW" {
			openOrders = append(openOrders, *order)
		}
	}
	return openOrders, nil
}

func (e *BacktestExchange) GetServerTime() (int64, error) {
	return time.Now().UnixMilli(), nil
}

func (e *BacktestExchange) GetDailyEquity() map[string]float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Return a copy to avoid external modification
	dailyEquityCopy := make(map[string]float64)
	for k, v := range e.dailyEquity {
		dailyEquityCopy[k] = v
	}
	return dailyEquityCopy
}

func (e *BacktestExchange) GetMaxWalletExposure() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.MaxWalletExposure
}

func (e *BacktestExchange) GetLastTrade(symbol string, orderID int64) (*models.Trade, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.TradeLog) > 0 {
		// In backtesting, we simply return the last trade as a simulation
		lastTrade := e.TradeLog[len(e.TradeLog)-1]
		return &models.Trade{
			Symbol: lastTrade.Symbol,
			Price:  strconv.FormatFloat(lastTrade.ExitPrice, 'f', -1, 64),
			Qty:    strconv.FormatFloat(lastTrade.Quantity, 'f', -1, 64),
		}, nil
	}
	return nil, fmt.Errorf("no trade record available in backtesting")
}

func (e *BacktestExchange) CancelOrder(symbol string, orderID int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if order, ok := e.orders[orderID]; ok {
		order.Status = "CANCELED"
		return nil
	}
	return fmt.Errorf("order ID %d not found in backtesting", orderID)
}

func (e *BacktestExchange) GetBalance() (float64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.Cash, nil
}

func (e *BacktestExchange) GetAccountInfo() (*models.AccountInfo, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	equity := e.Cash + e.Margin + e.UnrealizedPNL
	return &models.AccountInfo{
		TotalWalletBalance: fmt.Sprintf("%f", equity),
		AvailableBalance:   fmt.Sprintf("%f", e.Cash),
		Assets: []struct {
			Asset                  string `json:"asset"`
			WalletBalance          string `json:"walletBalance"`
			UnrealizedProfit       string `json:"unrealizedProfit"`
			MarginBalance          string `json:"marginBalance"`
			MaintMargin            string `json:"maintMargin"`
			InitialMargin          string `json:"initialMargin"`
			PositionInitialMargin  string `json:"positionInitialMargin"`
			OpenOrderInitialMargin string `json:"openOrderInitialMargin"`
			MaxWithdrawAmount      string `json:"maxWithdrawAmount"`
		}{
			{
				Asset:            "USDT",
				WalletBalance:    fmt.Sprintf("%f", equity),
				UnrealizedProfit: fmt.Sprintf("%f", e.UnrealizedPNL),
			},
		},
	}, nil
}

func (e *BacktestExchange) CreateListenKey() (string, error) {
	return "mock-listen-key", nil
}

func (e *BacktestExchange) KeepAliveListenKey(listenKey string) error {
	return nil
}

func (e *BacktestExchange) ConnectWebSocket(listenKey string) (*websocket.Conn, error) {
	// In backtesting mode, do not establish a real WebSocket connection
	return nil, nil
}

// Spot-specific methods (not supported in backtest mode)
func (e *BacktestExchange) GetSpotBalances() ([]models.SpotBalance, error) {
	return nil, fmt.Errorf("GetSpotBalances is not supported in backtest mode")
}

func (e *BacktestExchange) GetSpotBalance(asset string) (float64, error) {
	return 0, fmt.Errorf("GetSpotBalance is not supported in backtest mode")
}

func (e *BacktestExchange) GetSpotTradingFees(symbol string) (*models.TradingFees, error) {
	return nil, fmt.Errorf("GetSpotTradingFees is not supported in backtest mode")
}

// GetTradingMode returns the trading mode
func (e *BacktestExchange) GetTradingMode() string {
	return "futures" // Backtest currently only supports futures
}

// SupportsTradingMode checks if a trading mode is supported
func (e *BacktestExchange) SupportsTradingMode(mode string) bool {
	return mode == "futures" // Backtest currently only supports futures
}

// SetTradingMode sets the trading mode
func (e *BacktestExchange) SetTradingMode(mode string) error {
	if mode != "futures" {
		return fmt.Errorf("backtest exchange currently only supports futures trading mode")
	}
	return nil
}
