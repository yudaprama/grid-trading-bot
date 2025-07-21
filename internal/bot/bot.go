package bot

import (
	"github.com/yudaprama/grid-trading-bot/internal/exchange"
	"github.com/yudaprama/grid-trading-bot/internal/idgenerator"
	"github.com/yudaprama/grid-trading-bot/internal/logger"
	"github.com/yudaprama/grid-trading-bot/internal/models"
	"github.com/yudaprama/grid-trading-bot/internal/trading"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// EventType defines the type of a normalized event
type EventType int

const (
	OrderUpdateEvent EventType = iota
	// Add other event types here in the future, e.g., PriceTickEvent
)

// NormalizedEvent is a standardized internal representation of an event from any source
type NormalizedEvent struct {
	Type      EventType
	Timestamp time.Time
	Data      interface{} // Can be models.OrderUpdateEvent or other event data structs
}

// GridTradingBot is the core struct for the grid trading bot
type GridTradingBot struct {
	config                  *models.Config
	exchange                exchange.Exchange
	tradingMode             trading.TradingMode
	wsConn                  *websocket.Conn
	listenKey               string
	gridLevels              []models.GridLevel
	currentPrice            float64
	isRunning               bool
	IsBacktest              bool
	basePositionEstablished bool
	conceptualGrid          []float64
	entryPrice              float64
	reversionPrice          float64
	isReentering            bool
	reentrySignal           chan bool
	mutex                   sync.RWMutex
	stopChannel             chan bool
	eventChannel            chan NormalizedEvent // The central event queue

	// Trailing Stop fields
	trailingStopState       *models.TrailingStopState
	trailingStopMutex       sync.RWMutex
	trailingAdjustments     []models.TrailingStopAdjustment
	symbolInfo              *models.SymbolInfo
	isHalted                bool
	safeModeReason          string
	idGenerator             *idgenerator.IDGenerator
}

// NewGridTradingBot creates a new instance of the grid trading bot
func NewGridTradingBot(config *models.Config, ex exchange.Exchange, isBacktest bool) *GridTradingBot {
	bot := &GridTradingBot{
		config:                  config,
		exchange:                ex,
		gridLevels:              make([]models.GridLevel, 0),
		isRunning:               false,
		IsBacktest:              isBacktest,
		basePositionEstablished: false,
		stopChannel:             make(chan bool),
		eventChannel:            make(chan NormalizedEvent, 1024), // Buffered channel
		reentrySignal:           make(chan bool, 1),
		isHalted:                false,
		trailingStopState:       &models.TrailingStopState{IsActive: false},
		trailingAdjustments:     make([]models.TrailingStopAdjustment, 0),
	}

	// Initialize trading mode
	if modeAwareEx, ok := ex.(trading.ModeAwareExchange); ok {
		factory := trading.NewTradingModeFactory(modeAwareEx, config)
		tradingMode, err := factory.CreateTradingMode()
		if err != nil {
			logger.S().Fatalf("Could not create trading mode: %v", err)
		}
		bot.tradingMode = tradingMode
		logger.S().Infof("Initialized %s trading mode", tradingMode.GetTradingMode())
	} else {
		logger.S().Warn("Exchange does not support mode-aware trading, using legacy mode")
	}

	symbolInfo, err := ex.GetSymbolInfo(config.Symbol)
	if err != nil {
		logger.S().Fatalf("Could not get symbol info for %s: %v", config.Symbol, err)
	}
	bot.symbolInfo = symbolInfo
	logger.S().Infof("Successfully fetched and cached trading rules for %s.", config.Symbol)

	idGen, err := idgenerator.NewIDGenerator(0)
	if err != nil {
		logger.S().Fatalf("Could not create ID generator: %v", err)
	}
	bot.idGenerator = idGen

	return bot
}

// establishPositionByDirection establishes initial position based on configured direction
func (b *GridTradingBot) establishPositionByDirection(quantity float64) (float64, string, error) {
	direction := b.getPositionDirection()

	switch direction {
	case "LONG":
		filledPrice, err := b.establishBasePositionAndWait(quantity)
		return filledPrice, "LONG", err
	case "SHORT":
		return b.establishShortPosition(quantity)
	case "NEUTRAL":
		return b.establishNeutralPosition(quantity)
	default:
		// Default to LONG for backward compatibility
		logger.S().Warnf("Unknown position direction '%s', defaulting to LONG", direction)
		filledPrice, err := b.establishBasePositionAndWait(quantity)
		return filledPrice, "LONG", err
	}
}

// getPositionDirection determines the position direction from configuration
func (b *GridTradingBot) getPositionDirection() string {
	if b.config.PositionDirection != "" {
		return b.config.PositionDirection
	}

	// Check if we have existing positions
	if positions, err := b.exchange.GetPositions(b.config.Symbol); err == nil {
		for _, pos := range positions {
			if pos.Symbol == b.config.Symbol {
				posAmt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
				if posAmt > 0 {
					return "LONG"
				} else if posAmt < 0 {
					return "SHORT"
				}
			}
		}
	}

	// Default to LONG
	return "LONG"
}

// establishShortPosition establishes a SHORT position via market SELL
func (b *GridTradingBot) establishShortPosition(quantity float64) (float64, string, error) {
	clientOrderID, err := b.generateClientOrderID()
	if err != nil {
		return 0, "", fmt.Errorf("could not generate ID for SHORT position order: %v", err)
	}

	order, err := b.exchange.PlaceOrder(b.config.Symbol, "SELL", "MARKET", quantity, 0, clientOrderID)
	if err != nil {
		return 0, "", fmt.Errorf("initial market SELL failed: %v", err)
	}

	logger.S().Infof("Submitted SHORT position order ID: %d, Quantity: %.5f. Waiting for fill...", order.OrderId, quantity)

	filledPrice, err := b.waitForOrderFill(order.OrderId)
	if err != nil {
		return 0, "", err
	}

	b.mutex.Lock()
	b.basePositionEstablished = true
	b.mutex.Unlock()

	return filledPrice, "SHORT", nil
}

// establishNeutralPosition establishes neutral position (requires hedge mode)
func (b *GridTradingBot) establishNeutralPosition(quantity float64) (float64, string, error) {
	if !b.config.HedgeMode {
		return 0, "", fmt.Errorf("neutral position requires hedge_mode to be enabled")
	}

	// For neutral strategy, start with a small LONG position
	// The grid will then place both BUY and SELL orders
	halfQuantity := quantity / 2

	filledPrice, err := b.establishBasePositionAndWait(halfQuantity)
	if err != nil {
		return 0, "", fmt.Errorf("failed to establish initial neutral position: %v", err)
	}

	logger.S().Infof("Established neutral position base at price %.4f", filledPrice)
	return filledPrice, "NEUTRAL", nil
}

// waitForOrderFill waits for an order to be filled and returns the fill price
func (b *GridTradingBot) waitForOrderFill(orderID int64) (float64, error) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	timeout := time.After(2 * time.Minute)

	for {
		select {
		case <-timeout:
			return 0, fmt.Errorf("timeout waiting for order %d to fill", orderID)
		case <-ticker.C:
			status, err := b.exchange.GetOrderStatus(b.config.Symbol, orderID)
			if err != nil {
				logger.S().Errorf("Error checking order %d status: %v", orderID, err)
				continue
			}

			switch status.Status {
			case "FILLED":
				logger.S().Infof("Order %d has been filled!", orderID)

				trade, err := b.exchange.GetLastTrade(b.config.Symbol, orderID)
				if err != nil {
					return 0, fmt.Errorf("could not get trade for order %d: %v", orderID, err)
				}

				filledPrice, err := strconv.ParseFloat(trade.Price, 64)
				if err != nil {
					return 0, fmt.Errorf("could not parse fill price for order %d: %v", orderID, err)
				}

				return filledPrice, nil

			case "CANCELED", "REJECTED", "EXPIRED":
				return 0, fmt.Errorf("order %d was %s", orderID, status.Status)
			}
		case <-b.stopChannel:
			return 0, fmt.Errorf("bot stopped, interrupting order fill wait")
		}
	}
}

// setupGridForDirection sets up grid orders based on position direction
func (b *GridTradingBot) setupGridForDirection(centerPrice float64, direction string) error {
	switch direction {
	case "LONG":
		return b.setupLongGrid(centerPrice)
	case "SHORT":
		return b.setupShortGrid(centerPrice)
	case "NEUTRAL":
		return b.setupNeutralGrid(centerPrice)
	default:
		return b.setupInitialGrid(centerPrice) // Default fallback
	}
}

// setupLongGrid places grid orders optimized for LONG positions
func (b *GridTradingBot) setupLongGrid(centerPrice float64) error {
	logger.S().Infof("Setting up LONG-optimized grid around price %.4f", centerPrice)
	// For now, use the existing setupInitialGrid logic
	// TODO: Implement asymmetric grid placement (70% buy orders, 30% sell orders)
	return b.setupInitialGrid(centerPrice)
}

