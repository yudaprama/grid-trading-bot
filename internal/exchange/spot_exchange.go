package exchange

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
	
	"github.com/yudaprama/grid-trading-bot/internal/models"
	"github.com/gorilla/websocket"
)

// SpotExchange implements the Exchange interface for Binance Spot trading
type SpotExchange struct {
	apiKey     string
	secretKey  string
	baseURL    string
	wsURL      string
	httpClient *http.Client
	isTestnet  bool
}

// NewSpotExchange creates a new spot exchange instance
func NewSpotExchange(apiKey, secretKey string, config *models.Config) *SpotExchange {
	var baseURL, wsURL string
	
	if config.IsTestnet {
		baseURL = config.SpotTestnetAPIURL
		wsURL = config.SpotTestnetWSURL
	} else {
		baseURL = config.SpotAPIURL
		wsURL = config.SpotWSURL
	}
	
	// Default URLs if not configured
	if baseURL == "" {
		if config.IsTestnet {
			baseURL = "https://testnet.binance.vision"
		} else {
			baseURL = "https://api.binance.com"
		}
	}
	
	if wsURL == "" {
		if config.IsTestnet {
			wsURL = "wss://testnet.binance.vision"
		} else {
			wsURL = "wss://stream.binance.com"
		}
	}
	
	return &SpotExchange{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		wsURL:      wsURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		isTestnet:  config.IsTestnet,
	}
}

// generateSignature generates HMAC SHA256 signature for Binance API
func (s *SpotExchange) generateSignature(queryString string) string {
	h := hmac.New(sha256.New, []byte(s.secretKey))
	h.Write([]byte(queryString))
	return hex.EncodeToString(h.Sum(nil))
}

