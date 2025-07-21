# 📚 Usage Examples - Unified Trading Mode Implementation

## 🚀 **Quick Start Examples**

### **Example 1: Futures Trading (High Leverage)**

```bash
# 1. Set up environment
export BINANCE_API_KEY="your-futures-api-key"
export BINANCE_SECRET_KEY="your-futures-secret-key"

# 2. Create futures configuration
cat > config_futures.json << EOF
{
  "trading_mode": "futures",
  "is_testnet": true,
  "symbol": "BTCUSDT",
  "leverage": 10,
  "margin_type": "CROSSED",
  "hedge_mode": false,
  "initial_investment": 1000.0,
  "grid_spacing": 0.01,
  "grid_quantity": 0.001,
  "active_orders_count": 5,
  "wallet_exposure_limit": 0.8,
  "enable_trailing_up": true,
  "enable_trailing_down": true,
  "trailing_up_distance": 0.02,
  "trailing_down_distance": 0.015,
  "trailing_type": "percentage"
}
EOF

# 3. Run the bot
./grid-bot --mode live --config config_futures.json
```

### **Example 2: Spot Trading (No Leverage)**

```bash
# 1. Set up environment (same API keys work for both)
export BINANCE_API_KEY="your-api-key"
export BINANCE_SECRET_KEY="your-secret-key"

# 2. Create spot configuration
cat > config_spot.json << EOF
{
  "trading_mode": "spot",
  "is_testnet": true,
  "symbol": "BTCUSDT",
  "base_asset": "BTC",
  "quote_asset": "USDT",
  "initial_base_amount": 0.1,
  "initial_quote_amount": 1000.0,
  "grid_spacing": 0.01,
  "active_orders_count": 5,
  "wallet_exposure_limit": 0.5,
  "enable_trailing_up": true,
  "enable_trailing_down": true,
  "trailing_up_distance": 0.025,
  "trailing_down_distance": 0.02,
  "trailing_type": "percentage",
  "spot_api_url": "https://api.binance.com",
  "spot_ws_url": "wss://stream.binance.com",
  "spot_testnet_api_url": "https://testnet.binance.vision",
  "spot_testnet_ws_url": "wss://testnet.binance.vision"
}
EOF

# 3. Run the bot
./grid-bot --mode live --config config_spot.json
```

## 📊 **Configuration Comparison**

| Parameter | Futures Mode | Spot Mode | Description |
|-----------|-------------|-----------|-------------|
| `trading_mode` | `"futures"` | `"spot"` | **Required**: Trading mode selection |
| `leverage` | `1-125` | N/A | Leverage multiplier (futures only) |
| `margin_type` | `"CROSSED"/"ISOLATED"` | N/A | Margin mode (futures only) |
| `hedge_mode` | `true/false` | N/A | Position mode (futures only) |
| `base_asset` | N/A | `"BTC"` | Base asset symbol (spot only) |
| `quote_asset` | N/A | `"USDT"` | Quote asset symbol (spot only) |
| `initial_base_amount` | N/A | `0.1` | Starting base asset amount (spot only) |
| `initial_quote_amount` | N/A | `1000.0` | Starting quote asset amount (spot only) |
| `initial_investment` | `1000.0` | N/A | USDT investment amount (futures only) |
| `wallet_exposure_limit` | `0.8` (80%) | `0.5` (50%) | Risk exposure limit |

## 🔧 **Advanced Configuration Examples**

### **Conservative Futures Trading**
```json
{
  "trading_mode": "futures",
  "symbol": "ETHUSDT",
  "leverage": 3,
  "margin_type": "ISOLATED",
  "initial_investment": 500.0,
  "grid_spacing": 0.005,
  "grid_quantity": 0.01,
  "wallet_exposure_limit": 0.3,
  "enable_trailing_up": true,
  "trailing_up_distance": 0.015,
  "trailing_type": "percentage"
}
```

### **Aggressive Spot Trading**
```json
{
  "trading_mode": "spot",
  "symbol": "ADAUSDT",
  "base_asset": "ADA",
  "quote_asset": "USDT",
  "initial_base_amount": 1000.0,
  "initial_quote_amount": 500.0,
  "grid_spacing": 0.02,
  "active_orders_count": 10,
  "wallet_exposure_limit": 0.8
}
```

## 🎯 **Trading Strategy Examples**

### **Strategy 1: Range-Bound Market (Spot)**
- **Best for**: Sideways markets with clear support/resistance
- **Configuration**: Tight grid spacing (0.005-0.01), high order count
- **Risk**: Low (no liquidation risk)
- **Capital**: Requires both base and quote assets

### **Strategy 2: Trending Market (Futures)**
- **Best for**: Strong trending markets
- **Configuration**: Wider grid spacing (0.02-0.05), trailing stops enabled
- **Risk**: High (liquidation risk)
- **Capital**: Efficient with leverage

### **Strategy 3: Scalping (Both Modes)**
- **Best for**: High-volume, low-volatility periods
- **Configuration**: Very tight spacing (0.001-0.005), many small orders
- **Risk**: Medium
- **Capital**: Depends on mode choice

## 🚨 **Risk Management Examples**

### **Futures Risk Management**
```json
{
  "leverage": 5,
  "wallet_exposure_limit": 0.5,
  "enable_trailing_down": true,
  "trailing_down_distance": 0.03,
  "margin_type": "ISOLATED"
}
```

### **Spot Risk Management**
```json
{
  "wallet_exposure_limit": 0.3,
  "enable_trailing_up": true,
  "trailing_up_distance": 0.02,
  "initial_base_amount": 0.05,
  "initial_quote_amount": 500.0
}
```

## 📈 **Performance Monitoring**

### **Key Metrics to Track**

#### **Futures Mode:**
- Unrealized P&L
- Position size
- Margin utilization
- Liquidation distance
- ROI on margin

#### **Spot Mode:**
- Asset appreciation
- Inventory balance
- Total portfolio value
- Asset allocation ratio
- ROI on total capital

## 🔄 **Mode Switching**

To switch between trading modes:

1. **Stop the current bot**
2. **Update configuration file**
3. **Restart with new config**

```bash
# Stop current bot (Ctrl+C)
# Edit config file
vim config.json

# Change trading_mode and related parameters
# Restart bot
./grid-bot --mode live --config config.json
```

## 🧪 **Testing Recommendations**

### **Before Live Trading:**

1. **Test on Testnet**
   ```bash
   # Set testnet in config
   "is_testnet": true
   ```

2. **Start with Small Amounts**
   ```json
   // Futures
   "initial_investment": 100.0,
   "leverage": 2
   
   // Spot  
   "initial_base_amount": 0.01,
   "initial_quote_amount": 100.0
   ```

3. **Enable Conservative Settings**
   ```json
   "wallet_exposure_limit": 0.2,
   "active_orders_count": 3,
   "grid_spacing": 0.02
   ```

## 🎉 **Success Tips**

1. **Choose the Right Mode**: Futures for capital efficiency, Spot for safety
2. **Start Conservative**: Lower leverage, smaller positions, wider grids
3. **Monitor Actively**: Especially during high volatility
4. **Use Trailing Stops**: Protect profits and limit losses
5. **Diversify**: Don't put all capital in one strategy
6. **Keep Learning**: Markets evolve, strategies should too

## 📞 **Support**

- **Documentation**: Check README.md for detailed setup
- **Issues**: Report bugs on GitHub
- **Testing**: Always test on testnet first
- **Community**: Share experiences and strategies

**Happy Trading! 🚀💰**
