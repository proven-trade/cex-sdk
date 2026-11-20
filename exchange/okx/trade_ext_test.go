package okx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/transport"
)

type extCase struct {
	name   string
	method string
	path   string
	query  string
	body   string
	reply  string
	call   func(*Client) error
}

func TestTradeAndAccountExtensions(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	ordAck := `{"code":"0","msg":"","data":[{"ordId":"42","clOrdId":"b1","tag":"","ts":"1700000000000","sCode":"0","sMsg":""}]}`
	cases := []extCase{
		{
			name: "batch place", method: http.MethodPost, path: "/api/v5/trade/batch-orders",
			body:  `[{"instId":"BTC-USDT","tdMode":"cash","clOrdId":"b1","side":"buy","ordType":"limit","sz":"1","px":"2.15"}]`,
			reply: ordAck,
			call: func(c *Client) error {
				result, err := c.BatchPlaceOrders(context.Background(), BatchPlaceOrdersRequest{Orders: []PlaceOrderRequest{{
					InstrumentType: InstrumentTypeSpot, InstrumentID: "BTC-USDT", TradeMode: TradeModeCash,
					ClientOrderID: "b1", Side: SideBuy, OrderType: OrderTypeLimit, Quantity: "1", Price: "2.15",
				}}})
				if err == nil && (len(result.Orders) != 1 || result.Orders[0].OrderID != "42" || result.PartialSuccess) {
					t.Errorf("BatchPlaceOrders() = %+v", result)
				}
				return err
			},
		},
		{
			name: "batch cancel", method: http.MethodPost, path: "/api/v5/trade/cancel-batch-orders",
			body:  `[{"instId":"BTC-USDT","ordId":"42"}]`,
			reply: ordAck,
			call: func(c *Client) error {
				_, err := c.BatchCancelOrders(context.Background(), BatchCancelOrdersRequest{
					Orders: []CancelOrderRequest{{InstrumentID: "BTC-USDT", OrderID: "42"}},
				})
				return err
			},
		},
		{
			name: "amend order", method: http.MethodPost, path: "/api/v5/trade/amend-order",
			body:  `{"instId":"BTC-USDT","ordId":"42","reqId":"r1","newSz":"2"}`,
			reply: `{"code":"0","msg":"","data":[{"ordId":"42","clOrdId":"","ts":"1700000000000","reqId":"r1","sCode":"0","sMsg":""}]}`,
			call: func(c *Client) error {
				ref, err := c.AmendOrder(context.Background(), AmendOrderRequest{
					InstrumentID: "BTC-USDT", OrderID: "42", RequestID: "r1", NewSize: "2",
				})
				if err == nil && (ref.RequestID != "r1" || ref.OrderID != "42") {
					t.Errorf("AmendOrder() = %+v", ref)
				}
				return err
			},
		},
		{
			name: "batch amend", method: http.MethodPost, path: "/api/v5/trade/amend-batch-orders",
			body:  `[{"instId":"BTC-USDT","clOrdId":"b1","newPx":"3"}]`,
			reply: `{"code":"0","msg":"","data":[{"ordId":"42","clOrdId":"b1","reqId":"","sCode":"0","sMsg":""}]}`,
			call: func(c *Client) error {
				_, err := c.BatchAmendOrders(context.Background(), BatchAmendOrdersRequest{
					Orders: []AmendOrderRequest{{InstrumentID: "BTC-USDT", ClientOrderID: "b1", NewPrice: "3"}},
				})
				return err
			},
		},
		{
			name: "cancel all after", method: http.MethodPost, path: "/api/v5/trade/cancel-all-after",
			body:  `{"timeOut":"60","tag":"caa1"}`,
			reply: `{"code":"0","msg":"","data":[{"triggerTime":"1587971460","tag":"caa1","ts":"1587971400"}]}`,
			call: func(c *Client) error {
				result, err := c.CancelAllAfter(context.Background(), CancelAllAfterRequest{TimeOut: time.Minute, Tag: "caa1"})
				if err == nil && result.TriggerTime != "1587971460" {
					t.Errorf("CancelAllAfter() = %+v", result)
				}
				return err
			},
		},
		{
			name: "order precheck", method: http.MethodPost, path: "/api/v5/trade/order-precheck",
			body:  `{"instId":"BTC-USDT-SWAP","tdMode":"cross","side":"buy","ordType":"market","sz":"1"}`,
			reply: `{"code":"0","msg":"","data":[{"adjEq":"41.9","adjEqChg":"-226.0","imr":"0","imrChg":"57.7","liqPx":"6764.8","liqPxDiffRatio":"-0.89"}]}`,
			call: func(c *Client) error {
				result, err := c.OrderPrecheck(context.Background(), PlaceOrderRequest{
					InstrumentType: InstrumentTypeSwap, InstrumentID: "BTC-USDT-SWAP", TradeMode: TradeModeCross,
					Side: SideBuy, OrderType: OrderTypeMarket, Quantity: "1",
				})
				if err == nil && (result.AdjustedEquityChange != "-226.0" || result.InitialMarginChange != "57.7") {
					t.Errorf("OrderPrecheck() = %+v", result)
				}
				return err
			},
		},
		{
			name: "close position", method: http.MethodPost, path: "/api/v5/trade/close-position",
			body:  `{"instId":"BTC-USDT-SWAP","posSide":"long","mgnMode":"cross","autoCxl":true}`,
			reply: `{"code":"0","msg":"","data":[{"clOrdId":"","instId":"BTC-USDT-SWAP","posSide":"long","tag":""}]}`,
			call: func(c *Client) error {
				result, err := c.ClosePosition(context.Background(), ClosePositionRequest{
					InstrumentID: "BTC-USDT-SWAP", PositionSide: PositionSideLong, MarginMode: TradeModeCross, AutoCancel: true,
				})
				if err == nil && result.PositionSide != PositionSideLong {
					t.Errorf("ClosePosition() = %+v", result)
				}
				return err
			},
		},
		{
			name: "fills", method: http.MethodGet, path: "/api/v5/trade/fills", query: "instType=SPOT&limit=50&ordId=42",
			reply: `{"code":"0","msg":"","data":[{"instType":"SPOT","instId":"BTC-USDT","tradeId":"9","ordId":"42","fillPx":"64000","fillSz":"0.1","side":"buy","execType":"T","feeCcy":"BTC","fee":"-0.0001","ts":"1700000000000"}]}`,
			call: func(c *Client) error {
				page, err := c.Fills(context.Background(), FillsRequest{
					InstrumentType: InstrumentTypeSpot, OrderID: "42", Limit: 50,
				})
				if err == nil && (len(page.Fills) != 1 || page.Fills[0].FillPrice != "64000" || len(page.Fills[0].Raw) == 0) {
					t.Errorf("Fills() = %+v", page)
				}
				return err
			},
		},
		{
			name: "fills history", method: http.MethodGet, path: "/api/v5/trade/fills-history", query: "instType=SWAP",
			reply: `{"code":"0","msg":"","data":[]}`,
			call: func(c *Client) error {
				_, err := c.FillsHistory(context.Background(), FillsRequest{InstrumentType: InstrumentTypeSwap})
				return err
			},
		},
		{
			name: "place algo", method: http.MethodPost, path: "/api/v5/trade/order-algo",
			body:  `{"instId":"BTC-USDT","tdMode":"cash","side":"buy","ordType":"conditional","sz":"10","tgtCcy":"quote_ccy","tpTriggerPx":"70000","tpOrdPx":"-1"}`,
			reply: `{"code":"0","msg":"","data":[{"algoId":"77","clOrdId":"","algoClOrdId":"","sCode":"0","sMsg":"","tag":""}]}`,
			call: func(c *Client) error {
				ref, err := c.PlaceAlgoOrder(context.Background(), PlaceAlgoOrderRequest{
					InstrumentID: "BTC-USDT", TradeMode: TradeModeCash, Side: SideBuy,
					OrderType: AlgoOrderTypeConditional, Size: "10", TargetCurrency: TargetCurrencyQuote,
					TakeProfitPx: "70000", TakeProfitOrder: "-1",
				})
				if err == nil && ref.AlgoID != "77" {
					t.Errorf("PlaceAlgoOrder() = %+v", ref)
				}
				return err
			},
		},
		{
			name: "cancel algos", method: http.MethodPost, path: "/api/v5/trade/cancel-algos",
			body:  `[{"instId":"BTC-USDT","algoId":"77"}]`,
			reply: `{"code":"0","msg":"","data":[{"algoId":"77","sCode":"0","sMsg":""}]}`,
			call: func(c *Client) error {
				_, err := c.CancelAlgoOrders(context.Background(), CancelAlgoOrdersRequest{
					Orders: []CancelAlgoOrderRequest{{InstrumentID: "BTC-USDT", AlgoID: "77"}},
				})
				return err
			},
		},
		{
			name: "amend algos", method: http.MethodPost, path: "/api/v5/trade/amend-algos",
			body:  `{"instId":"BTC-USDT","algoId":"77","newSz":"2"}`,
			reply: `{"code":"0","msg":"","data":[{"algoId":"77","algoClOrdId":"","reqId":"","sCode":"0","sMsg":""}]}`,
			call: func(c *Client) error {
				_, err := c.AmendAlgoOrder(context.Background(), AmendAlgoOrderRequest{
					InstrumentID: "BTC-USDT", AlgoID: "77", NewSize: "2",
				})
				return err
			},
		},
		{
			name: "algo detail", method: http.MethodGet, path: "/api/v5/trade/order-algo", query: "algoId=77",
			reply: `{"code":"0","msg":"","data":[{"instType":"SPOT","instId":"BTC-USDT","algoId":"77","ordType":"conditional","state":"live","side":"buy","tdMode":"cash","sz":"10","ordIdList":[],"cTime":"1724751378980"}]}`,
			call: func(c *Client) error {
				order, err := c.AlgoOrderDetail(context.Background(), AlgoOrderDetailRequest{AlgoID: "77"})
				if err == nil && (order.State != "live" || order.OrderType != AlgoOrderTypeConditional) {
					t.Errorf("AlgoOrderDetail() = %+v", order)
				}
				return err
			},
		},
		{
			name: "algo pending", method: http.MethodGet, path: "/api/v5/trade/orders-algo-pending",
			query: "instType=SPOT&ordType=conditional%2Coco",
			reply: `{"code":"0","msg":"","data":[{"algoId":"77","state":"live"}]}`,
			call: func(c *Client) error {
				page, err := c.AlgoOrdersPending(context.Background(), AlgoOrdersRequest{
					OrderTypes:     []AlgoOrderType{AlgoOrderTypeConditional, AlgoOrderTypeOCO},
					InstrumentType: InstrumentTypeSpot,
				})
				if err == nil && len(page.Orders) != 1 {
					t.Errorf("AlgoOrdersPending() = %+v", page)
				}
				return err
			},
		},
		{
			name: "algo history", method: http.MethodGet, path: "/api/v5/trade/orders-algo-history",
			query: "ordType=conditional&state=effective",
			reply: `{"code":"0","msg":"","data":[]}`,
			call: func(c *Client) error {
				_, err := c.AlgoOrderHistory(context.Background(), AlgoOrdersRequest{
					OrderTypes: []AlgoOrderType{AlgoOrderTypeConditional}, State: "effective",
				})
				return err
			},
		},
		{
			name: "account config", method: http.MethodGet, path: "/api/v5/account/config",
			reply: `{"code":"0","msg":"","data":[{"uid":"1","acctLv":"2","posMode":"long_short_mode","autoLoan":false,"traderInsts":[],"settleCcyList":["USD","USDC"]}]}`,
			call: func(c *Client) error {
				config, err := c.AccountConfig(context.Background())
				if err == nil && (config.PositionMode != PositionModeLongShort || config.AccountLevel != "2") {
					t.Errorf("AccountConfig() = %+v", config)
				}
				return err
			},
		},
		{
			name: "set position mode", method: http.MethodPost, path: "/api/v5/account/set-position-mode",
			body:  `{"posMode":"net_mode"}`,
			reply: `{"code":"0","msg":"","data":[{"posMode":"net_mode"}]}`,
			call: func(c *Client) error {
				_, err := c.SetPositionMode(context.Background(), SetPositionModeRequest{PositionMode: PositionModeNet})
				return err
			},
		},
		{
			name: "set leverage", method: http.MethodPost, path: "/api/v5/account/set-leverage",
			body:  `{"instId":"BTC-USDT-SWAP","lever":"5","mgnMode":"isolated","posSide":"long"}`,
			reply: `{"code":"0","msg":"","data":[{"lever":"5","mgnMode":"isolated","instId":"BTC-USDT-SWAP","posSide":"long"}]}`,
			call: func(c *Client) error {
				result, err := c.SetLeverage(context.Background(), SetLeverageRequest{
					InstrumentID: "BTC-USDT-SWAP", Leverage: "5", MarginMode: TradeModeIsolated, PositionSide: PositionSideLong,
				})
				if err == nil && result.Leverage != "5" {
					t.Errorf("SetLeverage() = %+v", result)
				}
				return err
			},
		},
		{
			name: "leverage info", method: http.MethodGet, path: "/api/v5/account/leverage-info",
			query: "instId=BTC-USDT-SWAP&mgnMode=cross",
			reply: `{"code":"0","msg":"","data":[{"instId":"BTC-USDT-SWAP","mgnMode":"cross","posSide":"long","lever":"10"}]}`,
			call: func(c *Client) error {
				items, err := c.LeverageInfo(context.Background(), LeverageInfoRequest{
					InstrumentIDs: []string{"BTC-USDT-SWAP"}, MarginMode: TradeModeCross,
				})
				if err == nil && (len(items) != 1 || items[0].Leverage != "10") {
					t.Errorf("LeverageInfo() = %+v", items)
				}
				return err
			},
		},
		{
			name: "max size", method: http.MethodGet, path: "/api/v5/account/max-size",
			query: "instId=BTC-USDT&tdMode=isolated",
			reply: `{"code":"0","msg":"","data":[{"instId":"BTC-USDT","ccy":"BTC","maxBuy":"0.5","maxSell":"0.4"}]}`,
			call: func(c *Client) error {
				items, err := c.MaxSize(context.Background(), MaxSizeRequest{
					InstrumentIDs: []string{"BTC-USDT"}, TradeMode: TradeModeIsolated,
				})
				if err == nil && (len(items) != 1 || items[0].MaxBuy != "0.5") {
					t.Errorf("MaxSize() = %+v", items)
				}
				return err
			},
		},
		{
			name: "max avail size", method: http.MethodGet, path: "/api/v5/account/max-avail-size",
			query: "instId=BTC-USDT&reduceOnly=true&tdMode=cross",
			reply: `{"code":"0","msg":"","data":[{"instId":"BTC-USDT","availBuy":"1","availSell":"2"}]}`,
			call: func(c *Client) error {
				_, err := c.MaxAvailSize(context.Background(), MaxAvailSizeRequest{
					InstrumentID: "BTC-USDT", TradeMode: TradeModeCross, ReduceOnly: true,
				})
				return err
			},
		},
		{
			name: "adjust margin", method: http.MethodPost, path: "/api/v5/account/position/margin-balance",
			body:  `{"instId":"BTC-USDT-SWAP","posSide":"short","type":"add","amt":"1"}`,
			reply: `{"code":"0","msg":"","data":[{"instId":"BTC-USDT-SWAP","posSide":"short","amt":"1","type":"add","leverage":"","ccy":"USDT"}]}`,
			call: func(c *Client) error {
				_, err := c.AdjustPositionMargin(context.Background(), AdjustMarginRequest{
					InstrumentID: "BTC-USDT-SWAP", PositionSide: PositionSideShort, Type: MarginAdjustAdd, Amount: "1",
				})
				return err
			},
		},
		{
			name: "trade fee", method: http.MethodGet, path: "/api/v5/account/trade-fee",
			query: "instId=BTC-USDT&instType=SPOT",
			reply: `{"code":"0","msg":"","data":[{"level":"Lv1","instType":"SPOT","taker":"-0.001","maker":"-0.0008","ts":"1700000000000"}]}`,
			call: func(c *Client) error {
				fee, err := c.TradeFee(context.Background(), TradeFeeRequest{
					InstrumentType: InstrumentTypeSpot, InstrumentID: "BTC-USDT",
				})
				if err == nil && fee.Taker != "-0.001" {
					t.Errorf("TradeFee() = %+v", fee)
				}
				return err
			},
		},
		{
			name: "bills", method: http.MethodGet, path: "/api/v5/account/bills", query: "ccy=USDT&limit=20",
			reply: `{"code":"0","msg":"","data":[{"billId":"623950854533513219","type":"2","subType":"1","balChg":"0.02","bal":"8694.2","ccy":"USDT","instId":"BTC-USDT","ts":"1700000000000"}]}`,
			call: func(c *Client) error {
				page, err := c.AccountBills(context.Background(), AccountBillsRequest{Currency: "USDT", Limit: 20})
				if err == nil && (len(page.Bills) != 1 || page.Bills[0].BillID != "623950854533513219") {
					t.Errorf("AccountBills() = %+v", page)
				}
				return err
			},
		},
		{
			name: "bills archive", method: http.MethodGet, path: "/api/v5/account/bills-archive", query: "instType=SWAP",
			reply: `{"code":"0","msg":"","data":[]}`,
			call: func(c *Client) error {
				_, err := c.AccountBillsArchive(context.Background(), AccountBillsRequest{InstrumentType: InstrumentTypeSwap})
				return err
			},
		},
		{
			name: "positions history", method: http.MethodGet, path: "/api/v5/account/positions-history",
			query: "instType=SWAP&mgnMode=cross",
			reply: `{"code":"0","msg":"","data":[{"instType":"SWAP","instId":"BTC-USDT-SWAP","mgnMode":"cross","posId":"20","closeAvgPx":"64000","realizedPnl":"12.5","direction":"long","type":"2"}]}`,
			call: func(c *Client) error {
				items, err := c.PositionsHistory(context.Background(), PositionsHistoryRequest{
					InstrumentType: InstrumentTypeSwap, MarginMode: TradeModeCross,
				})
				if err == nil && (len(items) != 1 || items[0].RealizedPnL != "12.5") {
					t.Errorf("PositionsHistory() = %+v", items)
				}
				return err
			},
		},
		{
			name: "account position risk", method: http.MethodGet, path: "/api/v5/account/account-position-risk",
			query: "instType=SWAP",
			reply: `{"code":"0","msg":"","data":[{"ts":"1700000000000","adjEq":"100","balData":[{"ccy":"USDT","eq":"100","disEq":"99"}],"posData":[{"instType":"SWAP","mgnMode":"cross","posId":"20","instId":"BTC-USDT-SWAP","pos":"1","notionalUsd":"64000"}]}]}`,
			call: func(c *Client) error {
				risk, err := c.AccountPositionRisk(context.Background(), AccountPositionRiskRequest{InstrumentType: InstrumentTypeSwap})
				if err == nil && (risk.AdjustedEquity != "100" || len(risk.BalanceData) != 1 || len(risk.PositionData) != 1) {
					t.Errorf("AccountPositionRisk() = %+v", risk)
				}
				return err
			},
		},
		{
			name: "set isolated mode", method: http.MethodPost, path: "/api/v5/account/set-isolated-mode",
			body:  `{"isoMode":"automatic","type":"MARGIN"}`,
			reply: `{"code":"0","msg":"","data":[{"isoMode":"automatic"}]}`,
			call: func(c *Client) error {
				_, err := c.SetIsolatedMode(context.Background(), SetIsolatedModeRequest{
					IsolatedMode: IsolatedModeAutomatic, Type: IsolatedModeScopeMargin,
				})
				return err
			},
		},
		{
			name: "max withdrawal", method: http.MethodGet, path: "/api/v5/account/max-withdrawal", query: "ccy=BTC%2CETH",
			reply: `{"code":"0","msg":"","data":[{"ccy":"BTC","maxWd":"124","maxWdEx":"125","spotOffsetMaxWd":"","spotOffsetMaxWdEx":""}]}`,
			call: func(c *Client) error {
				items, err := c.MaxWithdrawal(context.Background(), MaxWithdrawalRequest{Currencies: []string{"BTC", "ETH"}})
				if err == nil && (len(items) != 1 || items[0].MaxWithdrawal != "124") {
					t.Errorf("MaxWithdrawal() = %+v", items)
				}
				return err
			},
		},
	}

	var current extCase
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":"50113","msg":"Invalid signature","data":[]}`, http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(request.Body)
		if request.Method != current.method || request.URL.Path != current.path ||
			request.URL.RawQuery != current.query || string(body) != current.body {
			http.Error(writer, `{"code":"51000","msg":"unexpected request","data":[]}`, http.StatusBadRequest)
			t.Errorf("%s: got %s %s?%s body=%s", current.name, request.Method, request.URL.Path, request.URL.RawQuery, body)
			return
		}
		_, _ = io.WriteString(writer, current.reply)
	}))
	defer server.Close()

	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return fixedNow },
	)
	// 서버 핸들러가 current를 읽으므로 사례는 순차 실행한다.
	for _, testCase := range cases {
		current = testCase
		if err := testCase.call(client); err != nil {
			t.Errorf("%s: error = %v", testCase.name, err)
		}
	}
}

