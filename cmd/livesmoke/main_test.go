package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/proven-trade/proven-trade-sdk/credential"
	coreexchange "github.com/proven-trade/proven-trade-sdk/exchange"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/ratelimit"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

type testSender struct{}

func (testSender) Do(
	context.Context,
	transport.EgressRouteID,
	*http.Request,
) (*http.Response, error) {
	return nil, errors.New("test sender must not be called")
}

func TestDecodeConfigResolvesPublicSmoke(t *testing.T) {
	t.Parallel()

	config, err := decodeConfig(strings.NewReader(`{
		"exchange":"binance",
		"routes":[
			{"id":"seoul-a","localPrivateIp":"10.0.10.21","expectedPublicIp":"203.0.113.10"},
			{"id":"seoul-b","localPrivateIp":"10.0.10.22","expectedPublicIp":"203.0.113.11"}
		],
		"egressRouteId":"seoul-b",
		"market":{"base":"BTC","quote":"USDT"},
		"publicIpEndpoint":"https://ip.example.test",
		"checkTimeout":"3s",
		"includeBalances":false
	}`), func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("decodeConfig() error = %v", err)
	}
	if config.exchange != model.ExchangeBinance || config.routeID != "seoul-b" ||
		config.market.Base != "BTC" || config.market.Quote != "USDT" ||
		config.publicIPEndpoint != "https://ip.example.test" ||
		config.checkTimeout != 3*time.Second || config.includeBalances ||
		config.descriptor != nil || config.provider != nil || len(config.routes) != 2 {
		t.Fatalf("config = %+v", config)
	}
	if config.routes[1].LocalPrivateIP.String() != "10.0.10.22" ||
		config.routes[1].ExpectedPublicIP.String() != "203.0.113.11" {
		t.Fatalf("routes = %+v", config.routes)
	}
}

func TestDecodeConfigBuildsEnvironmentCredentialProvider(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"OKX_API_KEY": "api-key", "OKX_SECRET_KEY": "secret-key",
		"OKX_PASSPHRASE": "passphrase",
	}
	lookup := func(name string) (string, bool) {
		value, exists := values[name]
		return value, exists
	}
	config, err := decodeConfig(strings.NewReader(`{
		"exchange":"okx",
		"routes":[{"id":"seoul-a","localPrivateIp":"10.0.10.21","expectedPublicIp":"203.0.113.10"}],
		"egressRouteId":"seoul-a",
		"market":{"base":"BTC","quote":"USDT"},
		"includeBalances":true,
		"credentials":{
			"accountId":"operations-read",
			"apiKeyEnv":"OKX_API_KEY",
			"secretKeyEnv":"OKX_SECRET_KEY",
			"passphraseEnv":"OKX_PASSPHRASE"
		}
	}`), lookup)
	if err != nil {
		t.Fatalf("decodeConfig() error = %v", err)
	}
	if config.descriptor == nil || config.descriptor.Exchange != model.ExchangeOKX ||
		config.descriptor.AccountID != "operations-read" ||
		!slices.Equal(config.descriptor.Permissions, []credential.Permission{credential.PermissionRead}) ||
		!slices.Equal(
			config.descriptor.AllowedEgressRouteIDs,
			[]transport.EgressRouteID{"seoul-a"},
		) {
		t.Fatalf("descriptor = %+v", config.descriptor)
	}
	material, err := config.provider.Resolve(context.Background(), config.descriptor.SecretRef)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if string(material.APIKey) != "api-key" || string(material.SecretKey) != "secret-key" ||
		string(material.Passphrase) != "passphrase" {
		t.Fatalf("material fields were not resolved")
	}
	material.Destroy()
	if !allZero(material.APIKey) || !allZero(material.SecretKey) || !allZero(material.Passphrase) {
		t.Fatal("Destroy() did not clear material")
	}
}

