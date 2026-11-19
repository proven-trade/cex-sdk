# Binance 공개 데이터 덤프

`exchange/binance/archive` 패키지는 Binance 공식 공개 데이터 저장소 `https://data.binance.vision`의 캔들·체결 덤프를 읽습니다. REST 캔들은 요청당 1,000개(Spot)·1,500개(USDⓈ-M)이고 REST 체결 조회도 요청당 최대 1,000건이며 모두 IP weight 한도를 공유하므로 전 상품의 상장 시점부터 백필하면 오래 걸립니다. 덤프는 1분봉 기준 상품·월당 파일 하나(약 43,000봉)이므로 초기 백필을 덤프로 받고, 최근 구간과 이음매 근처만 REST로 받는 구성을 권장합니다.

## 지원 덤프

| 메서드 | 데이터 | 상품 | 요청 | reader | 행 |
|---|---|---|---|---|---|
| `Klines` | 캔들 | Spot, USDⓈ-M | `KlinesRequest` | `*KlineReader` | `binance.Kline` |
| `MarkPriceKlines` | 마크가 캔들 | USDⓈ-M | `MarkPriceKlinesRequest` | `*KlineReader` | `binance.Kline` |
| `IndexPriceKlines` | 지수가 캔들 | USDⓈ-M | `IndexPriceKlinesRequest` | `*KlineReader` | `binance.Kline` |
| `PremiumIndexKlines` | 프리미엄 지수 캔들 | USDⓈ-M | `PremiumIndexKlinesRequest` | `*KlineReader` | `binance.Kline` |
| `AggTrades` | 집계 체결 | Spot, USDⓈ-M | `AggTradesRequest` | `*AggTradeReader` | `AggTrade` |
| `Trades` | 개별 체결 | Spot, USDⓈ-M | `TradesRequest` | `*TradeReader` | `Trade` |

저장소 경로는 공식 다운로드 스크립트와 같습니다. `{상품}`은 `spot` 또는 `futures/um`, `{주기}`는 `monthly` 또는 `daily`, `{기간}`은 월 파일이면 `YYYY-MM`, 일 파일이면 `YYYY-MM-DD`입니다. 파일마다 이름 뒤에 `.CHECKSUM`을 붙인 체크섬 파일이 같은 폴더에 있습니다.

| 종류 | 경로 |
|---|---|
| 캔들류(`klines`, `markPriceKlines`, `indexPriceKlines`, `premiumIndexKlines`) | `data/{상품}/{주기}/{데이터}/{SYMBOL}/{구간}/{SYMBOL}-{구간}-{기간}.zip` |
| 체결류(`aggTrades`, `trades`) | `data/{상품}/{주기}/{데이터}/{SYMBOL}/{SYMBOL}-{데이터}-{기간}.zip` |

모든 메서드는 인증이 필요 없는 공개 파일을 읽는 `OperationRead` 요청입니다. API Key를 보내지 않고 `credential.Permission`도 요구하지 않으며 `Config`에 자격증명이 없습니다.

## 사용법

```go
archiveClient, err := archive.New(archive.Config{Executor: executor, DefaultEgressRouteID: "seoul-a"})
if err != nil {
	return err
}
reader, err := archiveClient.Klines(ctx, archive.KlinesRequest{
	Market: archive.MarketSpot, Symbol: "BTCUSDT", Interval: binance.Kline1Minute,
	Year: 2025, Month: time.February,
})
if errors.Is(err, archive.ErrFileNotFound) {
	// 상장 전, 아직 게시 전, 상장 폐지 후 구간
}
if err != nil {
	return err
}
defer reader.Close()
for {
	kline, err := reader.Next()
	if errors.Is(err, io.EOF) {
		break
	}
	if err != nil {
		return err
	}
	store(kline)
}
```

| 필드 | 의미 |
|---|---|
| `Market` | `MarketSpot`(`data/spot`), `MarketUSDM`(`data/futures/um`). 마크가·지수가·프리미엄 지수 캔들 요청에는 이 필드가 없고 항상 USDⓈ-M입니다. |
| `Symbol` | REST와 같은 `BTCUSDT` 형식. 대문자·숫자와 `币安人生USDT` 같은 한자 심볼만 받고 소문자·구분자·경로 문자는 거부합니다. |
| `Interval` | `binance.KlineInterval`. 캔들류 요청에만 있습니다. 1초 캔들은 Spot 캔들만 게시하며, 1개월 구간은 저장소 표기 `1mo`로 변환합니다. |
| `Year`, `Month` | 파일의 연월. `Year`는 2017 이상입니다. |
| `Day` | `0`이면 월 파일(`monthly`), 1 이상이면 그날의 일 파일(`daily`) |