func TestBatchPartialSuccessAndFailure(t *testing.T) {
	t.Parallel()

	replies := map[string]string{
		"partial": `{"code":"2","msg":"Bulk operation partially succeeded.","data":[{"ordId":"1","clOrdId":"a","sCode":"0","sMsg":""},{"ordId":"","clOrdId":"b","sCode":"51008","sMsg":"insufficient"}]}`,
		"failed":  `{"code":"1","msg":"Operation failed.","data":[{"ordId":"","clOrdId":"a","sCode":"51008","sMsg":"insufficient"}]}`,
	}
	var mode string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, replies[mode])
	}))
	defer server.Close()
	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return time.UnixMilli(1_700_000_000_000) },
	)
	cancel := BatchCancelOrdersRequest{Orders: []CancelOrderRequest{
		{InstrumentID: "BTC-USDT", ClientOrderID: "a"}, {InstrumentID: "BTC-USDT", ClientOrderID: "b"},
	}}

	mode = "partial"
	result, err := client.BatchCancelOrders(context.Background(), cancel)
	if err != nil || !result.PartialSuccess || len(result.Orders) != 2 ||
		result.Orders[0].StatusCode != "0" || result.Orders[1].StatusCode != "51008" {
		t.Fatalf("partial BatchCancelOrders() = %+v, error = %v", result, err)
	}

	mode = "failed"
	result, err = client.BatchCancelOrders(context.Background(), cancel)
	if !errors.Is(err, trade.ErrInsufficientBalance) || len(result.Orders) != 1 {
		t.Fatalf("failed BatchCancelOrders() = %+v, error = %v", result, err)
	}
}

