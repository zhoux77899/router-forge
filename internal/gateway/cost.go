package gateway

import (
	"math/big"
	"strings"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func priceRequest(record *domain.RequestRecord, pricing *domain.Pricing) {
	record.CostAmount, record.Currency, record.CostSource = nil, nil, "unknown"
	if pricing == nil || !safeIdentifier(pricing.Currency, 16) {
		return
	}
	currency := pricing.Currency
	record.Currency = &currency
	record.CostSource = pricing.Source
	if record.CostSource == "" {
		record.CostSource = "configured"
	}
	if record.Usage.Completeness != "complete" || record.Usage.InputTokens == nil || record.Usage.OutputTokens == nil {
		return
	}
	inputRate, ok := decimalRate(pricing.InputPerMillion)
	if !ok {
		return
	}
	outputRate, ok := decimalRate(pricing.OutputPerMillion)
	if !ok {
		return
	}
	readRate, writeRate := inputRate, inputRate
	if pricing.CacheReadPerMillion != nil {
		readRate, ok = decimalRate(*pricing.CacheReadPerMillion)
		if !ok {
			return
		}
	}
	if pricing.CacheWritePerMillion != nil {
		writeRate, ok = decimalRate(*pricing.CacheWritePerMillion)
		if !ok {
			return
		}
	}
	usage := record.Usage
	input, output := *usage.InputTokens, *usage.OutputTokens
	if input < 0 || output < 0 {
		return
	}
	if total := sumTokens(usage.InputTokens, usage.OutputTokens); total == nil || usage.TotalTokens != nil && *usage.TotalTokens != *total {
		return
	}
	if usage.ReasoningTokens != nil && *usage.ReasoningTokens > output {
		return
	}
	var read, write int64
	if usage.CacheReadTokens != nil {
		read = *usage.CacheReadTokens
	} else if readRate.Cmp(inputRate) != 0 {
		return
	}
	if usage.CacheWriteTokens != nil {
		write = *usage.CacheWriteTokens
	} else if writeRate.Cmp(inputRate) != 0 {
		return
	}
	if read < 0 || write < 0 || read > input || write > input-read {
		return
	}
	amount := new(big.Rat).Mul(inputRate, new(big.Rat).SetInt64(input-read-write))
	amount.Add(amount, new(big.Rat).Mul(readRate, new(big.Rat).SetInt64(read)))
	amount.Add(amount, new(big.Rat).Mul(writeRate, new(big.Rat).SetInt64(write)))
	amount.Add(amount, new(big.Rat).Mul(outputRate, new(big.Rat).SetInt64(output)))
	amount.Quo(amount, new(big.Rat).SetInt64(1_000_000))
	value := exactDecimal(amount)
	record.CostAmount = &value
}

func decimalRate(value string) (*big.Rat, bool) {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 128 {
		return nil, false
	}
	digits, dots := 0, 0
	for _, c := range value {
		if c == '.' {
			dots++
			if dots > 1 {
				return nil, false
			}
		} else if c >= '0' && c <= '9' {
			digits++
		} else {
			return nil, false
		}
	}
	if digits == 0 {
		return nil, false
	}
	rate, ok := new(big.Rat).SetString(value)
	return rate, ok && rate.Sign() >= 0
}

func exactDecimal(value *big.Rat) string {
	if value.Sign() == 0 {
		return "0"
	}
	denominator := new(big.Int).Set(value.Denom())
	var twos, fives int
	remainder := new(big.Int)
	for _, factor := range []int64{2, 5} {
		divisor := big.NewInt(factor)
		for {
			quotient, _ := new(big.Int).QuoRem(denominator, divisor, remainder)
			if remainder.Sign() != 0 {
				break
			}
			denominator = quotient
			if factor == 2 {
				twos++
			} else {
				fives++
			}
		}
	}
	scale := twos
	if fives > scale {
		scale = fives
	}
	if scale == 0 {
		return value.FloatString(0)
	}
	// Rates are bounded finite decimals; dividing by a million remains a finite decimal.
	return strings.TrimRight(strings.TrimRight(value.FloatString(scale), "0"), ".")
}
