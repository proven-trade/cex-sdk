# 2026-09-14 로컬 공개 Spot smoke

15개 거래소의 공개 읽기 smoke가 모두 통과했습니다. 전체 90개 검사(송신 IP 확인, 마켓, 현재가, 호가, 최근 체결, 1분 캔들)가 성공했습니다.

- 대상 코드: `092ec4f8195c0d4c7bc854d3d6f817800107124d`
- 실행 환경: macOS arm64, Go 1.27.1, 실제 `en0` 원본 IP를 bind한 단일 로컬 route
- 실행 시작: `2026-09-14T13:14:04.876151Z`
- 실행 종료: `2026-09-14T13:14:10.95577Z`
- 빌드 정보: `vcs.modified=false`
- [기계 판독용 실행 증적](2026-09-14-public-smoke.json)

원본 설정·증적은 로컬에 보관하고, 공개 저장소의 증적에서는 local source IP·expected/observed public IP를 제거했습니다. IP 기대값은 같은 인터페이스로 사전 관측한 값을 사용하고 실제 runner에서도 다시 확인했습니다. 이는 로컬 연결의 일관성 검증이며 배포 인프라의 사전 확정된 공인 IP·다중 송신 경로 검증을 대체하지 않습니다.

| 거래소 | 마켓 | 검사 |
|---|---|---|
| binance | BTC/USDT | 6/6 |
| bitget | BTC/USDT | 6/6 |
| upbit | BTC/KRW | 6/6 |
| bybit | BTC/USDT | 6/6 |
| okx | BTC/USDT | 6/6 |
| coinbase | BTC/USD | 6/6 |
| kraken | BTC/USD | 6/6 |
| bithumb | BTC/KRW | 6/6 |
| coinone | BTC/KRW | 6/6 |
| korbit | BTC/KRW | 6/6 |
| kucoin | BTC/USDT | 6/6 |
| gateio | BTC/USDT | 6/6 |
| mexc | BTC/USDT | 6/6 |
| htx | BTC/USDT | 6/6 |
| cryptocom | BTC/USDT | 6/6 |

## 실제 응답으로 발견한 수정 사항

| 거래소 | 발견 내용 | 반영 |
|---|---|---|
| Bithumb | REST 호가에 수량 0 레벨이 포함됨 | 공통 호가에서 제외한 뒤 요청 depth 적용 |
| Gate.io | 비ASCII 상품 하나로 공통 마켓 전체 조회가 실패함 | 공통 계약으로 표현할 수 없는 상품만 제외 |
| MEXC | 비ASCII·괄호 포함 심볼이 있음 | native 지원 문법 밖의 상품을 공통 목록에서 제외 |
| HTX | 지수 표기 수량과 자산명이 바뀐 offline 심볼이 있음 | 정확한 일반 소수 변환, 비표현 상품·이전 이름의 offline 상품 제외 |
| Crypto.com | 외부시장 접미사 상품과 소문자 공개 체결 방향이 있음 | 외부시장 상품 제외, REST·stream 방향 표기 정규화 |

각 응답 형태는 네트워크 없이 실행하는 회귀 테스트에도 포함했습니다. native 상품 목록과 원본 응답은 보존합니다.

## 검증 범위

이 증적은 **로컬 공개 REST 검증**입니다. 계정 자격증명이 제공되지 않아 잔고·주문·취소는 실행하지 않았으며, WebSocket·선물·장시간 soak·배포 대상의 다중 IP 조합도 이 실행의 범위에 포함되지 않습니다. 지원 매트릭스의 `experimental`과 실계정 smoke 대기 상태는 유지합니다.

실제 배포 서버에서 [live smoke 실행 방법](../LIVE_SMOKE.md)에 따라 설정 파일의 source IP·expected public IP를 구성하고 같은 바이너리를 다시 실행해야 해당 환경의 증적을 확보할 수 있습니다.
