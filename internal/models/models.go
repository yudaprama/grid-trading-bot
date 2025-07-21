package models

import (
	"fmt"
	"time"
)

// Config is a struct that defines all the configuration parameters for the robot
type Config struct {
	IsTestnet                bool      `json:"is_testnet"` // 是否使用测试网
	LiveAPIURL               string    `json:"live_api_url"`
	LiveWSURL                string    `json:"live_ws_url"`
	TestnetAPIURL            string    `json:"testnet_api_url"`
	TestnetWSURL             string    `json:"testnet_ws_url"`
	Symbol                   string    `json:"symbol"`                                // 交易对，如 "BTCUSDT"
	GridSpacing              float64   `json:"grid_spacing"`                          // 网格间距比例
	GridValue                float64   `json:"grid_value,omitempty"`                  // 每个网格的交易价值 (USDT)
	GridQuantity             float64   `json:"grid_quantity,omitempty"`               // 新增：每个网格的交易数量（基础货币）
	MinNotionalValue         float64   `json:"min_notional_value"`                    // 新增: 交易所最小订单名义价值 (例如 5 USDT)
	InitialInvestment        float64   `json:"initial_investment"`                    // 初始投资额 (USDT), 用于市价买入
	Leverage                 int       `json:"leverage"`                              // 杠杆倍数
	MarginType               string    `json:"margin_type"`                           // 保证金模式: CROSSED 或 ISOLATED
	PositionMode             string    `json:"position_mode"`                         // 新增: 持仓模式, "Oneway" 或 "Hedge"
	StopLossRate             float64   `json:"stop_loss_rate,omitempty"`              // 新增: 止损率
	HedgeMode                bool      `json:"hedge_mode"`                            // 是否开启对冲模式 (双向持仓)
	GridCount                int       `json:"grid_count"`                            // 网格数量（对）
	ActiveOrdersCount        int       `json:"active_orders_count"`                   // 在价格两侧各挂的订单数量
	ReturnRate               float64   `json:"return_rate"`                           // 预期回归价格比例
	WalletExposureLimit      float64   `json:"wallet_exposure_limit"`                 // 新增：钱包风险暴露上限
	LogConfig                LogConfig `json:"log"`                                   // 新增：日志配置
	RetryAttempts            int       `json:"retry_attempts"`                        // 新增: 下单失败时的重试次数

	// Trailing Stop Configuration
	EnableTrailingUp         bool      `json:"enable_trailing_up"`                    // 启用追踪止盈功能
	EnableTrailingDown       bool      `json:"enable_trailing_down"`                  // 启用追踪止损功能
	TrailingUpDistance       float64   `json:"trailing_up_distance"`                  // 追踪止盈距离
	TrailingDownDistance     float64   `json:"trailing_down_distance"`                // 追踪止损距离
	TrailingType             string    `json:"trailing_type"`                         // 追踪类型: "percentage" 或 "absolute"

	// Trading Mode Configuration
	TradingMode              string    `json:"trading_mode"`                          // 交易模式: "futures" 或 "spot"

	// Spot Trading Configuration (only used when TradingMode == "spot")
	BaseAsset                string    `json:"base_asset,omitempty"`                  // 基础资产 (如 BTC)
	QuoteAsset               string    `json:"quote_asset,omitempty"`                 // 计价资产 (如 USDT)
	InitialBaseAmount        float64   `json:"initial_base_amount,omitempty"`         // 初始基础资产数量
	InitialQuoteAmount       float64   `json:"initial_quote_amount,omitempty"`        // 初始计价资产数量
	SpotAPIURL               string    `json:"spot_api_url,omitempty"`                // 现货API地址
	SpotWSURL                string    `json:"spot_ws_url,omitempty"`                 // 现货WebSocket地址
	SpotTestnetAPIURL        string    `json:"spot_testnet_api_url,omitempty"`        // 现货测试网API地址
	SpotTestnetWSURL         string    `json:"spot_testnet_ws_url,omitempty"`         // 现货测试网WebSocket地址
	RetryInitialDelayMs      int       `json:"retry_initial_delay_ms"`                // 新增: 重试前的初始延迟毫秒数
	WebSocketPingIntervalSec int       `json:"websocket_ping_interval_sec,omitempty"` // 新增: WebSocket Ping消息发送间隔(秒)
	WebSocketPongTimeoutSec  int       `json:"websocket_pong_timeout_sec,omitempty"`  // 新增: WebSocket Pong消息超时时间(秒)

	// Backtest engine specific configuration
	TakerFeeRate          float64 `json:"taker_fee_rate"`          // Taker fee rate
	MakerFeeRate          float64 `json:"maker_fee_rate"`          // Maker fee rate
	SlippageRate          float64 `json:"slippage_rate"`           // Slippage rate
	MaintenanceMarginRate float64 `json:"maintenance_margin_rate"` // Maintenance margin rate

	BaseURL   string `json:"base_url"`    // REST API base URL (will be set dynamically by the program)
	WSBaseURL string `json:"ws_base_url"` // WebSocket base URL (will be set dynamically by the program)
}