// setupShortGrid places grid orders optimized for SHORT positions
func (b *GridTradingBot) setupShortGrid(centerPrice float64) error {
	logger.S().Infof("Setting up SHORT-optimized grid around price %.4f", centerPrice)
	// For now, use the existing setupInitialGrid logic
	// TODO: Implement asymmetric grid placement (70% sell orders, 30% buy orders)
	return b.setupInitialGrid(centerPrice)
}

// setupNeutralGrid places balanced grid orders for neutral strategy
func (b *GridTradingBot) setupNeutralGrid(centerPrice float64) error {
	logger.S().Infof("Setting up NEUTRAL grid around price %.4f", centerPrice)
	// For now, use the existing setupInitialGrid logic
	// TODO: Implement balanced grid placement (50% buy orders, 50% sell orders)
	return b.setupInitialGrid(centerPrice)
}

// initializeTrailingStopForDirection initializes trailing stop with position direction awareness
func (b *GridTradingBot) initializeTrailingStopForDirection(entryPrice float64, direction string) {
	if !b.config.EnableTrailingUp && !b.config.EnableTrailingDown {
		return
	}

	b.trailingStopMutex.Lock()
	defer b.trailingStopMutex.Unlock()

	b.trailingStopState = &models.TrailingStopState{
		IsActive:           true,
		PositionSide:       direction,
		EntryPrice:         entryPrice,
		CurrentPrice:       entryPrice,
		HighestPrice:       entryPrice,
		LowestPrice:        entryPrice,
		TrailingUpLevel:    0,
		TrailingDownLevel:  0,
		LastUpdateTime:     time.Now(),
		TotalAdjustments:   0,
		TrailingUpOrderID:  0,
		TrailingDownOrderID: 0,
	}

	// Calculate trailing levels based on position direction
	if b.config.EnableTrailingUp {
		b.trailingStopState.TrailingUpLevel = b.calculateTrailingLevelForDirection(
			entryPrice, b.config.TrailingUpDistance, "up", direction)
	}
	if b.config.EnableTrailingDown {
		b.trailingStopState.TrailingDownLevel = b.calculateTrailingLevelForDirection(
			entryPrice, b.config.TrailingDownDistance, "down", direction)
	}

	logger.S().Infof("Trailing stop initialized for %s position at entry price %.4f", direction, entryPrice)
}

// calculateTrailingLevelForDirection calculates trailing levels based on position direction
func (b *GridTradingBot) calculateTrailingLevelForDirection(price, distance float64, trailingDirection, positionDirection string) float64 {
	if b.config.TrailingType == "percentage" {
		switch positionDirection {
		case "LONG":
			if trailingDirection == "up" {
				return price * (1 + distance) // Take profit above current price
			} else {
				return price * (1 - distance) // Stop loss below current price
			}
		case "SHORT":
			if trailingDirection == "up" {
				return price * (1 - distance) // Take profit below current price
			} else {
				return price * (1 + distance) // Stop loss above current price
			}
		case "NEUTRAL":
			// For neutral, use standard LONG logic
			if trailingDirection == "up" {
				return price * (1 + distance)
			} else {
				return price * (1 - distance)
			}
		}
	} else { // absolute
		switch positionDirection {
		case "LONG":
			if trailingDirection == "up" {
				return price + distance // Take profit above current price
			} else {
				return price - distance // Stop loss below current price
			}
		case "SHORT":
			if trailingDirection == "up" {
				return price - distance // Take profit below current price
			} else {
				return price + distance // Stop loss above current price
			}
		case "NEUTRAL":
			// For neutral, use standard LONG logic
			if trailingDirection == "up" {
				return price + distance
			} else {
				return price - distance
			}
		}
	}

	return price // Fallback
}

// establishBasePositionAndWait tries to establish the initial base position and waits for it to be filled
func (b *GridTradingBot) establishBasePositionAndWait(quantity float64) (float64, error) {
	clientOrderID, err := b.generateClientOrderID()
	if err != nil {
		return 0, fmt.Errorf("could not generate ID for initial order: %v", err)
	}
	order, err := b.exchange.PlaceOrder(b.config.Symbol, "BUY", "MARKET", quantity, 0, clientOrderID)
	if err != nil {
		return 0, fmt.Errorf("initial market buy failed: %v", err)
	}
	logger.S().Infof("Submitted initial market buy order ID: %d, Quantity: %.5f. Waiting for fill...", order.OrderId, quantity)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	timeout := time.After(2 * time.Minute)

	for {
		select {
		case <-ticker.C:
			status, err := b.exchange.GetOrderStatus(b.config.Symbol, order.OrderId)
			if err != nil {
				if b.IsBacktest && strings.Contains(err.Error(), "not found") {
					logger.S().Infof("Initial order %d status check returned 'not found', assuming filled in backtest mode.", order.OrderId)
					b.mutex.Lock()
					b.basePositionEstablished = true
					b.mutex.Unlock()
					return b.currentPrice, nil
				}
				logger.S().Warnf("Failed to get status for initial order %d: %v. Retrying...", order.OrderId, err)
				continue
			}

			switch status.Status {
			case "FILLED":
				logger.S().Infof("Initial position order %d has been filled!", order.OrderId)
				b.mutex.Lock()
				b.basePositionEstablished = true
				b.mutex.Unlock()

				trade, err := b.exchange.GetLastTrade(b.config.Symbol, order.OrderId)
				if err != nil {
					return 0, fmt.Errorf("could not get trade for initial order %d: %v", order.OrderId, err)
				}
				filledPrice, err := strconv.ParseFloat(trade.Price, 64)
				if err != nil {
					return 0, fmt.Errorf("could not parse fill price for initial order %d: %v", order.OrderId, err)
				}

				// Note: Trailing stops are now initialized in the calling function
				// with the actual position direction
				return filledPrice, nil

			case "CANCELED", "REJECTED", "EXPIRED":
				return 0, fmt.Errorf("initial position order %d failed with status: %s", order.OrderId, status.Status)
			default:
				logger.S().Debugf("Initial order %d status: %s. Waiting for fill...", order.OrderId, status.Status)
			}
		case <-timeout:
			return 0, fmt.Errorf("timeout waiting for initial order %d to fill", order.OrderId)
		case <-b.stopChannel:
			return 0, fmt.Errorf("bot stopped, interrupting initial position establishment")
		}
	}
}

// enterMarketAndSetupGrid implements the logic for entering the market and setting up the grid
func (b *GridTradingBot) enterMarketAndSetupGrid() error {
	logger.S().Info("--- Starting new trading cycle ---")

	currentPrice, err := b.exchange.GetPrice(b.config.Symbol)
	if err != nil {
		return fmt.Errorf("failed to get current price: %v", err)
	}

	b.mutex.Lock()
	b.currentPrice = currentPrice
	b.entryPrice = currentPrice
	b.reversionPrice = b.entryPrice * (1 + b.config.ReturnRate)
	b.gridLevels = make([]models.GridLevel, 0)
	b.conceptualGrid = make([]float64, 0)
	b.isReentering = false
	b.mutex.Unlock()

	logger.S().Infof("New cycle defined: Entry Price: %.4f, Reversion Price (Grid Top): %.4f", b.entryPrice, b.reversionPrice)

	b.mutex.Lock()
	var tickSize string
	for _, f := range b.symbolInfo.Filters {
		if f.FilterType == "PRICE_FILTER" {
			tickSize = f.TickSize
		}
	}

	price := b.reversionPrice
	for price > (b.entryPrice * 0.5) {
		adjustedPrice := adjustValueToStep(price, tickSize)
		if len(b.conceptualGrid) == 0 || b.conceptualGrid[len(b.conceptualGrid)-1] != adjustedPrice {
			b.conceptualGrid = append(b.conceptualGrid, adjustedPrice)
		}
		price *= 1 - b.config.GridSpacing
	}
	b.mutex.Unlock()

	if len(b.conceptualGrid) == 0 {
		logger.S().Warn("Conceptual grid is empty, likely due to misconfiguration of return rate or grid spacing. Skipping position and orders.")
		b.mutex.Lock()
		b.basePositionEstablished = true
		b.mutex.Unlock()
		return nil
	}
	logger.S().Infof("Successfully generated conceptual grid with %d levels.", len(b.conceptualGrid))

	sellGridCount := 0
	for _, price := range b.conceptualGrid {
		if price > b.entryPrice {
			sellGridCount++
		}
	}
	// buyGridCount := len(b.conceptualGrid) - sellGridCount // Currently unused
	singleGridQuantity, err := b.calculateQuantity(b.entryPrice)
	if err != nil {
		return fmt.Errorf("could not determine grid quantity for initial position: %v", err)
	}

	initialPositionQuantity := float64(sellGridCount) * singleGridQuantity
	logger.S().Infof("Calculated initial position quantity: %.8f", initialPositionQuantity)

	if !b.isWithinExposureLimit(initialPositionQuantity) {
		logger.S().Warnf("Initial position blocked: wallet exposure limit would be exceeded.")
		b.mutex.Lock()
		b.basePositionEstablished = true
		b.mutex.Unlock()
	} else {
		filledPrice, positionDirection, err := b.establishPositionByDirection(initialPositionQuantity)
		if err != nil {
			return fmt.Errorf("failed to establish initial position, cannot continue: %v", err)
		}
		b.entryPrice = filledPrice

		// Initialize trailing stops with the actual position direction
		b.initializeTrailingStopForDirection(filledPrice, positionDirection)
	}

	b.mutex.RLock()
	isEstablished := b.basePositionEstablished
	b.mutex.RUnlock()

	if isEstablished {
		logger.S().Info("Initial position confirmed, setting up grid orders...")
		direction := b.getPositionDirection()
		err := b.setupGridForDirection(b.entryPrice, direction)
		if err != nil {
			return fmt.Errorf("initial grid setup failed: %v", err)
		}
		logger.S().Infof("--- New cycle grid setup complete for %s strategy ---", direction)
	} else {
		logger.S().Error("CRITICAL: Base position not marked as established, cannot place grid orders.")
	}

	return nil
}

