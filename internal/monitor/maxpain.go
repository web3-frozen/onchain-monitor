package monitor

// MaxPainEntry holds liquidation max pain data for a single coin.
type MaxPainEntry struct {
	Symbol                   string  `json:"symbol"`
	Price                    float64 `json:"price"`
	MaxLongLiquidationPrice  float64 `json:"maxLongLiquidationPrice"`
	MaxShortLiquidationPrice float64 `json:"maxShortLiquidationPrice"`
	Interval                 string  `json:"interval"`
}

// maxpainIntervals maps window_minutes to interval strings.
var maxpainIntervals = map[int]string{
	720:   "12h",
	1440:  "24h",
	2880:  "48h",
	4320:  "3d",
	10080: "7d",
	20160: "2w",
	43200: "1M",
}

// IntervalFromMinutes converts window_minutes to an interval string.
func IntervalFromMinutes(minutes int) string {
	if iv, ok := maxpainIntervals[minutes]; ok {
		return iv
	}
	return "24h"
}
