# 송신 IP 선택 사용법

## 한 줄 요약

IP를 직접 요청에 넣는 방식이 아닙니다. 시작할 때 `route ID`와 `LocalSourceIP`를 연결해 등록하고, 요청할 때 사용할 `route ID`를 선택합니다.

```text
"seoul-a" 선택 → 10.0.10.21에 소켓 bind → 외부에서는 203.0.113.10으로 관측
"seoul-b" 선택 → 10.0.10.22에 소켓 bind → 외부에서는 203.0.113.11로 관측
```

- 기본 IP 선택: 거래소 클라이언트의 `DefaultEgressRouteID`
- REST 요청 한 건만 변경: `trade.WithEgressRoute("route-id")`
- WebSocket 연결 변경: stream 생성 시 `trade.WithEgressRoute("route-id")`

## 1. 등록할 IP 정하기

SDK가 사용하는 값은 다음 세 개입니다.

| 값 | 용도 | 예시 |
|---|---|---|
| `ID` | 코드에서 IP 대신 선택할 이름 | `seoul-a` |
| `LocalSourceIP` | 현재 서버 OS에 실제로 할당된 IPv4. 소켓 bind에 사용 | `10.0.10.21` |
| `ExpectedPublicIP` | 거래소나 외부 서비스에서 보여야 하는 공인 IPv4 | `203.0.113.10` |

배포 환경에 따라 두 IP의 관계가 다릅니다.

| 환경 | `LocalSourceIP` | `ExpectedPublicIP` |
|---|---|---|
| AWS secondary private IP → EIP | 서버에 보이는 사설 IP | 연결된 EIP |
| 서버에 public IP 직접 할당 | 서버에 보이는 public IP | 같은 public IP |

예를 들어 AWS에서 `10.0.10.21`이 `203.0.113.10` EIP로 NAT된다면 다음과 같이 등록합니다.

```go
transport.EgressRoute{
	ID:               "seoul-a",
	LocalSourceIP:    net.ParseIP("10.0.10.21"),
	ExpectedPublicIP: net.ParseIP("203.0.113.10"),
}
```

`LocalSourceIP`에는 EIP가 아니라 `ip -4 addr` 명령으로 서버에서 확인되는 주소를 넣어야 합니다. SDK는 IP를 서버에 할당하거나 라우팅을 설정하지 않습니다.

## 2. IP가 제대로 나가는지 먼저 확인하기

애플리케이션을 실행하기 전에 각 route를 진단합니다.

```bash
ip -4 addr

go run ./cmd/egressdiag \
  -route seoul-a,10.0.10.21,203.0.113.10 \
  -route seoul-b,10.0.10.22,203.0.113.11
```

`-route` 형식은 아래 순서이며 여러 번 지정할 수 있습니다.

```text
route-id,local-source-ip,expected-public-ip
```

성공 결과에서 route별로 `matchesExpected`가 `true`인지 확인합니다.

```json
[
  {
    "routeId": "seoul-a",
    "localSourceIp": "10.0.10.21",
    "expectedPublicIp": "203.0.113.10",
    "observedPublicIp": "203.0.113.10",
    "matchesExpected": true,
    "checkedAt": "2026-08-27T01:23:45Z"
  }
]
```

기본 확인 주소는 `https://api.ipify.org`입니다. 자체 확인 서버를 쓸 때는 `-endpoint`를 추가합니다.

## 3. Registry와 거래소 클라이언트 만들기

아래 예시는 `seoul-a`를 기본 IP로 사용하는 Binance Spot REST 클라이언트입니다.

```go
package main

import (
	"context"
	"fmt"
	"net"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/exchange/binance"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

func main() {
	routes := []transport.EgressRoute{
		{
			ID:               "seoul-a",
			LocalSourceIP:    net.ParseIP("10.0.10.21"),
			ExpectedPublicIP: net.ParseIP("203.0.113.10"),
		},
		{
			ID:               "seoul-b",
			LocalSourceIP:    net.ParseIP("10.0.10.22"),
			ExpectedPublicIP: net.ParseIP("203.0.113.11"),
		},
	}

	registry, err := transport.NewRegistry(routes)
	if err != nil {
		panic(err)
	}
	defer registry.Close()

	limiter, err := ratelimit.New()
	if err != nil {
		panic(err)
	}

	executor, err := exchange.NewExecutor(exchange.ExecutorConfig{
		Sender:  registry,
		Limiter: limiter,
	})
	if err != nil {
		panic(err)
	}

	client, err := binance.New(binance.Config{
		Executor:             executor,
		DefaultEgressRouteID: "seoul-a",
	})
	if err != nil {
		panic(err)
	}

	// 옵션이 없으므로 기본 route인 seoul-a를 사용합니다.
	ticker, err := client.TickerPrice(
		context.Background(),
		binance.TickerPriceRequest{Symbol: "BTCUSDT"},
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(ticker)

	// 이 요청 한 건만 seoul-b를 사용합니다.
	ticker, err = client.TickerPrice(
		context.Background(),
		binance.TickerPriceRequest{Symbol: "BTCUSDT"},
		trade.WithEgressRoute("seoul-b"),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(ticker)
}
```

