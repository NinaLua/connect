/*
 * Copyright 2024 Redpanda Data, Inc.
 *
 * Licensed as a Redpanda Enterprise file under the Redpanda Community
 * License (the "License"); you may not use this file except in compliance with
 * the License. You may obtain a copy of the License at
 *
 * https://github.com/redpanda-data/redpanda/blob/master/licenses/rcl.md
 */

package int128

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdd(t *testing.T) {
	require.Equal(t, MinInt128, Add(MaxInt128, Int64(1)))
	require.Equal(t, MaxInt128, Add(MinInt128, Int64(-1)))
	require.Equal(t, Int64(2), Add(Int64(1), Int64(1)))
	require.Equal(
		t,
		Bytes([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFE}),
		Add(Uint64(math.MaxUint64), Uint64(math.MaxUint64)),
	)
	require.Equal(
		t,
		Bytes([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}),
		Add(Int64(math.MaxInt64), Int64(1)),
	)
	require.Equal(
		t,
		Bytes([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}),
		Add(Uint64(math.MaxUint64), Int64(1)),
	)
}

func TestSub(t *testing.T) {
	require.Equal(t, MaxInt128, Sub(MinInt128, Int64(1)))
	require.Equal(t, MinInt128, Sub(MaxInt128, Int64(-1)))
	require.Equal(
		t,
		Bytes([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}),
		Sub(Int64(0), Int64(math.MaxInt64)),
	)
	require.Equal(
		t,
		Bytes([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}),
		Sub(Int64(0), Uint64(math.MaxUint64)),
	)
}

func SlowMul(a Int128, b Int128) Int128 {
	delta := Int64(-1)
	deltaFn := Add
	if Less(b, Int64(0)) {
		delta = Int64(1)
		deltaFn = Sub
	}
	r := Int64(0)
	for i := b; i != Int64(0); i = Add(i, delta) {
		r = deltaFn(r, a)
	}
	return r
}

func TestMul(t *testing.T) {
	tc := [][2]Int128{
		{Int64(10), Int64(10)},
		{Int64(1), Int64(10)},
		{Int64(0), Int64(10)},
		{Int64(0), Int64(0)},
		{Int64(math.MaxInt64), Int64(0)},
		{Int64(math.MaxInt64), Int64(1)},
		{Int64(math.MaxInt64), Int64(2)},
		{Int64(math.MaxInt64), Int64(3)},
		{Int64(math.MaxInt64), Int64(4)},
		{Int64(math.MaxInt64), Int64(10)},
		{Uint64(math.MaxUint64), Int64(10)},
		{Uint64(math.MaxUint64), Int64(2)},
		{Uint64(math.MaxUint64), Int64(100)},
		{MaxInt128, Int64(100)},
		{MaxInt128, Int64(10)},
		{MinInt128, Int64(10)},
		{MinInt128, Int64(-1)},
		{MaxInt128, Int64(-1)},
		{Int64(-1), Int64(-1)},
	}
	for _, c := range tc {
		a, b := c[0], c[1]
		expected := SlowMul(a, b)
		actual := Mul(a, b)
		require.Equal(
			t,
			expected,
			actual,
			"%s x %s, got: %s, want: %s",
			a.String(),
			b.String(),
			actual.String(),
			expected.String(),
		)
		actual = Mul(b, a)
		require.Equal(
			t,
			expected,
			actual,
			"%s x %s, got: %s, want: %s",
			b.String(),
			a.String(),
			actual.String(),
			expected.String(),
		)
	}
}

func TestShl(t *testing.T) {
	for i := uint(0); i < 64; i++ {
		require.Equal(t, Int128{lo: 1 << i}, Shl(Int64(1), i))
		require.Equal(t, Int128{hi: 1 << i}, Shl(Int64(1), i+64))
		require.Equal(t, Int128{hi: ^0, lo: uint64(int64(-1) << i)}, Shl(Int64(-1), i))
		require.Equal(t, Int128{hi: -1 << i}, Shl(Int64(-1), i+64))
	}
	require.Equal(t, Int128{}, Shl(Int64(1), 128))
	require.Equal(t, Int128{}, Shl(Int64(-1), 128))
}

func TestUshr(t *testing.T) {
	for i := uint(0); i < 64; i++ {
		require.Equal(t, Int128{hi: int64(uint64(1<<63) >> i)}, uShr(MinInt128, i), i)
		require.Equal(t, Int128{lo: (1 << 63) >> i}, uShr(MinInt128, i+64), i)
	}
	require.Equal(t, Int128{}, uShr(MinInt128, 128))
	require.Equal(t, Int128{}, uShr(Int64(-1), 128))
}