// LogConfig defines log-related configuration
type LogConfig struct {
	Level      string `json:"level"`       // Log level, e.g., "debug", "info", "warn", "error"
	Output     string `json:"output"`      // Output mode: "console", "file", "both"
	File       string `json:"file"`        // Log file path
	MaxSize    int    `json:"max_size"`    // Maximum size of a single log file (MB)
	MaxBackups int    `json:"max_backups"` // Maximum number of old log files to retain
	MaxAge     int    `json:"max_age"`     // Maximum retention days for old log files
	Compress   bool   `json:"compress"`    // Whether to compress old log files
}

// AccountInfo defines the account information for Binance
type AccountInfo struct {
	// Futures-specific fields
	TotalWalletBalance string `json:"totalWalletBalance"`
	AvailableBalance   string `json:"availableBalance"`
	Assets             []struct {
		Asset                  string `json:"asset"`
		WalletBalance          string `json:"walletBalance"`
		UnrealizedProfit       string `json:"unrealizedProfit"`
		MarginBalance          string `json:"marginBalance"`
		MaintMargin            string `json:"maintMargin"`
		InitialMargin          string `json:"initialMargin"`
		PositionInitialMargin  string `json:"positionInitialMargin"`
		OpenOrderInitialMargin string `json:"openOrderInitialMargin"`
		MaxWithdrawAmount      string `json:"maxWithdrawAmount"`
	} `json:"assets"`

	// Spot-specific fields
	MakerCommission  int    `json:"makerCommission,omitempty"`
	TakerCommission  int    `json:"takerCommission,omitempty"`
	BuyerCommission  int    `json:"buyerCommission,omitempty"`
	SellerCommission int    `json:"sellerCommission,omitempty"`
	CanTrade         bool   `json:"canTrade,omitempty"`
	CanWithdraw      bool   `json:"canWithdraw,omitempty"`
	CanDeposit       bool   `json:"canDeposit,omitempty"`
	UpdateTime       int64  `json:"updateTime,omitempty"`
	AccountType      string `json:"accountType,omitempty"`
}

// Position defines the position information
type Position struct {
	Symbol           string `json:"symbol"`
	PositionAmt      string `json:"positionAmt"`
	EntryPrice       string `json:"entryPrice"`
	MarkPrice        string `json:"markPrice"`
	UnrealizedProfit string `json:"unRealizedProfit"`
	LiquidationPrice string `json:"liquidationPrice"`
	Leverage         string `json:"leverage"`
	MaxNotionalValue string `json:"maxNotionalValue"`
	MarginType       string `json:"marginType"`
	IsolatedMargin   string `json:"isolatedMargin"`
	IsAutoAddMargin  string `json:"isAutoAddMargin"`
	PositionSide     string `json:"positionSide"`
	Notional         string `json:"notional"`
	IsolatedWallet   string `json:"isolatedWallet"`
	UpdateTime       int64  `json:"updateTime"`
}

// Order defines the order information
type Order struct {
	Symbol        string `json:"symbol"`
	OrderId       int64  `json:"orderId"`
	ClientOrderId string `json:"clientOrderId"`
	Price         string `json:"price"`
	OrigQty       string `json:"origQty"`
	ExecutedQty   string `json:"executedQty"`
	CumQuote      string `json:"cumQuote"`
	Status        string `json:"status"`
	TimeInForce   string `json:"timeInForce"`
	Type          string `json:"type"`
	Side          string `json:"side"`
	StopPrice     string `json:"stopPrice"`
	IcebergQty    string `json:"icebergQty"`
	Time          int64  `json:"time"`
	UpdateTime    int64  `json:"updateTime"`
	TransactTime  int64  `json:"transactTime"` // For spot trading
	IsWorking     bool   `json:"isWorking"`
	WorkingType   string `json:"workingType"`
	OrigType      string `json:"origType"`
	PositionSide  string `json:"positionSide"`
	ActivatePrice string `json:"activatePrice"`
	PriceRate     string `json:"priceRate"`
	ReduceOnly    bool   `json:"reduceOnly"`
	ClosePosition bool   `json:"closePosition"`
	PriceProtect  bool   `json:"priceProtect"`
}

