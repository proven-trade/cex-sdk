# 공통 Spot API

## 목적

`unified.SpotClient`는 Binance, Bitget, Upbit, Bybit, OKX, Coinbase, Kraken, Bithumb, Coinone, Korbit의 공통 현물 기능을 한 인터페이스로 제공합니다. 거래소 고유 기능과 원본 필드는 각 `exchange/<거래소>` native 클라이언트를 사용합니다.

공통 인터페이스가 제공하는 기능은 다음과 같습니다.

- 거래 마켓 목록과 최신 가격
- 호가, 최근 공개 체결, OHLCV 캔들
- 계정 잔고
- 주문 생성, 단건 조회, 취소, 미체결 목록
- 요청별 `trade.WithEgressRoute`와 `trade.WithTimeout`

각 native 클라이언트는 다음 생성 함수로 공통 어댑터에 연결합니다.

```go
binanceSpot, err := binance.NewUnifiedSpot(binanceClient)
bitgetSpot, err := bitget.NewUnifiedSpot(bitgetClient)
upbitSpot, err := upbit.NewUnifiedSpot(upbitClient)
bybitSpot, err := bybit.NewUnifiedSpot(bybitClient)
okxSpot, err := okx.NewUnifiedSpot(okxClient)
coinbaseSpot, err := coinbase.NewUnifiedSpot(coinbaseClient)
krakenSpot, err := kraken.NewUnifiedSpot(krakenClient)
bithumbSpot, err := bithumb.NewUnifiedSpot(bithumbClient)
coinoneSpot, err := coinone.NewUnifiedSpot(coinoneClient)
korbitSpot, err := korbit.NewUnifiedSpot(korbitClient)
```

열 값은 모두 `unified.SpotClient`를 구현합니다.

## 마켓 표현

공통 요청은 `unified.Market{Base: "BTC", Quote: "USDT"}`처럼 기준 자산과 결제 자산을 분리합니다. 어댑터는 다음 native 문자열로 변환합니다.

| 거래소 | 공통 `BTC/USDT`의 native 값 |
|---|---|
| Binance | `BTCUSDT` |
| Bitget | `BTCUSDT` |
| Upbit | `USDT-BTC` |
| Bybit | `BTCUSDT` |
| OKX | `BTC-USDT` |
| Coinbase | `BTC-USDT` |
| Kraken | `XBTUSDT` |
| Bithumb | `USDT-BTC` |
| Coinone | `USDT-BTC` |
| Korbit | `btc_usdt` |

응답에는 공통 `Market`과 거래소 원문인 `NativeMarket`을 함께 둡니다. 전체 마켓 미체결 주문처럼 구분자가 없는 native 심볼만 응답되는 경우에는 공통 자산을 안전하게 역추론할 수 없어 `Market`이 비어 있을 수 있으므로 `NativeMarket`을 확인해야 합니다.

## 숫자와 시장가 주문

가격, 수량, 금액은 모두 decimal 문자열입니다. `float32`와 `float64` 입력은 제공하지 않습니다.

시장가 주문은 매수와 매도의 입력 단위가 다릅니다.

| 주문 | 입력 필드 | 의미 |
|---|---|---|
| 시장가 매수 | `QuoteAmount` | 사용할 결제 자산 금액 |
| 시장가 매도 | `Quantity` | 매도할 기준 자산 수량 |
| 지정가 매수·매도 | `Quantity`, `Price` | 기준 자산 수량과 단가 |

이 구분은 Binance `quoteOrderQty`, Bitget Spot 시장가 매수 수량, Upbit `price`, Bybit `marketUnit`, OKX `tgtCcy`, Coinbase 주문 설정 객체, Kraken `viqc` 플래그, Bithumb `price` 주문, Coinone과 Korbit의 주문 금액 필드 차이를 어댑터 내부에서 변환합니다. 값의 자동 반올림은 하지 않으며 거래소 상품 규칙에 맞지 않으면 거래소가 거절합니다. 정밀도 사전 검증은 후속 공통 상품 규칙 단계에서 추가합니다.

Bybit UNIFIED 계정은 `availableToWithdraw`가 폐기되어 항상 빈 문자열이므로 공통 `Available`을 `walletBalance - spotBorrow - locked`로 계산합니다. 이는 차입금을 제외한 비잠금 자기자산이며 cross/portfolio margin의 주문 가능 증거금이나 추가 차입 가능액이 아닙니다. margin buying power가 필요한 전략은 Bybit native 계정 API를 사용해야 합니다.

## 공통 캔들 범위

열 거래소에서 의미를 동일하게 제공할 수 있는 다음 구간만 공통 인터페이스에 노출합니다.

`1m`, `3m`, `5m`, `15m`, `30m`, `1h`, `4h`

그 밖의 초봉, 일봉, 주봉과 기준 가격 캔들은 native 클라이언트를 사용합니다. 공통 호가 깊이는 Coinone 제한에 맞춰 최대 16, 최근 체결은 최대 100, 캔들은 최대 200으로 제한합니다. Coinone은 5·10·15·16단계 중 요청 이상인 최소 깊이를 조회한 뒤 정확한 요청 수만 반환합니다.

Coinbase, Kraken, Korbit은 native 3분봉이 없으므로 같은 요청별 EIP로 1분봉을 조회한 뒤 공통 epoch 기준 3분 버킷으로 합성합니다. Coinbase는 한 요청의 350개 제한, Korbit은 200개 제한에 맞춰 페이지를 나눕니다. 중복 시각은 한 번만 반영하고 OHLC와 거래량은 decimal 문자열 정밀도를 유지합니다.

## 주문 실패 안전성

공통 어댑터는 native 클라이언트의 오류를 그대로 보존합니다. 주문 생성·취소 중 전송 결과가 불명확하면 `trade.ErrUnknownExecutionState`가 반환되며 자동 재시도하지 않습니다.

가능하면 `ClientOrderID`를 지정하고, 오류 후 단건 조회로 최종 상태를 조정해야 합니다. 전체 미체결 주문 조회는 의도하지 않은 고비용 요청을 막기 위해 `AllMarkets: true`를 명시해야 합니다.

## 적합성 테스트

`conformance.RunSpotReadSuite`는 모든 공통 어댑터에 같은 시나리오를 실행해 다음 계약을 검사합니다.

- 거래소 ID
- 공통 마켓과 native 마켓 변환
- 가격 문자열과 원본 응답 보존
- 자산 잔고 변환
- 요청별 EIP 옵션 전달

각 어댑터는 컴파일 시 `unified.SpotClient` 구현 여부도 검사합니다.
