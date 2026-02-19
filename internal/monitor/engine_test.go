package monitor

import "testing"

func TestFormatNum(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{0.00123, "0.0012"},
		{0.5, "0.5000"},
		{999.99, "999.9900"},
		{1000, "1,000.00"},
		{1234.56, "1,234.56"},
		{12345.67, "12,345.67"},
		{123456.78, "123,456.78"},
		{999999.99, "999,999.99"},
		{1000000, "1.00M"},
		{1500000, "1.50M"},
		{123456789, "123.46M"},
	}
	for _, tt := range tests {
		got := formatNum(tt.input)
		if got != tt.want {
			t.Errorf("formatNum(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAddCommas(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"0", "0"},
		{"100", "100"},
		{"1000", "1,000"},
		{"12345", "12,345"},
		{"123456", "123,456"},
		{"1234567", "1,234,567"},
		{"1000.50", "1,000.50"},
		{"12345678.99", "12,345,678.99"},
		{"100.25", "100.25"},
	}
	for _, tt := range tests {
		got := addCommas(tt.input)
		if got != tt.want {
			t.Errorf("addCommas(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStringToUpper(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"hello", "HELLO"},
		{"HELLO", "HELLO"},
		{"Hello World", "HELLO WORLD"},
		{"abc123", "ABC123"},
		{"already MIXED", "ALREADY MIXED"},
	}
	for _, tt := range tests {
		got := stringToUpper(tt.input)
		if got != tt.want {
			t.Errorf("stringToUpper(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIntervalFromMinutes(t *testing.T) {
	tests := []struct {
		minutes int
		want    string
	}{
		{720, "12h"},
		{1440, "24h"},
		{2880, "48h"},
		{4320, "3d"},
		{10080, "7d"},
		{20160, "2w"},
		{43200, "1M"},
		{999, "24h"},  // unknown → default
		{0, "24h"},    // zero → default
		{-1, "24h"},   // negative → default
	}
	for _, tt := range tests {
		got := IntervalFromMinutes(tt.minutes)
		if got != tt.want {
			t.Errorf("IntervalFromMinutes(%d) = %q, want %q", tt.minutes, got, tt.want)
		}
	}
}
