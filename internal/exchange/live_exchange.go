package exchange

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yudaprama/grid-trading-bot/internal/models"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// LiveExchange implements the Exchange interface for interacting with the real Binance exchange.
type LiveExchange struct {
	apiKey     string
	secretKey  string
	baseURL    string
	wsBaseURL  string
	httpClient *http.Client
	logger     *zap.Logger
	wsConn     *websocket.Conn
	listenKey  string
	timeOffset int64
}

// NewLiveExchange creates a new LiveExchange instance and synchronizes time with server.
func NewLiveExchange(apiKey, secretKey, baseURL, wsBaseURL string, logger *zap.Logger) (*LiveExchange, error) {
	e := &LiveExchange{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		wsBaseURL:  wsBaseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		logger:     logger,
	}

	if err := e.syncTime(); err != nil {
		return nil, fmt.Errorf("failed to synchronize time with Binance server: %v", err)
	}

	return e, nil
}

// syncTime synchronizes time with Binance server and calculates time offset.
func (e *LiveExchange) syncTime() error {
	serverTime, err := e.GetServerTime()
	if err != nil {
		return err
	}
	localTime := time.Now().UnixMilli()
	e.timeOffset = serverTime - localTime
	e.logger.Info("Time synchronization with Binance server completed", zap.Int64("timeOffset (ms)", e.timeOffset))
	return nil
}