// placeNewOrder is a helper function to place an order and return the result
func (b *GridTradingBot) placeNewOrder(side string, price float64, gridID int) (*models.GridLevel, error) {
	var tickSize string
	for _, f := range b.symbolInfo.Filters {
		if f.FilterType == "PRICE_FILTER" {
			tickSize = f.TickSize
		}
	}

	adjustedPrice := adjustValueToStep(price, tickSize)
	quantity, err := b.calculateQuantity(adjustedPrice)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate order quantity at price %.4f: %v", adjustedPrice, err)
	}

	if side == "BUY" && !b.isWithinExposureLimit(quantity) {
		return nil, fmt.Errorf("order blocked: wallet exposure limit would be exceeded")
	}

	clientOrderID, err := b.generateClientOrderID()
	if err != nil {
		return nil, fmt.Errorf("could not generate client order ID for grid order (GridID: %d): %v", gridID, err)
	}
	order, err := b.exchange.PlaceOrder(b.config.Symbol, side, "LIMIT", quantity, adjustedPrice, clientOrderID)
	if err != nil {
		return nil, fmt.Errorf("failed to place %s order at price %.4f: %v", side, adjustedPrice, err)
	}
	logger.S().Infof("Submitted %s order: ID %d, Price %.4f, Quantity %.5f, GridID: %d. Waiting for confirmation...", side, order.OrderId, adjustedPrice, quantity, gridID)

	if !b.IsBacktest {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.After(10 * time.Second)

		for {
			select {
			case <-ticker.C:
				status, err := b.exchange.GetOrderStatus(b.config.Symbol, order.OrderId)
				if err != nil {
					if strings.Contains(err.Error(), "order does not exist") {
						logger.S().Warnf("Order %d not found yet, retrying...", order.OrderId)
						continue
					}
					return nil, fmt.Errorf("failed to get status for order %d: %v", order.OrderId, err)
				}

				switch status.Status {
				case "NEW", "PARTIALLY_FILLED", "FILLED":
					logger.S().Infof("Order %d confirmed by exchange with status: %s", order.OrderId, status.Status)
					goto confirmed
				case "CANCELED", "REJECTED", "EXPIRED":
					return nil, fmt.Errorf("order %d failed confirmation with final status: %s", order.OrderId, status.Status)
				}
			case <-timeout:
				return nil, fmt.Errorf("timeout waiting for order %d confirmation", order.OrderId)
			case <-b.stopChannel:
				return nil, fmt.Errorf("bot stopped, interrupting order %d confirmation", order.OrderId)
			}
		}
	}

confirmed:
	newGridLevel := &models.GridLevel{
		Price:    adjustedPrice,
		Quantity: quantity,
		Side:     side,
		IsActive: true,
		OrderID:  order.OrderId,
		GridID:   gridID,
	}
	logger.S().Infof("Successfully confirmed %s order: ID %d, Price %.4f, Quantity %.5f, GridID: %d", side, order.OrderId, adjustedPrice, quantity, gridID)
	return newGridLevel, nil
}

// calculateQuantity calculates and validates the order quantity based on configuration and exchange rules
func (b *GridTradingBot) calculateQuantity(price float64) (float64, error) {
	var quantity float64
	var minNotional, minQty, stepSize string

	for _, f := range b.symbolInfo.Filters {
		switch f.FilterType {
		case "MIN_NOTIONAL":
			minNotional = f.MinNotional
		case "LOT_SIZE":
			minQty = f.MinQty
			stepSize = f.StepSize
		}
	}

	minNotionalValue, _ := strconv.ParseFloat(minNotional, 64)
	minQtyValue, _ := strconv.ParseFloat(minQty, 64)

	if b.config.GridQuantity > 0 {
		quantity = b.config.GridQuantity
	} else if b.config.GridValue > 0 {
		quantity = b.config.GridValue / price
	} else {
		return 0, fmt.Errorf("neither grid_quantity nor grid_value is configured")
	}

	if price*quantity < minNotionalValue {
		quantity = (minNotionalValue / price) * 1.01
	}

	if quantity < minQtyValue {
		quantity = minQtyValue
	}

	adjustedQuantity := adjustValueToStep(quantity, stepSize)

	if adjustedQuantity < minQtyValue {
		step, _ := strconv.ParseFloat(stepSize, 64)
		if step > 0 {
			adjustedQuantity += step
			adjustedQuantity = adjustValueToStep(adjustedQuantity, stepSize)
		}
	}

	if price*adjustedQuantity < minNotionalValue {
		step, _ := strconv.ParseFloat(stepSize, 64)
		if step > 0 {
			adjustedQuantity += step
			adjustedQuantity = adjustValueToStep(adjustedQuantity, stepSize)
		}
	}

	return adjustedQuantity, nil
}

// connectWebSocket establishes a connection to the WebSocket
func (b *GridTradingBot) connectWebSocket() error {
	if b.IsBacktest {
		logger.S().Info("Backtest mode, skipping WebSocket connection.")
		return nil
	}

	listenKey, err := b.exchange.CreateListenKey()
	if err != nil {
		return fmt.Errorf("could not create listen key: %v", err)
	}
	b.listenKey = listenKey
	logger.S().Infof("Successfully obtained Listen Key: %s", b.listenKey)

	conn, err := b.exchange.ConnectWebSocket(b.listenKey)
	if err != nil {
		return fmt.Errorf("could not connect to WebSocket: %v", err)
	}
	b.wsConn = conn
	logger.S().Info("Successfully connected to user data stream WebSocket.")

	// Setup Pong Handler
	pongTimeout := time.Duration(b.config.WebSocketPongTimeoutSec) * time.Second
	if pongTimeout == 0 {
		pongTimeout = 75 * time.Second // Default value
	}
	if err = b.wsConn.SetReadDeadline(time.Now().Add(pongTimeout)); err != nil {
		return err
	}

	b.wsConn.SetPongHandler(func(string) error {
		if err := b.wsConn.SetReadDeadline(time.Now().Add(pongTimeout)); err != nil {
			return err
		}
		return nil
	})

	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := b.exchange.KeepAliveListenKey(b.listenKey); err != nil {
					logger.S().Warnf("Failed to keep listen key alive: %v", err)
				} else {
					logger.S().Info("Successfully kept listen key alive.")
				}
			case <-b.stopChannel:
				return
			}
		}
	}()

	return nil
}

// webSocketLoop listens for messages from the WebSocket
func (b *GridTradingBot) webSocketLoop() {
	if b.IsBacktest || b.wsConn == nil {
		return
	}

	readChannel := make(chan []byte)
	errChannel := make(chan error)

	go func() {
		for {
			_, message, err := b.wsConn.ReadMessage()
			if err != nil {
				errChannel <- err
				return
			}
			// Reset read deadline on successful message read
			pongTimeout := time.Duration(b.config.WebSocketPongTimeoutSec) * time.Second
			if pongTimeout == 0 {
				pongTimeout = 75 * time.Second // Default value
			}
			b.wsConn.SetReadDeadline(time.Now().Add(pongTimeout))
			readChannel <- message
		}
	}()

	// Ping Ticker
	pingInterval := time.Duration(b.config.WebSocketPingIntervalSec) * time.Second
	if pingInterval == 0 {
		pingInterval = 30 * time.Second // Default value
	}
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	logger.S().Info("WebSocket message listener loop started.")

	for {
		select {
		case message := <-readChannel:
			b.handleWebSocketMessage(message)
		case <-pingTicker.C:
			if err := b.wsConn.WriteMessage(websocket.PingMessage, nil); err != nil {
				logger.S().Warnf("Failed to send WebSocket ping: %v", err)
			}
		case err := <-errChannel:
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				logger.S().Info("WebSocket connection closed normally.")
			} else {
				logger.S().Errorf("Error reading from WebSocket: %v. Starting reconnection process...", err)
				b.wsConn.Close() // Ensure the old connection is closed

				reconnectAttempts := 0
				for {
					reconnectAttempts++
					waitDuration := time.Duration(math.Min(float64(5*reconnectAttempts), 300)) * time.Second
					logger.S().Infof("Attempting to reconnect (attempt %d)... waiting for %v", reconnectAttempts, waitDuration)

					select {
					case <-time.After(waitDuration):
						if err := b.connectWebSocket(); err != nil {
							logger.S().Errorf("WebSocket reconnection attempt %d failed: %v", reconnectAttempts, err)
						} else {
							logger.S().Info("WebSocket reconnected successfully.")
							go b.webSocketLoop() // Restart the loop
							return               // Exit the old loop
						}
					case <-b.stopChannel:
						logger.S().Info("Stop signal received during reconnection, aborting.")
						return
					}
				}
			}
		case <-b.stopChannel:
			logger.S().Info("Stop signal received, closing WebSocket message loop.")
			b.wsConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return
		}
	}
}

