# Changelog

이 프로젝트의 사용자 영향 변경 사항은 이 파일에 기록합니다. 버전은 Semantic Versioning을 따릅니다.

## 1.1.0

### Added

- Binance 공개 데이터 덤프(`data.binance.vision`) Spot·USDⓈ-M 캔들 월·일 파일을 SHA-256 검증 후 읽는 `exchange/binance/archive` 패키지 (#13)
- `stream.SessionConfig.IdleTimeout`과 모든 거래소 `StreamClientConfig.IdleTimeout`, `stream.ErrIdleTimeout`·`stream.ErrPingFailed`·`stream.HeartbeatDetector` (#12)
- `commonexchange.Execution.RateLimit`: HTTP 429 밖의 본문 수준 요청 한도 초과를 limiter block과 읽기 재시도로 처리 (#11)
- Bitget `StreamCandle` kline WebSocket 데이터 타입 (#7)
- Upbit `Config.PublicRequestsPerSecond` (#8)

### Changed

- WebSocket ping을 별도 goroutine에서 보내 pong 대기 중에도 수신을 계속 처리하고, ping 실패는 재연결합니다 (#9)
- Bybit HTTP 200 `retCode 10006`은 해당 bucket을 `X-Bapi-Limit-Reset-Timestamp`(없으면 5초)까지 막고 읽기 요청을 재시도합니다 (#11)
- Bitget 구독·해제·재구독 요청을 4096바이트 이하 메시지로 나눠 보냅니다 (#7)
- Bitget `history-candles` limiter를 거래소 한도인 초당 5회로 낮추었습니다 (#10)
- Upbit 시세 그룹 기본 limiter를 초당 10회에서 8회로 낮춰 창 경계 429를 피합니다 (#8)

### Fixed

- Bitget `KlineStreamArgument`가 interval 설정 전에 검증해 항상 실패하던 문제 (#3)
- Bitget WebSocket 오류 프레임의 숫자 `code`를 decode하지 못해 무한 재연결하던 문제 (#4)
- Bybit WebSocket symbol 검증이 날짜 만기 선물(`BTCUSDT-27NOV26`)을 거부하던 문제 (#5)
- Coinone이 한 글자 티커(`A`, `C`, `G`, `S`, `W`, `T`)를 거부하던 문제 (#6)

## 1.0.3

### Added

- OKX `HistoryCandles`(`/api/v5/market/history-candles`)와 1초 캔들
- Bitget `HistoryCandles`(`/api/v3/market/history-candles`)
- Kraken `PublicTrade.Unified`와 공통 `unified.TradeCandleBuilder`로 체결 이력 기반 과거 캔들 합성
- Gate.io 공식 과거 데이터 덤프를 읽는 `exchange/gateio/archive` 패키지
- 본문을 버퍼링하지 않는 읽기 전용 `Executor.Stream`

## 1.0.2

### Breaking

- 공통 주문 생성은 안전한 사후 조정을 위해 `ClientOrderID`를 필수로 요구합니다.
- 시장가 매수의 견적 자산 주문금액은 `Order.Quantity`가 아니라 `Order.QuoteAmount`로 반환합니다.
- 상태를 확정하지 않는 생성·취소 응답은 `new`·`canceled` 대신 `acknowledged`·`cancel_pending`으로 반환합니다.

### Added

- 공통 주문의 `QuoteAmount`, `acknowledged`, `cancel_pending` 의미
- `MarketInfo` 주문 단위·최소 주문값과 exact-decimal `ValidateOrder`
- 분산 rate-limit backend 계약과 REST 실행 관측 훅
- 읽기 REST의 제한적 재시도와 WebSocket decode 재연결 복구
- Binance USDⓈ-M algo 조건부 주문 endpoint

### Changed

- 로컬 rate limiter는 wall-clock fixed window 대신 rolling window를 사용합니다.
- 실계정 live smoke 전 거래소 상품은 지원 매트릭스에서 `experimental`로 표시합니다.
- REST redirect를 거부하고 전송 오류 문자열에서 서명 URL을 제거합니다.

### Security

- redirect를 통한 거래소 인증 헤더의 다른 origin 전달을 차단했습니다.
- 인증·전송 오류의 원본 URL과 credential-provider 오류가 기본 오류 문자열에 노출되지 않게 했습니다.

## 1.0.1

- 초기 다중 거래소 REST·WebSocket 어댑터와 문서 보완.

## 1.0.0

- 최초 공개 버전.