// makeRequest makes an HTTP request to the Binance Spot API
func (s *SpotExchange) makeRequest(method, endpoint string, params map[string]string, signed bool) ([]byte, error) {
	u, err := url.Parse(s.baseURL + endpoint)
	if err != nil {
		return nil, err
	}
	
	query := u.Query()
	for key, value := range params {
		query.Set(key, value)
	}
	
	if signed {
		query.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
		queryString := query.Encode()
		signature := s.generateSignature(queryString)
		query.Set("signature", signature)
	}
	
	u.RawQuery = query.Encode()
	
	var req *http.Request
	if method == "POST" {
		req, err = http.NewRequest(method, u.String(), bytes.NewBuffer([]byte{}))
	} else {
		req, err = http.NewRequest(method, u.String(), nil)
	}
	
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("X-MBX-APIKEY", s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	
	if resp.StatusCode != http.StatusOK {
		var apiError models.Error
		if err := json.Unmarshal(body, &apiError); err == nil {
			return nil, &apiError
		}
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}
	
	return body, nil
}

// GetPrice gets the current price for a symbol
func (s *SpotExchange) GetPrice(symbol string) (float64, error) {
	params := map[string]string{
		"symbol": symbol,
	}
	
	body, err := s.makeRequest("GET", "/api/v3/ticker/price", params, false)
	if err != nil {
		return 0, err
	}
	
	var priceResp struct {
		Symbol string `json:"symbol"`
		Price  string `json:"price"`
	}
	
	if err := json.Unmarshal(body, &priceResp); err != nil {
		return 0, err
	}
	
	return strconv.ParseFloat(priceResp.Price, 64)
}

// PlaceOrder places a new order
func (s *SpotExchange) PlaceOrder(symbol, side, orderType string, quantity, price float64, clientOrderID string) (*models.Order, error) {
	params := map[string]string{
		"symbol":           symbol,
		"side":             side,
		"type":             orderType,
		"quantity":         strconv.FormatFloat(quantity, 'f', -1, 64),
		"newClientOrderId": clientOrderID,
	}
	
	if orderType == "LIMIT" {
		params["price"] = strconv.FormatFloat(price, 'f', -1, 64)
		params["timeInForce"] = "GTC"
	}
	
	body, err := s.makeRequest("POST", "/api/v3/order", params, true)
	if err != nil {
		return nil, err
	}
	
	var order models.Order
	if err := json.Unmarshal(body, &order); err != nil {
		return nil, err
	}
	
	return &order, nil
}

// CancelOrder cancels an existing order
func (s *SpotExchange) CancelOrder(symbol string, orderID int64) error {
	params := map[string]string{
		"symbol":  symbol,
		"orderId": strconv.FormatInt(orderID, 10),
	}
	
	_, err := s.makeRequest("DELETE", "/api/v3/order", params, true)
	return err
}

// GetOrderStatus gets the status of an order
func (s *SpotExchange) GetOrderStatus(symbol string, orderID int64) (*models.Order, error) {
	params := map[string]string{
		"symbol":  symbol,
		"orderId": strconv.FormatInt(orderID, 10),
	}
	
	body, err := s.makeRequest("GET", "/api/v3/order", params, true)
	if err != nil {
		return nil, err
	}
	
	var order models.Order
	if err := json.Unmarshal(body, &order); err != nil {
		return nil, err
	}
	
	return &order, nil
}

// GetOpenOrders gets all open orders for a symbol
func (s *SpotExchange) GetOpenOrders(symbol string) ([]models.Order, error) {
	params := map[string]string{
		"symbol": symbol,
	}
	
	body, err := s.makeRequest("GET", "/api/v3/openOrders", params, true)
	if err != nil {
		return nil, err
	}
	
	var orders []models.Order
	if err := json.Unmarshal(body, &orders); err != nil {
		return nil, err
	}
	
	return orders, nil
}

// CancelAllOpenOrders cancels all open orders for a symbol
func (s *SpotExchange) CancelAllOpenOrders(symbol string) error {
	params := map[string]string{
		"symbol": symbol,
	}
	
	_, err := s.makeRequest("DELETE", "/api/v3/openOrders", params, true)
	return err
}

// GetSpotBalances gets account balances for spot trading
func (s *SpotExchange) GetSpotBalances() ([]models.SpotBalance, error) {
	body, err := s.makeRequest("GET", "/api/v3/account", map[string]string{}, true)
	if err != nil {
		return nil, err
	}
	
	var accountInfo struct {
		Balances []models.SpotBalance `json:"balances"`
	}
	
	if err := json.Unmarshal(body, &accountInfo); err != nil {
		return nil, err
	}
	
	return accountInfo.Balances, nil
}

// GetSymbolInfo gets symbol information
func (s *SpotExchange) GetSymbolInfo(symbol string) (*models.SymbolInfo, error) {
	body, err := s.makeRequest("GET", "/api/v3/exchangeInfo", map[string]string{}, false)
	if err != nil {
		return nil, err
	}
	
	var exchangeInfo struct {
		Symbols []models.SymbolInfo `json:"symbols"`
	}
	
	if err := json.Unmarshal(body, &exchangeInfo); err != nil {
		return nil, err
	}
	
	for _, symbolInfo := range exchangeInfo.Symbols {
		if symbolInfo.Symbol == symbol {
			return &symbolInfo, nil
		}
	}
	
	return nil, fmt.Errorf("symbol %s not found", symbol)
}

// GetServerTime gets server time
func (s *SpotExchange) GetServerTime() (int64, error) {
	body, err := s.makeRequest("GET", "/api/v3/time", map[string]string{}, false)
	if err != nil {
		return 0, err
	}
	
	var timeResp struct {
		ServerTime int64 `json:"serverTime"`
	}
	
	if err := json.Unmarshal(body, &timeResp); err != nil {
		return 0, err
	}
	
	return timeResp.ServerTime, nil
}

// Futures-specific methods (not supported in spot mode)
func (s *SpotExchange) GetPositions(symbol string) ([]models.Position, error) {
	return nil, fmt.Errorf("GetPositions is not supported in spot trading mode")
}

func (s *SpotExchange) SetLeverage(symbol string, leverage int) error {
	return fmt.Errorf("SetLeverage is not supported in spot trading mode")
}

func (s *SpotExchange) SetPositionMode(isHedgeMode bool) error {
	return fmt.Errorf("SetPositionMode is not supported in spot trading mode")
}

func (s *SpotExchange) GetPositionMode() (bool, error) {
	return false, fmt.Errorf("GetPositionMode is not supported in spot trading mode")
}

func (s *SpotExchange) SetMarginType(symbol string, marginType string) error {
	return fmt.Errorf("SetMarginType is not supported in spot trading mode")
}

func (s *SpotExchange) GetMarginType(symbol string) (string, error) {
	return "", fmt.Errorf("GetMarginType is not supported in spot trading mode")
}

func (s *SpotExchange) GetAccountState(symbol string) (positionValue float64, accountEquity float64, err error) {
	return 0, 0, fmt.Errorf("GetAccountState is not supported in spot trading mode")
}

func (s *SpotExchange) GetMaxWalletExposure() float64 {
	return 0 // Not applicable for spot trading
}

func (s *SpotExchange) GetBalance() (float64, error) {
	return 0, fmt.Errorf("GetBalance is not supported in spot trading mode (use GetSpotBalances instead)")
}

// GetSpotBalance gets balance for a specific asset (required by Exchange interface)
func (s *SpotExchange) GetSpotBalance(asset string) (float64, error) {
	balances, err := s.GetSpotBalances()
	if err != nil {
		return 0, err
	}

	for _, balance := range balances {
		if balance.Asset == asset {
			free, err := strconv.ParseFloat(balance.Free, 64)
			if err != nil {
				return 0, fmt.Errorf("failed to parse balance: %v", err)
			}
			return free, nil
		}
	}

	return 0, nil // Asset not found, return 0 balance
}

// GetSpotTradingFees gets trading fees for spot trading (required by Exchange interface)
func (s *SpotExchange) GetSpotTradingFees(symbol string) (*models.TradingFees, error) {
	// Default spot trading fees (would be fetched from API in real implementation)
	return &models.TradingFees{
		MakerFee:    0.001, // 0.1%
		TakerFee:    0.001, // 0.1%
		Symbol:      symbol,
		TradingMode: "spot",
	}, nil
}

// GetTradingMode returns the trading mode (required by Exchange interface)
func (s *SpotExchange) GetTradingMode() string {
	return "spot"
}

// SupportsTradingMode checks if a trading mode is supported (required by Exchange interface)
func (s *SpotExchange) SupportsTradingMode(mode string) bool {
	return mode == "spot"
}

// SetTradingMode sets the trading mode (required by Exchange interface)
func (s *SpotExchange) SetTradingMode(mode string) error {
	if mode != "spot" {
		return fmt.Errorf("spot exchange only supports spot trading mode")
	}
	return nil
}

// Common methods
func (s *SpotExchange) GetAccountInfo() (*models.AccountInfo, error) {
	// Placeholder implementation
	return &models.AccountInfo{}, nil
}

func (s *SpotExchange) GetCurrentTime() time.Time {
	return time.Now()
}

func (s *SpotExchange) GetLastTrade(symbol string, orderID int64) (*models.Trade, error) {
	// Placeholder implementation
	return &models.Trade{}, nil
}

func (s *SpotExchange) CreateListenKey() (string, error) {
	body, err := s.makeRequest("POST", "/api/v3/userDataStream", map[string]string{}, false)
	if err != nil {
		return "", err
	}
	
	var resp struct {
		ListenKey string `json:"listenKey"`
	}
	
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	
	return resp.ListenKey, nil
}

func (s *SpotExchange) KeepAliveListenKey(listenKey string) error {
	params := map[string]string{
		"listenKey": listenKey,
	}
	
	_, err := s.makeRequest("PUT", "/api/v3/userDataStream", params, false)
	return err
}

func (s *SpotExchange) ConnectWebSocket(listenKey string) (*websocket.Conn, error) {
	wsURL := fmt.Sprintf("%s/ws/%s", s.wsURL, listenKey)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	return conn, err
}