func TestTradeExtensionValidation(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(
		t, "http://127.0.0.1", &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, nil,
	)
	tooMany := make([]CancelOrderRequest, maxBatchOrders+1)
	for index := range tooMany {
		tooMany[index] = CancelOrderRequest{InstrumentID: "BTC-USDT", OrderID: "1"}
	}
	errs := []error{
		func() error {
			_, err := client.BatchCancelOrders(context.Background(), BatchCancelOrdersRequest{Orders: tooMany})
			return err
		}(),
		func() error {
			_, err := client.BatchPlaceOrders(context.Background(), BatchPlaceOrdersRequest{})
			return err
		}(),
		func() error {
			_, err := client.AmendOrder(context.Background(), AmendOrderRequest{InstrumentID: "BTC-USDT", OrderID: "1"})
			return err
		}(),
		func() error {
			_, err := client.CancelAllAfter(context.Background(), CancelAllAfterRequest{TimeOut: 5 * time.Second})
			return err
		}(),
		func() error {
			_, err := client.ClosePosition(context.Background(), ClosePositionRequest{InstrumentID: "BTC-USDT-SWAP"})
			return err
		}(),
		func() error {
			_, err := client.FillsHistory(context.Background(), FillsRequest{})
			return err
		}(),
		func() error {
			_, err := client.AlgoOrderHistory(context.Background(), AlgoOrdersRequest{
				OrderTypes: []AlgoOrderType{AlgoOrderTypeTrigger},
			})
			return err
		}(),
		func() error {
			_, err := client.PlaceAlgoOrder(context.Background(), PlaceAlgoOrderRequest{
				InstrumentID: "BTC-USDT", TradeMode: TradeModeCash, Side: SideBuy,
				OrderType: AlgoOrderTypeOCO, Size: "1", TakeProfitPx: "1",
			})
			return err
		}(),
		func() error {
			_, err := client.SetLeverage(context.Background(), SetLeverageRequest{
				InstrumentID: "BTC-USDT-SWAP", Currency: "BTC", Leverage: "3", MarginMode: TradeModeCross,
			})
			return err
		}(),
		func() error {
			_, err := client.SetIsolatedMode(context.Background(), SetIsolatedModeRequest{
				IsolatedMode: IsolatedModeAutoTransfersCcy, Type: IsolatedModeScopeContracts,
			})
			return err
		}(),
		func() error {
			_, err := client.MaxSize(context.Background(), MaxSizeRequest{TradeMode: TradeModeCash})
			return err
		}(),
	}
	for index, err := range errs {
		if !errors.Is(err, trade.ErrValidation) {
			t.Errorf("case %d: error = %v, want validation", index, err)
		}
	}
}
