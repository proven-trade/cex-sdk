# HTX Spot 구현 계획

## 기준

- 공식 문서: [HTX Spot API](https://huobiapi.github.io/docs/spot/v1/en/)
- REST 기본 호스트: `https://api.huobi.pro`
- AWS 최적화 REST 호스트: `https://api-aws.huobi.pro`
- 일반 시세 WebSocket: `wss://api.huobi.pro/ws`
- MBP 증분 WebSocket: `wss://api.huobi.pro/feed`
- 계정·주문 WebSocket: `wss://api.huobi.pro/ws/v2`
- 상품: Spot

공식 문서는 testnet이 중단되었다고 명시한다. 따라서 mock 서버 기반 자동 테스트를 구현 완료 기준으로 사용하고, 실제 계정과 지정 EIP가 필요한 검증은 지원 매트릭스의 별도 smoke 상태로 관리한다.

## 구현 범위

### 공개 REST

- 서버 시간과 거래 가능 상품
- 현재가와 최우선 호가
- 호가 snapshot
- 최근 체결과 캔들
- 각 호출의 `egressRouteId` 선택과 route별 연결 풀

### private REST

- 현물 계정 탐색과 잔고
- 주문 생성·단건 조회·취소
- 미체결 주문과 주문·체결 이력
- HMAC SHA-256, Base64 서명과 ASCII 순서 쿼리 정규화
- API Key에 허용되지 않은 route를 Secret 조회 전에 거부
- 주문 mutation의 전송 불명확 상태를 `UNKNOWN_EXECUTION_STATE`로 분류

### 공통 Spot API

- `Base`와 `Quote`를 HTX 소문자 결합 심볼로 변환
- 상품 규칙, ticker, order book, candle, 잔고, 주문 계약 정규화
- 거래소 원본 상태와 응답은 민감 정보를 제외하고 보존

### WebSocket

- gzip JSON public 시세 구독과 서버 ping 응답
- v2 private 인증과 주문·잔고 구독
- 재연결 시 같은 EIP 유지와 현재 구독 자동 복구
- MBP 증분의 sequence를 검증하는 로컬 오더북과 같은 EIP REST snapshot 복구

## 구현 순서

1. 공개 REST, 오류 정규화, 요청 제한과 mock 테스트
2. private REST, signer golden vector와 주문 안전 계약
3. 공통 Spot API와 적합성 테스트
4. public/private WebSocket과 gzip·heartbeat 계약
5. MBP 로컬 오더북과 sequence gap 복구
6. 실제 계정 read-only 및 명시적 소액 주문 smoke

각 코드 단계는 전체 formatter, 생성물 검사, 일반·race 테스트, vet, 한글 주석 검사를 통과한 뒤 별도 커밋으로 푸시한다.

## 운영 제약

- API Key는 공식 정책이 허용하는 IP에 바인딩한다.
- SDK의 여러 EIP 경로는 제한 우회가 아니라 키 허용 목록 분리와 가용성 목적으로만 사용한다.
- 지역 제한을 우회하는 endpoint나 프록시 기능은 제공하지 않는다.
- AWS 호스트 사용 여부는 endpoint 설정으로 명시하며, 서명에는 실제 요청 호스트를 사용한다.
- 입출금 실행은 프로젝트 비목표이므로 구현하지 않는다.
