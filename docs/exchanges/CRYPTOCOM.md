# Crypto.com Exchange v1 Spot 구현 계획

## 기준

- 공식 문서: [Crypto.com Exchange API v1](https://exchange-developer.crypto.com/exchange/v1)
- Production REST: `https://api.crypto.com/exchange/v1/{method}`
- UAT REST: `https://uat-api.3ona.co/exchange/v1/{method}`
- Production market WebSocket: `wss://stream.crypto.com/exchange/v1/market`
- Production user WebSocket: `wss://stream.crypto.com/exchange/v1/user`
- UAT market WebSocket: `wss://uat-stream.3ona.co/exchange/v1/market`
- UAT user WebSocket: `wss://uat-stream.3ona.co/exchange/v1/user`
- 초기 상품: Spot

공식 2026년 변경 로그가 유지되는 현행 Exchange v1만 대상으로 한다. 구형 기본 `book.{instrument_name}` 구독과 100ms full snapshot 구독은 이미 폐기됐으므로 구현하지 않는다. Margin·Derivatives와 고급 조건부 주문은 native 타입이 안정된 뒤 별도 상품 단계로 확장한다.

## 구현 범위

### 공개 REST

- `public/get-instruments`의 Spot 상품, 거래 가능 상태, 가격·수량 tick 규칙
- `public/get-ticker`의 단일·전체 ticker
- `public/get-book`의 최대 50단계 호가 snapshot
- `public/get-trades`의 최근 체결
- `public/get-candlestick`의 공식 timeframe 캔들
- 요청별 `egressRouteId`와 route별 독립 HTTP 연결 풀
- public 메서드별 IP 기준 100회/초 제한과 HTTP 429·`42901` 보정

공식 공통 규격은 숫자 필드를 문자열로 보내도록 요구한다. 가격·수량·금액은 decimal 원문을 보존하고, 식별자·millisecond·nanosecond 시각은 JSON 문자열과 숫자를 모두 안전하게 해석하되 범위를 넘는 값을 `float64`로 변환하지 않는다.

### private REST

- `private/user-balance`의 통화별 가용·예약 잔고
- `private/create-order`의 Spot LIMIT·MARKET, GTC·IOC·FOK와 POST_ONLY
- `private/get-order-detail`, `private/cancel-order`
- `private/get-open-orders`, `private/get-order-history`, `private/get-trades`
- API Key IP whitelist와 SDK credential route 허용 목록의 사전 일치 검사
- 전송 불명확 mutation을 `UNKNOWN_EXECUTION_STATE`로 분류하고 자동 재전송 금지

private 요청은 `method + id + api_key + paramsString + nonce`를 Secret Key로 HMAC SHA-256하고 소문자 hex 서명을 만든다. `paramsString`은 객체 key를 재귀적으로 정렬하고 배열 순서를 유지해 연결한다. signer golden vector는 중첩 객체·배열·빈 params·decimal 문자열을 포함하며, 서명 뒤 payload를 변경하지 않는다.

공식 제한 단위를 그대로 분리한다. 주문 생성·취소는 메서드별 API Key 기준 15회/100ms, 주문 상세는 30회/100ms, 체결·주문 이력은 각각 1회/초, 나머지 private 메서드는 각각 3회/100ms다. 허용 route와 `read`·`trade` 권한을 Secret 조회 전에 확인하고 사용한 민감 byte slice는 즉시 덮어쓴다.

주문 응답은 matching engine의 최종 승인이 아니라 비동기 접수다. `client_oid`를 필수 안전 식별자로 사용하고 주문 상세 또는 `user.order`로 `PENDING` 이후의 `ACTIVE`·`FILLED`·`CANCELED`·`REJECTED` 상태를 확인한다. 취소도 접수 응답만으로 최종 취소로 단정하지 않는다.

### 공통 Spot API

- `Base`·`Quote`와 underscore 형식 Spot `instrument_name`의 양방향 변환
- 상품 규칙, ticker, order book, 체결, candle, 잔고와 주문 계약 정규화
- MARKET 매수·매도 수량 의미와 지원 time-in-force를 명시적으로 검증
- 원본 응답과 미래 필드는 민감 정보를 제외하고 보존
- 공통 적합성 스위트와 모든 요청의 EIP 전달 검증

### WebSocket

- market의 `ticker`, `trade`, `candlestick`, 명시적 `book.{instrument_name}.{depth}` 구독
- user의 `public/auth` 후 `user.order`, `user.trade`, `user.balance` 구독
- `public/heartbeat`의 동일 ID를 `public/respond-heartbeat`로 5초 안에 응답
- 연결 직후 비례 요청 제한을 피하기 위한 공식 권장 1초 준비 구간
- market 100회/초, user 150회/초의 연결별 command 제한
- 동적 구독·해지의 성공 응답 확정과 실패 rollback
- 재연결마다 같은 EIP 유지, 새 nonce·서명 인증과 승인된 구독 복구

private user 연결은 API Key whitelist route와 `read` 권한을 연결 전에 검사한다. 주문 command를 WebSocket으로 보내는 기능은 첫 단계에서 제외하고 REST mutation과 user event 조합을 먼저 안정화한다.

### 로컬 오더북

현행 명시적 10·50단계 `SNAPSHOT_AND_UPDATE`를 사용한다. 최초 `book` full snapshot의 `u`를 기준으로 `book.update`의 `pu`가 직전 `u`와 같은지 검사하고, 수량 0은 해당 가격을 삭제한다. 서버가 큰 delta 대신 full snapshot을 보내면 기존 장부를 원자적으로 교체한다.

`pu` gap, update ID 역행, snapshot 이전 delta 또는 연결 세대 변경이 발생하면 불완전한 view를 공개하지 않고 같은 EIP route로 재구독해 새 full snapshot부터 복구한다. 공식 REST book에는 WebSocket `u`와 연결할 sequence가 없으므로 임의로 결합하지 않는다. 빈 delta heartbeat도 유효한 `u`·`pu` 연결로 처리한다.

## 구현 순서

1. 공개 REST, 오류 정규화, 요청 제한과 mock 테스트
2. private REST, signer golden vector와 주문 안전 계약
3. 공통 Spot API와 적합성 테스트
4. public market WebSocket과 heartbeat·동적 구독
5. private user WebSocket 인증과 주문·체결·잔고 구독
6. 10·50단계 로컬 오더북과 sequence gap 복구
7. UAT·production read-only 및 명시적 소액 주문 smoke

각 단계는 Go formatter, 생성물 검사, 일반·race 테스트, vet, 한글 주석 검사를 통과한 뒤 별도 커밋으로 푸시한다.

## 운영 제약

- API Key는 공식 IP whitelist에 실제 EIP를 등록하고 SDK route 허용 목록과 일치시킨다.
- 여러 EIP는 키 분리와 가용성을 위해 사용하며 거래소 요청 제한이나 지역 정책을 우회하지 않는다.
- Production과 UAT endpoint·credential은 혼용하지 않는다.
- 지역 제한 우회용 도메인이나 proxy 기능은 제공하지 않는다.
- 입출금, 내부 이체, staking 실행은 프로젝트 비목표에 따라 구현하지 않는다.
