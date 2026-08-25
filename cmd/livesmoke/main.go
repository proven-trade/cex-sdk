package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/proven-trade/proven-trade-sdk/credential"
	coreexchange "github.com/proven-trade/proven-trade-sdk/exchange"
	binanceexchange "github.com/proven-trade/proven-trade-sdk/exchange/binance"
	bitgetexchange "github.com/proven-trade/proven-trade-sdk/exchange/bitget"
	bithumbexchange "github.com/proven-trade/proven-trade-sdk/exchange/bithumb"
	bybitexchange "github.com/proven-trade/proven-trade-sdk/exchange/bybit"
	coinbaseexchange "github.com/proven-trade/proven-trade-sdk/exchange/coinbase"
	coinoneexchange "github.com/proven-trade/proven-trade-sdk/exchange/coinone"
	cryptocomexchange "github.com/proven-trade/proven-trade-sdk/exchange/cryptocom"
	gateioexchange "github.com/proven-trade/proven-trade-sdk/exchange/gateio"
	korbitexchange "github.com/proven-trade/proven-trade-sdk/exchange/korbit"
	krakenexchange "github.com/proven-trade/proven-trade-sdk/exchange/kraken"
	kucoinexchange "github.com/proven-trade/proven-trade-sdk/exchange/kucoin"
	mexcexchange "github.com/proven-trade/proven-trade-sdk/exchange/mexc"
	okxexchange "github.com/proven-trade/proven-trade-sdk/exchange/okx"
	upbitexchange "github.com/proven-trade/proven-trade-sdk/exchange/upbit"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/ratelimit"
	"github.com/proven-trade/proven-trade-sdk/smoke"
	"github.com/proven-trade/proven-trade-sdk/transport"
	"github.com/proven-trade/proven-trade-sdk/unified"
)

const maxConfigBytes = 1 << 20

var environmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type routeConfig struct {
	ID               string `json:"id"`
	LocalPrivateIP   string `json:"localPrivateIp"`
	ExpectedPublicIP string `json:"expectedPublicIp"`
}

type marketConfig struct {
	Base  string `json:"base"`
	Quote string `json:"quote"`
}

type credentialConfig struct {
	AccountID     string `json:"accountId"`
	APIKeyEnv     string `json:"apiKeyEnv"`
	SecretKeyEnv  string `json:"secretKeyEnv"`
	PassphraseEnv string `json:"passphraseEnv,omitempty"`
}

type fileConfig struct {
	Exchange         model.ExchangeID        `json:"exchange"`
	Routes           []routeConfig           `json:"routes"`
	EgressRouteID    transport.EgressRouteID `json:"egressRouteId"`
	Market           marketConfig            `json:"market"`
	PublicIPEndpoint string                  `json:"publicIpEndpoint,omitempty"`
	CheckTimeout     string                  `json:"checkTimeout,omitempty"`
	IncludeBalances  bool                    `json:"includeBalances"`
	Credentials      *credentialConfig       `json:"credentials,omitempty"`
}

type resolvedConfig struct {
	exchange         model.ExchangeID
	routes           []transport.EgressRoute
	routeID          transport.EgressRouteID
	market           unified.Market
	publicIPEndpoint string
	checkTimeout     time.Duration
	includeBalances  bool
	descriptor       *credential.Descriptor
	provider         credential.Provider
}

