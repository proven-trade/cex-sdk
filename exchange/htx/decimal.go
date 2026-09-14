package htx

import (
	"strconv"
	"strings"
)

// plainHTXDecimal expands wire exponents for unified decimal fields without
// converting through float64. Native responses and Raw retain their spelling.
// Invalid or excessively large values remain invalid for downstream validation.
func plainHTXDecimal(value Decimal) string {
	text := value.String()
	index := strings.IndexAny(text, "eE")
	if index < 0 {
		return text
	}
	exponent, err := strconv.Atoi(text[index+1:])
	if err != nil || exponent < -4096 || exponent > 4096 {
		return text
	}
	sign, mantissa := "", text[:index]
	if strings.HasPrefix(mantissa, "-") {
		sign, mantissa = "-", mantissa[1:]
	}
	point := strings.IndexByte(mantissa, '.')
	if point < 0 {
		point = len(mantissa)
	}
	digits := strings.ReplaceAll(mantissa, ".", "")
	point += exponent
	switch {
	case point <= 0:
		return sign + "0." + strings.Repeat("0", -point) + digits
	case point >= len(digits):
		return sign + digits + strings.Repeat("0", point-len(digits))
	default:
		return sign + digits[:point] + "." + digits[point:]
	}
}
