# Changelog

이 프로젝트의 사용자 영향 변경 사항은 이 파일에 기록합니다. 버전은 Semantic Versioning을 따릅니다.

## Unreleased

이번 변경에는 공개 주문 계약과 상태 의미의 호환되지 않는 변경이 포함되므로 다음 릴리스는 v2입니다.

### Breaking

- 공통 주문 생성은 안전한 사후 조정을 위해 `ClientOrderID`를 필수로 요구합니다.
- 시장가 매수의 견적 자산 주문금액은 `Order.Quantity`가 아니라 `Order.QuoteAmount`로 반환합니다.
- 상태를 확정하지 않는 생성·취소 응답은 `new`·`canceled` 대신 `acknowledged`·`cancel_pending`으로 반환합니다.

### Added

- Redis 서버 시계와 Lua 원자적 다중 차감을 사용하는 `ratelimit/redis` backend 및 실제 Redis 통합 테스트
- `unified.NewValidatedSpot`의 route별 마켓 규칙 캐시·주문 전 검증·전체 제한 시간 적용
- v2 모듈 경로와 마이그레이션 가이드, Go 1.27 CI
- 공통 주문의 `QuoteAmount`, `acknowledged`, `cancel_pending` 의미
- `MarketInfo` 주문 단위·최소 주문값과 exact-decimal `ValidateOrder`
- 분산 rate-limit backend 계약과 REST 실행 관측 훅
- 읽기 REST의 제한적 재시도와 WebSocket decode 재연결 복구

### Changed

- Redis 규칙 등록부터 quota 대기와 HTTP 전송까지 같은 요청 deadline을 전달합니다. backend 계약은 `SetRuleContext`로 요청 취소와 제한 시간을 받습니다.
- 마켓 규칙의 공유 조회를 시작한 호출자가 취소되면 다른 유효한 호출자는 메타데이터를 다시 조회하며 주문 제출은 재시도하지 않습니다.
- 모듈·내부 import 경로를 `github.com/proven-trade/cex-sdk/v2`로 전환했습니다.
- REST 관측 훅이 HTTP 오류와 거래소 envelope·주문 항목의 오류 분류를 기록합니다.
- Gate.io·MEXC·HTX 공통 마켓 목록은 표현할 수 없는 비ASCII·괄호 등 지원 문법 밖의 심볼을 제외하고, Crypto.com은 외부시장 접미사가 붙은 상품을 제외합니다. native 목록은 보존합니다.
- HTX 공통 응답의 지수 표기 decimal을 정확한 일반 소수로 변환하고, 빗썸 공통 호가의 수량 0 레벨을 제외합니다.
- Crypto.com 공개 체결의 소문자 방향 응답을 지원합니다.
- HTX의 자산 이름 변경으로 일치하지 않는 상장폐지 심볼을 공통 마켓 목록에서 제외합니다.
- 로컬 rate limiter는 wall-clock fixed window 대신 rolling window를 사용합니다.
- 실계정 live smoke 전 거래소 상품은 지원 매트릭스에서 `experimental`로 표시합니다.
- REST redirect를 거부하고 전송 오류 문자열에서 서명 URL을 제거합니다.

### Security

- 간접 의존성 `golang.org/x/sys`를 GO-2026-5024 수정 버전인 v0.44.0으로 올렸습니다.
- redirect를 통한 거래소 인증 헤더의 다른 origin 전달을 차단했습니다.
- 인증·전송 오류의 원본 URL과 credential-provider 오류가 기본 오류 문자열에 노출되지 않게 했습니다.

## 1.0.1

- 초기 다중 거래소 REST·WebSocket 어댑터와 문서 보완.

## 1.0.0

- 최초 공개 버전.