type environmentProvider struct {
	secretRef     string
	apiKeyEnv     string
	secretKeyEnv  string
	passphraseEnv string
	lookup        func(string) (string, bool)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "live smoke 실패: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if ctx == nil {
		return fmt.Errorf("live smoke context가 nil입니다")
	}
	flags := flag.NewFlagSet("livesmoke", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "평문 Secret이 없는 live smoke JSON 설정 파일")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("위치 인자는 사용할 수 없습니다")
	}
	if strings.TrimSpace(*configPath) == "" {
		flags.Usage()
		return fmt.Errorf("-config 설정 파일이 필요합니다")
	}
	config, err := readConfig(*configPath, os.LookupEnv)
	if err != nil {
		return err
	}
	registry, err := transport.NewRegistry(config.routes)
	if err != nil {
		return fmt.Errorf("송신 route 레지스트리 생성: %w", err)
	}
	defer registry.Close()
	limiter, err := ratelimit.New()
	if err != nil {
		return fmt.Errorf("요청 제한기 생성: %w", err)
	}
	executor, err := coreexchange.NewExecutor(coreexchange.ExecutorConfig{
		Sender: registry, Limiter: limiter,
	})
	if err != nil {
		return err
	}
	client, err := buildSpotClient(
		config.exchange, executor, config.routeID, config.descriptor, config.provider,
	)
	if err != nil {
		return err
	}
	runner, err := smoke.NewSpotReadRunner(smoke.SpotReadConfig{
		Client: client, EgressVerifier: registry, Market: config.market,
		EgressRouteID: config.routeID, PublicIPEndpoint: config.publicIPEndpoint,
		CheckTimeout: config.checkTimeout, IncludeBalances: config.includeBalances,
	})
	if err != nil {
		return err
	}
	report, runErr := runner.Run(ctx)
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("live smoke JSON 출력: %w", err)
	}
	return runErr
}

func readConfig(
	path string,
	lookup func(string) (string, bool),
) (resolvedConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return resolvedConfig{}, fmt.Errorf("live smoke 설정 열기: %w", err)
	}
	defer file.Close()
	return decodeConfig(file, lookup)
}

func decodeConfig(
	reader io.Reader,
	lookup func(string) (string, bool),
) (resolvedConfig, error) {
	if reader == nil {
		return resolvedConfig{}, fmt.Errorf("live smoke 설정 입력이 nil입니다")
	}
	if lookup == nil {
		return resolvedConfig{}, fmt.Errorf("환경변수 조회 함수가 nil입니다")
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxConfigBytes+1))
	if err != nil {
		return resolvedConfig{}, fmt.Errorf("live smoke 설정 읽기: %w", err)
	}
	if len(body) > maxConfigBytes {
		return resolvedConfig{}, fmt.Errorf("live smoke 설정은 %d바이트를 초과할 수 없습니다", maxConfigBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var config fileConfig
	if err := decoder.Decode(&config); err != nil {
		return resolvedConfig{}, fmt.Errorf("live smoke 설정 해석: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return resolvedConfig{}, fmt.Errorf("live smoke 설정 뒤에 추가 JSON 값이 있습니다")
		}
		return resolvedConfig{}, fmt.Errorf("live smoke 설정 끝 확인: %w", err)
	}
	return config.resolve(lookup)
}

func (config fileConfig) resolve(
	lookup func(string) (string, bool),
) (resolvedConfig, error) {
	if !supportedSpotExchange(config.Exchange) {
		return resolvedConfig{}, fmt.Errorf("지원하지 않는 Spot 거래소 %q", config.Exchange)
	}
	market := unified.Market{
		Base: strings.TrimSpace(config.Market.Base), Quote: strings.TrimSpace(config.Market.Quote),
	}
	if err := market.Validate(); err != nil {
		return resolvedConfig{}, err
	}
	routeID := transport.EgressRouteID(strings.TrimSpace(string(config.EgressRouteID)))
	if routeID == "" {
		return resolvedConfig{}, fmt.Errorf("egressRouteId가 비어 있습니다")
	}
	routes, err := resolveRoutes(config.Routes, routeID)
	if err != nil {
		return resolvedConfig{}, err
	}
	checkTimeout := 10 * time.Second
	if config.CheckTimeout != "" {
		checkTimeout, err = time.ParseDuration(config.CheckTimeout)
		if err != nil || checkTimeout <= 0 {
			return resolvedConfig{}, fmt.Errorf("checkTimeout은 양수 Go duration이어야 합니다")
		}
	}
	publicIPEndpoint := strings.TrimSpace(config.PublicIPEndpoint)
	if publicIPEndpoint == "" {
		publicIPEndpoint = smoke.DefaultPublicIPEndpoint
	}
	result := resolvedConfig{
		exchange: config.Exchange, routes: routes, routeID: routeID, market: market,
		publicIPEndpoint: publicIPEndpoint, checkTimeout: checkTimeout,
		includeBalances: config.IncludeBalances,
	}
	if !config.IncludeBalances {
		if config.Credentials != nil {
			return resolvedConfig{}, fmt.Errorf("public smoke에는 credentials를 설정할 수 없습니다")
		}
		return result, nil
	}
	descriptor, provider, err := resolveCredentials(
		config.Exchange, routeID, config.Credentials, lookup,
	)
	if err != nil {
		return resolvedConfig{}, err
	}
	result.descriptor = descriptor
	result.provider = provider
	return result, nil
}

func resolveRoutes(
	values []routeConfig,
	selected transport.EgressRouteID,
) ([]transport.EgressRoute, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("route를 한 개 이상 설정해야 합니다")
	}
	routes := make([]transport.EgressRoute, 0, len(values))
	seen := make(map[transport.EgressRouteID]struct{}, len(values))
	selectedFound := false
	for index, value := range values {
		id := transport.EgressRouteID(strings.TrimSpace(value.ID))
		if id == "" || id != transport.EgressRouteID(value.ID) {
			return nil, fmt.Errorf("routes[%d] ID가 비어 있거나 앞뒤 공백이 있습니다", index)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("중복 route ID %q", id)
		}
		localIP := net.ParseIP(strings.TrimSpace(value.LocalPrivateIP))
		if localIP == nil || localIP.To4() == nil || !localIP.IsPrivate() {
			return nil, fmt.Errorf("routes[%d] localPrivateIp는 private IPv4여야 합니다", index)
		}
		expectedIP := net.ParseIP(strings.TrimSpace(value.ExpectedPublicIP))
		if expectedIP == nil || expectedIP.To4() == nil || !expectedIP.IsGlobalUnicast() ||
			expectedIP.IsPrivate() {
			return nil, fmt.Errorf("routes[%d] expectedPublicIp는 공인 IPv4여야 합니다", index)
		}
		routes = append(routes, transport.EgressRoute{
			ID: id, LocalPrivateIP: append(net.IP(nil), localIP.To4()...),
			ExpectedPublicIP: append(net.IP(nil), expectedIP.To4()...),
		})
		seen[id] = struct{}{}
		if id == selected {
			selectedFound = true
		}
	}
	if !selectedFound {
		return nil, fmt.Errorf("선택한 egressRouteId %q가 routes에 없습니다", selected)
	}
	return routes, nil
}

