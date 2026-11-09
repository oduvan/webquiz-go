package quiz

import (
	"math"
	"math/big"
	"strconv"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// PointsDecimalPlaces is how many decimal places question points may have.
const PointsDecimalPlaces = 2

// NormalizePoints rounds a points value (or a sum of them) to 2 decimal
// places, like normalize_points. Sums of decimals pick up binary floating
// point errors (0.1 + 0.2 = 0.30000000000000004); rounding keeps scores exact
// to two decimals. Whole values stay whole, so JSON writes 3, not 3.0.
func NormalizePoints(value float64) float64 {
	rounded := pyval.Round(value, PointsDecimalPlaces)
	if rounded == 0 {
		return 0 // never -0
	}
	return rounded
}

// SumPoints adds points values and normalizes the sum, like sum_points.
func SumPoints(values ...float64) float64 {
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return NormalizePoints(sum)
}

// FormatPoints writes a points value the way Python's str(normalize_points(x))
// did: "3" for whole values, "0.5" otherwise.
func FormatPoints(value float64) string {
	value = NormalizePoints(value)
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return pyval.FloatRepr(value)
	}
	if value == math.Trunc(value) {
		whole, _ := new(big.Float).SetFloat64(value).Int(nil)
		return whole.String() // like Python's int(), exact for large values too
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}