func TestNeg(t *testing.T) {
	require.Equal(t, Int64(-1), Neg(Int64(1)))
	require.Equal(t, Int64(1), Neg(Int64(-1)))
	require.Equal(t, Sub(Int64(0), MaxInt64), Neg(MaxInt64))
	require.Equal(t, Add(MinInt128, Int64(1)), Neg(MaxInt128))
	require.Equal(t, MinInt128, Neg(MinInt128))
}

func TestDiv(t *testing.T) {
	type TestCase struct {
		dividend, divisor, quotient Int128
	}
	cases := []TestCase{
		{Int64(100), Int64(10), Int64(10)},
		{Int64(64), Int64(8), Int64(8)},
		{Int64(10), Int64(3), Int64(3)},
		{Int64(99), Int64(25), Int64(3)},
		{
			Int64(0x15f2a64138),
			Int64(0x67da05),
			Int64(0x15f2a64138 / 0x67da05),
		},
		{
			Int64(0x5e56d194af43045f),
			Int64(0xcf1543fb99),
			Int64(0x5e56d194af43045f / 0xcf1543fb99),
		},
		{
			Int64(0x15e61ed052036a),
			Int64(-0xc8e6),
			Int64(0x15e61ed052036a / -0xc8e6),
		},
		{
			Int64(0x88125a341e85),
			Int64(-0xd23fb77683),
			Int64(0x88125a341e85 / -0xd23fb77683),
		},
		{
			Int64(-0xc06e20),
			Int64(0x5a),
			Int64(-0xc06e20 / 0x5a),
		},
		{
			Int64(-0x4f100219aea3e85d),
			Int64(0xdcc56cb4efe993),
			Int64(-0x4f100219aea3e85d / 0xdcc56cb4efe993),
		},
		{
			Int64(-0x168d629105),
			Int64(-0xa7),
			Int64(-0x168d629105 / -0xa7),
		},
		{
			Int64(-0x7b44e92f03ab2375),
			Int64(-0x6516),
			Int64(-0x7b44e92f03ab2375 / -0x6516),
		},
		{
			Int128{0x6ada48d489007966, 0x3c9c5c98150d5d69},
			Int128{0x8bc308fb, 0x8cb9cc9a3b803344},
			Int64(0xc3b87e08),
		},
		{
			Int128{0xd6946511b5b, 0x4886c5c96546bf5f},
			Neg(Int128{0x263b, 0xfd516279efcfe2dc}),
			Int64(-0x59cbabf0),
		},
		{
			Neg(Int128{0x33db734f9e8d1399, 0x8447ac92482bca4d}),
			Int64(0x37495078240),
			Neg(Int128{0xf01f1, 0xbc0368bf9a77eae8}),
		},
		{
			Neg(Int128{0x13f837b409a07e7d, 0x7fc8e248a7d73560}),
			Int64(-0x1b9f),
			Int128{0xb9157556d724, 0xb14f635714d7563e},
		},
	}
	for _, c := range cases {
		c := c
		t.Run("", func(t *testing.T) {
			require.Equal(
				t,
				c.quotient,
				Div(c.dividend, c.divisor),
				"%s / %s = %s",
				c.dividend,
				c.divisor,
				c.quotient,
			)
		})
	}
}

func TestPow10(t *testing.T) {
	expected := Int64(1)
	for _, v := range Pow10Table {
		require.Equal(t, expected, v)
		expected = Mul(expected, Int64(10))
	}
}

func TestCompare(t *testing.T) {
	tc := [][2]Int128{
		{Int64(0), Int64(1)},
		{Int64(-1), Int64(0)},
		{MinInt128, Int64(0)},
		{MinInt128, Int64(-1)},
		{MinInt128, Int64(math.MinInt64)},
		{MinInt128, Uint64(math.MaxUint64)},
		{MinInt128, MaxInt128},
		{Int64(0), MaxInt128},
		{Int64(-1), MaxInt128},
		{Int64(math.MinInt64), MaxInt128},
		{Int64(math.MaxInt64), MaxInt128},
		{Uint64(math.MaxUint64), MaxInt128},
	}
	for _, vals := range tc {
		a, b := vals[0], vals[1]
		require.True(t, Less(a, b))
		require.False(t, Less(b, a))
		require.True(t, Greater(b, a))
		require.False(t, Greater(a, b))
		require.NotEqual(t, a, b)
		require.Equal(t, a, a)
		require.Equal(t, b, b)
	}
	require.Equal(t, Int64(0), Int64(0))
	require.NotEqual(t, Int64(1), Int64(0))
	require.Equal(t, Shl(Int64(1), 64), Add(Uint64(math.MaxUint64), Int64(1)))
}