func TestDecodeConfigRejectsUnsafeCredentialAndUnknownFields(t *testing.T) {
	t.Parallel()

	base := `{
		"exchange":"%s",
		"routes":[{"id":"seoul-a","localPrivateIp":"10.0.10.21","expectedPublicIp":"203.0.113.10"}],
		"egressRouteId":"seoul-a",
		"market":{"base":"BTC","quote":"USDT"},
		%s
	}`
	tests := []struct {
		name     string
		exchange string
		body     string
	}{
		{
			name: "public 자격증명 설정", exchange: "binance",
			body: `"includeBalances":false,"credentials":{"accountId":"a","apiKeyEnv":"A","secretKeyEnv":"B"}`,
		},
		{
			name: "passphrase 누락", exchange: "kucoin",
			body: `"includeBalances":true,"credentials":{"accountId":"a","apiKeyEnv":"A","secretKeyEnv":"B"}`,
		},
		{
			name: "같은 환경변수", exchange: "binance",
			body: `"includeBalances":true,"credentials":{"accountId":"a","apiKeyEnv":"KEY","secretKeyEnv":"KEY"}`,
		},
		{
			name: "평문 필드", exchange: "binance",
			body: `"includeBalances":false,"apiKey":"plain-secret"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := strings.NewReader(fmt.Sprintf(base, test.exchange, test.body))
			if _, err := decodeConfig(input, func(string) (string, bool) { return "", false }); err == nil {
				t.Fatal("decodeConfig() error = nil")
			}
		})
	}
}

func TestDecodeConfigRejectsInvalidRouteAndTrailingJSON(t *testing.T) {
	t.Parallel()

	invalidRoute := `{
		"exchange":"binance",
		"routes":[{"id":"seoul-a","localPrivateIp":"203.0.113.20","expectedPublicIp":"10.0.0.10"}],
		"egressRouteId":"seoul-a",
		"market":{"base":"BTC","quote":"USDT"},
		"includeBalances":false
	}`
	if _, err := decodeConfig(
		strings.NewReader(invalidRoute), func(string) (string, bool) { return "", false },
	); err == nil {
		t.Fatal("decodeConfig() accepted invalid route addresses")
	}
	valid := `{
		"exchange":"binance",
		"routes":[{"id":"seoul-a","localPrivateIp":"10.0.10.21","expectedPublicIp":"203.0.113.10"}],
		"egressRouteId":"seoul-a",
		"market":{"base":"BTC","quote":"USDT"},
		"includeBalances":false
	}`
	if _, err := decodeConfig(
		strings.NewReader(valid+` {}`), func(string) (string, bool) { return "", false },
	); err == nil {
		t.Fatal("decodeConfig() accepted trailing JSON")
	}
}

func TestBuildSpotClientSupportsEveryUnifiedExchange(t *testing.T) {
	t.Parallel()

	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := coreexchange.NewExecutor(coreexchange.ExecutorConfig{
		Sender: testSender{}, Limiter: limiter,
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	exchanges := []model.ExchangeID{
		model.ExchangeBinance, model.ExchangeBitget, model.ExchangeUpbit,
		model.ExchangeBybit, model.ExchangeOKX, model.ExchangeCoinbase,
		model.ExchangeKraken, model.ExchangeBithumb, model.ExchangeCoinone,
		model.ExchangeKorbit, model.ExchangeKuCoin, model.ExchangeGateIO,
		model.ExchangeCryptoCom, model.ExchangeMEXC, model.ExchangeHTX,
	}
	for _, exchangeID := range exchanges {
		client, err := buildSpotClient(exchangeID, executor, "route-a", nil, nil)
		if err != nil {
			t.Fatalf("buildSpotClient(%q) error = %v", exchangeID, err)
		}
		if client.Exchange() != exchangeID {
			t.Fatalf("buildSpotClient(%q).Exchange() = %q", exchangeID, client.Exchange())
		}
	}
	if _, err := buildSpotClient("unknown", executor, "route-a", nil, nil); err == nil {
		t.Fatal("buildSpotClient() accepted unknown exchange")
	}
}

func TestEnvironmentProviderRejectsMissingAndUnknownSecrets(t *testing.T) {
	t.Parallel()

	provider := &environmentProvider{
		secretRef: "expected", apiKeyEnv: "API_KEY", secretKeyEnv: "SECRET_KEY",
		lookup: func(name string) (string, bool) {
			if name == "API_KEY" {
				return "api-key", true
			}
			return "", false
		},
	}
	if _, err := provider.Resolve(context.Background(), "unknown"); err == nil {
		t.Fatal("Resolve() accepted unknown Secret reference")
	}
	if _, err := provider.Resolve(context.Background(), "expected"); err == nil ||
		!strings.Contains(err.Error(), "SECRET_KEY") {
		t.Fatalf("Resolve() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Resolve(canceled, "expected"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve(canceled) error = %v", err)
	}
}

func TestRunRequiresConfigWithoutWritingJSON(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(context.Background(), nil, &stdout, &stderr)
	if err == nil || stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("run() stdout = %q, stderr = %q, error = %v", stdout.String(), stderr.String(), err)
	}
	if err := run(nil, nil, io.Discard, io.Discard); err == nil {
		t.Fatal("run(nil) error = nil")
	}
}

func TestExampleConfigurationsRemainValid(t *testing.T) {
	t.Parallel()

	lookup := func(name string) (string, bool) {
		values := map[string]string{
			"PROVEN_BINANCE_API_KEY":    "api-key",
			"PROVEN_BINANCE_SECRET_KEY": "secret-key",
		}
		value, exists := values[name]
		return value, exists
	}
	for _, path := range []string{
		"../../examples/live-smoke/public.example.json",
		"../../examples/live-smoke/private-read.example.json",
	} {
		config, err := readConfig(path, lookup)
		if err != nil {
			t.Fatalf("readConfig(%q) error = %v", path, err)
		}
		if config.exchange != model.ExchangeBinance || config.routeID != "seoul-b" {
			t.Fatalf("readConfig(%q) = %+v", path, config)
		}
	}
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}