// doRequest is a generic request handler function for sending requests to Binance API.
func (e *LiveExchange) doRequest(method, endpoint string, params url.Values, signed bool) ([]byte, error) {
	// 1. Prepare base URL and parameters
	fullURL := fmt.Sprintf("%s%s", e.baseURL, endpoint)
	queryParams := url.Values{}
	for k, v := range params {
		queryParams[k] = v
	}

	var encodedParams string
	if signed {
		// 2. For signed requests, add timestamp and generate signature
		timestamp := time.Now().UnixMilli() + e.timeOffset
		queryParams.Set("timestamp", fmt.Sprintf("%d", timestamp))

		payloadToSign := queryParams.Encode()
		signature := e.sign(payloadToSign)
		// Append signature to encoded parameter string
		encodedParams = fmt.Sprintf("%s&signature=%s", payloadToSign, signature)
	} else {
		// For non-signed requests, encode directly
		encodedParams = queryParams.Encode()
	}

	// 3. Create request based on request method
	var req *http.Request
	var err error

	if method == "GET" {
		finalURL := fullURL
		if encodedParams != "" {
			finalURL = fmt.Sprintf("%s?%s", fullURL, encodedParams)
		}
		req, err = http.NewRequest(method, finalURL, nil)
	} else { // POST, PUT, DELETE
		req, err = http.NewRequest(method, fullURL, strings.NewReader(encodedParams))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// 4. Add API Key and execute request
	req.Header.Set("X-MBX-APIKEY", e.apiKey)
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// 5. Read and process response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	var binanceError models.Error
	// Try to parse response as Binance error structure
	if json.Unmarshal(body, &binanceError) == nil && binanceError.Code != 0 {
		// Special handling: Binance sometimes uses a "error" message body with code: 200 to indicate a successful operation,
		// for example, when there are no pending orders to cancel. We should not treat this as a real error.
		if binanceError.Code == 200 {
			// This is a successful response, continue execution as if no error occurred
		} else {
			// This is a real business logic error returned by Binance
			return body, &binanceError
		}
	}

	if resp.StatusCode != http.StatusOK {
		// When API returns non-200 status code, we return both response body and error
		// so that upper-level callers can log detailed error information.
		return body, fmt.Errorf("API request failed, status code: %d, response: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// sign signs the request parameters.
func (e *LiveExchange) sign(data string) string {
	h := hmac.New(sha256.New, []byte(e.secretKey))
	h.Write([]byte(data))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// --- Exchange interface implementation ---

// GetPrice gets the current price for the specified trading pair.
func (e *LiveExchange) GetPrice(symbol string) (float64, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	data, err := e.doRequest("GET", "/fapi/v1/ticker/price", params, false)
	if err != nil {
		return 0, err
	}

	var ticker struct {
		Price string `json:"price"`
	}
	if err := json.Unmarshal(data, &ticker); err != nil {
		return 0, err
	}

	return strconv.ParseFloat(ticker.Price, 64)
}

// GetPositions gets position information for the specified trading pair.
func (e *LiveExchange) GetPositions(symbol string) ([]models.Position, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	data, err := e.doRequest("GET", "/fapi/v2/positionRisk", params, true)
	if err != nil {
		return nil, err
	}

	var positions []models.Position
	if err := json.Unmarshal(data, &positions); err != nil {
		return nil, err
	}

	// Filter out entries with no position
	var activePositions []models.Position
	for _, p := range positions {
		posAmt, _ := strconv.ParseFloat(p.PositionAmt, 64)
		if posAmt != 0 {
			activePositions = append(activePositions, p)
		}
	}

	return activePositions, nil
}

// PlaceOrder places an order.
func (e *LiveExchange) PlaceOrder(symbol, side, orderType string, quantity, price float64, clientOrderID string) (*models.Order, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", side)
	params.Set("type", orderType)
	params.Set("quantity", fmt.Sprintf("%f", quantity))

	if orderType == "LIMIT" {
		params.Set("timeInForce", "GTC") // Good Till Cancel
		params.Set("price", fmt.Sprintf("%f", price))
	}
	if clientOrderID != "" {
		params.Set("newClientOrderId", clientOrderID)
	}

	data, err := e.doRequest("POST", "/fapi/v1/order", params, true)
	if err != nil {
		// When doRequest returns error, first return value is response body, second is error
		e.logger.Error("Order placement request failed, exchange returned error", zap.Error(err), zap.String("raw_response", string(data)))
		return nil, err
	}

	var order models.Order
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, err
	}

	return &order, nil
}

// CancelOrder cancels an order.
func (e *LiveExchange) CancelOrder(symbol string, orderID int64) error {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", strconv.FormatInt(orderID, 10))
	_, err := e.doRequest("DELETE", "/fapi/v1/order", params, true)
	return err
}

// SetLeverage sets leverage.
func (e *LiveExchange) SetLeverage(symbol string, leverage int) error {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("leverage", strconv.Itoa(leverage))
	_, err := e.doRequest("POST", "/fapi/v1/leverage", params, true)
	return err
}

// SetPositionMode sets position mode.
func (e *LiveExchange) SetPositionMode(isHedgeMode bool) error {
	params := url.Values{}
	params.Set("dualSidePosition", fmt.Sprintf("%v", isHedgeMode))
	_, err := e.doRequest("POST", "/fapi/v1/positionSide/dual", params, true)

	// If error is Binance-specific error with code -4059 (no need to change), ignore the error
	if err != nil {
		if binanceErr, ok := err.(*models.Error); ok && binanceErr.Code == -4059 {
			e.logger.Info("Position mode does not need to be changed, already in target mode.")
			return nil
		}
		return err
	}
	return nil
}

// GetPositionMode gets current position mode.
func (e *LiveExchange) GetPositionMode() (bool, error) {
	data, err := e.doRequest("GET", "/fapi/v1/positionSide/dual", nil, true)
	if err != nil {
		return false, fmt.Errorf("failed to get position mode: %v", err)
	}

	var result struct {
		DualSidePosition bool `json:"dualSidePosition"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return false, fmt.Errorf("failed to parse position mode response: %v", err)
	}

	return result.DualSidePosition, nil
}

// SetMarginType sets margin type.
func (e *LiveExchange) SetMarginType(symbol string, marginType string) error {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("marginType", marginType) // "ISOLATED" or "CROSSED"
	_, err := e.doRequest("POST", "/fapi/v1/marginType", params, true)

	// If error is Binance-specific error with code -4046 (No need to change margin type), ignore the error
	if err != nil {
		if binanceErr, ok := err.(*models.Error); ok && binanceErr.Code == -4046 {
			e.logger.Info("Margin type does not need to be changed, already in target mode.")
			return nil // Ignore this error as it's already in target state
		}
		return err // Return all other unhandled errors
	}

	return nil // No error, success
}

// GetMarginType gets margin type for the specified trading pair.
func (e *LiveExchange) GetMarginType(symbol string) (string, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	data, err := e.doRequest("GET", "/fapi/v2/positionRisk", params, true)
	if err != nil {
		return "", fmt.Errorf("failed to get position risk information to determine margin type: %v", err)
	}

	var positions []models.Position
	if err := json.Unmarshal(data, &positions); err != nil {
		return "", fmt.Errorf("failed to parse position risk response: %v", err)
	}

	if len(positions) == 0 {
		return "", fmt.Errorf("API did not return position risk information for trading pair %s", symbol)
	}

	// Margin type is per trading pair, so we can take the first result.
	// API returns lowercase (e.g., "cross", "isolated"), config uses uppercase, so conversion is needed.
	return strings.ToUpper(positions[0].MarginType), nil
}

// GetAccountInfo gets account information.
func (e *LiveExchange) GetAccountInfo() (*models.AccountInfo, error) {
	data, err := e.doRequest("GET", "/fapi/v2/account", nil, true)
	if err != nil {
		return nil, fmt.Errorf("failed to get account information: %v", err)
	}

	var accInfo models.AccountInfo
	if err := json.Unmarshal(data, &accInfo); err != nil {
		return nil, fmt.Errorf("failed to parse account information: %v", err)
	}
	return &accInfo, nil
}

// CancelAllOpenOrders cancels all open orders.
func (e *LiveExchange) CancelAllOpenOrders(symbol string) error {
	params := url.Values{}
	params.Set("symbol", symbol)
	body, err := e.doRequest("DELETE", "/fapi/v1/allOpenOrders", params, true)

	// Since doRequest has already handled the code:200 case, the logic here can be greatly simplified.
	// If err is not nil, then it's a real error that needs to be handled.
	if err != nil {
		e.logger.Error("Failed to cancel all open orders", zap.Error(err), zap.String("response", string(body)))
		return err
	}

	e.logger.Info("Successfully cancelled all open orders (or no orders to cancel).", zap.String("symbol", symbol))
	return nil
}

// GetOrderStatus gets order status.
func (e *LiveExchange) GetOrderStatus(symbol string, orderID int64) (*models.Order, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", strconv.FormatInt(orderID, 10))
	data, err := e.doRequest("GET", "/fapi/v1/order", params, true)
	if err != nil {
		return nil, err
	}

	var order models.Order
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

// GetCurrentTime returns current time. In live trading, we directly return system time.
func (e *LiveExchange) GetCurrentTime() time.Time {
	return time.Now()
}

// GetAccountState gets account state, including total position value and total account equity
func (e *LiveExchange) GetAccountState(symbol string) (positionValue float64, accountEquity float64, err error) {
	accInfo, err := e.GetAccountInfo()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get account state: %v", err)
	}

	equity, err := strconv.ParseFloat(accInfo.TotalWalletBalance, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse total account equity: %v", err)
	}

	positions, err := e.GetPositions(symbol)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get position information: %v", err)
	}

	var totalPositionValue float64
	for _, pos := range positions {
		notional, _ := strconv.ParseFloat(pos.Notional, 64)
		totalPositionValue += notional
	}

	return totalPositionValue, equity, nil
}

// GetSymbolInfo gets trading rules for the trading pair
func (e *LiveExchange) GetSymbolInfo(symbol string) (*models.SymbolInfo, error) {
	// [Key Fix] When getting exchange info, no parameters should be passed to get complete list of all trading pairs
	data, err := e.doRequest("GET", "/fapi/v1/exchangeInfo", nil, false)
	if err != nil {
		return nil, err
	}

	var exchangeInfo models.ExchangeInfo
	if err := json.Unmarshal(data, &exchangeInfo); err != nil {
		return nil, err
	}

	for _, s := range exchangeInfo.Symbols {
		if s.Symbol == symbol {
			return &s, nil
		}
	}

	return nil, fmt.Errorf("trading pair %s information not found", symbol)
}

// GetOpenOrders gets all open orders
func (e *LiveExchange) GetOpenOrders(symbol string) ([]models.Order, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	data, err := e.doRequest("GET", "/fapi/v1/openOrders", params, true)
	if err != nil {
		return nil, err
	}

	var openOrders []models.Order
	if err := json.Unmarshal(data, &openOrders); err != nil {
		return nil, err
	}
	return openOrders, nil
}

// GetServerTime gets server time
func (e *LiveExchange) GetServerTime() (int64, error) {
	data, err := e.doRequest("GET", "/fapi/v1/time", nil, false)
	if err != nil {
		return 0, err
	}
	var serverTime struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.Unmarshal(data, &serverTime); err != nil {
		return 0, err
	}
	return serverTime.ServerTime, nil
}

// GetLastTrade gets latest trade
func (e *LiveExchange) GetLastTrade(symbol string, orderID int64) (*models.Trade, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("limit", "1") // We only need the latest trade
	data, err := e.doRequest("GET", "/fapi/v1/userTrades", params, true)
	if err != nil {
		return nil, err
	}

	var trades []models.Trade
	if err := json.Unmarshal(data, &trades); err != nil {
		return nil, err
	}

	if len(trades) > 0 {
		return &trades[0], nil
	}

	return nil, fmt.Errorf("trade record for order %d not found", orderID)
}

// GetMaxWalletExposure not applicable in live trading, returns 0
func (e *LiveExchange) GetMaxWalletExposure() float64 {
	return 0
}

// CreateListenKey creates a new listenKey for WebSocket connection.
func (e *LiveExchange) CreateListenKey() (string, error) {
	data, err := e.doRequest("POST", "/fapi/v1/listenKey", nil, true)
	if err != nil {
		return "", fmt.Errorf("failed to create listenKey: %v", err)
	}

	var response struct {
		ListenKey string `json:"listenKey"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return "", fmt.Errorf("failed to parse listenKey response: %v", err)
	}
	e.listenKey = response.ListenKey
	return e.listenKey, nil
}

// KeepAliveListenKey extends the validity period of listenKey.
func (e *LiveExchange) KeepAliveListenKey(listenKey string) error {
	params := url.Values{}
	params.Set("listenKey", listenKey)
	_, err := e.doRequest("PUT", "/fapi/v1/listenKey", params, true)
	if err != nil {
		return fmt.Errorf("failed to keep listenKey alive: %v", err)
	}
	return nil
}

// GetBalance gets balance of specific asset in account
func (e *LiveExchange) GetBalance() (float64, error) {
	data, err := e.doRequest("GET", "/fapi/v2/balance", nil, true)
	if err != nil {
		return 0, fmt.Errorf("failed to get account balance: %v", err)
	}

	var balances []models.Balance
	if err := json.Unmarshal(data, &balances); err != nil {
		return 0, fmt.Errorf("failed to parse balance data: %v", err)
	}

	// Usually we care about USDT balance as margin and quote currency
	for _, b := range balances {
		if b.Asset == "USDT" {
			return strconv.ParseFloat(b.AvailableBalance, 64)
		}
	}

	return 0, fmt.Errorf("USDT balance not found")
}

// ConnectWebSocket establishes WebSocket connection to Binance user data stream
func (e *LiveExchange) ConnectWebSocket(listenKey string) (*websocket.Conn, error) {
	// Correct WebSocket URL format is wss://<wsBaseURL>/ws/<listenKey>
	wsURL := fmt.Sprintf("%s/ws/%s", e.wsBaseURL, listenKey)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to WebSocket: %v", err)
	}
	e.wsConn = conn
	return conn, nil
}