func TestParse(t *testing.T) {
	for _, expected := range [...]Int128{
		MinInt128,
		MaxInt128,
		Int64(0),
		Int64(-1),
		Int64(1),
		MinInt8,
		MaxInt8,
		MinInt16,
		MaxInt16,
		MinInt32,
		MaxInt32,
		MinInt64,
		MaxInt64,
		Add(MaxInt64, Uint64(1)),
	} {
		actual, ok := Parse(expected.String())
		require.True(t, ok, "%s", expected)
		require.Equal(t, expected, actual)
	}
	// One less than min
	_, ok := Parse("-170141183460469231731687303715884105729")
	require.False(t, ok)
	// One more than max
	_, ok = Parse("170141183460469231731687303715884105728")
	require.False(t, ok)
}

func TestString(t *testing.T) {
	require.Equal(t, "-170141183460469231731687303715884105728", MinInt128.String())
	require.Equal(t, "170141183460469231731687303715884105727", MaxInt128.String())
}

func TestByteWidth(t *testing.T) {
	tests := [][2]int64{
		{0, 1},
		{1, 1},
		{-1, 1},
		{-16, 1},
		{16, 1},
		{math.MaxInt8 - 1, 1},
		{math.MaxInt8, 1},
		{math.MaxInt8 + 1, 2},
		{math.MinInt8 - 1, 2},
		{math.MinInt8, 1},
		{math.MinInt8 + 1, 1},
		{math.MaxInt16 - 1, 2},
		{math.MaxInt16, 2},
		{math.MaxInt16 + 1, 4},
		{math.MinInt16 - 1, 4},
		{math.MinInt16, 2},
		{math.MinInt16 + 1, 2},
		{math.MaxInt32 - 1, 4},
		{math.MaxInt32, 4},
		{math.MaxInt32 + 1, 8},
		{math.MinInt32 - 1, 8},
		{math.MinInt32, 4},
		{math.MinInt32 + 1, 4},
		{math.MaxInt64 - 1, 8},
		{math.MaxInt64, 8},
		// {math.MaxInt64 + 1, 8},
		// {math.MinInt64 - 1, 8},
		{math.MinInt64, 8},
		{math.MinInt64 + 1, 8},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("byteWidth(%d)", tc[0]), func(t *testing.T) {
			require.Equal(t, int(tc[1]), ByteWidth(Int64(tc[0])))
		})
	}
	require.Equal(t, 16, ByteWidth(Sub(MinInt64, Int64(1))))
	require.Equal(t, 16, ByteWidth(MinInt128))
	require.Equal(t, 16, ByteWidth(Add(MaxInt64, Int64(1))))
	require.Equal(t, 16, ByteWidth(MaxInt128))
}

func TestIncreaseScaleBy(t *testing.T) {
	type TestCase struct {
		n        Int128
		scale    int32
		overflow bool
	}
	tests := []TestCase{
		{MinInt64, 1, false},
		{MaxInt64, 1, false},
		{MaxInt64, 2, false},
		{MinInt64, 2, false},
		{MaxInt128, 1, true},
		{MinInt128, 1, true},
		{MinInt128, 0, false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run("", func(t *testing.T) {
			v, err := Rescale(tc.n, 0, tc.scale)
			if tc.overflow {
				require.Error(t, err, "but got: %v", v)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFitsInPrec(t *testing.T) {
	// Examples from snowflake documentation
	snowflakeNumberMax := "+99999999999999999999999999999999999999"
	snowflakeNumberMin := "-99999999999999999999999999999999999999"
	require.True(t, MustParse(snowflakeNumberMax).FitsInPrecision(38), snowflakeNumberMax)
	require.True(t, MustParse(snowflakeNumberMin).FitsInPrecision(38), snowflakeNumberMin)
	snowflakeNumberTiny := "1.2e-36"
	n, err := String(snowflakeNumberTiny, 38, 37)
	require.NoError(t, err)
	require.True(t, n.FitsInPrecision(38), snowflakeNumberTiny)
}