요청 검증은 네트워크 요청 전에 하며 위반하면 `trade.ErrValidation`을 반환합니다.

`Next`는 REST와 같은 `binance.Kline`을 반환합니다. 2025년 이후 Spot 파일의 microsecond 시각은 millisecond로 바꾸고, 최근 USDⓈ-M 파일의 헤더 행은 건너뜁니다. 캔들류의 숫자 열은 원문 decimal 문자열을 그대로 보존하며 지수 표기처럼 예상하지 못한 형식은 줄 번호와 함께 오류로 반환합니다.

### 3d·1w·1M 구간은 월 파일만

3d·1w·1M(`1mo`) 구간은 월 파일만 받습니다. 공식 다운로드 스크립트의 `DAILY_INTERVALS`(`1s`, `1m`~`12h`, `1d`)가 이 구간을 제외하며 Spot에는 이 구간의 일 파일이 없습니다. USDⓈ-M에는 게시가 중단된 과거 파일이 일부 남아 있을 뿐입니다. 2026-11 BTCUSDT 실측으로 가장 늦은 일 파일은 3d가 2023-12-08, 1w가 2026-06-29, 1mo가 2023-05-01입니다. 그래서 이 구간에 `Day`를 지정하면 존재하지 않을 수 있는 파일을 요청하는 대신 요청 전에 `trade.ErrValidation`으로 거절합니다. 이 구간의 당월 값은 REST 캔들로 받습니다.

## 추가 덤프

### 체결 덤프

`AggTrades`와 `Trades`는 Spot과 USDⓈ-M의 체결 파일을 읽습니다. 요청 필드는 `Market`, `Symbol`, `Year`, `Month`, `Day`이며 `Interval`은 없습니다.

| 메서드 | 저장소 경로 | 권한 | 작업 |
|---|---|---|---|
| `AggTrades` | `data/{spot,futures/um}/{monthly,daily}/aggTrades/{SYMBOL}/{SYMBOL}-aggTrades-{기간}.zip` | 없음(공개 데이터) | 읽기(`OperationRead`) |
| `Trades` | `data/{spot,futures/um}/{monthly,daily}/trades/{SYMBOL}/{SYMBOL}-trades-{기간}.zip` | 없음(공개 데이터) | 읽기(`OperationRead`) |

```go
reader, err := archiveClient.AggTrades(ctx, archive.AggTradesRequest{
	Market: archive.MarketUSDM, Symbol: "BTCUSDT", Year: 2026, Month: time.November, Day: 16,
})
if err != nil {
	return err
}
defer reader.Close()
for {
	item, err := reader.Next() // archive.AggTrade
	if errors.Is(err, io.EOF) {
		break
	}
	if err != nil {
		return err
	}
	store(item)
}
```

`AggTrade`는 집계 체결 파일의 한 줄입니다.

| 필드 | CSV 열 | 설명 |
|---|---|---|
| `AggregateID` | `agg_trade_id` | 집계 체결 ID |
| `Price` | `price` | 가격 |
| `Quantity` | `quantity` | 수량 |
| `FirstTradeID`, `LastTradeID` | `first_trade_id`, `last_trade_id` | 집계에 포함된 개별 체결 ID 범위 |
| `Time` | `transact_time` | Unix millisecond |
| `BuyerMaker` | `is_buyer_maker` | 매수자가 maker인지 여부 |
| `BestMatch` | `is_best_match` | Spot 파일에만 있는 열이며 USDⓈ-M 파일에서는 항상 `false` |

`Trade`는 개별 체결 파일의 한 줄입니다.

| 필드 | CSV 열 | 설명 |
|---|---|---|
| `ID` | `id` | 체결 ID |
| `Price` | `price` | 가격 |
| `Quantity` | `qty` | 수량 |
| `QuoteQuantity` | `quote_qty` | 체결 대금(quote asset 수량) |
| `Time` | `time` | Unix millisecond |
| `BuyerMaker` | `is_buyer_maker` | 매수자가 maker인지 여부 |
| `BestMatch` | `is_best_match` | Spot 파일에만 있는 열이며 USDⓈ-M 파일에서는 항상 `false` |

