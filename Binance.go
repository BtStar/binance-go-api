package binance

import (
	"encoding/json"
	"errors"
	"fmt"
	. "github.com/BtStar/binance-api/Utils"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	EXCHANGE_NAME = "binance.com"

	API_BASE_URL = "https://www.binance.com/"
	API_V1       = API_BASE_URL + "api/v1/"
	API_V3       = API_BASE_URL + "api/v3/"

	TICKER_URI             = "ticker/24hr?symbol=%s"
	DEPTH_URI              = "depth?symbol=%s&limit=%d"
	ACCOUNT_URI            = "account"
	PLACE_ORDER_API        = "order"
	UNFINISHED_ORDERS_INFO = "openOrders"
)

type Binance struct {
	accessKey,
	secretKey string
	httpClient *http.Client
}

func (ba *Binance) buildPostForm(postForm *url.Values) error {
	postForm.Set("recvWindow", "6000000")
	tonce := strconv.FormatInt(time.Now().UnixNano(), 10)[0:13]
	postForm.Set("timestamp", tonce)
	payload := postForm.Encode()
	sign, _ := GetParamHmacSHA256Sign(ba.secretKey, payload)
	postForm.Set("signature", sign)
	return nil
}

func New(client *http.Client, api_key, secret_key string) *Binance {
	return &Binance{api_key, secret_key, client}
}

func (ba *Binance) GetExchangeName() string {
	return EXCHANGE_NAME
}

func (ba *Binance) GetTicker(currency CurrencyPair) (*Ticker, error) {
	tickerUri := API_V1 + fmt.Sprintf(TICKER_URI, currency.ToSymbol(""))
	bodyDataMap, err := HttpGet(ba.httpClient, tickerUri)

	if err != nil {
		log.Println("GetTicker error:", err)
		return nil, err
	}
	var tickerMap map[string]interface{} = bodyDataMap
	var ticker Ticker

	ticker.Date = uint64(tickerMap["closeTime"].(float64))
	ticker.Last, _ = strconv.ParseFloat(tickerMap["lastPrice"].(string), 10)
	ticker.Buy, _ = strconv.ParseFloat(tickerMap["bidPrice"].(string), 10)
	ticker.Sell, _ = strconv.ParseFloat(tickerMap["askPrice"].(string), 10)
	ticker.Low, _ = strconv.ParseFloat(tickerMap["lowPrice"].(string), 10)
	ticker.High, _ = strconv.ParseFloat(tickerMap["highPrice"].(string), 10)
	ticker.Vol, _ = strconv.ParseFloat(tickerMap["volume"].(string), 10)
	return &ticker, nil
}

func (ba *Binance) GetDepth(size int, currencyPair CurrencyPair) (*Depth, error) {
	if size > 100 {
		size = 100
	} else if size < 0 {
		size = 0
	}

	apiUrl := fmt.Sprintf(API_V1+DEPTH_URI, currencyPair.ToSymbol(""), size)
	resp, err := HttpGet(ba.httpClient, apiUrl)
	if err != nil {
		log.Println("GetDepth error:", err)
		return nil, err
	}

	if _, isok := resp["code"]; isok {
		return nil, errors.New(resp["msg"].(string))
	}

	bids := resp["bids"].([]interface{})
	asks := resp["asks"].([]interface{})

	//log.Println(bids)
	//log.Println(asks)

	depth := new(Depth)

	for _, bid := range bids {
		_bid := bid.([]interface{})
		amount := ToFloat64(_bid[1])
		price := ToFloat64(_bid[0])
		dr := DepthRecord{Amount: amount, Price: price}
		depth.BidList = append(depth.BidList, dr)
	}

	for _, ask := range asks {
		_ask := ask.([]interface{})
		amount := ToFloat64(_ask[1])
		price := ToFloat64(_ask[0])
		dr := DepthRecord{Amount: amount, Price: price}
		depth.AskList = append(depth.AskList, dr)
	}

	return depth, nil
}

func (ba *Binance) placeOrder(amount, price string, pair CurrencyPair, orderType, orderSide string) (*Order, error) {
	path := API_V3 + PLACE_ORDER_API
	params := url.Values{}
	params.Set("symbol", pair.ToSymbol(""))
	params.Set("side", orderSide)
	params.Set("type", orderType)

	params.Set("quantity", amount)
	params.Set("type", "LIMIT")
	params.Set("timeInForce", "GTC")

	switch orderType {
	case "LIMIT":
		params.Set("price", price)
	}

	ba.buildPostForm(&params)

	resp, err := HttpPostForm2(ba.httpClient, path, params,
		map[string]string{"X-MBX-APIKEY": ba.accessKey})
	//log.Println("resp:", string(resp), "err:", err)
	if err != nil {
		return nil, err
	}

	respmap := make(map[string]interface{})
	err = json.Unmarshal(resp, &respmap)
	if err != nil {
		log.Println(string(resp))
		return nil, err
	}

	orderId, isok := respmap["orderId"].(string)
	if !isok {
		return nil, errors.New(string(resp))
	}

	side := BUY
	if orderSide == "SELL" {
		side = SELL
	}
	return &Order{
		Currency:   pair,
		OrderID:    ToInt(orderId),
		Price:      ToFloat64(price),
		Amount:     ToFloat64(amount),
		DealAmount: 0,
		AvgPrice:   0,
		Side:       TradeSide(side),
		Status:     ORDER_UNFINISH,
		OrderTime:  int(time.Now().Unix())}, nil
}

