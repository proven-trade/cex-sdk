# Binance 공개 데이터 덤프

`exchange/binance/archive` 패키지는 Binance 공식 공개 데이터 저장소 `https://data.binance.vision`의 캔들 덤프를 읽습니다. REST 캔들은 요청당 1,000개(Spot)·1,500개(USDⓈ-M)이고 IP weight 한도를 공유하므로 전 상품의 상장 시점부터 백필하면 오래 걸립니다. 덤프는 1분봉 기준 상품·월당 파일 하나(약 43,000봉)이므로 초기 백필을 덤프로 받고, 최근 구간과 이음매 근처만 REST로 받는 구성을 권장합니다.

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
| `Market` | `MarketSpot`(`data/spot`), `MarketUSDM`(`data/futures/um`) |
| `Symbol` | REST와 같은 `BTCUSDT` 형식 |
| `Interval` | `binance.KlineInterval`. 1초 캔들은 Spot만 게시하며, 1개월 구간은 저장소 표기 `1mo`로 변환합니다. |
| `Year`, `Month` | 파일의 연월 |
| `Day` | `0`이면 월 파일(`monthly`), 1 이상이면 그날의 일 파일(`daily`) |

`Next`는 REST와 같은 `binance.Kline`을 반환합니다. 2025년 이후 Spot 파일의 microsecond 시각은 millisecond로 바꾸고, 최근 USDⓈ-M 파일의 헤더 행은 건너뜁니다. 숫자 열은 원문 decimal 문자열을 그대로 보존하며 지수 표기처럼 예상하지 못한 형식은 줄 번호와 함께 오류로 반환합니다.

## 무결성과 크기

`Klines`는 같은 이름의 `.CHECKSUM` 파일을 먼저 받아 SHA-256을 확인합니다. 체크섬 파일이 없으면 덤프를 받지 않고 `ErrFileNotFound`(`trade.ErrUnsupportedCapability`)를 반환합니다. 내려받은 zip의 해시가 다르면 `ErrChecksumMismatch`를 재시도 가능한 오류로 반환합니다. zip은 중앙 디렉터리가 파일 끝에 있고 검증 전에는 내용을 믿을 수 없으므로 압축 파일은 메모리에 받습니다. 압축 해제와 CSV 해석은 `Next` 호출마다 진행합니다. 압축 파일 크기 상한은 `Config.MaxFileBytes`(기본 256MB)이며 1분봉 월 파일은 수 MB입니다.

요청은 다른 클라이언트와 같은 `Executor`와 요청별 송신 경로를 사용합니다. `trade.WithTimeout`은 각 응답의 헤더 수신까지만 적용하고 본문 수신 시간은 context로 제어합니다. 저장소는 요청 제한을 공개하지 않으므로 동시 다운로드 수는 호출자가 조절해야 합니다.

## 게시 범위

2026-11 기준 BTCUSDT 실측 결과는 다음과 같습니다.

- Spot 1분봉 월 파일: 2017-08부터 지난달까지
- USDⓈ-M 1분봉 월 파일: 2020-01부터 지난달까지(2019-09 상장 직후 구간은 REST로 받습니다)
- 일 파일: 전날까지 게시되며 당월 구간은 일 파일로 채웁니다.

월 파일은 다음 달 초에 게시되므로, 월 파일이 `ErrFileNotFound`이면 그 달의 일 파일을 차례로 요청합니다. 그날의 일 파일도 아직 없으면 REST 캔들로 받습니다.