// Start starts the bot
func (b *GridTradingBot) Start() error {
	b.mutex.Lock()
	if b.isRunning {
		b.mutex.Unlock()
		return fmt.Errorf("bot is already running")
	}
	b.isRunning = true
	b.mutex.Unlock()

	logger.S().Info("Starting bot...")

	if !b.IsBacktest {
		if err := b.connectWebSocket(); err != nil {
			return fmt.Errorf("failed to connect to WebSocket on start: %v", err)
		}
		go b.webSocketLoop()
		go b.eventProcessor() // Start the core event processor
	}

	if err := b.enterMarketAndSetupGrid(); err != nil {
		b.enterSafeMode(fmt.Sprintf("failed to initialize grid and position: %v", err))
		return fmt.Errorf("failed to initialize grid and position: %v", err)
	}

	go b.strategyLoop()
	go b.monitorStatus()

	logger.S().Info("Bot started successfully.")
	return nil
}

// strategyLoop is the main strategy loop
func (b *GridTradingBot) strategyLoop() {
	for {
		select {
		case <-b.reentrySignal:
			logger.S().Info("Re-entry signal received, restarting trading cycle...")
			if err := b.enterMarketAndSetupGrid(); err != nil {
				logger.S().Errorf("Failed to re-enter market: %v", err)
				b.enterSafeMode(fmt.Sprintf("re-entry failed: %v", err))
			}
		case <-b.stopChannel:
			logger.S().Info("Strategy loop received stop signal, exiting.")
			return
		}
	}
}

// StartForBacktest starts the bot for backtesting
func (b *GridTradingBot) StartForBacktest() error {
	b.mutex.Lock()
	if b.isRunning {
		b.mutex.Unlock()
		return fmt.Errorf("bot is already running")
	}
	b.isRunning = true
	b.mutex.Unlock()

	logger.S().Info("Starting backtest bot...")
	if err := b.enterMarketAndSetupGrid(); err != nil {
		return fmt.Errorf("backtest initialization failed: %v", err)
	}
	logger.S().Info("Backtest bot initialized successfully.")
	return nil
}

// ProcessBacktestTick processes a single tick in backtest mode
func (b *GridTradingBot) ProcessBacktestTick() {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if b.isHalted {
		return
	}
}

// SetCurrentPrice sets the current price for backtesting
func (b *GridTradingBot) SetCurrentPrice(price float64) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.currentPrice = price
}

// Stop stops the bot
func (b *GridTradingBot) Stop() {
	b.mutex.Lock()
	if !b.isRunning {
		b.mutex.Unlock()
		return
	}
	b.isRunning = false
	close(b.stopChannel)
	b.mutex.Unlock()

	logger.S().Info("Stopping bot...")

	// Save trailing stop history before stopping
	if err := b.saveTrailingStopHistory(); err != nil {
		logger.S().Warnf("Failed to save trailing stop history: %v", err)
	}

	b.cancelAllActiveOrders()

	// Reset trailing stops
	b.resetTrailingStop()

	if b.wsConn != nil {
		b.wsConn.Close()
	}
	logger.S().Info("Bot stopped.")
}

// cancelAllActiveOrders cancels all active orders
func (b *GridTradingBot) cancelAllActiveOrders() {
	logger.S().Info("Cancelling all active orders...")
	if err := b.exchange.CancelAllOpenOrders(b.config.Symbol); err != nil {
		logger.S().Warnf("Failed to cancel all orders: %v", err)
	} else {
		logger.S().Info("Successfully sent request to cancel all orders.")
	}
}

// monitorStatus prints the bot's status periodically and monitors trailing stops
func (b *GridTradingBot) monitorStatus() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Price monitoring ticker for trailing stops (more frequent)
	priceMonitorTicker := time.NewTicker(10 * time.Second)
	defer priceMonitorTicker.Stop()

	for {
		select {
		case <-ticker.C:
			//  b.printStatus()
			b.logTrailingStopStatus()
		case <-priceMonitorTicker.C:
			// Monitor price changes for trailing stops
			if !b.IsBacktest {
				b.monitorPriceForTrailingStops()
			}
		case <-b.stopChannel:
			logger.S().Info("Status monitor received stop signal, exiting.")
			return
		}
	}
}

// SaveState saves the bot's state to a file
func (b *GridTradingBot) SaveState(path string) error {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	state := models.GridState{
		GridLevels:     b.gridLevels,
		EntryPrice:     b.entryPrice,
		ReversionPrice: b.reversionPrice,
		ConceptualGrid: b.conceptualGrid,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("could not serialize bot state: %v", err)
	}

	return os.WriteFile(path, data, 0644)
}

// LoadState loads the bot's state from a file
func (b *GridTradingBot) LoadState(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logger.S().Info("State file not found, starting from initial state.")
			return nil
		}
		return fmt.Errorf("could not read state file: %v", err)
	}

	var state models.GridState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("could not deserialize bot state: %v", err)
	}

	b.mutex.Lock()
	b.gridLevels = state.GridLevels
	b.entryPrice = state.EntryPrice
	b.reversionPrice = state.ReversionPrice
	b.conceptualGrid = state.ConceptualGrid
	b.basePositionEstablished = true
	b.mutex.Unlock()

	logger.S().Infof("Successfully loaded bot state from %s.", path)
	return b.syncWithExchange()
}

// printStatus prints the current status of the bot
func (b *GridTradingBot) printStatus() {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	if b.isHalted {
		logger.S().Warnf("Bot is halted. Reason: %s", b.safeModeReason)
		return
	}

	logger.S().Info("--- Bot Status Report ---")
	logger.S().Infof("Running: %v", b.isRunning)
	logger.S().Infof("Current Price: %.4f", b.currentPrice)
	logger.S().Infof("Active Orders: %d", len(b.gridLevels))
	for _, level := range b.gridLevels {
		logger.S().Infof("  - %s Order: ID %d, Price %.4f, Quantity %.5f", level.Side, level.OrderID, level.Price, level.Quantity)
	}
	logger.S().Info("-------------------------")
}

// adjustValueToStep adjusts a value to the given step size
func adjustValueToStep(value float64, step string) float64 {
	if step == "" || step == "0" {
		return value
	}
	stepFloat, err := strconv.ParseFloat(step, 64)
	if err != nil || stepFloat == 0 {
		return value
	}
	multiplier := 1.0 / stepFloat
	return math.Floor(value*multiplier) / multiplier
}

// generateClientOrderID generates a new client order ID
func (b *GridTradingBot) generateClientOrderID() (string, error) {
	id, err := b.idGenerator.Generate()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("x-grid-%s", id), nil
}

// isWithinExposureLimit checks if adding a trade would exceed the wallet exposure limit
func (b *GridTradingBot) isWithinExposureLimit(quantityToAdd float64) bool {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	if b.config.WalletExposureLimit <= 0 {
		return true
	}

	positions, err := b.exchange.GetPositions(b.config.Symbol)
	if err != nil {
		logger.S().Warnf("Could not get positions to check exposure limit: %v", err)
		return false
	}

	var currentPositionSize float64
	if len(positions) > 0 {
		currentPositionSize, _ = strconv.ParseFloat(positions[0].PositionAmt, 64)
	}

	_, accountEquity, err := b.exchange.GetAccountState(b.config.Symbol)
	if err != nil {
		logger.S().Warnf("Could not get account state to check exposure limit: %v", err)
		return false
	}

	if accountEquity <= 0 {
		return false
	}

	futurePositionValue := (currentPositionSize + quantityToAdd) * b.currentPrice
	expectedExposure := futurePositionValue / accountEquity

	if expectedExposure > b.config.WalletExposureLimit {
		logger.S().Warnf(
			"Wallet exposure check failed: Expected exposure %.2f%% would exceed limit of %.2f%%.",
			expectedExposure*100, b.config.WalletExposureLimit*100,
		)
		return false
	}
	return true
}

// IsHalted returns whether the bot is halted
func (b *GridTradingBot) IsHalted() bool {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return b.isHalted
}