func (ba *Binance) GetAccount() (*Account, error) {
	path := API_V3 + ACCOUNT_URI

	params := url.Values{}
	ba.buildPostForm(&params)
	respmap, err := HttpGet2(ba.httpClient, path, params, map[string]string{"X-MBX-APIKEY": ba.accessKey})
	if err != nil {
		log.Println(err)
		return nil, err
	}
	//log.Println(string(resp))

	acc := Account{}
	acc.Exchange = ba.GetExchangeName()
	acc.SubAccounts = make(map[Currency]SubAccount)

	balances := respmap["balances"].([]interface{})
	for _, v := range balances {
		log.Println(v)
		vv := v.(map[string]interface{})
		currency := NewCurrency(vv["asset"].(string), "")
		acc.SubAccounts[currency] = SubAccount{
			Currency:     currency,
			Amount:       ToFloat64(vv["free"]),
			ForzenAmount: ToFloat64(vv["locked"]),
		}
	}

	return &acc, nil
}

func (ba *Binance) LimitBuy(amount, price string, currencyPair CurrencyPair) (*Order, error) {
	return ba.placeOrder(amount, price, currencyPair, "LIMIT", "BUY")
}

func (ba *Binance) LimitSell(amount, price string, currencyPair CurrencyPair) (*Order, error) {
	return ba.placeOrder(amount, price, currencyPair, "LIMIT", "SELL")
}

func (ba *Binance) MarketBuy(amount, price string, currencyPair CurrencyPair) (*Order, error) {
	return ba.placeOrder(amount, price, currencyPair, "MARKET", "BUY")
}

func (ba *Binance) MarketSell(amount, price string, currencyPair CurrencyPair) (*Order, error) {
	return ba.placeOrder(amount, price, currencyPair, "MARKET", "SELL")
}

func (ba *Binance) CancelOrder(orderId string, currencyPair CurrencyPair) (bool, error) {
	path := API_V3 + PLACE_ORDER_API
	params := url.Values{}
	params.Set("symbol", currencyPair.ToSymbol(""))

	params.Set("orderId", orderId)

	ba.buildPostForm(&params)

	resp, err := HttpDeleteForm(ba.httpClient, path, params, map[string]string{"X-MBX-APIKEY": ba.accessKey})

	//log.Println("resp:", string(resp), "err:", err)
	if err != nil {
		return false, err
	}

	respmap := make(map[string]interface{})
	err = json.Unmarshal(resp, &respmap)
	if err != nil {
		log.Println(string(resp))
		return false, err
	}

	orderIdCanceled, isok := respmap["orderId"].(string)
	if !isok {
		return false, errors.New(string(resp))
	}
	if orderIdCanceled != orderId {
		return false, errors.New("orderId doesn't match")
	}

	return true, nil
}

func (ba *Binance) GetOneOrder(orderId string, currencyPair CurrencyPair) (*Order, error) {
	path := API_V3 + PLACE_ORDER_API
	params := url.Values{}
	params.Set("symbol", currencyPair.ToSymbol(""))
	params.Set("orderId", orderId)

	ba.buildPostForm(&params)

	respmap, err := HttpGet2(ba.httpClient, path, params,
		map[string]string{"X-MBX-APIKEY": ba.accessKey})

	if err != nil {
		return nil, err
	}
	status := respmap["status"].(string)

	ord := Order{}
	ord.Currency = currencyPair
	ord.OrderID = ToInt(orderId)

	if status == "FILLED" {
		ord.Status = ORDER_FINISH
	} else {
		ord.Status = ORDER_UNFINISH
	}
	ord.Amount = ToFloat64(respmap["origQty"].(string))
	ord.Price = ToFloat64(respmap["price"].(string))

	return &ord, nil
}

func (ba *Binance) GetUnfinishOrders(currencyPair CurrencyPair) ([]Order, error) {
	path := API_V3 + UNFINISHED_ORDERS_INFO
	params := url.Values{}
	params.Set("symbol", currencyPair.ToSymbol(""))

	ba.buildPostForm(&params)

	respmap, err := HttpGet2(ba.httpClient, path, params,
		map[string]string{"X-MBX-APIKEY": ba.accessKey})

	if err != nil {
		return nil, err
	}

	orders := make([]Order, 0)
	for _, v := range respmap {
		ord := v.(map[string]interface{})
		side := ord["type"].(string)
		orderSide := SELL
		if side == "BUY" {
			orderSide = BUY
		}

		orders = append(orders, Order{
			OrderID:   ToInt(ord["orderId"]),
			Currency:  currencyPair,
			Price:     ToFloat64(ord["price"]),
			Amount:    ToFloat64(ord["origQty"]),
			Side:      TradeSide(orderSide),
			Status:    ORDER_UNFINISH,
			OrderTime: ToInt(ord["time"])})
	}
	return orders, nil
}
