// Package credential은 Secret 조회와 송신 경로 권한 규칙을 정의한다.
package credential

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/transport"
)

var (
	// ErrInvalidDescriptor는 자격증명 메타데이터가 올바르지 않음을 나타낸다.
	ErrInvalidDescriptor = errors.New("invalid credential descriptor")
	// ErrEgressRouteNotAllowed는 자격증명으로 해당 송신 경로를 사용할 수 없음을 나타낸다.
	ErrEgressRouteNotAllowed = errors.New("egress route is not allowed for credentials")
	// ErrPermissionNotAllowed는 자격증명에 필요한 권한이 없음을 나타낸다.
	ErrPermissionNotAllowed = errors.New("permission is not allowed for credentials")
)

// Permission은 거래소 API Key에 요구되는 상위 수준 권한이다.
type Permission string

const (
	PermissionRead  Permission = "read"
	PermissionTrade Permission = "trade"
)

// Descriptor는 계정의 비밀이 아닌 메타데이터와 송신 경로 허용 목록을 담는다.
type Descriptor struct {
	AccountID             string
	Exchange              model.ExchangeID
	SecretRef             string
	Permissions           []Permission
	AllowedEgressRouteIDs []transport.EgressRouteID
}

// Validate는 자격증명의 비밀이 아닌 메타데이터를 검증한다.
func (descriptor Descriptor) Validate() error {
	if strings.TrimSpace(descriptor.AccountID) == "" {
		return fmt.Errorf("%w: account ID is required", ErrInvalidDescriptor)
	}
	if !descriptor.Exchange.Valid() {
		return fmt.Errorf("%w: exchange is required", ErrInvalidDescriptor)
	}
	if strings.TrimSpace(descriptor.SecretRef) == "" {
		return fmt.Errorf("%w: secret reference is required", ErrInvalidDescriptor)
	}
	if len(descriptor.Permissions) == 0 {
		return fmt.Errorf("%w: at least one permission is required", ErrInvalidDescriptor)
	}
	seenPermissions := make(map[Permission]struct{}, len(descriptor.Permissions))
	for _, permission := range descriptor.Permissions {
		if permission != PermissionRead && permission != PermissionTrade {
			return fmt.Errorf("%w: unknown permission %q", ErrInvalidDescriptor, permission)
		}
		if _, exists := seenPermissions[permission]; exists {
			return fmt.Errorf("%w: duplicate permission %q", ErrInvalidDescriptor, permission)
		}
		seenPermissions[permission] = struct{}{}
	}
	if len(descriptor.AllowedEgressRouteIDs) == 0 {
		return fmt.Errorf("%w: at least one allowed egress route is required", ErrInvalidDescriptor)
	}

	seen := make(map[transport.EgressRouteID]struct{}, len(descriptor.AllowedEgressRouteIDs))
	for _, routeID := range descriptor.AllowedEgressRouteIDs {
		cleanRouteID := strings.TrimSpace(string(routeID))
		if cleanRouteID == "" {
			return fmt.Errorf("%w: allowed egress route ID is empty", ErrInvalidDescriptor)
		}
		if cleanRouteID != string(routeID) {
			return fmt.Errorf("%w: allowed egress route %q has surrounding whitespace", ErrInvalidDescriptor, routeID)
		}
		if _, exists := seen[routeID]; exists {
			return fmt.Errorf("%w: duplicate allowed egress route %q", ErrInvalidDescriptor, routeID)
		}
		seen[routeID] = struct{}{}
	}

	return nil
}

// AllowsEgressRoute는 API Key를 routeID로 사용할 수 있는지 반환한다.
func (descriptor Descriptor) AllowsEgressRoute(routeID transport.EgressRouteID) bool {
	return slices.Contains(descriptor.AllowedEgressRouteIDs, routeID)
}

// AllowsPermission은 API Key에 요청한 상위 권한이 설정되어 있는지 반환한다.
func (descriptor Descriptor) AllowsPermission(permission Permission) bool {
	return slices.Contains(descriptor.Permissions, permission)
}

// RequirePermission은 필요한 상위 권한이 없으면 안전하게 거부한다.
func (descriptor Descriptor) RequirePermission(permission Permission) error {
	if descriptor.AllowsPermission(permission) {
		return nil
	}
	return fmt.Errorf(
		"%w: account %q does not have %q permission",
		ErrPermissionNotAllowed,
		descriptor.AccountID,
		permission,
	)
}

// RequireEgressRoute는 경로가 자격증명 허용 목록에 없으면 안전하게 거부한다.
func (descriptor Descriptor) RequireEgressRoute(routeID transport.EgressRouteID) error {
	if descriptor.AllowsEgressRoute(routeID) {
		return nil
	}
	return fmt.Errorf(
		"%w: account %q cannot use route %q",
		ErrEgressRouteNotAllowed,
		descriptor.AccountID,
		routeID,
	)
}

// Material은 Provider가 반환하는 민감 정보다. 거래소 어댑터는 공통
// key/secret/passphrase 구조에 맞지 않는 자격증명을 Extra에 저장할 수 있다.
type Material struct {
	APIKey     []byte
	SecretKey  []byte
	Passphrase []byte
	Extra      map[string][]byte
}

// Destroy는 이 값이 보유한 비밀 byte slice를 덮어쓴다.
// 호출자나 Go runtime이 이미 만든 복사본까지 지울 수는 없다.
func (material *Material) Destroy() {
	if material == nil {
		return
	}
	zero(material.APIKey)
	zero(material.SecretKey)
	zero(material.Passphrase)
	for key, value := range material.Extra {
		zero(value)
		delete(material.Extra, key)
	}
}

// Provider는 SDK 설정에 평문 자격증명을 넣지 않고 Secret 참조를 조회한다.
type Provider interface {
	Resolve(context.Context, string) (Material, error)
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