- 열 수는 상품마다 다릅니다. Spot은 집계 체결 8열·개별 체결 7열(`is_best_match` 포함), USDⓈ-M은 집계 체결 7열·개별 체결 6열이며 열 수가 다른 줄은 줄 번호와 함께 오류로 반환합니다.
- 행은 파일 순서(체결 순서)대로 반환합니다. 최근 USDⓈ-M 일 파일은 첫 줄이 열 이름(`agg_trade_id,...`, `id,...`)이라 건너뛰고 Spot 파일과 오래된 USDⓈ-M 월 파일에는 헤더가 없습니다.
- `Time`은 파일 단위와 관계없이 Unix millisecond입니다. 2025년 이후 Spot 파일은 microsecond로 기록하므로 millisecond로 내립니다.
- 가격·수량·대금은 `float64`로 바꾸지 않고 문자열로 보존합니다. 일부 USDⓈ-M 체결 파일(2026-11 일 파일 등)은 작거나 큰 값을 `7.72E-6`, `5.5738058E7`처럼 지수 표기로 기록하므로 부동소수점을 거치지 않고 자릿수만 옮겨 `0.00000772`, `55738058` 같은 일반 십진 문자열로 풀어 씁니다. 지수가 없는 값은 원문 그대로이며 지수의 절댓값이 30을 넘거나 값 앞에 부호가 붙은(음수 포함) 경우는 줄 번호와 함께 오류입니다.
- 불리언 열은 Spot의 `True`·`False`와 USDⓈ-M의 `true`·`false`를 모두 받습니다.
- 체결 월 파일은 큽니다. 2026-10 월 파일 실측으로 BTCUSDT 집계 체결은 Spot 약 380MB·USDⓈ-M 약 400MB, 개별 체결은 Spot 약 617MB·USDⓈ-M 약 659MB이고 USDⓈ-M ETHUSDT 개별 체결은 약 1.0GB입니다. 일 파일은 BTCUSDT 기준 17~35MB입니다. 압축 파일 크기 상한 `Config.MaxFileBytes`의 기본값은 256MB이므로 월 파일을 받으려면 값을 올리거나(예: `MaxFileBytes: 2 << 30`) 일 파일로 받습니다. 상한을 넘으면 재시도 불가 오류를 반환합니다.

### 가격 지표 캔들 덤프

`MarkPriceKlines`, `IndexPriceKlines`, `PremiumIndexKlines`는 USDⓈ-M(`data/futures/um`)의 마크가·지수가·프리미엄 지수 캔들 파일을 읽습니다. 요청 필드는 `Symbol`, `Interval`, `Year`, `Month`, `Day`이며 `Market`은 없습니다.

| 메서드 | 저장소 경로 | 권한 | 작업 |
|---|---|---|---|
| `MarkPriceKlines` | `data/futures/um/{monthly,daily}/markPriceKlines/{SYMBOL}/{구간}/{SYMBOL}-{구간}-{기간}.zip` | 없음(공개 데이터) | 읽기(`OperationRead`) |
| `IndexPriceKlines` | `data/futures/um/{monthly,daily}/indexPriceKlines/{SYMBOL}/{구간}/{SYMBOL}-{구간}-{기간}.zip` | 없음(공개 데이터) | 읽기(`OperationRead`) |
| `PremiumIndexKlines` | `data/futures/um/{monthly,daily}/premiumIndexKlines/{SYMBOL}/{구간}/{SYMBOL}-{구간}-{기간}.zip` | 없음(공개 데이터) | 읽기(`OperationRead`) |

```go
reader, err := archiveClient.PremiumIndexKlines(ctx, archive.PremiumIndexKlinesRequest{
	Symbol: "BTCUSDT", Interval: binance.Kline1Hour, Year: 2024, Month: time.December,
})
```