func resolveCredentials(
	exchangeID model.ExchangeID,
	routeID transport.EgressRouteID,
	config *credentialConfig,
	lookup func(string) (string, bool),
) (*credential.Descriptor, credential.Provider, error) {
	if config == nil {
		return nil, nil, fmt.Errorf("private 잔고 smoke에는 credentials가 필요합니다")
	}
	accountID := strings.TrimSpace(config.AccountID)
	if accountID == "" {
		return nil, nil, fmt.Errorf("credentials.accountId가 비어 있습니다")
	}
	for name, value := range map[string]string{
		"apiKeyEnv": config.APIKeyEnv, "secretKeyEnv": config.SecretKeyEnv,
	} {
		if !environmentNamePattern.MatchString(value) {
			return nil, nil, fmt.Errorf("credentials.%s가 올바른 환경변수 이름이 아닙니다", name)
		}
	}
	if config.APIKeyEnv == config.SecretKeyEnv {
		return nil, nil, fmt.Errorf("API Key와 Secret은 서로 다른 환경변수를 사용해야 합니다")
	}
	if requiresPassphrase(exchangeID) && !environmentNamePattern.MatchString(config.PassphraseEnv) {
		return nil, nil, fmt.Errorf("%s private smoke에는 passphraseEnv가 필요합니다", exchangeID)
	}
	if config.PassphraseEnv != "" && !environmentNamePattern.MatchString(config.PassphraseEnv) {
		return nil, nil, fmt.Errorf("credentials.passphraseEnv가 올바른 환경변수 이름이 아닙니다")
	}
	if config.PassphraseEnv != "" &&
		(config.PassphraseEnv == config.APIKeyEnv || config.PassphraseEnv == config.SecretKeyEnv) {
		return nil, nil, fmt.Errorf("Passphrase는 API Key·Secret과 다른 환경변수를 사용해야 합니다")
	}
	secretRef := "livesmoke-env:" + string(exchangeID)
	descriptor := &credential.Descriptor{
		AccountID: accountID, Exchange: exchangeID, SecretRef: secretRef,
		Permissions:           []credential.Permission{credential.PermissionRead},
		AllowedEgressRouteIDs: []transport.EgressRouteID{routeID},
	}
	if err := descriptor.Validate(); err != nil {
		return nil, nil, err
	}
	provider := &environmentProvider{
		secretRef: secretRef, apiKeyEnv: config.APIKeyEnv,
		secretKeyEnv: config.SecretKeyEnv, passphraseEnv: config.PassphraseEnv,
		lookup: lookup,
	}
	return descriptor, provider, nil
}