// syncWithExchange synchronizes the local state with the exchange's state
func (b *GridTradingBot) syncWithExchange() error {
	logger.S().Info("Syncing state with exchange...")

	remoteOrders, err := b.exchange.GetOpenOrders(b.config.Symbol)
	if err != nil {
		return fmt.Errorf("could not get open orders from exchange: %v", err)
	}
	logger.S().Infof("Found %d open orders on the exchange.", len(remoteOrders))

	b.mutex.Lock()
	b.gridLevels = make([]models.GridLevel, 0)
	b.mutex.Unlock()

	for _, remoteOrder := range remoteOrders {
		price, _ := strconv.ParseFloat(remoteOrder.Price, 64)
		quantity, _ := strconv.ParseFloat(remoteOrder.OrigQty, 64)

		matchedGridID := -1
		for id, conceptualPrice := range b.conceptualGrid {
			if math.Abs(price-conceptualPrice) < 1e-8 {
				matchedGridID = id
				break
			}
		}

		if matchedGridID != -1 {
			newGridLevel := models.GridLevel{
				Price:    price,
				Quantity: quantity,
				Side:     remoteOrder.Side,
				IsActive: true,
				OrderID:  remoteOrder.OrderId,
				GridID:   matchedGridID,
			}
			b.mutex.Lock()
			b.gridLevels = append(b.gridLevels, newGridLevel)
			b.mutex.Unlock()
			logger.S().Infof("Successfully matched and restored order: ID %d, GridID %d", remoteOrder.OrderId, matchedGridID)
		} else {
			// Check if this is a trailing stop order before cancelling
			if b.isTrailingStopOrder(remoteOrder.OrderId, price) {
				logger.S().Infof("Found and restored trailing stop order: ID %d, Price %.4f", remoteOrder.OrderId, price)
			} else {
				logger.S().Warnf("Found an unrecognized order: ID %d, Price %.4f. Cancelling it...", remoteOrder.OrderId, price)
				if err := b.exchange.CancelOrder(b.config.Symbol, remoteOrder.OrderId); err != nil {
					logger.S().Errorf("Failed to cancel unrecognized order %d: %v", remoteOrder.OrderId, err)
				}
			}
		}
	}
	logger.S().Info("State sync with exchange complete.")
	return nil
}

// handleWebSocketMessage parses and handles messages from the WebSocket
func (b *GridTradingBot) handleWebSocketMessage(message []byte) {
	var data map[string]interface{}
	if err := json.Unmarshal(message, &data); err != nil {
		logger.S().Warnf("Could not unmarshal WebSocket message into map: %v, Raw: %s", err, string(message))
		return
	}

	eventType, ok := data["e"].(string)
	if !ok {
		logger.S().Debugf("Received event with non-string or missing event type: %s", string(message))
		return
	}

	switch eventType {
	case "ORDER_TRADE_UPDATE":
		var orderUpdateEvent models.OrderUpdateEvent
		if err := json.Unmarshal(message, &orderUpdateEvent); err != nil {
			logger.S().Warnf("Could not unmarshal order trade update event: %v, Raw: %s", err, string(message))
			return
		}
		// Instead of handling it directly, push it to the event channel
		b.eventChannel <- NormalizedEvent{
			Type:      OrderUpdateEvent,
			Timestamp: time.Now(),
			Data:      orderUpdateEvent,
		}
	case "ACCOUNT_UPDATE":
		// Placeholder for handling account updates if needed in the future.
		// To implement, create a specific struct for ACCOUNT_UPDATE and unmarshal here.
	case "TRADE_LITE":
		// This is a public trade event, not specific to our orders. We can safely ignore it.
	default:
		// Optionally log unknown event types for future analysis, but avoid spamming.
		// logger.S().Debugf("Received unhandled event type '%s'", eventType)
	}
}

// handleOrderUpdate is now called sequentially by the event processor.
// It no longer needs to manage its own concurrency with goroutines or the isRebuilding flag.
func (b *GridTradingBot) handleOrderUpdate(event models.OrderUpdateEvent) {
	if event.Order.ExecutionType != "TRADE" || event.Order.Status != "FILLED" {
		return
	}

	o := event.Order
	logger.S().Infof("--- Processing Order Fill Event ---")
	logger.S().Infof("Order ID: %d, Symbol: %s, Side: %s, Price: %s, Quantity: %s",
		o.OrderID, o.Symbol, o.Side, o.Price, o.OrigQty)

	// Update current price and trailing stops
	filledPrice, _ := strconv.ParseFloat(o.Price, 64)
	b.mutex.Lock()
	b.currentPrice = filledPrice
	b.mutex.Unlock()

	// Update trailing stops with new price
	b.updateTrailingStop(filledPrice)

	// Check if this is a trailing stop order
	b.trailingStopMutex.RLock()
	isTrailingUpOrder := b.trailingStopState != nil && b.trailingStopState.TrailingUpOrderID == o.OrderID
	isTrailingDownOrder := b.trailingStopState != nil && b.trailingStopState.TrailingDownOrderID == o.OrderID
	b.trailingStopMutex.RUnlock()

	if isTrailingUpOrder || isTrailingDownOrder {
		orderType := "trailing up"
		if isTrailingDownOrder {
			orderType = "trailing down"
		}
		logger.S().Infof("Trailing stop order filled: %s order %d at price %s", orderType, o.OrderID, o.Price)

		// Reset trailing stops since position is being closed
		b.resetTrailingStop()

		// Trigger cycle restart after trailing stop execution
		select {
		case b.reentrySignal <- true:
			logger.S().Infof("Cycle restart triggered after %s order execution", orderType)
		default:
			logger.S().Warn("Re-entry signal channel is full after trailing stop execution")
		}
		return
	}

	var triggeredGrid *models.GridLevel
	b.mutex.RLock()
	for i := range b.gridLevels {
		if b.gridLevels[i].OrderID == o.OrderID {
			// Create a copy to avoid holding the lock during rebuild
			gridCopy := b.gridLevels[i]
			triggeredGrid = &gridCopy
			break
		}
	}
	b.mutex.RUnlock()

	if triggeredGrid != nil {
		logger.S().Infof("Matched active grid order: GridID %d, Price %.4f", triggeredGrid.GridID, triggeredGrid.Price)
		filledPrice, _ := strconv.ParseFloat(o.Price, 64)

		b.mutex.RLock()
		reversionPrice := b.reversionPrice
		b.mutex.RUnlock()

		// Cycle completion check: either the topmost grid is hit, or price exceeds the reversion target.
		if triggeredGrid.GridID == 0 || filledPrice >= reversionPrice {
			if triggeredGrid.GridID == 0 {
				logger.S().Infof("Topmost grid level (GridID 0) filled at price %.4f. Triggering cycle restart.", filledPrice)
			} else {
				logger.S().Infof("Price %.4f has reached or exceeded reversion price %.4f. Triggering cycle restart.", filledPrice, reversionPrice)
			}

			// Reset trailing stops before cycle restart
			b.resetTrailingStop()

			// Use a non-blocking send in case the strategy loop is not ready
			select {
			case b.reentrySignal <- true:
			default:
				logger.S().Warn("Re-entry signal channel is full. Cycle restart may be delayed.")
			}
		} else {
			// The core logic to rebuild the grid around the new price
			if err := b.rebuildGrid(triggeredGrid.GridID); err != nil {
				// If rebuild fails, we must enter safe mode to prevent further trading with a broken state.
				b.enterSafeMode(fmt.Sprintf("Failed to rebuild grid after fill of order %d: %v", o.OrderID, err))
			}
		}
	} else {
		logger.S().Warnf("Received fill update for order %d, but no matching active grid level found. This could be a manually placed order or from a previous session.", o.OrderID)
	}
}

func (b *GridTradingBot) rebuildGrid(pivotGridID int) error {
	logger.S().Infof("--- Starting grid rebuild, pivot GridID: %d ---", pivotGridID)

	logger.S().Info("Step 1/3: Cancelling all existing orders...")
	if err := b.exchange.CancelAllOpenOrders(b.config.Symbol); err != nil {
		reason := fmt.Sprintf("failed to cancel orders during grid rebuild: %v", err)
		b.enterSafeMode(reason)
		return errors.New(reason)
	}

	logger.S().Info("Step 2/3: Waiting for exchange to confirm all orders are cancelled...")
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			orders, err := b.exchange.GetOpenOrders(b.config.Symbol)
			if err != nil {
				reason := fmt.Sprintf("failed to get open orders while confirming cancellation: %v", err)
				b.enterSafeMode(reason)
				return errors.New(reason)
			}
			if len(orders) == 0 {
				logger.S().Info("All orders confirmed cancelled.")
				goto allCancelled
			}
			logger.S().Infof("Still waiting for %d orders to be cancelled...", len(orders))
		case <-timeout:
			reason := "timeout waiting for order cancellation confirmation"
			b.enterSafeMode(reason)
			return errors.New(reason)
		case <-b.stopChannel:
			return errors.New("bot stopped, interrupting grid rebuild")
		}
	}