핵심은 다음 두 줄입니다.

```go
DefaultEgressRouteID: "seoul-a"          // 평소 사용할 IP
trade.WithEgressRoute("seoul-b")         // 이 호출만 다른 IP 사용
```

같은 `Registry`를 여러 거래소 클라이언트가 공유해도 됩니다. route마다 HTTP 연결 풀이 분리되므로 서로 다른 송신 IP의 keep-alive 연결이 섞이지 않습니다.

## 4. WebSocket에서 IP 고르기

WebSocket도 stream을 만들 때 같은 옵션을 사용합니다.

```go
connector, err := stream.NewWebSocketConnector(stream.ConnectorConfig{
	HTTPClients: registry,
})
if err != nil {
	panic(err)
}

streamClient, err := binance.NewStreamClient(binance.StreamClientConfig{
	Connector:            connector,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	panic(err)
}

market, err := streamClient.MarketStream(
	binance.MarketStreamRequest{
		Streams:  []string{"btcusdt@trade"},
		TimeUnit: binance.StreamTimeMilliseconds,
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	panic(err)
}
defer market.Close()
```

이 연결은 `seoul-b`에 고정됩니다. 자동 재연결이 일어나도 계속 `seoul-b`를 사용합니다. 이미 열린 WebSocket의 IP를 바꾸려면 기존 stream을 닫고 다른 route로 새 stream을 만들어야 합니다.

거래소마다 stream 생성 메서드와 요청 타입은 다르지만 route 선택 방식은 동일합니다.

## 5. Private API에서 허용 IP 제한하기

private API는 거래소에 등록한 API Key 허용 IP와 SDK의 허용 route를 함께 맞춰야 합니다.

```go
descriptor := credential.Descriptor{
	AccountID:   "main-account",
	Exchange:    model.ExchangeBinance,
	SecretRef:   "secret/binance/main",
	Permissions: []credential.Permission{
		credential.PermissionRead,
		credential.PermissionTrade,
	},
	AllowedEgressRouteIDs: []transport.EgressRouteID{
		"seoul-a",
		"seoul-b",
	},
}
```

`AllowedEgressRouteIDs`에 없는 route를 선택하면 SDK가 Secret을 읽거나 네트워크 요청을 보내기 전에 `credential.ErrEgressRouteNotAllowed`로 거부합니다.

운영 설정은 다음 세 항목을 같은 목록으로 유지하는 것이 안전합니다.

1. 거래소 API Key에 등록한 공인 IP 목록
2. `EgressRoute.ExpectedPublicIP` 목록
3. `credential.Descriptor.AllowedEgressRouteIDs` 목록

## 선택 규칙 정리

| 상황 | 선택 방법 |
|---|---|
| REST 기본 IP | 클라이언트 생성 시 `DefaultEgressRouteID` |
| REST 한 요청만 다른 IP | 호출 마지막 인자에 `trade.WithEgressRoute(...)` |
| WebSocket 기본 IP | stream 클라이언트 생성 시 `DefaultEgressRouteID` |
| WebSocket 연결 하나만 다른 IP | stream 생성 시 `trade.WithEgressRoute(...)` |
| 이미 열린 WebSocket의 IP 변경 | 기존 연결을 닫고 다른 route로 새로 생성 |
| private API Key가 쓸 수 있는 IP 제한 | `AllowedEgressRouteIDs` |

## 자주 발생하는 오류

| 오류 | 원인과 조치 |
|---|---|
| `local address unavailable` | `LocalSourceIP`가 서버 OS에 없습니다. `ip -4 addr`와 컨테이너 네트워크 namespace를 확인합니다. |
| `unknown egress route` | `WithEgressRoute`에 Registry에 등록하지 않은 ID를 넣었습니다. |
| `observed public IP does not match expected public IP` | NAT, 정책 라우팅 또는 `ExpectedPublicIP` 설정이 실제 외부 IP와 다릅니다. |
| `egress route is not allowed for credentials` | 선택한 route가 해당 API Key의 `AllowedEgressRouteIDs`에 없습니다. |
| 연결은 되지만 거래소가 IP를 거부함 | 거래소 API Key의 IP 허용 목록에 `ExpectedPublicIP`를 등록했는지 확인합니다. |

## 운영 시 주의사항

- 현재 IPv4만 지원합니다.
- route ID와 `LocalSourceIP`는 Registry 안에서 각각 중복될 수 없습니다.
- Registry 생성 뒤 route를 동적으로 추가할 수 없습니다. 추가하려면 새 Registry로 교체하거나 프로세스를 재시작합니다.
- 환경변수 HTTP proxy는 route 계약을 깨뜨릴 수 있어 기본 전송에서 사용하지 않습니다.
- 여러 IP를 거래소의 계정·UID 제한이나 이용 정책을 우회하는 용도로 사용하면 안 됩니다.