// GridLevel represents a price level in the grid
type GridLevel struct {
	Price           float64 `json:"price"`
	Quantity        float64 `json:"quantity"`
	Side            string  `json:"side"`
	IsActive        bool    `json:"is_active"`
	OrderID         int64   `json:"order_id"`
	GridID          int     `json:"grid_id"`                     // New: record the theoretical grid ID associated with this order (index of conceptualGrid)
	PairID          int     `json:"pair_id"`                     // Used to pair buy and sell orders
	PairedSellPrice float64 `json:"paired_sell_price,omitempty"` // Only used in buy orders, records the corresponding sell price
}

// CompletedTrade is a struct that records a completed trade (buy and sell)
type CompletedTrade struct {
	Symbol       string
	Quantity     float64
	EntryTime    time.Time
	ExitTime     time.Time
	HoldDuration time.Duration // Hold duration
	EntryPrice   float64
	ExitPrice    float64 // Record sell price
	Profit       float64
	Fee          float64 // Single trade fee
	Slippage     float64 // Single trade slippage cost
}

// BuyTrade records the details of a single buy-in for FIFO accounting.
type BuyTrade struct {
	Timestamp time.Time
	Quantity  float64
	Price     float64
}

// ExchangeInfo holds the full exchange information response
type ExchangeInfo struct {
	Symbols []SymbolInfo `json:"symbols"`
}

// SymbolInfo holds trading rules for a single symbol
type SymbolInfo struct {
	Symbol  string   `json:"symbol"`
	Status  string   `json:"status"`  // For spot trading
	Filters []Filter `json:"filters"`
}

// Filter holds filter data, we are interested in PRICE_FILTER and LOT_SIZE
type Filter struct {
	FilterType  string `json:"filterType"`
	TickSize    string `json:"tickSize,omitempty"`    // For PRICE_FILTER
	StepSize    string `json:"stepSize,omitempty"`    // For LOT_SIZE
	MinQty      string `json:"minQty,omitempty"`      // For LOT_SIZE
	MaxQty      string `json:"maxQty,omitempty"`      // For LOT_SIZE
	MinPrice    string `json:"minPrice,omitempty"`    // For PRICE_FILTER
	MaxPrice    string `json:"maxPrice,omitempty"`    // For PRICE_FILTER
	MinNotional string `json:"minNotional,omitempty"` // For MIN_NOTIONAL
}

// SymbolFilter is an alias for Filter (for compatibility)
type SymbolFilter = Filter

// TrailingStopState represents the current state of trailing stops for a position
type TrailingStopState struct {
	IsActive              bool      `json:"is_active"`                // Whether trailing stop is currently active
	PositionSide          string    `json:"position_side"`            // "LONG" or "SHORT"
	EntryPrice            float64   `json:"entry_price"`              // Original entry price of the position
	CurrentPrice          float64   `json:"current_price"`            // Current market price
	HighestPrice          float64   `json:"highest_price"`            // Highest price seen since position opened (for trailing up)
	LowestPrice           float64   `json:"lowest_price"`             // Lowest price seen since position opened (for trailing down)
	TrailingUpLevel       float64   `json:"trailing_up_level"`        // Current trailing take profit level
	TrailingDownLevel     float64   `json:"trailing_down_level"`      // Current trailing stop loss level
	LastUpdateTime        time.Time `json:"last_update_time"`         // Last time trailing levels were updated
	TotalAdjustments      int       `json:"total_adjustments"`        // Total number of trailing adjustments made
	TrailingUpOrderID     int64     `json:"trailing_up_order_id"`     // Order ID for trailing take profit order
	TrailingDownOrderID   int64     `json:"trailing_down_order_id"`   // Order ID for trailing stop loss order
}

// TrailingStopAdjustment represents a single trailing stop adjustment event
type TrailingStopAdjustment struct {
	Timestamp         time.Time `json:"timestamp"`           // When the adjustment occurred
	TriggerPrice      float64   `json:"trigger_price"`       // Price that triggered the adjustment
	AdjustmentType    string    `json:"adjustment_type"`     // "trailing_up" or "trailing_down"
	OldLevel          float64   `json:"old_level"`           // Previous trailing level
	NewLevel          float64   `json:"new_level"`           // New trailing level
	Distance          float64   `json:"distance"`            // Distance/percentage used for adjustment
	Reason            string    `json:"reason"`              // Reason for the adjustment
}

