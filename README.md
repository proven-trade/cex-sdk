# Proven Trade SDK

여러 중앙화 거래소(CEX)의 REST/WebSocket API를 하나의 일관된 인터페이스로 제공하고, 요청별로 지정한 AWS Elastic IP를 통해 통신할 수 있게 하는 SDK 프로젝트입니다.

현재 단계는 기획 및 아키텍처 확정 단계입니다.

## 문서

- [프로젝트 기획서](docs/PROJECT_PLAN.md)

## 현재 제안 기준

- 1차 거래소: Binance, Bitget, Upbit
- 구현 언어: Go
- 네트워크: 단일 ENI의 여러 secondary private IPv4와 EIP 1:1 연결
- IP 선택: 클라이언트 기본값과 요청별 `egressRouteId` 재정의

구현을 시작하기 전에 [기획서의 남은 오픈 이슈](docs/PROJECT_PLAN.md#20-구현-전-확정할-사항)를 확정합니다.