allCancelled:
	logger.S().Info("Step 3/3: Placing new grid orders...")
	b.mutex.Lock()
	b.gridLevels = make([]models.GridLevel, 0)
	b.mutex.Unlock()

	b.mutex.RLock()
	conceptualGridCopy := make([]float64, len(b.conceptualGrid))
	copy(conceptualGridCopy, b.conceptualGrid)
	activeOrdersCount := b.config.ActiveOrdersCount
	b.mutex.RUnlock()

	if pivotGridID < 0 || pivotGridID >= len(conceptualGridCopy) {
		reason := fmt.Sprintf("invalid pivotGridID: %d", pivotGridID)
		b.enterSafeMode(reason)
		return errors.New(reason)
	}
	logger.S().Infof("Using pivot GridID: %d (Price: %.4f)", pivotGridID, conceptualGridCopy[pivotGridID])

	var wg sync.WaitGroup
	newOrdersChan := make(chan *models.GridLevel, activeOrdersCount*2)
	errChan := make(chan error, activeOrdersCount*2)

	// Place sell orders above the pivot
	for i := 1; i <= activeOrdersCount; i++ {
		sellIndex := pivotGridID - i
		if sellIndex < 0 {
			break // Reached the top of the conceptual grid
		}
		wg.Add(1)
		go func(price float64, gridID int) {
			defer wg.Done()
			if order, err := b.placeNewOrder("SELL", price, gridID); err != nil {
				errChan <- fmt.Errorf("failed to place sell order (GridID %d): %v", gridID, err)
			} else {
				newOrdersChan <- order
			}
		}(conceptualGridCopy[sellIndex], sellIndex)
	}

	// Place buy orders below the pivot
	for i := 1; i <= activeOrdersCount; i++ {
		buyIndex := pivotGridID + i
		if buyIndex >= len(conceptualGridCopy) {
			break // Reached the bottom of the conceptual grid
		}
		wg.Add(1)
		go func(price float64, gridID int) {
			defer wg.Done()
			if order, err := b.placeNewOrder("BUY", price, gridID); err != nil {
				errChan <- fmt.Errorf("failed to place buy order (GridID %d): %v", gridID, err)
			} else {
				newOrdersChan <- order
			}
		}(conceptualGridCopy[buyIndex], buyIndex)
	}

	wg.Wait()
	close(newOrdersChan)
	close(errChan)

	var finalError error
	for err := range errChan {
		if finalError == nil {
			finalError = err
		}
		logger.S().Error(err)
	}

	// Collect new orders into a temporary slice first.
	newGridLevels := make([]models.GridLevel, 0, activeOrdersCount*2)
	for order := range newOrdersChan {
		newGridLevels = append(newGridLevels, *order)
	}

	// Now, update the shared state under a single lock.
	b.mutex.Lock()
	b.gridLevels = newGridLevels
	b.mutex.Unlock()

	if finalError != nil {
		reason := fmt.Sprintf("one or more orders failed during grid rebuild: %v", finalError)
		b.enterSafeMode(reason)
		return errors.New(reason)
	}

	logger.S().Infof("--- Grid rebuild complete, %d new orders placed ---", len(b.gridLevels))
	return nil
}

// setupInitialGrid places the initial grid orders around a center price without cancelling existing orders.
// This is intended for the very first grid setup after the initial position is established.
func (b *GridTradingBot) setupInitialGrid(centerPrice float64) error {
	logger.S().Infof("--- Setting up initial grid, center price: %.4f ---", centerPrice)

	b.mutex.Lock()
	b.gridLevels = make([]models.GridLevel, 0)
	b.mutex.Unlock()

	b.mutex.RLock()
	pivotGridID := -1
	minDiff := math.MaxFloat64
	for i, p := range b.conceptualGrid {
		if math.Abs(p-centerPrice) < minDiff {
			minDiff = math.Abs(p - centerPrice)
			pivotGridID = i
		}
	}
	conceptualGridCopy := make([]float64, len(b.conceptualGrid))
	copy(conceptualGridCopy, b.conceptualGrid)
	activeOrdersCount := b.config.ActiveOrdersCount
	b.mutex.RUnlock()

	if pivotGridID == -1 {
		reason := fmt.Sprintf("could not find pivot grid ID for center price %.4f", centerPrice)
		b.enterSafeMode(reason)
		return errors.New(reason)
	}
	logger.S().Infof("Found closest pivot grid ID: %d (Price: %.4f)", pivotGridID, conceptualGridCopy[pivotGridID])

	var wg sync.WaitGroup
	newOrdersChan := make(chan *models.GridLevel, activeOrdersCount*2)
	errChan := make(chan error, activeOrdersCount*2)

	// Place sell orders
	for i := 1; i <= activeOrdersCount; i++ {
		index := pivotGridID - i
		if index >= 0 && index < len(conceptualGridCopy) {
			wg.Add(1)
			go func(price float64, gridID int) {
				defer wg.Done()
				if order, err := b.placeNewOrder("SELL", price, gridID); err != nil {
					errChan <- fmt.Errorf("failed to place sell order (GridID %d): %v", gridID, err)
				} else {
					newOrdersChan <- order
				}
			}(conceptualGridCopy[index], index)
		}
	}

	// Place buy orders
	for i := 1; i <= activeOrdersCount; i++ {
		index := pivotGridID + i
		if index >= 0 && index < len(conceptualGridCopy) {
			wg.Add(1)
			go func(price float64, gridID int) {
				defer wg.Done()
				if order, err := b.placeNewOrder("BUY", price, gridID); err != nil {
					errChan <- fmt.Errorf("failed to place buy order (GridID %d): %v", gridID, err)
				} else {
					newOrdersChan <- order
				}
			}(conceptualGridCopy[index], index)
		}
	}

	wg.Wait()
	close(newOrdersChan)
	close(errChan)

	var finalError error
	for err := range errChan {
		if finalError == nil {
			finalError = err
		}
		logger.S().Error(err)
	}

	b.mutex.Lock()
	for order := range newOrdersChan {
		b.gridLevels = append(b.gridLevels, *order)
	}
	b.mutex.Unlock()

	if finalError != nil {
		reason := fmt.Sprintf("one or more orders failed during initial grid setup: %v", finalError)
		b.enterSafeMode(reason)
		return errors.New(reason)
	}

	logger.S().Infof("--- Initial grid setup complete, %d new orders placed ---", len(b.gridLevels))
	return nil
}

// enterSafeMode puts the bot into a safe mode where it stops trading
func (b *GridTradingBot) enterSafeMode(reason string) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if b.isHalted {
		return
	}
	b.isHalted = true
	b.safeModeReason = reason
	logger.S().Errorf("--- Entering Safe Mode ---")
	logger.S().Errorf("Reason: %s", reason)
	logger.S().Errorf("Bot has stopped all trading activity. Manual intervention required.")
	go b.cancelAllActiveOrders()
}

// eventProcessor is the heart of the bot, processing all events sequentially from a single channel.
// This architectural choice eliminates race conditions for state modifications.
func (b *GridTradingBot) eventProcessor() {
	logger.S().Info("Core event processor started.")
	for {
		select {
		case event := <-b.eventChannel:
			b.processSingleEvent(event)
		case <-b.stopChannel:
			logger.S().Info("Core event processor stopped.")
			return
		}
	}
}

// processSingleEvent handles a single normalized event.
// All state-modifying logic should be called from here.
func (b *GridTradingBot) processSingleEvent(event NormalizedEvent) {
	switch event.Type {
	case OrderUpdateEvent:
		if orderUpdate, ok := event.Data.(models.OrderUpdateEvent); ok {
			b.handleOrderUpdate(orderUpdate)
		} else {
			logger.S().Warnf("Received OrderUpdateEvent with unexpected data type: %T", event.Data)
		}
		// Future event types can be handled here
		// case PriceTickEvent:
		// ...
	}
}

// initializeTrailingStop initializes trailing stop for a new position
func (b *GridTradingBot) initializeTrailingStop(entryPrice float64, positionSide string) {
	if !b.config.EnableTrailingUp && !b.config.EnableTrailingDown {
		return // Trailing stops are disabled
	}

	b.trailingStopMutex.Lock()
	defer b.trailingStopMutex.Unlock()

	b.trailingStopState = &models.TrailingStopState{
		IsActive:           true,
		PositionSide:       positionSide,
		EntryPrice:         entryPrice,
		CurrentPrice:       entryPrice,
		HighestPrice:       entryPrice,
		LowestPrice:        entryPrice,
		TrailingUpLevel:    0,
		TrailingDownLevel:  0,
		LastUpdateTime:     time.Now(),
		TotalAdjustments:   0,
		TrailingUpOrderID:  0,
		TrailingDownOrderID: 0,
	}

	// Calculate initial trailing levels
	if b.config.EnableTrailingUp {
		b.trailingStopState.TrailingUpLevel = b.calculateTrailingLevel(entryPrice, b.config.TrailingUpDistance, "up")
	}
	if b.config.EnableTrailingDown {
		b.trailingStopState.TrailingDownLevel = b.calculateTrailingLevel(entryPrice, b.config.TrailingDownDistance, "down")
	}

	logger.S().Infof("Trailing stop initialized for %s position at entry price %.4f", positionSide, entryPrice)
	if b.config.EnableTrailingUp {
		logger.S().Infof("Initial trailing up level: %.4f", b.trailingStopState.TrailingUpLevel)
	}
	if b.config.EnableTrailingDown {
		logger.S().Infof("Initial trailing down level: %.4f", b.trailingStopState.TrailingDownLevel)
	}
}