// Trade defines information for a single trade execution
type Trade struct {
	Symbol          string `json:"symbol"`
	ID              int64  `json:"id"`
	OrderID         int64  `json:"orderId"`
	Side            string `json:"side"`
	Price           string `json:"price"`
	Qty             string `json:"qty"`
	RealizedPnl     string `json:"realizedPnl"`
	MarginAsset     string `json:"marginAsset"`
	QuoteQty        string `json:"quoteQty"`
	Commission      string `json:"commission"`
	CommissionAsset string `json:"commissionAsset"`
	Time            int64  `json:"time"`
	PositionSide    string `json:"positionSide"`
	Buyer           bool   `json:"buyer"`
	Maker           bool   `json:"maker"`
}

// TradeEvent defines the trade event received from the WebSocket
type TradeEvent struct {
	EventType string `json:"e"` // Event type
	EventTime int64  `json:"E"` // Event time
	Symbol    string `json:"s"` // Symbol
	TradeID   int64  `json:"a"` // Aggregate trade ID
	Price     string `json:"p"` // Price
	Quantity  string `json:"q"` // Quantity
	FirstID   int64  `json:"f"` // First trade ID
	LastID    int64  `json:"l"` // Last trade ID
	TradeTime int64  `json:"T"` // Trade time
	IsMaker   bool   `json:"m"` // Is the buyer the market maker?
}

// UserDataEvent is the general event structure received from the user data stream
type UserDataEvent struct {
	EventType string `json:"e"` // Event type, e.g., "executionReport"
	EventTime int64  `json:"E"` // Event time
	// Depending on the event type, here can be different payloads
	ExecutionReport ExecutionReport `json:"o"`
}

// ExecutionReport contains the detailed information of the order update
type ExecutionReport struct {
	Symbol        string `json:"s"`  // Symbol
	ClientOrderID string `json:"c"`  // Client Order ID
	Side          string `json:"S"`  // Side
	OrderType     string `json:"o"`  // Order Type
	TimeInForce   string `json:"f"`  // Time in Force
	OrigQty       string `json:"q"`  // Original Quantity
	Price         string `json:"p"`  // Price
	AvgPrice      string `json:"ap"` // Average Price
	StopPrice     string `json:"sp"` // Stop Price
	ExecType      string `json:"x"`  // Execution Type
	OrderStatus   string `json:"X"`  // Order Status
	OrderID       int64  `json:"i"`  // Order ID
	ExecutedQty   string `json:"l"`  // Last Executed Quantity
	CumQty        string `json:"z"`  // Cumulative Filled Quantity
	ExecutedPrice string `json:"L"`  // Last Executed Price
	CommissionAmt string `json:"n"`  // Commission Amount
	CommissionAs  string `json:"N"`  // Commission Asset
	TradeTime     int64  `json:"T"`  // Trade Time
	TradeID       int64  `json:"t"`  // Trade ID
}

// BotState defines the state of the robot that needs to be saved and loaded
type BotState struct {
	GridLevels              []GridLevel `json:"grid_levels"`
	BasePositionEstablished bool        `json:"base_position_established"`
	ConceptualGrid          []float64   `json:"conceptual_grid"`
	EntryPrice              float64     `json:"entry_price"`
	ReversionPrice          float64     `json:"reversion_price"`
	IsReentering            bool        `json:"is_reentering"`
	CurrentPrice            float64     `json:"current_price"`
	CurrentTime             time.Time   `json:"current_time"`
}

// Balance defines the balance information of a specific asset in the account
type Balance struct {
	Asset              string `json:"asset"`
	Balance            string `json:"balance"`
	CrossWalletBalance string `json:"crossWalletBalance"`
	AvailableBalance   string `json:"availableBalance"`
}

// Error defines the error information structure returned by Binance API
type Error struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// Error method makes BinanceError implement the error interface
func (e *Error) Error() string {
	return fmt.Sprintf("API Error: code=%d, msg=%s", e.Code, e.Msg)
}

// GridState defines the state of the robot that needs to be persisted
type GridState struct {
	GridLevels     []GridLevel `json:"grid_levels"`
	EntryPrice     float64     `json:"entry_price"`
	ReversionPrice float64     `json:"reversion_price"`
	ConceptualGrid []float64   `json:"conceptual_grid"`
}

// OrderUpdateEvent is the complete structure of the order update event received from the user data stream
type OrderUpdateEvent struct {
	EventType       string          `json:"e"` // Event type, e.g., "ORDER_TRADE_UPDATE"
	EventTime       int64           `json:"E"` // Event time
	TransactionTime int64           `json:"T"` // Transaction time
	Order           OrderUpdateInfo `json:"o"` // Order information
}