func (provider *environmentProvider) Resolve(
	ctx context.Context,
	secretRef string,
) (credential.Material, error) {
	if ctx == nil {
		return credential.Material{}, fmt.Errorf("자격증명 context가 nil입니다")
	}
	if err := ctx.Err(); err != nil {
		return credential.Material{}, err
	}
	if secretRef != provider.secretRef {
		return credential.Material{}, fmt.Errorf("알 수 없는 Secret 참조입니다")
	}
	apiKey, ok := provider.lookup(provider.apiKeyEnv)
	if !ok || apiKey == "" {
		return credential.Material{}, fmt.Errorf("환경변수 %s가 비어 있습니다", provider.apiKeyEnv)
	}
	secretKey, ok := provider.lookup(provider.secretKeyEnv)
	if !ok || secretKey == "" {
		return credential.Material{}, fmt.Errorf("환경변수 %s가 비어 있습니다", provider.secretKeyEnv)
	}
	material := credential.Material{
		APIKey: []byte(apiKey), SecretKey: []byte(secretKey),
	}
	if provider.passphraseEnv != "" {
		passphrase, exists := provider.lookup(provider.passphraseEnv)
		if !exists || passphrase == "" {
			material.Destroy()
			return credential.Material{}, fmt.Errorf(
				"환경변수 %s가 비어 있습니다", provider.passphraseEnv,
			)
		}
		material.Passphrase = []byte(passphrase)
	}
	return material, nil
}