- 반환하는 행은 캔들과 같은 `binance.Kline`이며 구간·`Day` 규칙도 `Klines`와 같습니다. 이 덤프에는 1초 구간이 없어 `1s`를 지정하면 검증 오류입니다.
- 지표에는 거래가 없어 `Volume`, `QuoteAssetVolume`, `TakerBuyBaseAssetVolume`, `TakerBuyQuoteAssetVolume`은 실측 파일에서 모두 `"0"`이며 reader는 파일의 값을 그대로 반환합니다. `TradeCount`는 체결 수가 아니라 파일의 `count` 열 값이며 BTCUSDT 1시간봉 실측으로 마크가·지수가는 3600, 프리미엄 지수는 720입니다.
- 프리미엄 지수는 선물이 지수보다 싸면 음수이므로 `PremiumIndexKlines`만 `Open`, `High`, `Low`, `Close`의 음수를 허용합니다. 다른 열의 음수와 마크가·지수가 캔들의 음수는 줄 번호와 함께 오류입니다.
- 최근 파일은 첫 줄이 열 이름(`open_time,...`)이라 건너뜁니다.

## 무결성과 크기

모든 메서드는 같은 이름의 `.CHECKSUM` 파일을 먼저 받아 SHA-256을 확인합니다. 체크섬 파일이 없으면 덤프를 받지 않고 `ErrFileNotFound`(`trade.ErrUnsupportedCapability`)를 반환합니다. 내려받은 zip의 해시가 다르면 `ErrChecksumMismatch`를 재시도 가능한 오류로 반환합니다. zip은 중앙 디렉터리가 파일 끝에 있고 검증 전에는 내용을 믿을 수 없으므로 압축 파일은 메모리에 받으며 파일 이름과 같은 CSV 항목 하나만 있어야 합니다. 압축 해제와 CSV 해석은 `Next` 호출마다 진행합니다. 압축 파일 크기 상한은 `Config.MaxFileBytes`(기본 256MB)이며 1분봉 월 파일은 수 MB입니다(2026-10 USDⓈ-M BTCUSDT 1분봉 월 파일은 캔들 약 1.8MB, 마크가 약 1.0MB, 프리미엄 지수 약 0.9MB).

요청은 다른 클라이언트와 같은 `Executor`와 요청별 송신 경로를 사용합니다. `trade.WithTimeout`은 각 응답의 헤더 수신까지만 적용하고 본문 수신 시간은 context로 제어합니다. 저장소는 요청 제한을 공개하지 않으므로 동시 다운로드 수는 호출자가 조절해야 합니다.

## 게시 범위

2026-11 기준 BTCUSDT 실측 결과는 다음과 같습니다.

- Spot 1분봉·집계 체결·개별 체결 월 파일: 2017-08부터 지난달까지
- USDⓈ-M 1분봉 월 파일: 2020-01부터 지난달까지(2019-09 상장 직후 구간은 REST로 받습니다)
- USDⓈ-M 마크가·지수가·프리미엄 지수 1분봉 월 파일: 2020-01부터 지난달까지
- USDⓈ-M 집계 체결 월 파일: 2020-01부터, 개별 체결 월 파일: 2019-09부터 지난달까지
- 일 파일: 전날까지 게시되며 당월 구간은 일 파일로 채웁니다.

월 파일은 다음 달 초에 게시되므로, 월 파일이 `ErrFileNotFound`이면 그 달의 일 파일을 차례로 요청합니다. 그날의 일 파일도 아직 없으면 REST 캔들·체결로 받습니다.

## 지원하지 않는 덤프

저장소에는 COIN-M 선물(`data/futures/cm`)과 옵션(`data/option`) 덤프, 그리고 USDⓈ-M의 `bookTicker`, `fundingRate`, `bookDepth`, `metrics` 디렉터리가 있습니다. 앞의 두 상품은 이 패키지의 범위가 아니고, 뒤의 디렉터리는 공식 README와 다운로드 스크립트가 열 구성을 문서화하지 않아 읽지 않습니다. USDⓈ-M 인도 선물 심볼(`BTCUSDT_250627`처럼 `_`가 붙은 심볼)도 `Symbol` 검증이 거부합니다.

## 참고

- [Binance Public Data README](https://github.com/binance/binance-public-data)
- [다운로드 스크립트 상수(`enums.py`, `INTERVALS`, `DAILY_INTERVALS`)](https://github.com/binance/binance-public-data/blob/master/python/enums.py)
- [다운로드 스크립트(`download-aggTrade.py`, `download-trade.py`, `download-kline.py`, `download-futures-markPriceKlines.py`, `download-futures-indexPriceKlines.py`, `download-futures-premiumIndexKlines.py`)](https://github.com/binance/binance-public-data/tree/master/python)
