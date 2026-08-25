# 거래소 지원 매트릭스

이 문서는 `config/exchange-support.yaml`에서 자동 생성됩니다. 직접 수정하지 않습니다.

`구현`은 코드·자동 테스트·문서가 저장소에 있다는 뜻이며 운영 검증 완료를 뜻하지 않습니다. `읽기 smoke`와 `거래 smoke`가 모두 `구현`이어야 실제 계정과 지정 EIP를 이용한 운영 검증까지 끝난 상태입니다.

| 등급 | 거래소 | 상품 | REST | WS public | WS private | Unified | 자동 테스트 | 읽기 smoke | 거래 smoke | 문서 |
|---|---|---|---|---|---|---|---|---|---|---|
| P0 | Binance | Spot | 구현 | 구현 | 구현 | 구현 | 구현 | 대기 | 대기 | [문서](exchanges/BINANCE_WEBSOCKET.md) |
| P0 | Binance | USDⓈ-M Futures | 구현 | 구현 | 구현 | 해당 없음 | 구현 | 대기 | 대기 | [문서](exchanges/BINANCE_USDM.md) |
| P0 | Bitget | Spot | 구현 | 구현 | 구현 | 구현 | 구현 | 대기 | 대기 | [문서](exchanges/BITGET.md) |
| P0 | Bitget | USDT-M Futures | 구현 | 구현 | 구현 | 해당 없음 | 구현 | 대기 | 대기 | [문서](exchanges/BITGET.md) |
| P0 | Upbit | Spot | 구현 | 구현 | 구현 | 구현 | 구현 | 대기 | 대기 | [문서](exchanges/UPBIT.md) |
| P1 | Bybit | Spot | 구현 | 구현 | 구현 | 구현 | 구현 | 대기 | 대기 | [문서](exchanges/BYBIT.md) |
| P1 | Bybit | Linear Perpetual | 구현 | 구현 | 구현 | 해당 없음 | 구현 | 대기 | 대기 | [문서](exchanges/BYBIT.md) |
| P1 | OKX | Spot | 구현 | 구현 | 구현 | 구현 | 구현 | 대기 | 대기 | [문서](exchanges/OKX.md) |
| P1 | OKX | SWAP | 구현 | 구현 | 구현 | 해당 없음 | 구현 | 대기 | 대기 | [문서](exchanges/OKX.md) |
| P1 | Coinbase Advanced | Spot | 구현 | 구현 | 구현 | 구현 | 구현 | 대기 | 대기 | [문서](exchanges/COINBASE.md) |
| P1 | Kraken | Spot | 구현 | 구현 | 구현 | 구현 | 구현 | 대기 | 대기 | [문서](exchanges/KRAKEN.md) |
| P1 | Kraken | Futures | 구현 | 구현 | 구현 | 해당 없음 | 구현 | 대기 | 대기 | [문서](exchanges/KRAKEN_FUTURES.md) |
| P1 | Bithumb | Spot | 구현 | 구현 | 구현 | 구현 | 구현 | 대기 | 대기 | [문서](exchanges/BITHUMB.md) |
| P1 | Coinone | Spot | 구현 | 구현 | 구현 | 구현 | 구현 | 대기 | 대기 | [문서](exchanges/COINONE.md) |
| P1 | Korbit | Spot | 구현 | 구현 | 구현 | 구현 | 구현 | 대기 | 대기 | [문서](exchanges/KORBIT.md) |
| P2 | KuCoin | Spot | 구현 | 구현 | 구현 | 구현 | 구현 | 대기 | 대기 | [문서](exchanges/KUCOIN.md) |
| P2 | KuCoin | Futures | 구현 | 구현 | 구현 | 해당 없음 | 구현 | 예정 | 예정 | [문서](exchanges/KUCOIN_FUTURES.md) |
| P2 | Gate.io | Spot | 구현 | 구현 | 구현 | 구현 | 구현 | 예정 | 예정 | [문서](exchanges/GATEIO.md) |
| P2 | Gate.io | Futures | 구현 | 구현 | 구현 | 해당 없음 | 구현 | 예정 | 예정 | [문서](exchanges/GATEIO_FUTURES.md) |
| P3 | MEXC | Spot | 구현 | 구현 | 구현 | 구현 | 구현 | 예정 | 예정 | [문서](exchanges/MEXC.md) |
| P4 | HTX | Spot | 구현 | 구현 | 구현 | 구현 | 구현 | 예정 | 예정 | [문서](exchanges/HTX.md) |
| P4 | Crypto.com | Spot | 구현 | 예정 | 예정 | 구현 | 구현 | 예정 | 예정 | [문서](exchanges/CRYPTOCOM.md) |

현재 REST 구현 상품군은 22개이고 계획 상품군은 0개입니다.

상태 의미: `구현`은 저장소 구현 완료, `예정`은 계획됨, `대기`는 외부 환경이나 실제 계정 검증 대기, `해당 없음`은 공통 계약의 대상이 아님을 뜻합니다.