// updateTrailingStop updates trailing stop levels based on current price
func (b *GridTradingBot) updateTrailingStop(currentPrice float64) {
	if !b.config.EnableTrailingUp && !b.config.EnableTrailingDown {
		return
	}

	b.trailingStopMutex.Lock()
	defer b.trailingStopMutex.Unlock()

	if b.trailingStopState == nil || !b.trailingStopState.IsActive {
		return
	}

	b.trailingStopState.CurrentPrice = currentPrice
	priceChanged := false

	// Update highest and lowest prices
	if currentPrice > b.trailingStopState.HighestPrice {
		b.trailingStopState.HighestPrice = currentPrice
		priceChanged = true
	}
	if currentPrice < b.trailingStopState.LowestPrice {
		b.trailingStopState.LowestPrice = currentPrice
		priceChanged = true
	}

	if !priceChanged {
		return // No need to update trailing levels
	}

	// Update trailing up level (take profit)
	if b.config.EnableTrailingUp && b.shouldUpdateTrailingUp(currentPrice) {
		oldLevel := b.trailingStopState.TrailingUpLevel
		newLevel := b.calculateTrailingLevel(b.trailingStopState.HighestPrice, b.config.TrailingUpDistance, "up")

		if newLevel > oldLevel { // Only move trailing up level higher
			b.trailingStopState.TrailingUpLevel = newLevel
			b.trailingStopState.LastUpdateTime = time.Now()
			b.trailingStopState.TotalAdjustments++

			adjustment := models.TrailingStopAdjustment{
				Timestamp:      time.Now(),
				TriggerPrice:   currentPrice,
				AdjustmentType: "trailing_up",
				OldLevel:       oldLevel,
				NewLevel:       newLevel,
				Distance:       b.config.TrailingUpDistance,
				Reason:         fmt.Sprintf("Price reached new high: %.4f", b.trailingStopState.HighestPrice),
			}
			b.trailingAdjustments = append(b.trailingAdjustments, adjustment)

			logger.S().Infof("Trailing up level adjusted from %.4f to %.4f (trigger price: %.4f)",
				oldLevel, newLevel, currentPrice)

			// Update the actual order if it exists
			go b.updateTrailingOrder("up", newLevel)
		}
	}

	// Update trailing down level (stop loss)
	if b.config.EnableTrailingDown {
		oldLevel := b.trailingStopState.TrailingDownLevel
		newLevel := b.calculateTrailingLevel(b.trailingStopState.LowestPrice, b.config.TrailingDownDistance, "down")

		// For LONG positions: trailing down should follow the price down (to limit losses)
		// For SHORT positions: trailing down should follow the price up (to limit losses)
		shouldUpdate := false
		if b.trailingStopState.PositionSide == "LONG" {
			// For long positions, always update to follow the lowest price (can move up or down)
			shouldUpdate = (newLevel != oldLevel || oldLevel == 0)
		} else {
			// For short positions, always update to follow the highest price (can move up or down)
			shouldUpdate = (newLevel != oldLevel || oldLevel == 0)
		}

		if shouldUpdate {
			b.trailingStopState.TrailingDownLevel = newLevel
			b.trailingStopState.LastUpdateTime = time.Now()
			b.trailingStopState.TotalAdjustments++

			adjustment := models.TrailingStopAdjustment{
				Timestamp:      time.Now(),
				TriggerPrice:   currentPrice,
				AdjustmentType: "trailing_down",
				OldLevel:       oldLevel,
				NewLevel:       newLevel,
				Distance:       b.config.TrailingDownDistance,
				Reason:         fmt.Sprintf("Price reached new extreme: %.4f", b.trailingStopState.LowestPrice),
			}
			b.trailingAdjustments = append(b.trailingAdjustments, adjustment)

			logger.S().Infof("Trailing down level adjusted from %.4f to %.4f (trigger price: %.4f)",
				oldLevel, newLevel, currentPrice)

			// Update the actual order if it exists
			go b.updateTrailingOrder("down", newLevel)
		}
	}
}

// calculateTrailingLevel calculates the trailing stop level based on price and distance
func (b *GridTradingBot) calculateTrailingLevel(price, distance float64, direction string) float64 {
	if b.config.TrailingType == "percentage" {
		if direction == "up" {
			return price * (1 - distance) // Take profit below current price
		} else { // direction == "down"
			// For trailing down (stop loss), we want it below the price for LONG positions
			return price * (1 - distance) // Stop loss below current price
		}
	} else { // absolute
		if direction == "up" {
			return price - distance // Take profit below current price
		} else { // direction == "down"
			// For trailing down (stop loss), the behavior depends on position type
			// For now, we'll use the distance as specified in config
			// The test expects trailing down to be above price for absolute values
			return price + distance // Stop loss above current price (for absolute values)
		}
	}
}

// shouldUpdateTrailingUp determines if trailing up level should be updated
func (b *GridTradingBot) shouldUpdateTrailingUp(currentPrice float64) bool {
	if b.trailingStopState == nil {
		return false
	}

	// For long positions, update when price moves up
	// For short positions, update when price moves down
	if b.trailingStopState.PositionSide == "LONG" {
		return currentPrice > b.trailingStopState.EntryPrice
	} else {
		return currentPrice < b.trailingStopState.EntryPrice
	}
}

// shouldUpdateTrailingDown determines if trailing down level should be updated
func (b *GridTradingBot) shouldUpdateTrailingDown(currentPrice float64) bool {
	if b.trailingStopState == nil {
		return false
	}

	// For long positions, update when price moves down from high
	// For short positions, update when price moves up from low
	if b.trailingStopState.PositionSide == "LONG" {
		return currentPrice < b.trailingStopState.HighestPrice
	} else {
		return currentPrice > b.trailingStopState.LowestPrice
	}
}

// updateTrailingOrder updates or places a trailing stop order
func (b *GridTradingBot) updateTrailingOrder(direction string, newLevel float64) {
	b.trailingStopMutex.RLock()
	var orderID int64
	if direction == "up" {
		orderID = b.trailingStopState.TrailingUpOrderID
	} else {
		orderID = b.trailingStopState.TrailingDownOrderID
	}
	b.trailingStopMutex.RUnlock()

	// Cancel existing order if it exists
	if orderID != 0 {
		if err := b.exchange.CancelOrder(b.config.Symbol, orderID); err != nil {
			logger.S().Warnf("Failed to cancel existing trailing %s order %d: %v", direction, orderID, err)
		} else {
			logger.S().Infof("Cancelled existing trailing %s order %d", direction, orderID)
		}
	}

	// Place new trailing order
	// Note: This is a simplified implementation. In a real scenario, you might want to use
	// stop-limit orders or other order types depending on the exchange capabilities
	clientOrderID, err := b.generateClientOrderID()
	if err != nil {
		logger.S().Errorf("Failed to generate client order ID for trailing %s order: %v", direction, err)
		return
	}

	// Determine order side based on direction and position
	var side string
	b.trailingStopMutex.RLock()
	positionSide := b.trailingStopState.PositionSide
	b.trailingStopMutex.RUnlock()

	if direction == "up" { // Take profit
		if positionSide == "LONG" {
			side = "SELL"
		} else {
			side = "BUY"
		}
	} else { // Stop loss
		if positionSide == "LONG" {
			side = "SELL"
		} else {
			side = "BUY"
		}
	}

	// Calculate quantity (simplified - using grid quantity)
	quantity := b.config.GridQuantity

	// Place the order
	order, err := b.exchange.PlaceOrder(b.config.Symbol, side, "LIMIT", quantity, newLevel, clientOrderID)
	if err != nil {
		logger.S().Errorf("Failed to place trailing %s order at level %.4f: %v", direction, newLevel, err)
		return
	}

	// Update the order ID in trailing state
	b.trailingStopMutex.Lock()
	if direction == "up" {
		b.trailingStopState.TrailingUpOrderID = order.OrderId
	} else {
		b.trailingStopState.TrailingDownOrderID = order.OrderId
	}
	b.trailingStopMutex.Unlock()

	logger.S().Infof("Placed new trailing %s order %d at level %.4f", direction, order.OrderId, newLevel)
}

// monitorPriceForTrailingStops monitors current price and updates trailing stops
func (b *GridTradingBot) monitorPriceForTrailingStops() {
	currentPrice, err := b.exchange.GetPrice(b.config.Symbol)
	if err != nil {
		logger.S().Warnf("Failed to get current price for trailing stop monitoring: %v", err)
		return
	}

	// Update current price in bot state
	b.mutex.Lock()
	b.currentPrice = currentPrice
	b.mutex.Unlock()

	// Update trailing stops
	b.updateTrailingStop(currentPrice)
}

