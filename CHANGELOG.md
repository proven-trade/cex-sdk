# Changelog

이 프로젝트의 사용자 영향 변경 사항은 이 파일에 기록합니다. 버전은 Semantic Versioning을 따릅니다.

## Unreleased

### Added

- `credential.PermissionTransfer`(내부 이체·서브계정·입금 주소 생성)와 `credential.PermissionWithdraw`(외부 출금). 출금 API는 Descriptor에 `PermissionWithdraw`를 명시한 키만 호출할 수 있습니다
- Korbit: 자산 목록·공지·시장 경보, 수수료 정책·API 키·계정 목록, 자전거래 방지 체결, 코인·원화 입출금, 계정 간 이전 API와 `Config.AssetRequestsPerSecond`
- Coinone: 호가 단위·단일 마켓·전체/UTC 티커·통화 정보, 잔고·거래 수수료·주문 리워드, 전체 취소, 입금 주소·입출금 내역·출금 한도·주소록·출금 API
- `binance/archive`: Spot·USDⓈ-M `AggTrades`·`Trades`, USDⓈ-M `MarkPriceKlines`·`IndexPriceKlines`·`PremiumIndexKlines` 월·일 덤프
- Binance Spot: 집계·과거·블록 체결, UI 캔들, 평균가, 24시간·거래일·롤링 티커, 실행 규칙·참조가, 테스트 주문·cancelReplace·keepPriority 정정·전체 취소, OCO/OTO/OTOCO/OPO/OPOCO 주문 리스트, SOR, 체결·할당·자전거래 방지·수수료·주문 한도·정정 이력·필터 조회 API와 [REST 문서](docs/exchanges/BINANCE.md)
- Binance USDⓈ-M: 집계·과거 체결, 연속계약·지수가·마크가·프리미엄 캔들, 마크가·펀딩비 이력·펀딩 정보, 24시간·최우선 호가, 미결제약정과 `/futures/data` 통계, 복합지수·자산 지수·구성 거래소, 보험기금·ADL 위험·RPI 호가·거래 세션 일정 시세 API
- Binance USDⓈ-M: 테스트 주문·일괄 주문/정정/취소·전체 취소·카운트다운 취소·Algo 전체 취소·정정 이력, 레버리지·포지션 모드·멀티에셋·격리 마진, 체결·강제청산·ADL 분위, v3 잔고·계정/심볼 설정·수입·수수료율·레버리지 구간·주문 한도·거래 상태·BNB 수수료 차감·비동기 다운로드·PM 계정, Convert API
- Bybit: 주문 정정·전체 취소·일괄 생성/정정/취소·DCP·Spot 차입 확인, 레버리지·마진/포지션 모드·TP/SL·자동 마진·마진 추가·실현 손익·체결·포지션 이전, 주문 생성 TP/SL·트리거·SMP 필드
- HTX: 24시간 요약·시장 상태·심볼/통화/체인 설정, 일괄 주문·조건별/ID별 일괄 취소·dead-man·최근 주문 이력·수수료율·조건부 주문, 자산 평가·변동 이력·원장·계정/선물/포인트 이체·UID·API 키 API
- OKX: 일괄 주문/취소/정정·정정·dead-man·사전 점검·포지션 청산·체결 이력, Algo 주문, 계정 설정·포지션 모드·레버리지·최대 수량·마진 조정·수수료·bills·포지션 이력·위험, 마크가·펀딩비·미결제약정·가격 제한·전체 호가·지수/마크 캔들·포지션 티어·보험기금, 자산·이체·입출금·Convert, 서브계정 API
- Kraken Futures: 주문 편집·일괄·전체 취소·dead-man, 자전거래 전략·레버리지/PnL 설정, 상품 상태·펀딩 이력·시장/계정 이력·차트 분석, 언와인드 큐·공지·API 키 확인·이체·서브계정·포트폴리오 마진 API. Derivatives WebSocket v1 종료일(2027-03-01) 안내
- Bybit: 마크/지수/프리미엄 캔들·펀딩 이력·미결제약정·보험기금·리스크 한도·정산가·롱숏 비율·변동성, 계정 정보·수수료율·거래 로그·담보·마진 모드, 자산 이체·입출금·Convert, 서브계정·API 키 관리 API
- Bitget(UTA): 서버 시간·RPI 호가·미결제약정·펀딩비·포지션 티어, 일괄 주문/취소·정정·전체 취소·카운트다운·TP/SL·전략 주문, 레버리지·마진/포지션 모드·수수료·재무 기록·설정, 이체·입출금·서브계정·API 키 API
- KuCoin Spot: 통화·심볼·전체 티커·통계·전체 호가·서버 시간/상태, HF 주문 정정·동기·테스트·일괄·전체 취소·dead-man·종료 주문·체결, Stop·OCO 주문, 계정 상세·원장·수수료·API 키·입출금·이체·서브계정 API
- Kraken Spot: 자산·시스템 상태·점검 일정·스프레드·그룹/L3 호가·PreTrade/PostTrade, 주문 정정·편집·일괄 등록/취소·전체 취소·dead-man, 전략 주문, 잔고·체결·포지션·원장·거래량·정정 이력·API 키, 입출금·지갑 이체·서브계정, Earn, 리포트 export API
- Coinbase: `BestBidAsk`, 주문 정정·미리보기·`ClosePosition`, Convert, 거래 요약·키 권한·결제 수단, Portfolio 관리·자금 이동, CFM 잔고·포지션·일중 마진·sweep API
- Upbit: 초·일·주·월·연 캔들, 마켓별 전체 티커·호가 정책, 주문 가능 정보·테스트 주문·다건 조회/취소·전체 취소·취소 후 재주문, 입금·트래블룰·출금, 포켓·통합 이체, 지갑 상태·API 키, WebSocket 구독 목록 조회와 추가 채널
- Bithumb: 일·주·월 캔들, 경보·공지·입출금 수수료, 주문 가능 정보, 다건 주문·조회·취소, TWAP, 입출금 API

### Changed

- Korbit 기본 REST·WebSocket 호스트를 공식 문서 기준 `api.digitalx.miraeasset.com`·`ws-api.digitalx.miraeasset.com`으로 바꾸었습니다. 기존 호스트는 `Config.BaseURL`로 계속 지정할 수 있습니다
- `binance/archive` `Klines`에서 3d·1w·1M 구간 일 파일(`Day>=1`) 요청은 게시되지 않는 파일이므로 요청 전에 `ErrValidation`으로 거절합니다
- Korbit 통화 심볼 1글자를 허용하고, `NOT_FOUND`는 `ErrorOrderNotFound`, `ACCOUNT_SEQ_NOT_ALLOWED`는 `ErrorAuthorization`으로 분류합니다

## 1.1.1

### Fixed

- `binance/archive`가 Binance Spot 한자 심볼(`币安人生USDT` 등) 덤프를 검증 오류로 거부하던 문제 (#14)

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