// OrderUpdateInfo contains the specific information of the order update
type OrderUpdateInfo struct {
	Symbol          string `json:"s"`  // Symbol
	ClientOrderID   string `json:"c"`  // Client Order ID
	Side            string `json:"S"`  // Side
	OrderType       string `json:"o"`  // Order Type
	TimeInForce     string `json:"f"`  // Time in Force
	OrigQty         string `json:"q"`  // Original Quantity
	Price           string `json:"p"`  // Price
	AvgPrice        string `json:"ap"` // Average Price
	StopPrice       string `json:"sp"` // Stop Price
	ExecutionType   string `json:"x"`  // Execution Type
	Status          string `json:"X"`  // Order Status
	OrderID         int64  `json:"i"`  // Order ID
	ExecutedQty     string `json:"l"`  // Last Executed Quantity
	CumQty          string `json:"z"`  // Cumulative Filled Quantity
	ExecutedPrice   string `json:"L"`  // Last Executed Price
	CommissionAmt   string `json:"n"`  // Commission Amount
	CommissionAsset string `json:"N"`  // Commission Asset, will be null if not traded
	TradeTime       int64  `json:"T"`  // Trade Time
	TradeID         int64  `json:"t"`  // Trade ID
	BidsNotional    string `json:"b"`  // Bids Notional
	AsksNotional    string `json:"a"`  // Asks Notional
	IsMaker         bool   `json:"m"`  // Is the trade a maker trade?
	IsReduceOnly    bool   `json:"R"`  // Is this a reduce only order?
	WorkingType     string `json:"wt"` // Stop Price Working Type
	OrigType        string `json:"ot"` // Original Order Type
	PositionSide    string `json:"ps"` // Position Side
	ClosePosition   bool   `json:"cp"` // If conditional order, is it close position?
	ActivationPrice string `json:"AP"` // Activation Price, only available for TRAILING_STOP_MARKET order
	CallbackRate    string `json:"cr"` // Callback Rate, only available for TRAILING_STOP_MARKET order
	RealizedProfit  string `json:"rp"` // Realized Profit of the trade
}

// AccountUpdateEvent represents the complete structure of the ACCOUNT_UPDATE WebSocket event.
type AccountUpdateEvent struct {
	EventType       string            `json:"e"` // Event type
	EventTime       int64             `json:"E"` // Event time
	TransactionTime int64             `json:"T"` // Transaction time
	UpdateData      AccountUpdateData `json:"a"` // Account update data
}

// AccountUpdateData contains the balance and position information in the account update
type AccountUpdateData struct {
	Reason    string           `json:"m"` // Reason for the event, e.g., "ORDER"
	Balances  []BalanceUpdate  `json:"B"` // Balance update list
	Positions []PositionUpdate `json:"P"` // Position update list
}

// BalanceUpdate represents the balance update of a single asset
type BalanceUpdate struct {
	Asset              string `json:"a"`  // Asset name
	WalletBalance      string `json:"wb"` // Wallet balance
	CrossWalletBalance string `json:"cw"` // Cross wallet balance
	BalanceChange      string `json:"bc"` // Balance change (excluding profit and fees)
}

// PositionUpdate represents the update of a single position
type PositionUpdate struct {
	Symbol              string `json:"s"`   // Symbol
	PositionAmount      string `json:"pa"`  // Position amount
	EntryPrice          string `json:"ep"`  // Entry price
	AccumulatedRealized string `json:"cr"`  // Accumulated realized profit and loss
	UnrealizedPnl       string `json:"up"`  // Unrealized profit and loss
	MarginType          string `json:"mt"`  // Margin type (cross/isolated)
	IsolatedWallet      string `json:"iw"`  // Isolated wallet balance
	PositionSide        string `json:"ps"`  // Position side (BOTH, LONG, SHORT)
	BreakEvenPrice      string `json:"bep"` // Break-even price
}

// SpotBalance represents a spot trading balance
type SpotBalance struct {
	Asset  string `json:"asset"`  // Asset symbol (BTC, USDT, etc.)
	Free   string `json:"free"`   // Available balance
	Locked string `json:"locked"` // Locked in orders
}

// TradingFees represents the fee structure for trading
type TradingFees struct {
	MakerFee    float64 `json:"maker_fee"`    // Maker fee rate
	TakerFee    float64 `json:"taker_fee"`    // Taker fee rate
	Symbol      string  `json:"symbol"`       // Trading symbol
	TradingMode string  `json:"trading_mode"` // "futures" or "spot"
}