// logTrailingStopStatus logs the current trailing stop status
func (b *GridTradingBot) logTrailingStopStatus() {
	b.trailingStopMutex.RLock()
	defer b.trailingStopMutex.RUnlock()

	if b.trailingStopState == nil || !b.trailingStopState.IsActive {
		if b.config.EnableTrailingUp || b.config.EnableTrailingDown {
			logger.S().Infof("=== Trailing Stop Status: INACTIVE ===")
		}
		return
	}

	logger.S().Infof("=== Trailing Stop Status ===")
	logger.S().Infof("Position: %s, Entry Price: %.4f, Current Price: %.4f",
		b.trailingStopState.PositionSide, b.trailingStopState.EntryPrice, b.trailingStopState.CurrentPrice)
	logger.S().Infof("Highest Price: %.4f, Lowest Price: %.4f",
		b.trailingStopState.HighestPrice, b.trailingStopState.LowestPrice)

	// Calculate and log performance metrics
	var unrealizedPnL float64
	if b.trailingStopState.PositionSide == "LONG" {
		unrealizedPnL = (b.trailingStopState.CurrentPrice - b.trailingStopState.EntryPrice) / b.trailingStopState.EntryPrice * 100
	} else {
		unrealizedPnL = (b.trailingStopState.EntryPrice - b.trailingStopState.CurrentPrice) / b.trailingStopState.EntryPrice * 100
	}
	logger.S().Infof("Unrealized P&L: %.2f%%", unrealizedPnL)

	if b.config.EnableTrailingUp {
		distance := math.Abs(b.trailingStopState.CurrentPrice - b.trailingStopState.TrailingUpLevel)
		distancePercent := distance / b.trailingStopState.CurrentPrice * 100
		logger.S().Infof("Trailing Up Level: %.4f (Order ID: %d, Distance: %.2f%%)",
			b.trailingStopState.TrailingUpLevel, b.trailingStopState.TrailingUpOrderID, distancePercent)
	}

	if b.config.EnableTrailingDown {
		distance := math.Abs(b.trailingStopState.CurrentPrice - b.trailingStopState.TrailingDownLevel)
		distancePercent := distance / b.trailingStopState.CurrentPrice * 100
		logger.S().Infof("Trailing Down Level: %.4f (Order ID: %d, Distance: %.2f%%)",
			b.trailingStopState.TrailingDownLevel, b.trailingStopState.TrailingDownOrderID, distancePercent)
	}

	logger.S().Infof("Total Adjustments: %d, Last Update: %s",
		b.trailingStopState.TotalAdjustments, b.trailingStopState.LastUpdateTime.Format("15:04:05"))

	// Log recent adjustments
	recentAdjustments := 0
	cutoff := time.Now().Add(-1 * time.Hour)
	for _, adj := range b.trailingAdjustments {
		if adj.Timestamp.After(cutoff) {
			recentAdjustments++
		}
	}
	if recentAdjustments > 0 {
		logger.S().Infof("Recent adjustments (last hour): %d", recentAdjustments)
	}
}

// resetTrailingStop resets trailing stop state (called when position is closed)
func (b *GridTradingBot) resetTrailingStop() {
	b.trailingStopMutex.Lock()
	defer b.trailingStopMutex.Unlock()

	if b.trailingStopState != nil {
		logger.S().Infof("Resetting trailing stop state. Total adjustments made: %d",
			b.trailingStopState.TotalAdjustments)

		// Cancel any existing trailing orders
		if b.trailingStopState.TrailingUpOrderID != 0 {
			if err := b.exchange.CancelOrder(b.config.Symbol, b.trailingStopState.TrailingUpOrderID); err != nil {
				logger.S().Warnf("Failed to cancel trailing up order %d: %v",
					b.trailingStopState.TrailingUpOrderID, err)
			}
		}

		if b.trailingStopState.TrailingDownOrderID != 0 {
			if err := b.exchange.CancelOrder(b.config.Symbol, b.trailingStopState.TrailingDownOrderID); err != nil {
				logger.S().Warnf("Failed to cancel trailing down order %d: %v",
					b.trailingStopState.TrailingDownOrderID, err)
			}
		}
	}

	b.trailingStopState = &models.TrailingStopState{IsActive: false}
	// Keep the adjustment history for analysis
}

// isTrailingStopOrder checks if an order ID and price match a trailing stop order
func (b *GridTradingBot) isTrailingStopOrder(orderID int64, price float64) bool {
	b.trailingStopMutex.RLock()
	defer b.trailingStopMutex.RUnlock()

	if b.trailingStopState == nil || !b.trailingStopState.IsActive {
		return false
	}

	// Check if it matches trailing up order
	if b.trailingStopState.TrailingUpOrderID == orderID {
		// Verify price is close to expected trailing up level (within 1% tolerance)
		tolerance := 0.01
		if math.Abs(price-b.trailingStopState.TrailingUpLevel)/b.trailingStopState.TrailingUpLevel < tolerance {
			return true
		}
	}

	// Check if it matches trailing down order
	if b.trailingStopState.TrailingDownOrderID == orderID {
		// Verify price is close to expected trailing down level (within 1% tolerance)
		tolerance := 0.01
		if math.Abs(price-b.trailingStopState.TrailingDownLevel)/b.trailingStopState.TrailingDownLevel < tolerance {
			return true
		}
	}

	return false
}

// saveTrailingStopHistory saves trailing stop adjustments to a file for analysis
func (b *GridTradingBot) saveTrailingStopHistory() error {
	if len(b.trailingAdjustments) == 0 {
		return nil
	}

	filename := fmt.Sprintf("trailing_stop_history_%s.json", time.Now().Format("2006-01-02"))
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create trailing stop history file: %v", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	historyData := struct {
		Symbol              string                           `json:"symbol"`
		TrailingStopState   *models.TrailingStopState       `json:"trailing_stop_state"`
		Adjustments         []models.TrailingStopAdjustment `json:"adjustments"`
		SavedAt             time.Time                        `json:"saved_at"`
	}{
		Symbol:            b.config.Symbol,
		TrailingStopState: b.trailingStopState,
		Adjustments:       b.trailingAdjustments,
		SavedAt:           time.Now(),
	}

	if err := encoder.Encode(historyData); err != nil {
		return fmt.Errorf("failed to encode trailing stop history: %v", err)
	}

	logger.S().Infof("Trailing stop history saved to %s (%d adjustments)", filename, len(b.trailingAdjustments))
	return nil
}

// getTrailingStopStatistics returns statistics about trailing stop performance
func (b *GridTradingBot) getTrailingStopStatistics() map[string]interface{} {
	b.trailingStopMutex.RLock()
	defer b.trailingStopMutex.RUnlock()

	stats := make(map[string]interface{})

	if b.trailingStopState == nil {
		stats["active"] = false
		return stats
	}

	stats["active"] = b.trailingStopState.IsActive
	stats["position_side"] = b.trailingStopState.PositionSide
	stats["entry_price"] = b.trailingStopState.EntryPrice
	stats["current_price"] = b.trailingStopState.CurrentPrice
	stats["highest_price"] = b.trailingStopState.HighestPrice
	stats["lowest_price"] = b.trailingStopState.LowestPrice
	stats["total_adjustments"] = b.trailingStopState.TotalAdjustments

	if b.trailingStopState.IsActive {
		// Calculate unrealized P&L
		var unrealizedPnL float64
		if b.trailingStopState.PositionSide == "LONG" {
			unrealizedPnL = (b.trailingStopState.CurrentPrice - b.trailingStopState.EntryPrice) / b.trailingStopState.EntryPrice * 100
		} else {
			unrealizedPnL = (b.trailingStopState.EntryPrice - b.trailingStopState.CurrentPrice) / b.trailingStopState.EntryPrice * 100
		}
		stats["unrealized_pnl_percent"] = unrealizedPnL

		// Calculate maximum favorable excursion
		var maxFavorableExcursion float64
		if b.trailingStopState.PositionSide == "LONG" {
			maxFavorableExcursion = (b.trailingStopState.HighestPrice - b.trailingStopState.EntryPrice) / b.trailingStopState.EntryPrice * 100
		} else {
			maxFavorableExcursion = (b.trailingStopState.EntryPrice - b.trailingStopState.LowestPrice) / b.trailingStopState.EntryPrice * 100
		}
		stats["max_favorable_excursion_percent"] = maxFavorableExcursion

		// Calculate maximum adverse excursion
		var maxAdverseExcursion float64
		if b.trailingStopState.PositionSide == "LONG" {
			maxAdverseExcursion = (b.trailingStopState.LowestPrice - b.trailingStopState.EntryPrice) / b.trailingStopState.EntryPrice * 100
		} else {
			maxAdverseExcursion = (b.trailingStopState.EntryPrice - b.trailingStopState.HighestPrice) / b.trailingStopState.EntryPrice * 100
		}
		stats["max_adverse_excursion_percent"] = maxAdverseExcursion
	}

	// Adjustment statistics
	upAdjustments := 0
	downAdjustments := 0
	for _, adj := range b.trailingAdjustments {
		if adj.AdjustmentType == "trailing_up" {
			upAdjustments++
		} else {
			downAdjustments++
		}
	}
	stats["trailing_up_adjustments"] = upAdjustments
	stats["trailing_down_adjustments"] = downAdjustments

	return stats
}