func buildSpotClient(
	exchangeID model.ExchangeID,
	executor *coreexchange.Executor,
	routeID transport.EgressRouteID,
	descriptor *credential.Descriptor,
	provider credential.Provider,
) (unified.SpotClient, error) {
	switch exchangeID {
	case model.ExchangeBinance:
		client, err := binanceexchange.New(binanceexchange.Config{
			Executor: executor, Credentials: descriptor, CredentialProvider: provider,
			DefaultEgressRouteID: routeID,
		})
		if err != nil {
			return nil, err
		}
		return binanceexchange.NewUnifiedSpot(client)
	case model.ExchangeBitget:
		client, err := bitgetexchange.New(bitgetexchange.Config{
			Executor: executor, Credentials: descriptor, CredentialProvider: provider,
			DefaultEgressRouteID: routeID,
		})
		if err != nil {
			return nil, err
		}
		return bitgetexchange.NewUnifiedSpot(client)
	case model.ExchangeUpbit:
		client, err := upbitexchange.New(upbitexchange.Config{
			Executor: executor, Credentials: descriptor, CredentialProvider: provider,
			DefaultEgressRouteID: routeID,
		})
		if err != nil {
			return nil, err
		}
		return upbitexchange.NewUnifiedSpot(client)
	case model.ExchangeBybit:
		client, err := bybitexchange.New(bybitexchange.Config{
			Executor: executor, Credentials: descriptor, CredentialProvider: provider,
			DefaultEgressRouteID: routeID,
		})
		if err != nil {
			return nil, err
		}
		return bybitexchange.NewUnifiedSpot(client)
	case model.ExchangeOKX:
		client, err := okxexchange.New(okxexchange.Config{
			Executor: executor, Credentials: descriptor, CredentialProvider: provider,
			DefaultEgressRouteID: routeID,
		})
		if err != nil {
			return nil, err
		}
		return okxexchange.NewUnifiedSpot(client)
	case model.ExchangeCoinbase:
		client, err := coinbaseexchange.New(coinbaseexchange.Config{
			Executor: executor, Credentials: descriptor, CredentialProvider: provider,
			DefaultEgressRouteID: routeID,
		})
		if err != nil {
			return nil, err
		}
		return coinbaseexchange.NewUnifiedSpot(client)
	case model.ExchangeKraken:
		client, err := krakenexchange.New(krakenexchange.Config{
			Executor: executor, Credentials: descriptor, CredentialProvider: provider,
			DefaultEgressRouteID: routeID,
		})
		if err != nil {
			return nil, err
		}
		return krakenexchange.NewUnifiedSpot(client)
	case model.ExchangeBithumb:
		client, err := bithumbexchange.New(bithumbexchange.Config{
			Executor: executor, Credentials: descriptor, CredentialProvider: provider,
			DefaultEgressRouteID: routeID,
		})
		if err != nil {
			return nil, err
		}
		return bithumbexchange.NewUnifiedSpot(client)
	case model.ExchangeCoinone:
		client, err := coinoneexchange.New(coinoneexchange.Config{
			Executor: executor, Credentials: descriptor, CredentialProvider: provider,
			DefaultEgressRouteID: routeID,
		})
		if err != nil {
			return nil, err
		}
		return coinoneexchange.NewUnifiedSpot(client)
	case model.ExchangeKorbit:
		client, err := korbitexchange.New(korbitexchange.Config{
			Executor: executor, Credentials: descriptor, CredentialProvider: provider,
			DefaultEgressRouteID: routeID,
		})
		if err != nil {
			return nil, err
		}
		return korbitexchange.NewUnifiedSpot(client)
	case model.ExchangeKuCoin:
		client, err := kucoinexchange.New(kucoinexchange.Config{
			Executor: executor, Credentials: descriptor, CredentialProvider: provider,
			DefaultEgressRouteID: routeID,
		})
		if err != nil {
			return nil, err
		}
		return kucoinexchange.NewUnifiedSpot(client)
	case model.ExchangeGateIO:
		client, err := gateioexchange.New(gateioexchange.Config{
			Executor: executor, Credentials: descriptor, CredentialProvider: provider,
			DefaultEgressRouteID: routeID,
		})
		if err != nil {
			return nil, err
		}
		return gateioexchange.NewUnifiedSpot(client)
	case model.ExchangeCryptoCom:
		client, err := cryptocomexchange.New(cryptocomexchange.Config{
			Executor: executor, Credentials: descriptor, CredentialProvider: provider,
			DefaultEgressRouteID: routeID,
		})
		if err != nil {
			return nil, err
		}
		return cryptocomexchange.NewUnifiedSpot(client)
	case model.ExchangeMEXC:
		client, err := mexcexchange.New(mexcexchange.Config{
			Executor: executor, Credentials: descriptor, CredentialProvider: provider,
			DefaultEgressRouteID: routeID,
		})
		if err != nil {
			return nil, err
		}
		return mexcexchange.NewUnifiedSpot(client)
	default:
		return nil, fmt.Errorf("지원하지 않는 Spot 거래소 %q", exchangeID)
	}
}

func supportedSpotExchange(exchangeID model.ExchangeID) bool {
	switch exchangeID {
	case model.ExchangeBinance, model.ExchangeBitget, model.ExchangeUpbit,
		model.ExchangeBybit, model.ExchangeOKX, model.ExchangeCoinbase,
		model.ExchangeKraken, model.ExchangeBithumb, model.ExchangeCoinone,
		model.ExchangeKorbit, model.ExchangeKuCoin, model.ExchangeGateIO,
		model.ExchangeCryptoCom, model.ExchangeMEXC:
		return true
	default:
		return false
	}
}

func requiresPassphrase(exchangeID model.ExchangeID) bool {
	return exchangeID == model.ExchangeBitget || exchangeID == model.ExchangeOKX ||
		exchangeID == model.ExchangeKuCoin
}
