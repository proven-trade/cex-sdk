package unified

import (
	"fmt"
	"math/big"
	"strings"
)

// DecimalIncrement는 소수 자릿수를 정확한 decimal 주문 단위로 변환한다.
// 음수 정밀도는 알려지지 않은 값으로 취급해 빈 문자열을 반환한다.
func DecimalIncrement(places int) string {
	if places < 0 {
		return ""
	}
	if places == 0 {
		return "1"
	}
	return "0." + strings.Repeat("0", places-1) + "1"
}

// ValidateOrder는 마켓 메타데이터가 제공하는 주문 단위와 최소값을
// 부동소수점 반올림 없이 검증한다. 빈 규칙은 거래소가 제공하지 않은 값이다.
func (info MarketInfo) ValidateOrder(request PlaceOrderRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if info.Market != request.Market {
		return validationError("order market %s does not match market rules for %s", request.Market, info.Market)
	}
	if request.Type == OrderTypeLimit {
		if err := validateIncrement("price", request.Price, info.PriceIncrement); err != nil {
			return err
		}
	}
	if request.Quantity != "" {
		if err := validateIncrement("quantity", request.Quantity, info.QuantityIncrement); err != nil {
			return err
		}
		if err := validateMinimum("quantity", request.Quantity, info.MinimumBaseQuantity); err != nil {
			return err
		}
	}
	if request.QuoteAmount != "" {
		if err := validateIncrement("quote amount", request.QuoteAmount, info.QuoteAmountIncrement); err != nil {
			return err
		}
		if err := validateMinimum("quote amount", request.QuoteAmount, info.MinimumQuoteAmount); err != nil {
			return err
		}
	}
	if request.Type == OrderTypeLimit && info.MinimumQuoteAmount != "" {
		notional, err := decimalProduct(request.Price, request.Quantity)
		if err != nil {
			return validationError("cannot calculate limit order notional: %v", err)
		}
		minimum, err := decimalRat(info.MinimumQuoteAmount)
		if err != nil {
			return fmt.Errorf("invalid market minimum quote amount %q: %w", info.MinimumQuoteAmount, err)
		}
		if notional.Cmp(minimum) < 0 {
			return validationError("limit order notional is below minimum quote amount %s", info.MinimumQuoteAmount)
		}
	}
	return nil
}

func validateIncrement(name, value, increment string) error {
	if increment == "" {
		return nil
	}
	valueRat, err := decimalRat(value)
	if err != nil {
		return validationError("%s is not a decimal: %v", name, err)
	}
	incrementRat, err := decimalRat(increment)
	if err != nil || incrementRat.Sign() <= 0 {
		return fmt.Errorf("invalid market %s increment %q", name, increment)
	}
	quotient := new(big.Rat).Quo(valueRat, incrementRat)
	if !quotient.IsInt() {
		return validationError("%s must be a multiple of %s", name, increment)
	}
	return nil
}

func validateMinimum(name, value, minimum string) error {
	if minimum == "" {
		return nil
	}
	valueRat, err := decimalRat(value)
	if err != nil {
		return validationError("%s is not a decimal: %v", name, err)
	}
	minimumRat, err := decimalRat(minimum)
	if err != nil || minimumRat.Sign() < 0 {
		return fmt.Errorf("invalid market minimum %s %q", name, minimum)
	}
	if valueRat.Cmp(minimumRat) < 0 {
		return validationError("%s is below minimum %s", name, minimum)
	}
	return nil
}

func decimalProduct(left, right string) (*big.Rat, error) {
	leftRat, err := decimalRat(left)
	if err != nil {
		return nil, err
	}
	rightRat, err := decimalRat(right)
	if err != nil {
		return nil, err
	}
	return new(big.Rat).Mul(leftRat, rightRat), nil
}

func decimalRat(value string) (*big.Rat, error) {
	if !positiveDecimalPattern.MatchString(value) && value != "0" && value != "0.0" {
		return nil, fmt.Errorf("invalid decimal %q", value)
	}
	result, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, fmt.Errorf("invalid decimal %q", value)
	}
	return result, nil
}
