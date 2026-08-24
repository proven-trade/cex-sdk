package credential

import (
	"errors"
	"testing"

	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

func TestDescriptorRequiresAllowedRoute(t *testing.T) {
	t.Parallel()

	descriptor := Descriptor{
		AccountID:   "main",
		Exchange:    model.ExchangeUpbit,
		SecretRef:   "secret/upbit/main",
		Permissions: []Permission{PermissionRead},
	}
	if err := descriptor.Validate(); !errors.Is(err, ErrInvalidDescriptor) {
		t.Fatalf("Validate() error = %v, want ErrInvalidDescriptor", err)
	}
}

func TestDescriptorFailsClosedForUnlistedRoute(t *testing.T) {
	t.Parallel()

	descriptor := Descriptor{
		AccountID:             "main",
		Exchange:              model.ExchangeUpbit,
		SecretRef:             "secret/upbit/main",
		Permissions:           []Permission{PermissionRead, PermissionTrade},
		AllowedEgressRouteIDs: []transport.EgressRouteID{"eip-a"},
	}
	if err := descriptor.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := descriptor.RequireEgressRoute("eip-a"); err != nil {
		t.Fatalf("RequireEgressRoute(allowed) error = %v", err)
	}
	if err := descriptor.RequireEgressRoute("eip-b"); !errors.Is(err, ErrEgressRouteNotAllowed) {
		t.Fatalf("RequireEgressRoute(denied) error = %v, want ErrEgressRouteNotAllowed", err)
	}
}

func TestDescriptorRejectsUnknownOrDuplicatePermissions(t *testing.T) {
	t.Parallel()

	for name, permissions := range map[string][]Permission{
		"unknown":   {"withdraw"},
		"duplicate": {PermissionRead, PermissionRead},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			descriptor := Descriptor{
				AccountID:             "main",
				Exchange:              model.ExchangeUpbit,
				SecretRef:             "secret/upbit/main",
				Permissions:           permissions,
				AllowedEgressRouteIDs: []transport.EgressRouteID{"eip-a"},
			}
			if err := descriptor.Validate(); !errors.Is(err, ErrInvalidDescriptor) {
				t.Fatalf("Validate() error = %v, want ErrInvalidDescriptor", err)
			}
		})
	}
}

func TestDescriptorRequiresConfiguredPermission(t *testing.T) {
	t.Parallel()

	descriptor := Descriptor{
		AccountID:             "main",
		Exchange:              model.ExchangeBinance,
		SecretRef:             "secret/binance/main",
		Permissions:           []Permission{PermissionRead},
		AllowedEgressRouteIDs: []transport.EgressRouteID{"eip-a"},
	}
	if err := descriptor.RequirePermission(PermissionRead); err != nil {
		t.Fatalf("RequirePermission(read) error = %v", err)
	}
	if err := descriptor.RequirePermission(PermissionTrade); !errors.Is(err, ErrPermissionNotAllowed) {
		t.Fatalf("RequirePermission(trade) error = %v, want ErrPermissionNotAllowed", err)
	}
}

func TestMaterialDestroyOverwritesSecrets(t *testing.T) {
	t.Parallel()

	material := Material{
		APIKey:     []byte("api-key"),
		SecretKey:  []byte("secret-key"),
		Passphrase: []byte("passphrase"),
		Extra:      map[string][]byte{"pem": []byte("private-key")},
	}
	material.Destroy()

	for name, value := range map[string][]byte{
		"api key":    material.APIKey,
		"secret key": material.SecretKey,
		"passphrase": material.Passphrase,
	} {
		for _, item := range value {
			if item != 0 {
				t.Fatalf("%s was not overwritten", name)
			}
		}
	}
	if len(material.Extra) != 0 {
		t.Fatalf("Extra has %d entries after Destroy(), want 0", len(material.Extra))
	}
}
